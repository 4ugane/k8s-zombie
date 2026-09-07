package detector_test

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/4ugane/k8s-zombie/pkg/detector"
	"github.com/4ugane/k8s-zombie/pkg/finding"
)

func newConfigMap(ns, name string) *corev1.ConfigMap {
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
}

func newSecret(ns, name string, secretType corev1.SecretType) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Type:       secretType,
	}
}

func basicPod(ns, name string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
}

func findFinding(findings []finding.Finding, kind, name string) *finding.Finding {
	for i := range findings {
		if findings[i].Kind == kind && findings[i].Name == name {
			return &findings[i]
		}
	}
	return nil
}

func TestUnusedConfigMapSecretDetector_ConfigMapReferencedByEnvFromIsNotReported(t *testing.T) {
	cm := newConfigMap("default", "used-cm")
	pod := basicPod("default", "app")
	pod.Spec.Containers = []corev1.Container{
		{
			Name:    "main",
			EnvFrom: []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "used-cm"}}}},
		},
	}

	clientset := k8sfake.NewSimpleClientset(cm, pod)
	d := detector.NewUnusedConfigMapSecretDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if f := findFinding(findings, "ConfigMap", "used-cm"); f != nil {
		t.Fatalf("want used-cm not reported, got %+v", *f)
	}
}

func TestUnusedConfigMapSecretDetector_ConfigMapReferencedByVolumeIsNotReported(t *testing.T) {
	cm := newConfigMap("default", "used-cm")
	pod := basicPod("default", "app")
	pod.Spec.Volumes = []corev1.Volume{
		{
			Name: "cfg",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "used-cm"}},
			},
		},
	}

	clientset := k8sfake.NewSimpleClientset(cm, pod)
	d := detector.NewUnusedConfigMapSecretDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if f := findFinding(findings, "ConfigMap", "used-cm"); f != nil {
		t.Fatalf("want used-cm not reported, got %+v", *f)
	}
}

func TestUnusedConfigMapSecretDetector_ConfigMapReferencedByProjectedVolumeIsNotReported(t *testing.T) {
	cm := newConfigMap("default", "used-cm")
	pod := basicPod("default", "app")
	pod.Spec.Volumes = []corev1.Volume{
		{
			Name: "proj",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{ConfigMap: &corev1.ConfigMapProjection{LocalObjectReference: corev1.LocalObjectReference{Name: "used-cm"}}},
					},
				},
			},
		},
	}

	clientset := k8sfake.NewSimpleClientset(cm, pod)
	d := detector.NewUnusedConfigMapSecretDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if f := findFinding(findings, "ConfigMap", "used-cm"); f != nil {
		t.Fatalf("want used-cm not reported, got %+v", *f)
	}
}

func TestUnusedConfigMapSecretDetector_ConfigMapReferencedByEnvValueFromIsNotReported(t *testing.T) {
	cm := newConfigMap("default", "used-cm")
	pod := basicPod("default", "app")
	pod.Spec.Containers = []corev1.Container{
		{
			Name: "main",
			Env: []corev1.EnvVar{
				{
					Name: "SOME_KEY",
					ValueFrom: &corev1.EnvVarSource{
						ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "used-cm"},
							Key:                  "key",
						},
					},
				},
			},
		},
	}

	clientset := k8sfake.NewSimpleClientset(cm, pod)
	d := detector.NewUnusedConfigMapSecretDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if f := findFinding(findings, "ConfigMap", "used-cm"); f != nil {
		t.Fatalf("want used-cm not reported, got %+v", *f)
	}
}

func TestUnusedConfigMapSecretDetector_UnreferencedConfigMapIsReportedWithLowConfidence(t *testing.T) {
	cm := newConfigMap("default", "orphan-cm")

	clientset := k8sfake.NewSimpleClientset(cm)
	d := detector.NewUnusedConfigMapSecretDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	f := findFinding(findings, "ConfigMap", "orphan-cm")
	if f == nil {
		t.Fatalf("want orphan-cm reported, got %+v", findings)
	}
	if f.Namespace != "default" {
		t.Errorf("want Namespace=default, got %q", f.Namespace)
	}
	if f.Confidence != finding.ConfidenceLow {
		t.Errorf("want Confidence=Low, got %q", f.Confidence)
	}
	if f.Status != finding.StatusOrphaned {
		t.Errorf("want Status=Orphaned, got %q", f.Status)
	}
	if f.Detector != "unused-configmap-secret" {
		t.Errorf("want Detector=unused-configmap-secret, got %q", f.Detector)
	}
}

