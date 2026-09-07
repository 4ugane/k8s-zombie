package detector

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/4ugane/k8s-zombie/pkg/cost"
	"github.com/4ugane/k8s-zombie/pkg/finding"
)

// ZeroEndpointServiceDetector reports Services (including LoadBalancer-type) with
// zero ready endpoints (spec section 5, detector #2). ExternalName Services are
// never reported — they proxy to an external DNS name and have no endpoints by
// design, so "zero endpoints" is not a meaningful signal for them.
type ZeroEndpointServiceDetector struct {
	estimator *cost.Estimator
}

// NewZeroEndpointServiceDetector builds the detector. estimator may be nil to skip
// cost estimation.
func NewZeroEndpointServiceDetector(estimator *cost.Estimator) *ZeroEndpointServiceDetector {
	return &ZeroEndpointServiceDetector{estimator: estimator}
}

func (d *ZeroEndpointServiceDetector) Name() string { return "zero-endpoint-service" }

func (d *ZeroEndpointServiceDetector) Scan(ctx context.Context, clientset kubernetes.Interface) ([]finding.Finding, error) {
	services, err := clientset.CoreV1().Services(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing services: %w", err)
	}
	slices, err := clientset.DiscoveryV1().EndpointSlices(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing endpointslices: %w", err)
	}
	readyCount := readyEndpointCountByService(slices.Items)

	var findings []finding.Finding
	for _, svc := range services.Items {
		if svc.Spec.Type == corev1.ServiceTypeExternalName {
			continue
		}
		if readyCount[svc.Namespace+"/"+svc.Name] > 0 {
			continue
		}
		if isIgnored(svc.Labels) {
			continue
		}
		findings = append(findings, finding.Finding{
			Detector:        d.Name(),
			Kind:            "Service",
			Namespace:       svc.Namespace,
			Name:            svc.Name,
			Reason:          withAge("0 ready endpoints", "created", svc.CreationTimestamp),
			Confidence:      finding.ConfidenceHigh,
			Status:          finding.StatusOrphaned,
			CostUSDPerMonth: d.estimateCost(svc),
			CreatedAt:       svc.CreationTimestamp.Time,
		})
	}
	return findings, nil
}

// readyEndpointCountByService counts ready endpoints per Service, keyed by
// "namespace/name", by summing across every EndpointSlice labeled for that Service
// (a Service can have more than one EndpointSlice).
func readyEndpointCountByService(slices []discoveryv1.EndpointSlice) map[string]int {
	counts := map[string]int{}
	for _, slice := range slices {
		svcName, ok := slice.Labels[discoveryv1.LabelServiceName]
		if !ok {
			continue
		}
		key := slice.Namespace + "/" + svcName
		for _, ep := range slice.Endpoints {
			if endpointIsReady(ep) {
				counts[key]++
			}
		}
	}
	return counts
}

// endpointIsReady treats a nil Ready condition as ready, per the EndpointConditions
// API doc: "A nil value should be interpreted as true."
func endpointIsReady(ep discoveryv1.Endpoint) bool {
	return ep.Conditions.Ready == nil || *ep.Conditions.Ready
}

// estimateCost only attaches a cost to LoadBalancer-type Services — ClusterIP and
// NodePort Services have no direct AWS cost line (spec section 5, detector #2).
func (d *ZeroEndpointServiceDetector) estimateCost(svc corev1.Service) *float64 {
	if d.estimator == nil || svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		return nil
	}
	c := d.estimator.LoadBalancerMonthlyCost()
	return &c
}
