package detector_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/4ugane/k8s-zombie/pkg/detector"
	"github.com/4ugane/k8s-zombie/pkg/finding"
)

func hpaWithTarget(ns, name, kind, targetName string) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind: kind,
				Name: targetName,
			},
		},
	}
}

func deployment(ns, name string) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
}

func statefulSet(ns, name string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
}

func TestStaleHPADetector_ExistingDeploymentTargetIsNotReported(t *testing.T) {
	hpa := hpaWithTarget("default", "my-hpa", "Deployment", "my-app")
	dep := deployment("default", "my-app")
	clientset := k8sfake.NewSimpleClientset(hpa, dep)
	d := detector.NewStaleHPADetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for an existing Deployment target, got %+v", findings)
	}
}

func TestStaleHPADetector_MissingDeploymentTargetIsReported(t *testing.T) {
	hpa := hpaWithTarget("default", "my-hpa", "Deployment", "gone-app")
	clientset := k8sfake.NewSimpleClientset(hpa)
	d := detector.NewStaleHPADetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %+v", findings)
	}
	f := findings[0]
	if f.Kind != "HorizontalPodAutoscaler" || f.Name != "my-hpa" || f.Namespace != "default" {
		t.Errorf("unexpected finding identity: %+v", f)
	}
	if f.Status != finding.StatusOrphaned {
		t.Errorf("want Status=Orphaned, got %q", f.Status)
	}
	if f.Confidence != finding.ConfidenceHigh {
		t.Errorf("want Confidence=High, got %q", f.Confidence)
	}
	if !strings.Contains(f.Reason, "Deployment") {
		t.Errorf("want Reason to mention Deployment, got %q", f.Reason)
	}
}

func TestStaleHPADetector_ExistingStatefulSetTargetIsNotReported(t *testing.T) {
	hpa := hpaWithTarget("default", "my-hpa", "StatefulSet", "my-sts")
	sts := statefulSet("default", "my-sts")
	clientset := k8sfake.NewSimpleClientset(hpa, sts)
	d := detector.NewStaleHPADetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for an existing StatefulSet target, got %+v", findings)
	}
}

func TestStaleHPADetector_MissingStatefulSetTargetIsReported(t *testing.T) {
	hpa := hpaWithTarget("default", "my-hpa", "StatefulSet", "gone-sts")
	clientset := k8sfake.NewSimpleClientset(hpa)
	d := detector.NewStaleHPADetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding, got %+v", findings)
	}
	if !strings.Contains(findings[0].Reason, "StatefulSet") {
		t.Errorf("want Reason to mention StatefulSet, got %q", findings[0].Reason)
	}
}

func TestStaleHPADetector_UnsupportedTargetKindIsSkippedWithoutError(t *testing.T) {
	hpa := hpaWithTarget("default", "my-hpa", "ReplicaSet", "some-rs")
	clientset := k8sfake.NewSimpleClientset(hpa)
	d := detector.NewStaleHPADetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil for an unsupported target kind", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for an unsupported target kind, got %+v", findings)
	}
}

func TestStaleHPADetector_ListErrorIsReturned(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset()
	clientset.PrependReactor("list", "horizontalpodautoscalers", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("simulated api error")
	})
	d := detector.NewStaleHPADetector()

	_, err := d.Scan(context.Background(), clientset)
	if err == nil {
		t.Fatal("want an error when listing HPAs fails, got nil")
	}
}

func TestStaleHPADetector_NonNotFoundGetErrorIsReturned(t *testing.T) {
	hpa := hpaWithTarget("default", "my-hpa", "Deployment", "my-app")
	clientset := k8sfake.NewSimpleClientset(hpa)
	forbiddenErr := apierrors.NewForbidden(
		schema.GroupResource{Group: "apps", Resource: "deployments"},
		"my-app", errors.New("user cannot get resource"),
	)
	clientset.PrependReactor("get", "deployments", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, forbiddenErr
	})
	d := detector.NewStaleHPADetector()

	_, err := d.Scan(context.Background(), clientset)
	if err == nil {
		t.Fatal("want a non-NotFound Get error to propagate, got nil")
	}
	if apierrors.IsNotFound(err) {
		t.Fatalf("want a non-NotFound error, got %v", err)
	}
}

func TestStaleHPADetector_IgnoreLabelExcludesResource(t *testing.T) {
	hpa := hpaWithTarget("default", "my-hpa", "Deployment", "gone-app")
	hpa.Labels = map[string]string{"k8s-zombie.io/ignore": "true"}

	clientset := k8sfake.NewSimpleClientset(hpa)
	d := detector.NewStaleHPADetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("want 0 findings for an ignore-labeled HPA, got %+v", findings)
	}
}
