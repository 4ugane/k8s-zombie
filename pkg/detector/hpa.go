package detector

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/4ugane/k8s-zombie/pkg/finding"
)

// StaleHPADetector reports HorizontalPodAutoscalers whose scaleTargetRef points at a
// Deployment or StatefulSet that no longer exists.
//
// Known v1 limitation: only Deployment and StatefulSet target kinds are verified.
// Any other Kind (a ReplicaSet, a CRD-backed target, etc.) is skipped rather than
// reported, since there is no dynamic client available in v1 to look it up generically.
type StaleHPADetector struct{}

// NewStaleHPADetector builds the detector.
func NewStaleHPADetector() *StaleHPADetector {
	return &StaleHPADetector{}
}

func (d *StaleHPADetector) Name() string { return "stale-hpa" }

func (d *StaleHPADetector) Scan(ctx context.Context, clientset kubernetes.Interface) ([]finding.Finding, error) {
	hpas, err := clientset.AutoscalingV2().HorizontalPodAutoscalers(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing horizontalpodautoscalers: %w", err)
	}

	var findings []finding.Finding
	for _, hpa := range hpas.Items {
		targetKind := hpa.Spec.ScaleTargetRef.Kind
		targetName := hpa.Spec.ScaleTargetRef.Name

		var notFound bool
		switch targetKind {
		case "Deployment":
			_, getErr := clientset.AppsV1().Deployments(hpa.Namespace).Get(ctx, targetName, metav1.GetOptions{})
			if getErr != nil {
				if !apierrors.IsNotFound(getErr) {
					return nil, fmt.Errorf("getting deployment %s/%s (HPA %s target): %w", hpa.Namespace, targetName, hpa.Name, getErr)
				}
				notFound = true
			}
		case "StatefulSet":
			_, getErr := clientset.AppsV1().StatefulSets(hpa.Namespace).Get(ctx, targetName, metav1.GetOptions{})
			if getErr != nil {
				if !apierrors.IsNotFound(getErr) {
					return nil, fmt.Errorf("getting statefulset %s/%s (HPA %s target): %w", hpa.Namespace, targetName, hpa.Name, getErr)
				}
				notFound = true
			}
		default:
			// Unsupported target kind: cannot verify in v1, skip silently.
			continue
		}

		if !notFound {
			continue
		}
		if isIgnored(hpa.Labels) {
			continue
		}
		findings = append(findings, finding.Finding{
			Detector:   d.Name(),
			Kind:       "HorizontalPodAutoscaler",
			Namespace:  hpa.Namespace,
			Name:       hpa.Name,
			Reason:     withAge(fmt.Sprintf("target %s %q no longer exists", targetKind, targetName), "created", hpa.CreationTimestamp),
			Confidence: finding.ConfidenceHigh,
			Status:     finding.StatusOrphaned,
			CreatedAt:  hpa.CreationTimestamp.Time,
		})
	}
	return findings, nil
}
