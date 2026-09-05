package detector_test

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/4ugane/k8s-zombie/pkg/detector"
	"github.com/4ugane/k8s-zombie/pkg/finding"
)

// ingressTestService builds a Service fixture of the given type for the fake
// clientset, so OrphanedIngressDetector's Service lookup can find it.
func ingressTestService(ns, name string, svcType corev1.ServiceType) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec:       corev1.ServiceSpec{Type: svcType},
	}
}

func ingressTestEndpointSlice(ns, sliceName, serviceName string, ready ...bool) *discoveryv1.EndpointSlice {
	var endpoints []discoveryv1.Endpoint
	for _, r := range ready {
		endpoints = append(endpoints, discoveryv1.Endpoint{
			Addresses:  []string{"10.0.0.1"},
			Conditions: discoveryv1.EndpointConditions{Ready: ingressBoolPtr(r)},
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

func ingressBoolPtr(b bool) *bool { return &b }

func ingressServiceBackend(name string) networkingv1.IngressBackend {
	return networkingv1.IngressBackend{
		Service: &networkingv1.IngressServiceBackend{
			Name: name,
			Port: networkingv1.ServiceBackendPort{Number: 80},
		},
	}
}

// ingressWithRuleBackends builds an Ingress whose single rule has one HTTP path per
// backend Service name given.
func ingressWithRuleBackends(ns, name string, serviceNames ...string) *networkingv1.Ingress {
	var paths []networkingv1.HTTPIngressPath
	for _, svcName := range serviceNames {
		paths = append(paths, networkingv1.HTTPIngressPath{Backend: ingressServiceBackend(svcName)})
	}
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{Paths: paths},
					},
				},
			},
		},
	}
}

func ingressWithDefaultBackend(ns, name, serviceName string) *networkingv1.Ingress {
	backend := ingressServiceBackend(serviceName)
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec: networkingv1.IngressSpec{
			DefaultBackend: &backend,
		},
	}
}

func ingressWithNoBackends(ns, name string) *networkingv1.Ingress {
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec:       networkingv1.IngressSpec{},
	}
}

func TestOrphanedIngressDetector_Name(t *testing.T) {
	d := detector.NewOrphanedIngressDetector()
	if d.Name() != "orphaned-ingress" {
		t.Errorf("want Name()=orphaned-ingress, got %q", d.Name())
	}
}

func TestOrphanedIngressDetector_SingleBackendHealthyIsNotReported(t *testing.T) {
	ing := ingressWithRuleBackends("default", "healthy-ing", "healthy-svc")
	svc := ingressTestService("default", "healthy-svc", corev1.ServiceTypeClusterIP)
	slice := ingressTestEndpointSlice("default", "healthy-svc-abc", "healthy-svc", true)

	clientset := k8sfake.NewSimpleClientset(ing, svc, slice)
	d := detector.NewOrphanedIngressDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings, got %+v", findings)
	}
}

func TestOrphanedIngressDetector_SingleBackendZeroReadyIsReported(t *testing.T) {
	ing := ingressWithRuleBackends("default", "dead-ing", "dead-svc")
	svc := ingressTestService("default", "dead-svc", corev1.ServiceTypeClusterIP)
	slice := ingressTestEndpointSlice("default", "dead-svc-abc", "dead-svc", false, false)

	clientset := k8sfake.NewSimpleClientset(ing, svc, slice)
	d := detector.NewOrphanedIngressDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %+v", findings)
	}
	f := findings[0]
	if f.Kind != "Ingress" || f.Name != "dead-ing" || f.Namespace != "default" {
		t.Errorf("unexpected finding identity: %+v", f)
	}
	if f.Detector != "orphaned-ingress" {
		t.Errorf("want Detector=orphaned-ingress, got %q", f.Detector)
	}
	if f.Confidence != finding.ConfidenceHigh {
		t.Errorf("want Confidence=High, got %q", f.Confidence)
	}
	if f.Status != finding.StatusOrphaned {
		t.Errorf("want Status=Orphaned, got %q", f.Status)
	}
}

func TestOrphanedIngressDetector_SingleBackendMissingServiceIsReported(t *testing.T) {
	ing := ingressWithRuleBackends("default", "missing-svc-ing", "does-not-exist")

	clientset := k8sfake.NewSimpleClientset(ing)
	d := detector.NewOrphanedIngressDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for a backend with no EndpointSlice, got %+v", findings)
	}
}

func TestOrphanedIngressDetector_TwoBackendsOneHealthyIsNotReported(t *testing.T) {
	ing := ingressWithRuleBackends("default", "mixed-ing", "healthy-svc", "dead-svc")
	healthySvc := ingressTestService("default", "healthy-svc", corev1.ServiceTypeClusterIP)
	deadSvc := ingressTestService("default", "dead-svc", corev1.ServiceTypeClusterIP)
	healthySlice := ingressTestEndpointSlice("default", "healthy-svc-abc", "healthy-svc", true)
	deadSlice := ingressTestEndpointSlice("default", "dead-svc-abc", "dead-svc", false)

	clientset := k8sfake.NewSimpleClientset(ing, healthySvc, deadSvc, healthySlice, deadSlice)
	d := detector.NewOrphanedIngressDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings when at least one backend is healthy, got %+v", findings)
	}
}

