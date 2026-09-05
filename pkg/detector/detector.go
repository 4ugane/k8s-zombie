// Package detector defines the Detector interface and a registry that runs a set of
// detectors concurrently against a cluster, per ADR-0002.
package detector

import (
	"context"

	"k8s.io/client-go/kubernetes"

	"github.com/4ugane/k8s-zombie/pkg/finding"
)

// Detector scans a cluster for one kind of orphaned resource.
type Detector interface {
	Name() string
	Scan(ctx context.Context, clientset kubernetes.Interface) ([]finding.Finding, error)
}
