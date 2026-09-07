package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/4ugane/k8s-zombie/pkg/cli"
	"github.com/4ugane/k8s-zombie/pkg/cost"
	"github.com/4ugane/k8s-zombie/pkg/finding"
)

func testPricingTable() *cost.PricingTable {
	return &cost.PricingTable{
		DefaultEBSVolumeType:    "gp3",
		EBSUSDPerGBMonth:        map[string]float64{"gp3": 0.08},
		LoadBalancerUSDPerMonth: 16.43,
	}
}

func orphanPVC(ns, name string, sizeGiB int64) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: *resource.NewQuantity(sizeGiB*1024*1024*1024, resource.BinarySI),
				},
			},
		},
	}
}

func TestRun_RendersFindingsAsTable(t *testing.T) {
	pvc := orphanPVC("default", "orphan-pvc", 10)
	clientset := k8sfake.NewSimpleClientset(pvc)

	var out bytes.Buffer
	err := cli.Run(context.Background(), clientset, cli.Options{Output: "table", PricingTable: testPricingTable()}, &out)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("orphan-pvc")) {
		t.Errorf("want table output to contain %q, got:\n%s", "orphan-pvc", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("PersistentVolumeClaim")) {
		t.Errorf("want table output to contain Kind, got:\n%s", out.String())
	}
}

func TestRun_DefaultOutputIsTable(t *testing.T) {
	pvc := orphanPVC("default", "orphan-pvc", 10)
	clientset := k8sfake.NewSimpleClientset(pvc)

	var out bytes.Buffer
	err := cli.Run(context.Background(), clientset, cli.Options{PricingTable: testPricingTable()}, &out)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("NAMESPACE")) {
		t.Errorf("want default output to look like a table (header row), got:\n%s", out.String())
	}
}

func TestRun_HTMLOutputIsAWellFormedDocument(t *testing.T) {
	pvc := orphanPVC("default", "orphan-pvc", 10)
	clientset := k8sfake.NewSimpleClientset(pvc)

	var out bytes.Buffer
	err := cli.Run(context.Background(), clientset, cli.Options{Output: "html", PricingTable: testPricingTable()}, &out)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("<!DOCTYPE html>")) {
		t.Errorf("want a valid HTML document, got:\n%s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("orphan-pvc")) {
		t.Errorf("want HTML output to contain %q, got:\n%s", "orphan-pvc", out.String())
	}
}

func TestRun_UnknownOutputFormatReturnsError(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset()

	var out bytes.Buffer
	err := cli.Run(context.Background(), clientset, cli.Options{Output: "yaml", PricingTable: testPricingTable()}, &out)
	if err == nil {
		t.Fatal("want an error for an unknown output format, got nil")
	}
}

func TestRun_JSONOutputIsValidAndRoundTrips(t *testing.T) {
	pvc := orphanPVC("default", "orphan-pvc", 100)
	clientset := k8sfake.NewSimpleClientset(pvc)

	var out bytes.Buffer
	err := cli.Run(context.Background(), clientset, cli.Options{Output: "json", PricingTable: testPricingTable()}, &out)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	var got []finding.Finding
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; output:\n%s", err, out.String())
	}
	found := false
	for _, f := range got {
		if f.Name == "orphan-pvc" && f.Kind == "PersistentVolumeClaim" {
			found = true
		}
	}
	if !found {
		t.Errorf("want a PersistentVolumeClaim/orphan-pvc finding in JSON output, got %+v", got)
	}
}

func TestRun_NamespaceFilterKeepsOnlyMatchingNamespace(t *testing.T) {
	pvcA := orphanPVC("ns-a", "orphan-a", 10)
	pvcB := orphanPVC("ns-b", "orphan-b", 10)
	clientset := k8sfake.NewSimpleClientset(pvcA, pvcB)

	var out bytes.Buffer
	err := cli.Run(context.Background(), clientset, cli.Options{Output: "json", Namespace: "ns-a", PricingTable: testPricingTable()}, &out)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	var got []finding.Finding
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	for _, f := range got {
		if f.Namespace != "" && f.Namespace != "ns-a" {
			t.Errorf("want only ns-a findings (or cluster-scoped), got one in %q: %+v", f.Namespace, f)
		}
	}
	if len(got) == 0 {
		t.Error("want at least the ns-a finding, got none")
	}
}

