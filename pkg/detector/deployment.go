package detector

import (
	"context"
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/4ugane/k8s-zombie/pkg/finding"
)

// IdleDeploymentDetector reports Deployments with 0 ready replicas that are not
// managed by any HorizontalPodAutoscaler.
type IdleDeploymentDetector struct{}

// NewIdleDeploymentDetector builds the detector.
func NewIdleDeploymentDetector() *IdleDeploymentDetector {
	return &IdleDeploymentDetector{}
}

func (d *IdleDeploymentDetector) Name() string { return "idle-deployment" }

func (d *IdleDeploymentDetector) Scan(ctx context.Context, clientset kubernetes.Interface) ([]finding.Finding, error) {
	deployments, err := clientset.AppsV1().Deployments(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing deployments: %w", err)
	}
	hpas, err := clientset.AutoscalingV2().HorizontalPodAutoscalers(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing horizontalpodautoscalers: %w", err)
	}
	hpaTargets := deploymentHPATargetKeys(hpas.Items)

	var findings []finding.Finding
	for _, dep := range deployments.Items {
		if dep.Status.ReadyReplicas != 0 {
			continue
		}
		// A live HPA targeting this Deployment means its replica count is a
		// managed/expected state (e.g. scaled to a low count by design), not
		// necessarily an orphan signal. Skip it to avoid false positives on
		// legitimate HPA-driven scale-down behavior.
		if hpaTargets[dep.Namespace+"/Deployment/"+dep.Name] {
			continue
		}
		if isIgnored(dep.Labels) {
			continue
		}
		findings = append(findings, finding.Finding{
			Detector:   d.Name(),
			Kind:       "Deployment",
			Namespace:  dep.Namespace,
			Name:       dep.Name,
			Reason:     withAge("0 ready replicas, not managed by any HorizontalPodAutoscaler", "created", dep.CreationTimestamp),
			Confidence: finding.ConfidenceHigh,
			Status:     finding.StatusOrphaned,
		})
	}
	return findings, nil
}

func deploymentHPATargetKeys(hpas []autoscalingv2.HorizontalPodAutoscaler) map[string]bool {
	keys := map[string]bool{}
	for _, hpa := range hpas {
		ref := hpa.Spec.ScaleTargetRef
		keys[hpa.Namespace+"/"+ref.Kind+"/"+ref.Name] = true
	}
	return keys
}
