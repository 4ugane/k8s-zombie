package render_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/4ugane/k8s-zombie/pkg/finding"
	"github.com/4ugane/k8s-zombie/pkg/render"
)

func TestJSONRenderer_RoundTripsAllFieldsIncludingNilAndNonNilCost(t *testing.T) {
	in := fixture()

	var buf strings.Builder
	if err := (render.JSONRenderer{}).Render(&buf, in); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	var out []finding.Finding
	if err := json.Unmarshal([]byte(buf.String()), &out); err != nil {
		t.Fatalf("failed to unmarshal rendered JSON: %v\noutput:\n%s", err, buf.String())
	}
	if len(out) != len(in) {
		t.Fatalf("want %d findings round-tripped, got %d", len(in), len(out))
	}

	// Build a lookup by Name+Detector since order is sorted, not identical to input order.
	byKey := make(map[string]finding.Finding, len(out))
	for _, f := range out {
		byKey[f.Detector+"|"+f.Name] = f
	}
	for _, want := range in {
		got, ok := byKey[want.Detector+"|"+want.Name]
		if !ok {
			t.Fatalf("finding for detector=%q name=%q missing from round-trip output", want.Detector, want.Name)
		}
		if got.Kind != want.Kind || got.Namespace != want.Namespace || got.Reason != want.Reason ||
			got.Confidence != want.Confidence || got.Status != want.Status {
			t.Errorf("round-tripped finding mismatch: want %+v, got %+v", want, got)
		}
		switch {
		case want.CostUSDPerMonth == nil && got.CostUSDPerMonth != nil:
			t.Errorf("want nil cost for %q, got %v", want.Name, *got.CostUSDPerMonth)
		case want.CostUSDPerMonth != nil && got.CostUSDPerMonth == nil:
			t.Errorf("want non-nil cost for %q, got nil", want.Name)
		case want.CostUSDPerMonth != nil && got.CostUSDPerMonth != nil && *want.CostUSDPerMonth != *got.CostUSDPerMonth:
			t.Errorf("want cost %v for %q, got %v", *want.CostUSDPerMonth, want.Name, *got.CostUSDPerMonth)
		}
	}
}

func TestJSONRenderer_NilCostOmitsKeyFromOutput(t *testing.T) {
	findings := []finding.Finding{
		{Kind: "Service", Namespace: "web", Name: "orphan-svc", Detector: "d"},
	}
	var buf strings.Builder
	if err := (render.JSONRenderer{}).Render(&buf, findings); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if strings.Contains(buf.String(), "cost_usd_per_month") {
		t.Errorf("want cost_usd_per_month key omitted for nil cost, got:\n%s", buf.String())
	}
}

func TestJSONRenderer_NonNilCostRendersRawFloat(t *testing.T) {
	findings := []finding.Finding{
		{Kind: "PersistentVolume", Namespace: "data", Name: "pv-1", Detector: "d", CostUSDPerMonth: costPtr(12.5)},
	}
	var buf strings.Builder
	if err := (render.JSONRenderer{}).Render(&buf, findings); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	var out []finding.Finding
	if err := json.Unmarshal([]byte(buf.String()), &out); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(out) != 1 || out[0].CostUSDPerMonth == nil || *out[0].CostUSDPerMonth != 12.5 {
		t.Errorf("want cost 12.5, got %+v", out)
	}
}

func TestJSONRenderer_EmptyFindingsRendersEmptyJSONArray(t *testing.T) {
	var buf strings.Builder
	if err := (render.JSONRenderer{}).Render(&buf, []finding.Finding{}); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	var out []finding.Finding
	if err := json.Unmarshal([]byte(buf.String()), &out); err != nil {
		t.Fatalf("failed to unmarshal: %v\noutput:\n%s", err, buf.String())
	}
	if out == nil {
		t.Error("want non-nil empty slice after unmarshal, got nil")
	}
	if len(out) != 0 {
		t.Errorf("want 0 findings, got %d", len(out))
	}
}

func TestJSONRenderer_NilFindingsSliceRendersEmptyJSONArray(t *testing.T) {
	var buf strings.Builder
	if err := (render.JSONRenderer{}).Render(&buf, nil); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	var out []finding.Finding
	if err := json.Unmarshal([]byte(buf.String()), &out); err != nil {
		t.Fatalf("failed to unmarshal: %v\noutput:\n%s", err, buf.String())
	}
	if len(out) != 0 {
		t.Errorf("want 0 findings, got %d", len(out))
	}
}

func TestJSONRenderer_DoesNotMutateCallerSlice(t *testing.T) {
	findings := fixture()
	before := make([]finding.Finding, len(findings))
	copy(before, findings)

	var buf strings.Builder
	if err := (render.JSONRenderer{}).Render(&buf, findings); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	for i := range findings {
		if findings[i] != before[i] {
			t.Fatalf("caller slice was mutated at index %d: before=%+v after=%+v", i, before[i], findings[i])
		}
	}
}
