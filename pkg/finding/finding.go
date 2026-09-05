// Package finding defines the shared Finding type every detector reports.
package finding

// Confidence indicates how certain a detector is that a resource is truly orphaned.
type Confidence string

const (
	ConfidenceHigh   Confidence = "High"
	ConfidenceMedium Confidence = "Medium"
	ConfidenceLow    Confidence = "Low"
)

// Status indicates whether a Finding represents an orphaned resource or a detector
// that could not complete (e.g. an RBAC gap on the resource type it scans).
type Status string

const (
	StatusOrphaned Status = "Orphaned"
	StatusSkipped  Status = "Skipped"
)

// Finding is one detected orphan (or one detector-skip warning).
type Finding struct {
	Detector        string     `json:"detector"`
	Kind            string     `json:"kind"`
	Namespace       string     `json:"namespace"`
	Name            string     `json:"name"`
	Reason          string     `json:"reason"`
	Confidence      Confidence `json:"confidence"`
	CostUSDPerMonth *float64   `json:"cost_usd_per_month,omitempty"`
	Status          Status     `json:"status"`
}
