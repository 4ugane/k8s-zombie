package detector

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func testStatefulSet(ns, name string, replicas int32, vctName string) appsv1.StatefulSet {
	return appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{ObjectMeta: metav1.ObjectMeta{Name: vctName}},
			},
		},
	}
}

func TestIsStatefulSetScaleDownLeftover_DifferentNamespaceDoesNotMatch(t *testing.T) {
	statefulSets := []appsv1.StatefulSet{testStatefulSet("other-ns", "myapp", 1, "data")}
	if isStatefulSetScaleDownLeftover("default", "data-myapp-1", statefulSets) {
		t.Error("want a StatefulSet in a different namespace to never match")
	}
}

func TestIsStatefulSetScaleDownLeftover_UnrelatedPVCNameDoesNotMatch(t *testing.T) {
	statefulSets := []appsv1.StatefulSet{testStatefulSet("default", "myapp", 1, "data")}
	if isStatefulSetScaleDownLeftover("default", "totally-unrelated-pvc", statefulSets) {
		t.Error("want a PVC name matching no StatefulSet's naming convention to never match")
	}
}

func TestIsStatefulSetScaleDownLeftover_NonNumericOrdinalDoesNotMatch(t *testing.T) {
	statefulSets := []appsv1.StatefulSet{testStatefulSet("default", "myapp", 1, "data")}
	if isStatefulSetScaleDownLeftover("default", "data-myapp-abc", statefulSets) {
		t.Error("want a non-numeric ordinal suffix to never match")
	}
}

func TestIsStatefulSetScaleDownLeftover_NegativeOrdinalDoesNotMatch(t *testing.T) {
	statefulSets := []appsv1.StatefulSet{testStatefulSet("default", "myapp", 1, "data")}
	if isStatefulSetScaleDownLeftover("default", "data-myapp--1", statefulSets) {
		t.Error("want a negative-looking ordinal suffix to never match")
	}
}

func TestIsStatefulSetScaleDownLeftover_ChecksEveryVolumeClaimTemplate(t *testing.T) {
	sts := testStatefulSet("default", "myapp", 1, "data")
	sts.Spec.VolumeClaimTemplates = append(sts.Spec.VolumeClaimTemplates,
		corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "logs"}})
	statefulSets := []appsv1.StatefulSet{sts}

	if !isStatefulSetScaleDownLeftover("default", "logs-myapp-1", statefulSets) {
		t.Error("want a match against the second volumeClaimTemplate, not just the first")
	}
}

func TestIsStatefulSetScaleDownLeftover_NilReplicasDefaultsToOne(t *testing.T) {
	sts := testStatefulSet("default", "myapp", 1, "data")
	sts.Spec.Replicas = nil // unset Replicas defaults to 1 per the Kubernetes API
	statefulSets := []appsv1.StatefulSet{sts}

	if isStatefulSetScaleDownLeftover("default", "data-myapp-0", statefulSets) {
		t.Error("want ordinal 0 (within the default-1 active range) to not match")
	}
	if !isStatefulSetScaleDownLeftover("default", "data-myapp-1", statefulSets) {
		t.Error("want ordinal 1 (beyond the default-1 active range) to match")
	}
}
