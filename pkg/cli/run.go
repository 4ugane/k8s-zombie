// Package cli wires the detector registry, cost estimator, and renderers together
// into one testable Run function, independent of any flag-parsing/cobra concerns
// (kept in cmd/k8s-zombie for that reason).
package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"k8s.io/client-go/kubernetes"

	"github.com/4ugane/k8s-zombie/pkg/cost"
	"github.com/4ugane/k8s-zombie/pkg/detector"
	"github.com/4ugane/k8s-zombie/pkg/finding"
	"github.com/4ugane/k8s-zombie/pkg/render"
)

// Options configures one scan run.
type Options struct {
	// Namespace, if non-empty, keeps only findings in that namespace (plus every
	// cluster-scoped finding, e.g. from the unused-namespace detector).
	Namespace string
	// ExcludeNamespaces drops any (namespaced) finding in one of these namespaces.
	ExcludeNamespaces []string
	// Output selects the render format: "table" (default), "json", "markdown", or
	// "html".
	Output string
	// PricingTable feeds the cost estimator shared by every cost-attributed
	// detector. Must not be nil — the caller loads it via cost.DefaultPricingTable()
	// or cost.LoadPricingTable() (for --pricing-file) before calling Run.
	PricingTable *cost.PricingTable
	// MinAge, if non-zero, drops any finding whose resource was created more
	// recently than MinAge ago — a grace period to avoid false positives during
	// an active rollout (e.g. a Deployment reporting 0 ready replicas seconds
	// after being created). Zero (the default) disables the filter entirely.
	// A finding with a zero CreatedAt (see finding.Finding) is never affected,
	// regardless of MinAge.
	MinAge time.Duration
}

// Run scans clientset with every v1 detector, filters the results per opts, and
// renders them to w in the requested format.
func Run(ctx context.Context, clientset kubernetes.Interface, opts Options, w io.Writer) error {
	renderer, err := rendererFor(opts.Output)
	if err != nil {
		return err
	}

	estimator := cost.NewEstimator(opts.PricingTable)
	registry := detector.NewRegistry(
		detector.NewUnattachedPVCDetector(estimator),
		detector.NewOrphanedPVDetector(estimator),
		detector.NewZeroEndpointServiceDetector(estimator),
		detector.NewUnusedNamespaceDetector(),
		detector.NewUnusedConfigMapSecretDetector(),
		detector.NewOrphanedIngressDetector(),
		detector.NewIdleDeploymentDetector(),
		detector.NewStaleHPADetector(),
		detector.NewCompletedJobsAndPodsDetector(),
	)

	findings := filterFindings(registry.RunAll(ctx, clientset), opts)
	return renderer.Render(w, findings)
}

// filterFindings applies Namespace/ExcludeNamespaces. Cluster-scoped findings
// (Namespace == "") are never filtered out — a namespace filter has no meaningful
// interpretation for a resource that isn't namespaced.
func filterFindings(findings []finding.Finding, opts Options) []finding.Finding {
	exclude := make(map[string]bool, len(opts.ExcludeNamespaces))
	for _, ns := range opts.ExcludeNamespaces {
		exclude[ns] = true
	}

	var out []finding.Finding
	for _, f := range findings {
		if f.Namespace != "" {
			if opts.Namespace != "" && f.Namespace != opts.Namespace {
				continue
			}
			if exclude[f.Namespace] {
				continue
			}
		}
		if opts.MinAge > 0 && !f.CreatedAt.IsZero() && time.Since(f.CreatedAt) < opts.MinAge {
			continue
		}
		out = append(out, f)
	}
	return out
}

func rendererFor(output string) (render.Renderer, error) {
	switch output {
	case "", "table":
		return render.TableRenderer{}, nil
	case "json":
		return render.JSONRenderer{}, nil
	case "markdown":
		return render.MarkdownRenderer{}, nil
	case "html":
		return render.HTMLRenderer{}, nil
	default:
		return nil, fmt.Errorf("unknown output format %q (want table, json, markdown, or html)", output)
	}
}