func TestOrphanedIngressDetector_TwoBackendsBothDeadIsReported(t *testing.T) {
	ing := ingressWithRuleBackends("default", "both-dead-ing", "dead-svc-1", "dead-svc-2")
	svc1 := ingressTestService("default", "dead-svc-1", corev1.ServiceTypeClusterIP)
	svc2 := ingressTestService("default", "dead-svc-2", corev1.ServiceTypeClusterIP)
	slice1 := ingressTestEndpointSlice("default", "dead-svc-1-abc", "dead-svc-1", false)
	slice2 := ingressTestEndpointSlice("default", "dead-svc-2-abc", "dead-svc-2", false)

	clientset := k8sfake.NewSimpleClientset(ing, svc1, svc2, slice1, slice2)
	d := detector.NewOrphanedIngressDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding when all backends are dead, got %+v", findings)
	}
}

func TestOrphanedIngressDetector_DefaultBackendOnlyDeadIsReported(t *testing.T) {
	ing := ingressWithDefaultBackend("default", "default-backend-ing", "dead-svc")
	svc := ingressTestService("default", "dead-svc", corev1.ServiceTypeClusterIP)

	clientset := k8sfake.NewSimpleClientset(ing, svc)
	d := detector.NewOrphanedIngressDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for a dead DefaultBackend, got %+v", findings)
	}
}

func TestOrphanedIngressDetector_NoBackendReferencesIsSkipped(t *testing.T) {
	ing := ingressWithNoBackends("default", "no-backend-ing")

	clientset := k8sfake.NewSimpleClientset(ing)
	d := detector.NewOrphanedIngressDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for an Ingress with no backend references, got %+v", findings)
	}
}

func TestOrphanedIngressDetector_IngressListErrorIsReturned(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset()
	clientset.PrependReactor("list", "ingresses", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("simulated api error")
	})
	d := detector.NewOrphanedIngressDetector()

	_, err := d.Scan(context.Background(), clientset)
	if err == nil {
		t.Fatal("want an error when listing Ingresses fails, got nil")
	}
}

func TestOrphanedIngressDetector_EndpointSliceListErrorIsReturned(t *testing.T) {
	ing := ingressWithRuleBackends("default", "some-ing", "some-svc")
	clientset := k8sfake.NewSimpleClientset(ing)
	clientset.PrependReactor("list", "endpointslices", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("simulated api error")
	})
	d := detector.NewOrphanedIngressDetector()

	_, err := d.Scan(context.Background(), clientset)
	if err == nil {
		t.Fatal("want an error when listing EndpointSlices fails, got nil")
	}
}

func TestOrphanedIngressDetector_ServiceListErrorIsReturned(t *testing.T) {
	ing := ingressWithRuleBackends("default", "some-ing2", "some-svc")
	clientset := k8sfake.NewSimpleClientset(ing)
	clientset.PrependReactor("list", "services", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("simulated api error")
	})
	d := detector.NewOrphanedIngressDetector()

	_, err := d.Scan(context.Background(), clientset)
	if err == nil {
		t.Fatal("want an error when listing Services fails, got nil")
	}
}

func TestOrphanedIngressDetector_ExternalNameBackendIsNotReported(t *testing.T) {
	ing := ingressWithRuleBackends("default", "extname-ing", "extname-svc")
	svc := ingressTestService("default", "extname-svc", corev1.ServiceTypeExternalName)
	// ExternalName Services never get an EndpointSlice from the EndpointSlice
	// controller — deliberately no slice seeded here.

	clientset := k8sfake.NewSimpleClientset(ing, svc)
	d := detector.NewOrphanedIngressDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for an ExternalName backend (liveness can't be determined), got %+v", findings)
	}
}

func TestOrphanedIngressDetector_ExternalNamePlusDeadBackendIsNotReported(t *testing.T) {
	ing := ingressWithRuleBackends("default", "mixed-extname-ing", "extname-svc", "dead-svc")
	extSvc := ingressTestService("default", "extname-svc", corev1.ServiceTypeExternalName)
	deadSvc := ingressTestService("default", "dead-svc", corev1.ServiceTypeClusterIP)
	deadSlice := ingressTestEndpointSlice("default", "dead-svc-abc", "dead-svc", false)
	// No EndpointSlice for extname-svc — ExternalName Services never get one.

	clientset := k8sfake.NewSimpleClientset(ing, extSvc, deadSvc, deadSlice)
	d := detector.NewOrphanedIngressDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings: an ExternalName backend is never counted as dead, so not ALL backends are dead, got %+v", findings)
	}
}

func TestOrphanedIngressDetector_IgnoreLabelExcludesResource(t *testing.T) {
	ing := ingressWithRuleBackends("default", "dead-ing", "dead-svc")
	ing.Labels = map[string]string{"k8s-zombie.io/ignore": "true"}
	svc := ingressTestService("default", "dead-svc", corev1.ServiceTypeClusterIP)
	slice := ingressTestEndpointSlice("default", "dead-svc-abc", "dead-svc", false, false)

	clientset := k8sfake.NewSimpleClientset(ing, svc, slice)
	d := detector.NewOrphanedIngressDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for an ignore-labeled Ingress, got %+v", findings)
	}
}
