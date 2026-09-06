//go:build integration

// Package integration runs a full scan against a real Kubernetes API server
// (a local kind cluster in CI, or any cluster reachable via kubeconfig for
// local runs) with real controllers, so detector logic that depends on
// controller-driven status (EndpointSlices, Deployment readiness, Job
// completion, PV lifecycle) is exercised end-to-end rather than hand-faked
// as it is in the per-detector unit tests. Run with:
//
//	go test -tags=integration ./test/integration/...
//
// Set INTEGRATION_KUBE_CONTEXT to target a context other than the
// kubeconfig's current one (e.g. "kind-k8s-zombie" in CI).
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/4ugane/k8s-zombie/pkg/cli"
	"github.com/4ugane/k8s-zombie/pkg/cost"
	"github.com/4ugane/k8s-zombie/pkg/finding"
)

const testNamespace = "k8s-zombie-test"

// detectorNameToExpectedFinding maps each detector to one finding name it must
// report against the fixtures in testdata/fixtures.yaml. orphaned-pv is filled
// in at runtime since the PV name is controller-generated.
var detectorNameToExpectedFinding = map[string]string{
	"unattached-pvc":          "orphan-pvc",
	"zero-endpoint-service":   "dead-svc",
	"unused-namespace":        "k8s-zombie-test-empty",
	"unused-configmap-secret": "orphan-cm",
	"orphaned-ingress":        "dead-ingress",
	"idle-deployment":         "idle-worker",
	"stale-hpa":               "stale-hpa",
	"completed-jobs-and-pods": "quick-job",
}

func TestScan(t *testing.T) {
	ctx := context.Background()
	clientset := buildClientset(t)
	fixtures := filepath.Join("testdata", "fixtures.yaml")

	runKubectl(t, "apply", "-f", fixtures)
	t.Cleanup(func() { cleanup(t, clientset, fixtures) })

	waitForJobCompletion(t, ctx, clientset)
	pvName := releasePV(t, ctx, clientset)

	table, err := cost.DefaultPricingTable()
	if err != nil {
		t.Fatalf("loading default pricing table: %v", err)
	}
	var buf bytes.Buffer
	opts := cli.Options{Output: "json", PricingTable: table}
	if err := cli.Run(ctx, clientset, opts, &buf); err != nil {
		t.Fatalf("cli.Run: %v", err)
	}

	var findings []finding.Finding
	if err := json.Unmarshal(buf.Bytes(), &findings); err != nil {
		t.Fatalf("unmarshaling findings JSON: %v\noutput:\n%s", err, buf.String())
	}

	byDetector := make(map[string][]finding.Finding, len(findings))
	for _, f := range findings {
		byDetector[f.Detector] = append(byDetector[f.Detector], f)
	}

	want := detectorNameToExpectedFinding
	want["orphaned-pv"] = pvName
	for detector, name := range want {
		if !containsName(byDetector[detector], name) {
			t.Errorf("detector %q: expected a finding named %q, got %+v", detector, name, byDetector[detector])
		}
	}
}

func containsName(findings []finding.Finding, name string) bool {
	for _, f := range findings {
		if f.Name == name {
			return true
		}
	}
	return false
}

func buildClientset(t *testing.T) kubernetes.Interface {
	t.Helper()
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}
	if kubeContext := os.Getenv("INTEGRATION_KUBE_CONTEXT"); kubeContext != "" {
		overrides.CurrentContext = kubeContext
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		t.Fatalf("loading kubeconfig: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("building clientset: %v", err)
	}
	return clientset
}

func runKubectl(t *testing.T, args ...string) {
	t.Helper()
	if kubeContext := os.Getenv("INTEGRATION_KUBE_CONTEXT"); kubeContext != "" {
		args = append(args, "--context", kubeContext)
	}
	out, err := exec.Command("kubectl", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("kubectl %v: %v\n%s", args, err, out)
	}
}

