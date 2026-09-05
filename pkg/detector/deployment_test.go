package detector_test

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/4ugane/k8s-zombie/pkg/detector"
	"github.com/4ugane/k8s-zombie/pkg/finding"
)

func deploymentWithReadyReplicas(ns, name string, ready int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: ready},
	}
}

func hpaTargeting(ns, hpaName, targetKind, targetName string) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: hpaName},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: targetKind,
				Name: targetName,
			},
		},
	}
}

func TestIdleDeploymentDetector_ReadyDeploymentIsNotReported(t *testing.T) {
	dep := deploymentWithReadyReplicas("default", "app", 3)
	clientset := k8sfake.NewSimpleClientset(dep)
	d := detector.NewIdleDeploymentDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for a ready deployment, got %+v", findings)
	}
}

func TestIdleDeploymentDetector_ZeroReadyReplicasNoHPAIsReported(t *testing.T) {
	dep := deploymentWithReadyReplicas("default", "app", 0)
	clientset := k8sfake.NewSimpleClientset(dep)
	d := detector.NewIdleDeploymentDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %+v", findings)
	}
	f := findings[0]
	if f.Detector != "idle-deployment" || f.Kind != "Deployment" || f.Namespace != "default" || f.Name != "app" {
		t.Errorf("unexpected finding identity: %+v", f)
	}
	if f.Confidence != finding.ConfidenceHigh {
		t.Errorf("want Confidence=High, got %q", f.Confidence)
	}
	if f.Status != finding.StatusOrphaned {
		t.Errorf("want Status=Orphaned, got %q", f.Status)
	}
	if f.Reason != "0 ready replicas, not managed by any HorizontalPodAutoscaler" {
		t.Errorf("unexpected reason: %q", f.Reason)
	}
}

func TestIdleDeploymentDetector_ZeroReadyReplicasManagedByHPAIsNotReported(t *testing.T) {
	dep := deploymentWithReadyReplicas("default", "app", 0)
	hpa := hpaTargeting("default", "app-hpa", "Deployment", "app")
	clientset := k8sfake.NewSimpleClientset(dep, hpa)
	d := detector.NewIdleDeploymentDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for an HPA-managed deployment, got %+v", findings)
	}
}

func TestIdleDeploymentDetector_HPATargetingDifferentDeploymentIsReported(t *testing.T) {
	dep := deploymentWithReadyReplicas("default", "app", 0)
	hpa := hpaTargeting("default", "other-hpa", "Deployment", "other-app")
	clientset := k8sfake.NewSimpleClientset(dep, hpa)
	d := detector.NewIdleDeploymentDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding for a deployment not covered by any HPA, got %+v", findings)
	}
	if findings[0].Name != "app" {
		t.Errorf("want finding for 'app', got %q", findings[0].Name)
	}
}

func TestIdleDeploymentDetector_DeploymentListErrorIsReturned(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset()
	clientset.PrependReactor("list", "deployments", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("simulated api error")
	})
	d := detector.NewIdleDeploymentDetector()

	_, err := d.Scan(context.Background(), clientset)
	if err == nil {
		t.Fatal("want an error when listing Deployments fails, got nil")
	}
}

func TestIdleDeploymentDetector_HPAListErrorIsReturned(t *testing.T) {
	dep := deploymentWithReadyReplicas("default", "app", 0)
	clientset := k8sfake.NewSimpleClientset(dep)
	clientset.PrependReactor("list", "horizontalpodautoscalers", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("simulated api error")
	})
	d := detector.NewIdleDeploymentDetector()

	_, err := d.Scan(context.Background(), clientset)
	if err == nil {
		t.Fatal("want an error when listing HorizontalPodAutoscalers fails, got nil")
	}
}

func TestIdleDeploymentDetector_IgnoreLabelExcludesResource(t *testing.T) {
	dep := deploymentWithReadyReplicas("default", "app", 0)
	dep.Labels = map[string]string{"k8s-zombie.io/ignore": "true"}

	clientset := k8sfake.NewSimpleClientset(dep)
	d := detector.NewIdleDeploymentDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for an ignore-labeled Deployment, got %+v", findings)
	}
}
