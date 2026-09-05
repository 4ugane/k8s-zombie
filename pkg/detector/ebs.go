package detector

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const bytesPerGB = 1024 * 1024 * 1024

// storageClassVolumeTypes lists every StorageClass once and returns a name->"type"
// parameter lookup map. Listing once per Scan (rather than a Get per orphaned
// resource) avoids an N+1 API call pattern when many PVCs/PVs share a handful of
// StorageClasses.
func storageClassVolumeTypes(ctx context.Context, clientset kubernetes.Interface) (map[string]string, error) {
	scs, err := clientset.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing storageclasses: %w", err)
	}
	types := make(map[string]string, len(scs.Items))
	for _, sc := range scs.Items {
		types[sc.Name] = sc.Parameters["type"]
	}
	return types, nil
}

// storageGB reads the "storage" quantity from a ResourceList and returns it in GB.
func storageGB(rl corev1.ResourceList) float64 {
	qty, ok := rl[corev1.ResourceStorage]
	if !ok {
		return 0
	}
	return float64(qty.Value()) / bytesPerGB
}
