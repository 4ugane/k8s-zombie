package detector

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/4ugane/k8s-zombie/pkg/finding"
)

// kubeRootCAConfigMapName is auto-created by Kubernetes in every namespace and is
// never itself referenced by a Pod, so it must be excluded from reporting.
const kubeRootCAConfigMapName = "kube-root-ca.crt"

// helmReleaseSecretType marks Secrets Helm uses to store release state; these are
// referenced by the Helm client/controller, not by Pods, so they must be excluded.
const helmReleaseSecretType = "helm.sh/release.v1"

// UnusedConfigMapSecretDetector reports ConfigMaps and Secrets that are not
// referenced by any Pod's containers, init containers, or volumes.
//
// Confidence is always Low: this detector can only see references made directly by
// live Pods. It cannot see references made by Helm hooks, CRDs/operators that
// template ConfigMaps/Secrets into resources other than Pods, or controllers that
// have not yet reconciled a Pod for a workload (per the project's documented
// design-spec limitation for this detector).
type UnusedConfigMapSecretDetector struct{}

// NewUnusedConfigMapSecretDetector builds the detector.
func NewUnusedConfigMapSecretDetector() *UnusedConfigMapSecretDetector {
	return &UnusedConfigMapSecretDetector{}
}

func (d *UnusedConfigMapSecretDetector) Name() string { return "unused-configmap-secret" }

func (d *UnusedConfigMapSecretDetector) Scan(ctx context.Context, clientset kubernetes.Interface) ([]finding.Finding, error) {
	configMaps, err := clientset.CoreV1().ConfigMaps(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing configmaps: %w", err)
	}
	secrets, err := clientset.CoreV1().Secrets(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing secrets: %w", err)
	}
	pods, err := clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing pods: %w", err)
	}

	referencedConfigMaps, referencedSecrets := referencedConfigMapsAndSecrets(pods.Items)

	var findings []finding.Finding
	for _, cm := range configMaps.Items {
		if cm.Name == kubeRootCAConfigMapName {
			continue
		}
		if referencedConfigMaps[cm.Namespace+"/"+cm.Name] {
			continue
		}
		if isIgnored(cm.Labels) {
			continue
		}
		if isHelmHook(cm.Annotations) {
			continue
		}
		findings = append(findings, finding.Finding{
			Detector:   d.Name(),
			Kind:       "ConfigMap",
			Namespace:  cm.Namespace,
			Name:       cm.Name,
			Reason:     withAge("not referenced by any Pod (env, volume, or projected volume)", "created", cm.CreationTimestamp),
			Confidence: finding.ConfidenceLow,
			Status:     finding.StatusOrphaned,
			CreatedAt:  cm.CreationTimestamp.Time,
		})
	}
	for _, secret := range secrets.Items {
		if secret.Type == corev1.SecretTypeServiceAccountToken || secret.Type == helmReleaseSecretType {
			continue
		}
		if referencedSecrets[secret.Namespace+"/"+secret.Name] {
			continue
		}
		if isIgnored(secret.Labels) {
			continue
		}
		if isHelmHook(secret.Annotations) {
			continue
		}
		findings = append(findings, finding.Finding{
			Detector:   d.Name(),
			Kind:       "Secret",
			Namespace:  secret.Namespace,
			Name:       secret.Name,
			Reason:     withAge("not referenced by any Pod (env, volume, or projected volume)", "created", secret.CreationTimestamp),
			Confidence: finding.ConfidenceLow,
			Status:     finding.StatusOrphaned,
			CreatedAt:  secret.CreationTimestamp.Time,
		})
	}
	return findings, nil
}

// referencedConfigMapsAndSecrets scans every Pod's containers, init containers, and
// volumes and returns two "namespace/name" sets: ConfigMaps referenced, and Secrets
// referenced.
func referencedConfigMapsAndSecrets(pods []corev1.Pod) (map[string]bool, map[string]bool) {
	configMapRefs := map[string]bool{}
	secretRefs := map[string]bool{}

	for _, pod := range pods {
		addVolumeRefs(pod, configMapRefs, secretRefs)
		addImagePullSecretRefs(pod, secretRefs)

		containers := make([]corev1.Container, 0, len(pod.Spec.Containers)+len(pod.Spec.InitContainers))
		containers = append(containers, pod.Spec.Containers...)
		containers = append(containers, pod.Spec.InitContainers...)
		for _, c := range containers {
			addContainerRefs(pod.Namespace, c, configMapRefs, secretRefs)
		}
	}
	return configMapRefs, secretRefs
}

func addVolumeRefs(pod corev1.Pod, configMapRefs, secretRefs map[string]bool) {
	for _, vol := range pod.Spec.Volumes {
		if vol.ConfigMap != nil {
			configMapRefs[pod.Namespace+"/"+vol.ConfigMap.Name] = true
		}
		if vol.Secret != nil {
			secretRefs[pod.Namespace+"/"+vol.Secret.SecretName] = true
		}
		if vol.Projected != nil {
			for _, src := range vol.Projected.Sources {
				if src.ConfigMap != nil {
					configMapRefs[pod.Namespace+"/"+src.ConfigMap.Name] = true
				}
				if src.Secret != nil {
					secretRefs[pod.Namespace+"/"+src.Secret.Name] = true
				}
			}
		}
	}
}

// addImagePullSecretRefs adds Secrets referenced via pod.Spec.ImagePullSecrets, the
// standard mechanism for supplying private-registry pull credentials to a Pod.
func addImagePullSecretRefs(pod corev1.Pod, secretRefs map[string]bool) {
	for _, ref := range pod.Spec.ImagePullSecrets {
		secretRefs[pod.Namespace+"/"+ref.Name] = true
	}
}

func addContainerRefs(namespace string, c corev1.Container, configMapRefs, secretRefs map[string]bool) {
	for _, envFrom := range c.EnvFrom {
		if envFrom.ConfigMapRef != nil {
			configMapRefs[namespace+"/"+envFrom.ConfigMapRef.Name] = true
		}
		if envFrom.SecretRef != nil {
			secretRefs[namespace+"/"+envFrom.SecretRef.Name] = true
		}
	}
	for _, env := range c.Env {
		if env.ValueFrom == nil {
			continue
		}
		if env.ValueFrom.ConfigMapKeyRef != nil {
			configMapRefs[namespace+"/"+env.ValueFrom.ConfigMapKeyRef.Name] = true
		}
		if env.ValueFrom.SecretKeyRef != nil {
			secretRefs[namespace+"/"+env.ValueFrom.SecretKeyRef.Name] = true
		}
	}
}
