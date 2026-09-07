package detector_test

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/4ugane/k8s-zombie/pkg/cost"
	"github.com/4ugane/k8s-zombie/pkg/detector"
	"github.com/4ugane/k8s-zombie/pkg/finding"
)

func statefulSetWithVolumeClaimTemplate(ns, name string, replicas int32, vctName string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{ObjectMeta: metav1.ObjectMeta{Name: vctName}},
			},
		},
	}
}

func gp3StorageClass(name string) *storagev1.StorageClass {
	return &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Parameters: map[string]string{"type": "gp3"},
	}
}

func quantityGiB(sizeGiB int64) resource.Quantity {
	return *resource.NewQuantity(sizeGiB*1024*1024*1024, resource.BinarySI)
}

func pvcWithStorage(ns, name, scName string, sizeGiB int64) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &scName,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: quantityGiB(sizeGiB)},
			},
		},
	}
}

func podWithPVC(ns, podName, claimName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: podName},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claimName},
					},
				},
			},
		},
	}
}

func testEstimator() *cost.Estimator {
	return cost.NewEstimator(&cost.PricingTable{
		DefaultEBSVolumeType:    "gp3",
		EBSUSDPerGBMonth:        map[string]float64{"gp3": 0.08},
		LoadBalancerUSDPerMonth: 16.43,
	})
}

func TestUnattachedPVCDetector_ReferencedPVCIsNotReported(t *testing.T) {
	sc := gp3StorageClass("gp3")
	pvc := pvcWithStorage("default", "used-pvc", "gp3", 10)
	pod := podWithPVC("default", "app", "used-pvc")

	clientset := k8sfake.NewSimpleClientset(sc, pvc, pod)
	d := detector.NewUnattachedPVCDetector(testEstimator())

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for a referenced PVC, got %+v", findings)
	}
}

func TestUnattachedPVCDetector_UnreferencedPVCIsReportedWithCost(t *testing.T) {
	sc := gp3StorageClass("gp3")
	pvc := pvcWithStorage("default", "orphan-pvc", "gp3", 100)

	clientset := k8sfake.NewSimpleClientset(sc, pvc)
	d := detector.NewUnattachedPVCDetector(testEstimator())

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %+v", findings)
	}
	f := findings[0]
	if f.Kind != "PersistentVolumeClaim" || f.Name != "orphan-pvc" || f.Namespace != "default" {
		t.Errorf("unexpected finding identity: %+v", f)
	}
	if f.Status != finding.StatusOrphaned {
		t.Errorf("want Status=Orphaned, got %q", f.Status)
	}
	if f.CostUSDPerMonth == nil || *f.CostUSDPerMonth != 8.0 {
		t.Errorf("want CostUSDPerMonth=8.0 (100GB * 0.08), got %v", f.CostUSDPerMonth)
	}
}

func TestUnattachedPVCDetector_MissingStorageClassFallsBackToDefaultCost(t *testing.T) {
	pvc := pvcWithStorage("default", "orphan-pvc", "does-not-exist", 50)

	clientset := k8sfake.NewSimpleClientset(pvc)
	d := detector.NewUnattachedPVCDetector(testEstimator())

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %+v", findings)
	}
	if findings[0].CostUSDPerMonth == nil || *findings[0].CostUSDPerMonth != 4.0 {
		t.Errorf("want fallback cost 4.0 (50GB * default gp3 0.08), got %v", findings[0].CostUSDPerMonth)
	}
}

func TestUnattachedPVCDetector_NilEstimatorSkipsCost(t *testing.T) {
	pvc := pvcWithStorage("default", "orphan-pvc", "gp3", 10)
	clientset := k8sfake.NewSimpleClientset(pvc)
	d := detector.NewUnattachedPVCDetector(nil)

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %+v", findings)
	}
	if findings[0].CostUSDPerMonth != nil {
		t.Errorf("want nil CostUSDPerMonth with a nil estimator, got %v", *findings[0].CostUSDPerMonth)
	}
}

func TestUnattachedPVCDetector_ListErrorIsReturned(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset()
	clientset.PrependReactor("list", "persistentvolumeclaims", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("simulated api error")
	})
	d := detector.NewUnattachedPVCDetector(testEstimator())

	_, err := d.Scan(context.Background(), clientset)
	if err == nil {
		t.Fatal("want an error when listing PVCs fails, got nil")
	}
}

func TestUnattachedPVCDetector_StorageClassListErrorIsReturned(t *testing.T) {
	pvc := pvcWithStorage("default", "orphan-pvc", "gp3", 10)
	clientset := k8sfake.NewSimpleClientset(pvc)
	clientset.PrependReactor("list", "storageclasses", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("simulated api error")
	})
	d := detector.NewUnattachedPVCDetector(testEstimator())

	_, err := d.Scan(context.Background(), clientset)
	if err == nil {
		t.Fatal("want an error when listing StorageClasses fails, got nil")
	}
}

