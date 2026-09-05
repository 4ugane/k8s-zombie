package detector

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/4ugane/k8s-zombie/pkg/finding"
)

// OrphanedIngressDetector reports Ingresses whose backend Services are all either
// missing or have 0 ready endpoints, meaning the Ingress cannot route traffic
// anywhere.
type OrphanedIngressDetector struct{}

// NewOrphanedIngressDetector builds the detector.
func NewOrphanedIngressDetector() *OrphanedIngressDetector {
	return &OrphanedIngressDetector{}
}

func (d *OrphanedIngressDetector) Name() string { return "orphaned-ingress" }

func (d *OrphanedIngressDetector) Scan(ctx context.Context, clientset kubernetes.Interface) ([]finding.Finding, error) {
	ingresses, err := clientset.NetworkingV1().Ingresses(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing ingresses: %w", err)
	}
	services, err := clientset.CoreV1().Services(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing services: %w", err)
	}
	slices, err := clientset.DiscoveryV1().EndpointSlices(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing endpointslices: %w", err)
	}
	readyCount := readyEndpointCountByService(slices.Items)
	svcTypes := serviceTypeByKey(services.Items)

	var findings []finding.Finding
	for _, ing := range ingresses.Items {
		backends := backendServiceNames(ing)
		if len(backends) == 0 {
			// No backend Service references at all (malformed/non-standard Ingress) —
			// out of scope, skip entirely rather than guess.
			continue
		}

		// Conservative rule: only report when EVERY distinct backend Service is dead.
		// If even one backend still has ready endpoints, the Ingress still routes
		// live traffic somewhere, so it isn't orphaned. A backend is "dead" if its
		// Service is missing entirely, or if it exists, isn't ExternalName, and has
		// 0 ready endpoints. ExternalName Services never get EndpointSlices by
		// design (they proxy to an external DNS name), so this detector has no way
		// to verify their liveness — matching the conservative philosophy above,
		// they're never counted as dead.
		allDead := true
		for svcName := range backends {
			key := ing.Namespace + "/" + svcName
			svcType, exists := svcTypes[key]
			dead := !exists || (svcType != corev1.ServiceTypeExternalName && readyCount[key] == 0)
			if !dead {
				allDead = false
				break
			}
		}
		if !allDead {
			continue
		}
		if isIgnored(ing.Labels) {
			continue
		}

		findings = append(findings, finding.Finding{
			Detector:   d.Name(),
			Kind:       "Ingress",
			Namespace:  ing.Namespace,
			Name:       ing.Name,
			Reason:     withAge("all backend Services are missing or have 0 ready endpoints", "created", ing.CreationTimestamp),
			Confidence: finding.ConfidenceHigh,
			Status:     finding.StatusOrphaned,
		})
	}
	return findings, nil
}

// serviceTypeByKey maps each Service's "namespace/name" key to its Spec.Type, so
// callers can tell an ExternalName Service (no EndpointSlices by design) apart from
// a Service that's genuinely dead.
func serviceTypeByKey(services []corev1.Service) map[string]corev1.ServiceType {
	types := map[string]corev1.ServiceType{}
	for _, svc := range services {
		types[svc.Namespace+"/"+svc.Name] = svc.Spec.Type
	}
	return types
}

// backendServiceNames collects the set of distinct backend Service names an Ingress
// references, across its DefaultBackend and all rule paths.
func backendServiceNames(ing networkingv1.Ingress) map[string]struct{} {
	names := map[string]struct{}{}
	if ing.Spec.DefaultBackend != nil && ing.Spec.DefaultBackend.Service != nil {
		names[ing.Spec.DefaultBackend.Service.Name] = struct{}{}
	}
	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service != nil {
				names[path.Backend.Service.Name] = struct{}{}
			}
		}
	}
	return names
}
