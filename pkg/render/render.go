// Package render formats a slice of finding.Finding into human- or
// machine-readable output (table, JSON, markdown, HTML).
package render

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/4ugane/k8s-zombie/pkg/finding"
)

// Renderer writes a slice of findings to w in some output format.
type Renderer interface {
	Render(w io.Writer, findings []finding.Finding) error
}

// sortedFindings returns a new slice, sorted by Kind, then Namespace, then
// Name, then Detector (all ascending). It never mutates findings.
func sortedFindings(findings []finding.Finding) []finding.Finding {
	sorted := make([]finding.Finding, len(findings))
	copy(sorted, findings)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Detector < b.Detector
	})
	return sorted
}

// kindGroup is one resource Kind's findings, in sortedFindings order.
type kindGroup struct {
	Kind     string
	Detector string // see detectorLabel
	Findings []finding.Finding
}

// groupedByKind splits an already-sorted (by Kind first) findings slice into
// per-Kind groups, preserving order. The table/markdown/HTML renderers use this
// so a resource Kind reads as one labeled section instead of a repeated column
// value on every row.
func groupedByKind(sorted []finding.Finding) []kindGroup {
	var groups []kindGroup
	for _, f := range sorted {
		if len(groups) == 0 || groups[len(groups)-1].Kind != f.Kind {
			groups = append(groups, kindGroup{Kind: f.Kind})
		}
		last := &groups[len(groups)-1]
		last.Findings = append(last.Findings, f)
	}
	for i := range groups {
		groups[i].Detector = detectorLabel(groups[i].Findings)
	}
	return groups
}

// detectorLabel names the detector(s) behind a group's findings, for the group
// header — every v1 detector maps 1:1 to a Kind, so a group's Detector column
// would otherwise repeat the same value on every single row. If a future
// detector ever emits a Kind another detector also uses, this joins the
// distinct names instead of silently picking one.
func detectorLabel(findings []finding.Finding) string {
	seen := map[string]bool{}
	var names []string
	for _, f := range findings {
		if !seen[f.Detector] {
			seen[f.Detector] = true
			names = append(names, f.Detector)
		}
	}
	return strings.Join(names, ", ")
}

// summaryStats gives the reader the gist before the detail: how many findings,
// and — since not every detector has a cost line — how many of those carry a
// cost estimate and what they add up to.
type summaryStats struct {
	Total        int
	CostedCount  int
	TotalCostUSD float64
}

func summarize(findings []finding.Finding) summaryStats {
	var s summaryStats
	s.Total = len(findings)
	for _, f := range findings {
		if f.CostUSDPerMonth != nil {
			s.CostedCount++
			s.TotalCostUSD += *f.CostUSDPerMonth
		}
	}
	return s
}

// summaryLine renders "N orphaned resource(s) found[, M with an estimated cost,
// totaling $X.XX/month]" — shared text used by both the table and markdown
// renderers (HTML renders the same numbers into its own summary markup instead).
func summaryLine(stats summaryStats) string {
	line := fmt.Sprintf("%d orphaned resource(s) found", stats.Total)
	if stats.CostedCount > 0 {
		line += fmt.Sprintf(" (%d with an estimated cost, totaling $%.2f/month)", stats.CostedCount, stats.TotalCostUSD)
	}
	return line
}

// formatCost renders a nil cost as "-" and a non-nil cost as "$12.50".
func formatCost(cost *float64) string {
	if cost == nil {
		return "-"
	}
	return fmt.Sprintf("$%.2f", *cost)
}

// formatNamespace renders an empty namespace (a cluster-scoped resource, e.g. the
// unused-namespace or orphaned-pv detectors) as "-" rather than a blank cell, so
// it reads as deliberate rather than missing data.
func formatNamespace(ns string) string {
	if ns == "" {
		return "-"
	}
	return ns
}