func TestUnattachedPVCDetector_StorageClassLookupIsListNotGet(t *testing.T) {
	// Regression test for the N+1 fix: StorageClasses must be listed once per Scan,
	// never fetched individually per PVC.
	sc := gp3StorageClass("gp3")
	pvc1 := pvcWithStorage("default", "orphan-1", "gp3", 10)
	pvc2 := pvcWithStorage("default", "orphan-2", "gp3", 20)
	clientset := k8sfake.NewSimpleClientset(sc, pvc1, pvc2)

	getCalls := 0
	clientset.PrependReactor("get", "storageclasses", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		getCalls++
		return false, nil, nil // let the default reactor still handle it
	})

	d := detector.NewUnattachedPVCDetector(testEstimator())
	if _, err := d.Scan(context.Background(), clientset); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if getCalls != 0 {
		t.Errorf("want 0 per-resource StorageClass Get calls for 2 PVCs sharing 1 StorageClass, got %d", getCalls)
	}
}

func TestUnattachedPVCDetector_StatefulSetScaleDownLeftoverIsNotReported(t *testing.T) {
	// Replicas=1 but this PVC is ordinal 1 (data-myapp-1) — a scale-down
	// leftover Kubernetes retains by default for a future scale-up, not an
	// abandoned PVC.
	sts := statefulSetWithVolumeClaimTemplate("default", "myapp", 1, "data")
	pvc := pvcWithStorage("default", "data-myapp-1", "gp3", 10)

	clientset := k8sfake.NewSimpleClientset(sts, pvc)
	d := detector.NewUnattachedPVCDetector(nil)

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for a StatefulSet scale-down leftover PVC, got %+v", findings)
	}
}

func TestUnattachedPVCDetector_StatefulSetActiveOrdinalStillReportedIfUnreferenced(t *testing.T) {
	// Replicas=2, ordinal 0 is within the active range — if it's genuinely
	// unmounted (no Pod references it), that's still a real orphan; the
	// StatefulSet exclusion must not blanket-exempt every StatefulSet PVC.
	sts := statefulSetWithVolumeClaimTemplate("default", "myapp", 2, "data")
	pvc := pvcWithStorage("default", "data-myapp-0", "gp3", 10)

	clientset := k8sfake.NewSimpleClientset(sts, pvc)
	d := detector.NewUnattachedPVCDetector(nil)

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for an unreferenced active-ordinal StatefulSet PVC, got %+v", findings)
	}
}

func TestUnattachedPVCDetector_StatefulSetListErrorIsReturned(t *testing.T) {
	pvc := pvcWithStorage("default", "orphan-pvc", "gp3", 10)
	clientset := k8sfake.NewSimpleClientset(pvc)
	clientset.PrependReactor("list", "statefulsets", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("simulated api error")
	})
	d := detector.NewUnattachedPVCDetector(testEstimator())

	_, err := d.Scan(context.Background(), clientset)
	if err == nil {
		t.Fatal("want an error when listing StatefulSets fails, got nil")
	}
}

func TestUnattachedPVCDetector_StatefulSetLookupIsListNotGet(t *testing.T) {
	// Regression test: StatefulSets must be listed once per Scan, never fetched
	// individually per PVC (same N+1 risk the StorageClass lookup already guards
	// against above).
	sts := statefulSetWithVolumeClaimTemplate("default", "myapp", 1, "data")
	pvc1 := pvcWithStorage("default", "orphan-1", "gp3", 10)
	pvc2 := pvcWithStorage("default", "orphan-2", "gp3", 20)
	clientset := k8sfake.NewSimpleClientset(sts, pvc1, pvc2)

	getCalls := 0
	clientset.PrependReactor("get", "statefulsets", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		getCalls++
		return false, nil, nil
	})

	d := detector.NewUnattachedPVCDetector(testEstimator())
	if _, err := d.Scan(context.Background(), clientset); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if getCalls != 0 {
		t.Errorf("want 0 per-resource StatefulSet Get calls for 2 PVCs, got %d", getCalls)
	}
}

func TestUnattachedPVCDetector_IgnoreLabelExcludesResource(t *testing.T) {
	pvc := pvcWithStorage("default", "orphan-pvc", "gp3", 100)
	pvc.Labels = map[string]string{"k8s-zombie.io/ignore": "true"}

	clientset := k8sfake.NewSimpleClientset(pvc)
	d := detector.NewUnattachedPVCDetector(testEstimator())

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for an ignore-labeled PVC, got %+v", findings)
	}
}