func TestUnusedConfigMapSecretDetector_KubeRootCAConfigMapIsExcluded(t *testing.T) {
	cm := newConfigMap("default", "kube-root-ca.crt")

	clientset := k8sfake.NewSimpleClientset(cm)
	d := detector.NewUnusedConfigMapSecretDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if f := findFinding(findings, "ConfigMap", "kube-root-ca.crt"); f != nil {
		t.Fatalf("want kube-root-ca.crt excluded, got %+v", *f)
	}
}

func TestUnusedConfigMapSecretDetector_SecretReferencedByEnvFromIsNotReported(t *testing.T) {
	secret := newSecret("default", "used-secret", corev1.SecretTypeOpaque)
	pod := basicPod("default", "app")
	pod.Spec.Containers = []corev1.Container{
		{
			Name:    "main",
			EnvFrom: []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "used-secret"}}}},
		},
	}

	clientset := k8sfake.NewSimpleClientset(secret, pod)
	d := detector.NewUnusedConfigMapSecretDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if f := findFinding(findings, "Secret", "used-secret"); f != nil {
		t.Fatalf("want used-secret not reported, got %+v", *f)
	}
}

func TestUnusedConfigMapSecretDetector_SecretReferencedByVolumeIsNotReported(t *testing.T) {
	secret := newSecret("default", "used-secret", corev1.SecretTypeOpaque)
	pod := basicPod("default", "app")
	pod.Spec.Volumes = []corev1.Volume{
		{
			Name: "sec",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: "used-secret"},
			},
		},
	}

	clientset := k8sfake.NewSimpleClientset(secret, pod)
	d := detector.NewUnusedConfigMapSecretDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if f := findFinding(findings, "Secret", "used-secret"); f != nil {
		t.Fatalf("want used-secret not reported, got %+v", *f)
	}
}

func TestUnusedConfigMapSecretDetector_SecretReferencedByImagePullSecretsIsNotReported(t *testing.T) {
	secret := newSecret("default", "used-pull-secret", corev1.SecretTypeDockerConfigJson)
	pod := basicPod("default", "app")
	pod.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "used-pull-secret"}}

	clientset := k8sfake.NewSimpleClientset(secret, pod)
	d := detector.NewUnusedConfigMapSecretDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if f := findFinding(findings, "Secret", "used-pull-secret"); f != nil {
		t.Fatalf("want used-pull-secret not reported, got %+v", *f)
	}
}

func TestUnusedConfigMapSecretDetector_UnreferencedSecretIsReported(t *testing.T) {
	secret := newSecret("default", "orphan-secret", corev1.SecretTypeOpaque)

	clientset := k8sfake.NewSimpleClientset(secret)
	d := detector.NewUnusedConfigMapSecretDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	f := findFinding(findings, "Secret", "orphan-secret")
	if f == nil {
		t.Fatalf("want orphan-secret reported, got %+v", findings)
	}
	if f.Confidence != finding.ConfidenceLow {
		t.Errorf("want Confidence=Low, got %q", f.Confidence)
	}
	if f.Status != finding.StatusOrphaned {
		t.Errorf("want Status=Orphaned, got %q", f.Status)
	}
}

func TestUnusedConfigMapSecretDetector_ServiceAccountTokenSecretIsExcluded(t *testing.T) {
	secret := newSecret("default", "default-token-abcde", corev1.SecretTypeServiceAccountToken)

	clientset := k8sfake.NewSimpleClientset(secret)
	d := detector.NewUnusedConfigMapSecretDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if f := findFinding(findings, "Secret", "default-token-abcde"); f != nil {
		t.Fatalf("want service-account-token secret excluded, got %+v", *f)
	}
}

func TestUnusedConfigMapSecretDetector_HelmReleaseSecretIsExcluded(t *testing.T) {
	secret := newSecret("default", "sh.helm.release.v1.myapp.v1", "helm.sh/release.v1")

	clientset := k8sfake.NewSimpleClientset(secret)
	d := detector.NewUnusedConfigMapSecretDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if f := findFinding(findings, "Secret", "sh.helm.release.v1.myapp.v1"); f != nil {
		t.Fatalf("want helm release secret excluded, got %+v", *f)
	}
}

