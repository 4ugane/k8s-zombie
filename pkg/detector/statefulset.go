package detector

import (
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
)

// isStatefulSetScaleDownLeftover reports whether pvcName in namespace matches a
// StatefulSet's volumeClaimTemplate naming convention
// (<template-name>-<statefulset-name>-<ordinal>, per the Kubernetes docs) for an
// ordinal at or beyond that StatefulSet's current replica count. Kubernetes
// retains such a PVC by default after a scale-down so a future scale-up can
// reattach it unchanged — it's a deliberate leftover, not an abandoned one, so
// unattached-pvc must not report it.
//
// A PVC within the active ordinal range (ordinal < replicas) is unaffected:
// if it's genuinely unreferenced, that's still a real orphan.
func isStatefulSetScaleDownLeftover(namespace, pvcName string, statefulSets []appsv1.StatefulSet) bool {
	for _, sts := range statefulSets {
		if sts.Namespace != namespace {
			continue
		}
		replicas := int32(1)
		if sts.Spec.Replicas != nil {
			replicas = *sts.Spec.Replicas
		}
		for _, vct := range sts.Spec.VolumeClaimTemplates {
			prefix := vct.Name + "-" + sts.Name + "-"
			ordinalStr, ok := strings.CutPrefix(pvcName, prefix)
			if !ok {
				continue
			}
			ordinal, err := strconv.Atoi(ordinalStr)
			if err != nil || ordinal < 0 {
				continue
			}
			if int32(ordinal) >= replicas {
				return true
			}
		}
	}
	return false
}
