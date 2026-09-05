package detector

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/4ugane/k8s-zombie/pkg/cost"
	"github.com/4ugane/k8s-zombie/pkg/finding"
)

// OrphanedPVDetector reports PersistentVolumes left in the Released or Available
// phase — Retain-policy leftovers whose underlying EBS volume keeps billing after
// their claim is gone (spec section 5, detector #8).
type OrphanedPVDetector struct {
	estimator *cost.Estimator
}

// NewOrphanedPVDetector builds the detector. estimator may be nil to skip cost
// estimation.
func NewOrphanedPVDetector(estimator *cost.Estimator) *OrphanedPVDetector {
	return &OrphanedPVDetector{estimator: estimator}
}

func (d *OrphanedPVDetector) Name() string { return "orphaned-pv" }

func (d *OrphanedPVDetector) Scan(ctx context.Context, clientset kubernetes.Interface) ([]finding.Finding, error) {
	pvs, err := clientset.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing persistentvolumes: %w", err)
	}

	var scTypes map[string]string
	if d.estimator != nil {
		scTypes, err = storageClassVolumeTypes(ctx, clientset)
		if err != nil {
			return nil, err
		}
	}

	var findings []finding.Finding
	for _, pv := range pvs.Items {
		if pv.Status.Phase != corev1.VolumeReleased && pv.Status.Phase != corev1.VolumeAvailable {
			continue
		}
		if isIgnored(pv.Labels) {
			continue
		}
		findings = append(findings, finding.Finding{
			Detector:        d.Name(),
			Kind:            "PersistentVolume",
			Name:            pv.Name,
			Reason:          withAge("PersistentVolume is "+string(pv.Status.Phase)+" (Retain-policy leftover still billing for the underlying volume)", "created", pv.CreationTimestamp),
			Confidence:      finding.ConfidenceHigh,
			Status:          finding.StatusOrphaned,
			CostUSDPerMonth: d.estimateCost(scTypes, pv),
		})
	}
	return findings, nil
}

func (d *OrphanedPVDetector) estimateCost(scTypes map[string]string, pv corev1.PersistentVolume) *float64 {
	if d.estimator == nil {
		return nil
	}
	volumeType := scTypes[pv.Spec.StorageClassName]
	sizeGB := storageGB(pv.Spec.Capacity)
	c := d.estimator.EBSMonthlyCost(volumeType, sizeGB)
	return &c
}