func TestUnusedConfigMapSecretDetector_ReferenceFromInitContainerIsNotReported(t *testing.T) {
	cm := newConfigMap("default", "init-cm")
	secret := newSecret("default", "init-secret", corev1.SecretTypeOpaque)
	pod := basicPod("default", "app")
	pod.Spec.InitContainers = []corev1.Container{
		{
			Name: "init",
			EnvFrom: []corev1.EnvFromSource{
				{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "init-cm"}}},
				{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "init-secret"}}},
			},
		},
	}

	clientset := k8sfake.NewSimpleClientset(cm, secret, pod)
	d := detector.NewUnusedConfigMapSecretDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if f := findFinding(findings, "ConfigMap", "init-cm"); f != nil {
		t.Fatalf("want init-cm not reported (referenced by init container), got %+v", *f)
	}
	if f := findFinding(findings, "Secret", "init-secret"); f != nil {
		t.Fatalf("want init-secret not reported (referenced by init container), got %+v", *f)
	}
}

func TestUnusedConfigMapSecretDetector_ConfigMapListErrorIsReturned(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset()
	clientset.PrependReactor("list", "configmaps", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("simulated api error")
	})
	d := detector.NewUnusedConfigMapSecretDetector()

	_, err := d.Scan(context.Background(), clientset)
	if err == nil {
		t.Fatal("want an error when listing ConfigMaps fails, got nil")
	}
}

func TestUnusedConfigMapSecretDetector_SecretListErrorIsReturned(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset()
	clientset.PrependReactor("list", "secrets", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("simulated api error")
	})
	d := detector.NewUnusedConfigMapSecretDetector()

	_, err := d.Scan(context.Background(), clientset)
	if err == nil {
		t.Fatal("want an error when listing Secrets fails, got nil")
	}
}

func TestUnusedConfigMapSecretDetector_PodListErrorIsReturned(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset()
	clientset.PrependReactor("list", "pods", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("simulated api error")
	})
	d := detector.NewUnusedConfigMapSecretDetector()

	_, err := d.Scan(context.Background(), clientset)
	if err == nil {
		t.Fatal("want an error when listing Pods fails, got nil")
	}
}

func TestUnusedConfigMapSecretDetector_IgnoreLabelExcludesConfigMap(t *testing.T) {
	cm := newConfigMap("default", "orphan-cm")
	cm.Labels = map[string]string{"k8s-zombie.io/ignore": "true"}

	clientset := k8sfake.NewSimpleClientset(cm)
	d := detector.NewUnusedConfigMapSecretDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if f := findFinding(findings, "ConfigMap", "orphan-cm"); f != nil {
		t.Fatalf("want ignore-labeled orphan-cm not reported, got %+v", *f)
	}
}

func TestUnusedConfigMapSecretDetector_HelmHookAnnotationExcludesConfigMap(t *testing.T) {
	cm := newConfigMap("default", "hook-cm")
	cm.Annotations = map[string]string{"helm.sh/hook": "pre-install"}

	clientset := k8sfake.NewSimpleClientset(cm)
	d := detector.NewUnusedConfigMapSecretDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if f := findFinding(findings, "ConfigMap", "hook-cm"); f != nil {
		t.Fatalf("want Helm-hook-annotated hook-cm not reported, got %+v", *f)
	}
}

func TestUnusedConfigMapSecretDetector_HelmHookAnnotationExcludesSecret(t *testing.T) {
	secret := newSecret("default", "hook-secret", corev1.SecretTypeOpaque)
	secret.Annotations = map[string]string{"helm.sh/hook": "pre-upgrade,post-upgrade"}

	clientset := k8sfake.NewSimpleClientset(secret)
	d := detector.NewUnusedConfigMapSecretDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if f := findFinding(findings, "Secret", "hook-secret"); f != nil {
		t.Fatalf("want Helm-hook-annotated hook-secret not reported, got %+v", *f)
	}
}

func TestUnusedConfigMapSecretDetector_IgnoreLabelExcludesSecret(t *testing.T) {
	secret := newSecret("default", "orphan-secret", corev1.SecretTypeOpaque)
	secret.Labels = map[string]string{"k8s-zombie.io/ignore": "true"}

	clientset := k8sfake.NewSimpleClientset(secret)
	d := detector.NewUnusedConfigMapSecretDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if f := findFinding(findings, "Secret", "orphan-secret"); f != nil {
		t.Fatalf("want ignore-labeled orphan-secret not reported, got %+v", *f)
	}
}
