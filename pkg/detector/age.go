package detector

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ageString renders a human-readable age ("just now", "5h", "3d") for a
// resource's timestamp, so a Finding's Reason carries real per-resource
// evidence instead of a boilerplate sentence repeated identically across every
// finding of that detector. Returns "" for a zero timestamp (nothing to show).
func ageString(t metav1.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t.Time)
	switch {
	case d < time.Hour:
		return "just now"
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// withAge appends a "(<label> <age> ago)" suffix to reason when t is a non-zero
// timestamp, e.g. withAge("...", "created", pvc.CreationTimestamp) renders
// "... (created 3d ago)". Returns reason unchanged for a zero timestamp.
func withAge(reason, label string, t metav1.Time) string {
	age := ageString(t)
	if age == "" {
		return reason
	}
	return fmt.Sprintf("%s (%s %s ago)", reason, label, age)
}
