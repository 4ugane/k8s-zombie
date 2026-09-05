package detector_test

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/4ugane/k8s-zombie/pkg/detector"
	"github.com/4ugane/k8s-zombie/pkg/finding"
)

func pvWithPhase(name, scName string, sizeGiB int64, phase corev1.PersistentVolumePhase) *corev1.PersistentVolume {
	return &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PersistentVolumeSpec{
			StorageClassName: scName,
			Capacity:         corev1.ResourceList{corev1.ResourceStorage: quantityGiB(sizeGiB)},
		},
		Status: corev1.PersistentVolumeStatus{Phase: phase},
	}
}

func TestOrphanedPVDetector_BoundPVIsNotReported(t *testing.T) {
	pv := pvWithPhase("bound-pv", "gp3", 20, corev1.VolumeBound)
	clientset := k8sfake.NewSimpleClientset(pv)
	d := detector.NewOrphanedPVDetector(testEstimator())

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for a Bound PV, got %+v", findings)
	}
}

func TestOrphanedPVDetector_ReleasedPVIsReportedWithCost(t *testing.T) {
	sc := gp3StorageClass("gp3")
	pv := pvWithPhase("released-pv", "gp3", 200, corev1.VolumeReleased)
	clientset := k8sfake.NewSimpleClientset(sc, pv)
	d := detector.NewOrphanedPVDetector(testEstimator())

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %+v", findings)
	}
	f := findings[0]
	if f.Kind != "PersistentVolume" || f.Name != "released-pv" {
		t.Errorf("unexpected finding identity: %+v", f)
	}
	if f.Status != finding.StatusOrphaned {
		t.Errorf("want Status=Orphaned, got %q", f.Status)
	}
	if f.CostUSDPerMonth == nil || *f.CostUSDPerMonth != 16.0 {
		t.Errorf("want CostUSDPerMonth=16.0 (200GB * 0.08), got %v", f.CostUSDPerMonth)
	}
}

func TestOrphanedPVDetector_AvailablePVIsReported(t *testing.T) {
	pv := pvWithPhase("available-pv", "gp3", 10, corev1.VolumeAvailable)
	clientset := k8sfake.NewSimpleClientset(pv)
	d := detector.NewOrphanedPVDetector(testEstimator())

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for an Available PV, got %+v", findings)
	}
}

func TestOrphanedPVDetector_NilEstimatorSkipsCost(t *testing.T) {
	pv := pvWithPhase("released-pv", "gp3", 10, corev1.VolumeReleased)
	clientset := k8sfake.NewSimpleClientset(pv)
	d := detector.NewOrphanedPVDetector(nil)

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

func TestOrphanedPVDetector_ListErrorIsReturned(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset()
	clientset.PrependReactor("list", "persistentvolumes", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("simulated api error")
	})
	d := detector.NewOrphanedPVDetector(testEstimator())

	_, err := d.Scan(context.Background(), clientset)
	if err == nil {
		t.Fatal("want an error when listing PVs fails, got nil")
	}
}

func TestOrphanedPVDetector_StorageClassListErrorIsReturned(t *testing.T) {
	pv := pvWithPhase("released-pv", "gp3", 10, corev1.VolumeReleased)
	clientset := k8sfake.NewSimpleClientset(pv)
	clientset.PrependReactor("list", "storageclasses", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("simulated api error")
	})
	d := detector.NewOrphanedPVDetector(testEstimator())

	_, err := d.Scan(context.Background(), clientset)
	if err == nil {
		t.Fatal("want an error when listing StorageClasses fails, got nil")
	}
}

func TestOrphanedPVDetector_StorageClassLookupIsListNotGet(t *testing.T) {
	sc := gp3StorageClass("gp3")
	pv1 := pvWithPhase("released-1", "gp3", 10, corev1.VolumeReleased)
	pv2 := pvWithPhase("released-2", "gp3", 20, corev1.VolumeReleased)
	clientset := k8sfake.NewSimpleClientset(sc, pv1, pv2)

	getCalls := 0
	clientset.PrependReactor("get", "storageclasses", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		getCalls++
		return false, nil, nil
	})

	d := detector.NewOrphanedPVDetector(testEstimator())
	if _, err := d.Scan(context.Background(), clientset); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if getCalls != 0 {
		t.Errorf("want 0 per-resource StorageClass Get calls for 2 PVs sharing 1 StorageClass, got %d", getCalls)
	}
}

func TestOrphanedPVDetector_IgnoreLabelExcludesResource(t *testing.T) {
	pv := pvWithPhase("released-pv", "gp3", 200, corev1.VolumeReleased)
	pv.Labels = map[string]string{"k8s-zombie.io/ignore": "true"}

	clientset := k8sfake.NewSimpleClientset(pv)
	d := detector.NewOrphanedPVDetector(testEstimator())

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for an ignore-labeled PV, got %+v", findings)
	}
}
