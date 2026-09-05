package render

import (
	"encoding/json"
	"io"

	"github.com/4ugane/k8s-zombie/pkg/finding"
)

// JSONRenderer writes findings as an indented JSON array.
type JSONRenderer struct{}

func (JSONRenderer) Render(w io.Writer, findings []finding.Finding) error {
	sorted := sortedFindings(findings)
	// Ensure a non-nil slice so empty input marshals to "[]", not "null".
	out := make([]finding.Finding, 0, len(sorted))
	out = append(out, sorted...)

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}