// waitFor polls check every 2s until it returns true, or fails the test after
// timeout. A NotFound error is treated as "not ready yet", not a hard failure,
// since fixtures.yaml apply and controller reconciliation both race the poll.
func waitFor(t *testing.T, timeout time.Duration, check func() (bool, error)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		ok, err := check()
		if err != nil && !apierrors.IsNotFound(err) {
			t.Fatalf("waiting: %v", err)
		}
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for condition", timeout)
		}
		time.Sleep(2 * time.Second)
	}
}

func waitForJobCompletion(t *testing.T, ctx context.Context, clientset kubernetes.Interface) {
	t.Helper()
	waitFor(t, 60*time.Second, func() (bool, error) {
		job, err := clientset.BatchV1().Jobs(testNamespace).Get(ctx, "quick-job", metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return job.Status.Succeeded > 0, nil
	})
}

// releasePV waits for retain-pvc to bind and its PersistentVolume to be
// created, deletes the consuming pod and the PVC, then waits for the real
// PV controller to transition that volume to Released — the same
// Retain-policy leftover the orphaned-pv detector looks for, produced by an
// actual controller instead of a hand-set Status field. Returns the PV name.
func releasePV(t *testing.T, ctx context.Context, clientset kubernetes.Interface) string {
	t.Helper()

	var pvcUID string
	waitFor(t, 60*time.Second, func() (bool, error) {
		pvc, err := clientset.CoreV1().PersistentVolumeClaims(testNamespace).Get(ctx, "retain-pvc", metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if pvc.Status.Phase != corev1.ClaimBound {
			return false, nil
		}
		pvcUID = string(pvc.UID)
		return true, nil
	})

	var pvName string
	waitFor(t, 30*time.Second, func() (bool, error) {
		pvs, err := clientset.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, pv := range pvs.Items {
			if pv.Spec.ClaimRef != nil && string(pv.Spec.ClaimRef.UID) == pvcUID {
				pvName = pv.Name
				return true, nil
			}
		}
		return false, nil
	})

	if err := clientset.CoreV1().Pods(testNamespace).Delete(ctx, "retain-consumer", metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("deleting retain-consumer pod: %v", err)
	}
	if err := clientset.CoreV1().PersistentVolumeClaims(testNamespace).Delete(ctx, "retain-pvc", metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("deleting retain-pvc: %v", err)
	}

	waitFor(t, 60*time.Second, func() (bool, error) {
		pv, err := clientset.CoreV1().PersistentVolumes().Get(ctx, pvName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return pv.Status.Phase == corev1.VolumeReleased, nil
	})

	return pvName
}

// cleanup removes everything fixtures.yaml created. The retain-pvc/pod are
// already gone by the time this runs (releasePV deletes them deliberately to
// produce the Released PV); delete -f handles the rest (namespaces cascade,
// StorageClass is cluster-scoped) except the Released PV itself, which
// Retain-policy leaves behind on purpose and so needs an explicit delete.
func cleanup(t *testing.T, clientset kubernetes.Interface, fixtures string) {
	t.Helper()
	args := []string{"delete", "-f", fixtures, "--ignore-not-found", "--wait=false"}
	if kubeContext := os.Getenv("INTEGRATION_KUBE_CONTEXT"); kubeContext != "" {
		args = append(args, "--context", kubeContext)
	}
	if out, err := exec.Command("kubectl", args...).CombinedOutput(); err != nil {
		t.Logf("cleanup: kubectl %v: %v\n%s", args, err, out)
	}

	ctx := context.Background()
	pvs, err := clientset.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Logf("cleanup: listing PersistentVolumes: %v", err)
		return
	}
	for _, pv := range pvs.Items {
		if pv.Spec.StorageClassName != "retain-test" {
			continue
		}
		if err := clientset.CoreV1().PersistentVolumes().Delete(ctx, pv.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			t.Logf("cleanup: deleting PersistentVolume %s: %v", pv.Name, err)
		}
	}
}
