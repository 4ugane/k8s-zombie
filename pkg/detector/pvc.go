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

// UnattachedPVCDetector reports PersistentVolumeClaims not referenced by any Pod
// volume (spec section 5, detector #3).
type UnattachedPVCDetector struct {
	estimator *cost.Estimator
}

// NewUnattachedPVCDetector builds the detector. estimator may be nil to skip cost
// estimation (e.g. in tests that don't care about pricing).
func NewUnattachedPVCDetector(estimator *cost.Estimator) *UnattachedPVCDetector {
	return &UnattachedPVCDetector{estimator: estimator}
}

func (d *UnattachedPVCDetector) Name() string { return "unattached-pvc" }

func (d *UnattachedPVCDetector) Scan(ctx context.Context, clientset kubernetes.Interface) ([]finding.Finding, error) {
	pvcs, err := clientset.CoreV1().PersistentVolumeClaims(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing persistentvolumeclaims: %w", err)
	}
	pods, err := clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing pods: %w", err)
	}
	referenced := referencedPVCs(pods.Items)

	statefulSets, err := clientset.AppsV1().StatefulSets(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing statefulsets: %w", err)
	}

	var scTypes map[string]string
	if d.estimator != nil {
		scTypes, err = storageClassVolumeTypes(ctx, clientset)
		if err != nil {
			return nil, err
		}
	}

	var findings []finding.Finding
	for _, pvc := range pvcs.Items {
		if referenced[pvc.Namespace+"/"+pvc.Name] {
			continue
		}
		if isStatefulSetScaleDownLeftover(pvc.Namespace, pvc.Name, statefulSets.Items) {
			continue
		}
		if isIgnored(pvc.Labels) {
			continue
		}
		findings = append(findings, finding.Finding{
			Detector:        d.Name(),
			Kind:            "PersistentVolumeClaim",
			Namespace:       pvc.Namespace,
			Name:            pvc.Name,
			Reason:          withAge("not referenced by any Pod volume", "created", pvc.CreationTimestamp),
			Confidence:      finding.ConfidenceHigh,
			Status:          finding.StatusOrphaned,
			CostUSDPerMonth: d.estimateCost(scTypes, pvc),
			CreatedAt:       pvc.CreationTimestamp.Time,
		})
	}
	return findings, nil
}

func referencedPVCs(pods []corev1.Pod) map[string]bool {
	refs := map[string]bool{}
	for _, pod := range pods {
		for _, vol := range pod.Spec.Volumes {
			if vol.PersistentVolumeClaim != nil {
				refs[pod.Namespace+"/"+vol.PersistentVolumeClaim.ClaimName] = true
			}
		}
	}
	return refs
}

func (d *UnattachedPVCDetector) estimateCost(scTypes map[string]string, pvc corev1.PersistentVolumeClaim) *float64 {
	if d.estimator == nil {
		return nil
	}
	scName := ""
	if pvc.Spec.StorageClassName != nil {
		scName = *pvc.Spec.StorageClassName
	}
	volumeType := scTypes[scName]
	sizeGB := storageGB(pvc.Spec.Resources.Requests)
	c := d.estimator.EBSMonthlyCost(volumeType, sizeGB)
	return &c
}
