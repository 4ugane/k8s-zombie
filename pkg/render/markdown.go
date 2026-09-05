package render

import (
	"fmt"
	"io"

	"github.com/4ugane/k8s-zombie/pkg/finding"
)

// MarkdownRenderer writes findings as a GFM markdown table.
type MarkdownRenderer struct{}

func (MarkdownRenderer) Render(w io.Writer, findings []finding.Finding) error {
	sorted := sortedFindings(findings)
	if len(sorted) == 0 {
		_, err := io.WriteString(w, "No orphaned resources found.\n")
		return err
	}

	if _, err := fmt.Fprintf(w, "**%s**\n", summaryLine(summarize(sorted))); err != nil {
		return err
	}

	for _, group := range groupedByKind(sorted) {
		if _, err := fmt.Fprintf(w, "\n### %s — %s (%d)\n\n", group.Kind, group.Detector, len(group.Findings)); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "| NAMESPACE | NAME | REASON | CONFIDENCE | COST/MO | STATUS |\n"); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "|---|---|---|---|---|---|\n"); err != nil {
			return err
		}
		for _, f := range group.Findings {
			if _, err := fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s |\n",
				formatNamespace(f.Namespace), f.Name, f.Reason, f.Confidence, formatCost(f.CostUSDPerMonth), f.Status,
			); err != nil {
				return err
			}
		}
	}
	return nil
}
