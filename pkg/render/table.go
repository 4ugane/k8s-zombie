package render

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/4ugane/k8s-zombie/pkg/finding"
)

// TableRenderer writes findings as a human-readable, tab-aligned table.
type TableRenderer struct{}

func (TableRenderer) Render(w io.Writer, findings []finding.Finding) error {
	sorted := sortedFindings(findings)
	if len(sorted) == 0 {
		_, err := io.WriteString(w, "No orphaned resources found.\n")
		return err
	}

	if _, err := fmt.Fprintln(w, summaryLine(summarize(sorted))); err != nil {
		return err
	}

	for _, group := range groupedByKind(sorted) {
		if _, err := fmt.Fprintf(w, "\n%s — %s (%d)\n", group.Kind, group.Detector, len(group.Findings)); err != nil {
			return err
		}
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		if _, err := fmt.Fprintln(tw, "NAMESPACE\tNAME\tREASON\tCONFIDENCE\tCOST/MO\tSTATUS"); err != nil {
			return err
		}
		for _, f := range group.Findings {
			if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				formatNamespace(f.Namespace), f.Name, f.Reason, f.Confidence, formatCost(f.CostUSDPerMonth), f.Status,
			); err != nil {
				return err
			}
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	return nil
}
