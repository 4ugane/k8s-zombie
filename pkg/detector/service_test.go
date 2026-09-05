package detector_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/4ugane/k8s-zombie/pkg/detector"
	"github.com/4ugane/k8s-zombie/pkg/finding"
)

func boolPtr(b bool) *bool { return &b }

func serviceOfType(ns, name string, svcType corev1.ServiceType) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec:       corev1.ServiceSpec{Type: svcType},
	}
}

func endpointSlice(ns, sliceName, serviceName string, ready ...bool) *discoveryv1.EndpointSlice {
	var endpoints []discoveryv1.Endpoint
	for _, r := range ready {
		endpoints = append(endpoints, discoveryv1.Endpoint{
			Addresses:  []string{"10.0.0.1"},
			Conditions: discoveryv1.EndpointConditions{Ready: boolPtr(r)},
		})
	}
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      sliceName,
			Labels:    map[string]string{discoveryv1.LabelServiceName: serviceName},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints:   endpoints,
	}
}

func TestZeroEndpointServiceDetector_ServiceWithReadyEndpointIsNotReported(t *testing.T) {
	svc := serviceOfType("default", "healthy-svc", corev1.ServiceTypeClusterIP)
	slice := endpointSlice("default", "healthy-svc-abc", "healthy-svc", true)

	clientset := k8sfake.NewSimpleClientset(svc, slice)
	d := detector.NewZeroEndpointServiceDetector(testEstimator())

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for a Service with a ready endpoint, got %+v", findings)
	}
}

func TestZeroEndpointServiceDetector_AllEndpointsNotReadyIsReported(t *testing.T) {
	svc := serviceOfType("default", "dead-svc", corev1.ServiceTypeClusterIP)
	slice := endpointSlice("default", "dead-svc-abc", "dead-svc", false, false)

	clientset := k8sfake.NewSimpleClientset(svc, slice)
	d := detector.NewZeroEndpointServiceDetector(testEstimator())

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %+v", findings)
	}
	f := findings[0]
	if f.Kind != "Service" || f.Name != "dead-svc" || f.Namespace != "default" {
		t.Errorf("unexpected finding identity: %+v", f)
	}
	if f.Status != finding.StatusOrphaned {
		t.Errorf("want Status=Orphaned, got %q", f.Status)
	}
}

func TestZeroEndpointServiceDetector_NoEndpointSliceAtAllIsReported(t *testing.T) {
	svc := serviceOfType("default", "orphan-svc", corev1.ServiceTypeClusterIP)

	clientset := k8sfake.NewSimpleClientset(svc)
	d := detector.NewZeroEndpointServiceDetector(testEstimator())

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for a Service with no EndpointSlice, got %+v", findings)
	}
}

func TestZeroEndpointServiceDetector_ExternalNameServiceIsNeverReported(t *testing.T) {
	svc := serviceOfType("default", "external-svc", corev1.ServiceTypeExternalName)

	clientset := k8sfake.NewSimpleClientset(svc)
	d := detector.NewZeroEndpointServiceDetector(testEstimator())

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for an ExternalName Service, got %+v", findings)
	}
}

func TestZeroEndpointServiceDetector_LoadBalancerTypeGetsCostEstimate(t *testing.T) {
	svc := serviceOfType("default", "dead-lb", corev1.ServiceTypeLoadBalancer)

	clientset := k8sfake.NewSimpleClientset(svc)
	d := detector.NewZeroEndpointServiceDetector(testEstimator())

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %+v", findings)
	}
	if findings[0].CostUSDPerMonth == nil || *findings[0].CostUSDPerMonth != 16.43 {
		t.Errorf("want CostUSDPerMonth=16.43, got %v", findings[0].CostUSDPerMonth)
	}
}

func TestZeroEndpointServiceDetector_ClusterIPTypeHasNoCostEstimate(t *testing.T) {
	svc := serviceOfType("default", "dead-clusterip", corev1.ServiceTypeClusterIP)

	clientset := k8sfake.NewSimpleClientset(svc)
	d := detector.NewZeroEndpointServiceDetector(testEstimator())

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %+v", findings)
	}
	if findings[0].CostUSDPerMonth != nil {
		t.Errorf("want nil CostUSDPerMonth for a ClusterIP Service, got %v", *findings[0].CostUSDPerMonth)
	}
}

func TestZeroEndpointServiceDetector_IgnoreLabelExcludesResource(t *testing.T) {
	svc := serviceOfType("default", "dead-svc", corev1.ServiceTypeClusterIP)
	svc.Labels = map[string]string{"k8s-zombie.io/ignore": "true"}
	slice := endpointSlice("default", "dead-svc-abc", "dead-svc", false, false)

	clientset := k8sfake.NewSimpleClientset(svc, slice)
	d := detector.NewZeroEndpointServiceDetector(testEstimator())

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for an ignore-labeled Service, got %+v", findings)
	}
}
