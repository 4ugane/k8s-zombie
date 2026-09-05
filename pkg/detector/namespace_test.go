package detector_test

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/4ugane/k8s-zombie/pkg/detector"
	"github.com/4ugane/k8s-zombie/pkg/finding"
)

func namespaceObj(name string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func podIn(ns, name string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
}

func deploymentIn(ns, name string) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
}

func statefulSetIn(ns, name string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
}

func cronJobIn(ns, name string) *batchv1.CronJob {
	return &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
}

func findNamespaceFinding(findings []finding.Finding, name string) *finding.Finding {
	for i := range findings {
		if findings[i].Name == name {
			return &findings[i]
		}
	}
	return nil
}

func TestUnusedNamespaceDetector_NamespaceWithPodIsNotReported(t *testing.T) {
	ns := namespaceObj("has-pod")
	pod := podIn("has-pod", "app")

	clientset := k8sfake.NewSimpleClientset(ns, pod)
	d := detector.NewUnusedNamespaceDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if f := findNamespaceFinding(findings, "has-pod"); f != nil {
		t.Errorf("want namespace with a Pod not reported, got %+v", f)
	}
}

func TestUnusedNamespaceDetector_NamespaceWithDeploymentIsNotReported(t *testing.T) {
	ns := namespaceObj("has-deploy")
	dep := deploymentIn("has-deploy", "app")

	clientset := k8sfake.NewSimpleClientset(ns, dep)
	d := detector.NewUnusedNamespaceDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if f := findNamespaceFinding(findings, "has-deploy"); f != nil {
		t.Errorf("want namespace with a Deployment not reported, got %+v", f)
	}
}

func TestUnusedNamespaceDetector_NamespaceWithStatefulSetIsNotReported(t *testing.T) {
	ns := namespaceObj("has-sts")
	sts := statefulSetIn("has-sts", "app")

	clientset := k8sfake.NewSimpleClientset(ns, sts)
	d := detector.NewUnusedNamespaceDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if f := findNamespaceFinding(findings, "has-sts"); f != nil {
		t.Errorf("want namespace with a StatefulSet not reported, got %+v", f)
	}
}

func TestUnusedNamespaceDetector_NamespaceWithCronJobIsNotReported(t *testing.T) {
	ns := namespaceObj("has-cron")
	cj := cronJobIn("has-cron", "app")

	clientset := k8sfake.NewSimpleClientset(ns, cj)
	d := detector.NewUnusedNamespaceDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if f := findNamespaceFinding(findings, "has-cron"); f != nil {
		t.Errorf("want namespace with a CronJob not reported, got %+v", f)
	}
}

func TestUnusedNamespaceDetector_EmptyNamespaceIsReported(t *testing.T) {
	ns := namespaceObj("empty-ns")

	clientset := k8sfake.NewSimpleClientset(ns)
	d := detector.NewUnusedNamespaceDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	f := findNamespaceFinding(findings, "empty-ns")
	if f == nil {
		t.Fatalf("want empty-ns to be reported, got %+v", findings)
	}
	if f.Kind != "Namespace" || f.Namespace != "" {
		t.Errorf("unexpected finding identity: %+v", f)
	}
	if f.Detector != "unused-namespace" {
		t.Errorf("want Detector=unused-namespace, got %q", f.Detector)
	}
	if f.Confidence != finding.ConfidenceHigh {
		t.Errorf("want Confidence=High, got %q", f.Confidence)
	}
	if f.Status != finding.StatusOrphaned {
		t.Errorf("want Status=Orphaned, got %q", f.Status)
	}
	if f.CostUSDPerMonth != nil {
		t.Errorf("want nil CostUSDPerMonth, got %v", *f.CostUSDPerMonth)
	}
}

func TestUnusedNamespaceDetector_SystemNamespacesAreExcluded(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset(
		namespaceObj("kube-system"),
		namespaceObj("kube-public"),
		namespaceObj("kube-node-lease"),
		namespaceObj("default"),
	)
	d := detector.NewUnusedNamespaceDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for hardcoded system namespaces, got %+v", findings)
	}
}

func TestUnusedNamespaceDetector_ResourceListsAreListedOnceNotPerNamespace(t *testing.T) {
	// Regression test for the N+1 fix: Pods/Deployments/StatefulSets/CronJobs must
	// be listed once cluster-wide per Scan, never once per namespace.
	clientset := k8sfake.NewSimpleClientset(
		namespaceObj("ns-a"),
		namespaceObj("ns-b"),
		namespaceObj("ns-c"),
		podIn("ns-a", "app"),
		deploymentIn("ns-b", "app"),
		cronJobIn("ns-c", "app"),
	)

	podListCalls, deployListCalls, stsListCalls, cronListCalls := 0, 0, 0, 0
	clientset.PrependReactor("list", "pods", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		podListCalls++
		return false, nil, nil
	})
	clientset.PrependReactor("list", "deployments", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		deployListCalls++
		return false, nil, nil
	})
	clientset.PrependReactor("list", "statefulsets", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		stsListCalls++
		return false, nil, nil
	})
	clientset.PrependReactor("list", "cronjobs", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		cronListCalls++
		return false, nil, nil
	})

	d := detector.NewUnusedNamespaceDetector()
	if _, err := d.Scan(context.Background(), clientset); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	if podListCalls != 1 {
		t.Errorf("want 1 Pods List call for 3 namespaces, got %d", podListCalls)
	}
	if deployListCalls != 1 {
		t.Errorf("want 1 Deployments List call for 3 namespaces, got %d", deployListCalls)
	}
	if stsListCalls != 1 {
		t.Errorf("want 1 StatefulSets List call for 3 namespaces, got %d", stsListCalls)
	}
	if cronListCalls != 1 {
		t.Errorf("want 1 CronJobs List call for 3 namespaces, got %d", cronListCalls)
	}
}

func TestUnusedNamespaceDetector_ListErrorIsReturned(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset()
	clientset.PrependReactor("list", "namespaces", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("simulated api error")
	})
	d := detector.NewUnusedNamespaceDetector()

	_, err := d.Scan(context.Background(), clientset)
	if err == nil {
		t.Fatal("want an error when listing Namespaces fails, got nil")
	}
}

func TestUnusedNamespaceDetector_IgnoreLabelExcludesResource(t *testing.T) {
	ns := namespaceObj("empty-ns")
	ns.Labels = map[string]string{"k8s-zombie.io/ignore": "true"}

	clientset := k8sfake.NewSimpleClientset(ns)
	d := detector.NewUnusedNamespaceDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if f := findNamespaceFinding(findings, "empty-ns"); f != nil {
		t.Fatalf("want ignore-labeled empty-ns not reported, got %+v", f)
	}
}
