package detector

import (
	"context"
	"fmt"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"

	"github.com/4ugane/k8s-zombie/pkg/finding"
)

// Registry runs a fixed set of Detectors concurrently and aggregates their findings.
type Registry struct {
	detectors []Detector
}

// NewRegistry builds a Registry from the given detectors.
func NewRegistry(detectors ...Detector) *Registry {
	return &Registry{detectors: detectors}
}

// RunAll runs every registered detector concurrently against clientset. A detector
// that fails with an RBAC-forbidden error, any other returned error, or a panic
// (e.g. a bug in a third-party detector) never fails the whole run — it contributes
// a single Skipped finding instead, per spec section 7 and ADR-0002's isolation goal.
func (r *Registry) RunAll(ctx context.Context, clientset kubernetes.Interface) []finding.Finding {
	results := make([][]finding.Finding, len(r.detectors))

	var wg sync.WaitGroup
	for i, d := range r.detectors {
		wg.Add(1)
		go func(i int, d Detector) {
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					results[i] = []finding.Finding{skippedFinding(d.Name(), fmt.Errorf("panic: %v", rec))}
				}
			}()
			findings, err := d.Scan(ctx, clientset)
			if err != nil {
				results[i] = []finding.Finding{skippedFinding(d.Name(), err)}
				return
			}
			results[i] = findings
		}(i, d)
	}
	wg.Wait()

	var all []finding.Finding
	for _, fs := range results {
		all = append(all, fs...)
	}
	return all
}

func skippedFinding(detectorName string, err error) finding.Finding {
	reason := err.Error()
	if apierrors.IsForbidden(err) {
		reason = "RBAC: " + reason
	}
	return finding.Finding{
		Detector: detectorName,
		Reason:   reason,
		Status:   finding.StatusSkipped,
	}
}