func TestRun_ExcludeNamespaceFilterDropsMatches(t *testing.T) {
	pvcA := orphanPVC("ns-a", "orphan-a", 10)
	pvcB := orphanPVC("ns-b", "orphan-b", 10)
	clientset := k8sfake.NewSimpleClientset(pvcA, pvcB)

	var out bytes.Buffer
	err := cli.Run(context.Background(), clientset, cli.Options{Output: "json", ExcludeNamespaces: []string{"ns-b"}, PricingTable: testPricingTable()}, &out)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	var got []finding.Finding
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	for _, f := range got {
		if f.Namespace == "ns-b" {
			t.Errorf("want ns-b excluded, got a finding in it: %+v", f)
		}
	}
}

func TestRun_MinAgeFilterExcludesRecentlyCreatedResource(t *testing.T) {
	pvc := orphanPVC("default", "orphan-pvc", 10)
	pvc.CreationTimestamp = metav1.NewTime(time.Now())
	clientset := k8sfake.NewSimpleClientset(pvc)

	var out bytes.Buffer
	err := cli.Run(context.Background(), clientset, cli.Options{Output: "json", MinAge: time.Hour, PricingTable: testPricingTable()}, &out)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	var got []finding.Finding
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	for _, f := range got {
		if f.Name == "orphan-pvc" {
			t.Errorf("want a resource created moments ago excluded under MinAge=1h, got it reported: %+v", f)
		}
	}
}

func TestRun_MinAgeFilterKeepsOlderResource(t *testing.T) {
	pvc := orphanPVC("default", "orphan-pvc", 10)
	pvc.CreationTimestamp = metav1.NewTime(time.Now().Add(-48 * time.Hour))
	clientset := k8sfake.NewSimpleClientset(pvc)

	var out bytes.Buffer
	err := cli.Run(context.Background(), clientset, cli.Options{Output: "json", MinAge: time.Hour, PricingTable: testPricingTable()}, &out)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	var got []finding.Finding
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	found := false
	for _, f := range got {
		if f.Name == "orphan-pvc" {
			found = true
		}
	}
	if !found {
		t.Errorf("want a resource created 48h ago still reported under MinAge=1h, got none: %+v", got)
	}
}

func TestRun_MinAgeZeroValueDisablesFiltering(t *testing.T) {
	pvc := orphanPVC("default", "orphan-pvc", 10)
	pvc.CreationTimestamp = metav1.NewTime(time.Now())
	clientset := k8sfake.NewSimpleClientset(pvc)

	var out bytes.Buffer
	// MinAge left unset (zero value) — the flag is opt-in.
	err := cli.Run(context.Background(), clientset, cli.Options{Output: "json", PricingTable: testPricingTable()}, &out)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	var got []finding.Finding
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	found := false
	for _, f := range got {
		if f.Name == "orphan-pvc" {
			found = true
		}
	}
	if !found {
		t.Errorf("want MinAge=0 (unset) to report even a brand-new resource, got none: %+v", got)
	}
}

func TestRun_MinAgeNeverExcludesFindingsWithoutACreationTimestamp(t *testing.T) {
	// completed-jobs-and-pods findings deliberately don't carry a CreatedAt (a
	// terminal state, not an initialization race) — MinAge must never suppress
	// them even when set very high.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "finished-pod"},
		Status:     corev1.PodStatus{Phase: corev1.PodSucceeded},
	}
	clientset := k8sfake.NewSimpleClientset(pod)

	var out bytes.Buffer
	err := cli.Run(context.Background(), clientset, cli.Options{Output: "json", MinAge: 24 * time.Hour, PricingTable: testPricingTable()}, &out)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	var got []finding.Finding
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	found := false
	for _, f := range got {
		if f.Name == "finished-pod" {
			found = true
		}
	}
	if !found {
		t.Errorf("want a completed Pod finding to survive MinAge filtering (no CreatedAt set), got none: %+v", got)
	}
}

func TestRun_ClusterScopedFindingsAlwaysIncludedRegardlessOfNamespaceFilter(t *testing.T) {
	// An empty, non-excluded Namespace object triggers the unused-namespace
	// detector, which reports a cluster-scoped finding (Namespace: "").
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "totally-empty"}}
	clientset := k8sfake.NewSimpleClientset(ns)

	var out bytes.Buffer
	err := cli.Run(context.Background(), clientset, cli.Options{Output: "json", Namespace: "some-other-ns", PricingTable: testPricingTable()}, &out)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	var got []finding.Finding
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	found := false
	for _, f := range got {
		if f.Kind == "Namespace" && f.Name == "totally-empty" {
			found = true
		}
	}
	if !found {
		t.Errorf("want the cluster-scoped Namespace finding to survive an unrelated --namespace filter, got %+v", got)
	}
}
