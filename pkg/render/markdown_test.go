package render_test

import (
	"strings"
	"testing"

	"github.com/4ugane/k8s-zombie/pkg/finding"
	"github.com/4ugane/k8s-zombie/pkg/render"
)

func TestMarkdownRenderer_SortsByKindThenNamespaceThenNameThenDetector(t *testing.T) {
	var buf strings.Builder
	if err := (render.MarkdownRenderer{}).Render(&buf, fixture()); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()

	idxDeployment := strings.Index(out, "cron-runner")
	idxPV := strings.Index(out, "pv-1")
	idxService := strings.Index(out, "orphan-svc")

	if idxDeployment == -1 || idxPV == -1 || idxService == -1 {
		t.Fatalf("expected all rows present, got:\n%s", out)
	}
	if !(idxDeployment < idxPV && idxPV < idxService) {
		t.Errorf("want order Deployment < PersistentVolume < Service, got:\n%s", out)
	}
}

func TestMarkdownRenderer_HasHeaderAndSeparatorRowPerGroup(t *testing.T) {
	var buf strings.Builder
	if err := (render.MarkdownRenderer{}).Render(&buf, fixture()); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()
	for _, col := range []string{"NAMESPACE", "NAME", "REASON", "CONFIDENCE", "COST/MO", "STATUS"} {
		if !strings.Contains(out, col) {
			t.Errorf("want a table header to contain %q, got:\n%s", col, out)
		}
	}
	if strings.Contains(out, "DETECTOR") {
		t.Errorf("want no DETECTOR column header (Detector now lives in the group header), got:\n%s", out)
	}
	if !strings.Contains(out, "|---") {
		t.Errorf("want a separator row of dashes, got:\n%s", out)
	}
}

func TestMarkdownRenderer_GroupsByKindWithHeaderAndCount(t *testing.T) {
	var buf strings.Builder
	if err := (render.MarkdownRenderer{}).Render(&buf, fixture()); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"Deployment — idle-deployment (1)",
		"PersistentVolume — rbac-gap, unattached-pvc (2)",
		"Service — unused-service (1)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want a %q group header, got:\n%s", want, out)
		}
	}
	idxDeployment := strings.Index(out, "Deployment — idle-deployment (1)")
	idxPV := strings.Index(out, "PersistentVolume — rbac-gap, unattached-pvc (2)")
	idxService := strings.Index(out, "Service — unused-service (1)")
	if !(idxDeployment < idxPV && idxPV < idxService) {
		t.Errorf("want group headers in Kind order, got:\n%s", out)
	}
}

func TestMarkdownRenderer_SummaryLineShowsTotalAndCost(t *testing.T) {
	var buf strings.Builder
	if err := (render.MarkdownRenderer{}).Render(&buf, fixture()); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "4 orphaned resource") {
		t.Errorf("want a total-count summary line, got:\n%s", out)
	}
	if !strings.Contains(out, "$15.50") {
		t.Errorf("want the combined cost (12.50 + 3.00) in the summary line, got:\n%s", out)
	}
}

func TestMarkdownRenderer_NilCostRendersAsDash(t *testing.T) {
	var buf strings.Builder
	findings := []finding.Finding{
		{Kind: "Service", Namespace: "web", Name: "orphan-svc", Detector: "d"},
	}
	if err := (render.MarkdownRenderer{}).Render(&buf, findings); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "| - |") && !strings.Contains(out, "|-|") {
		t.Errorf("want dash for nil cost, got:\n%s", out)
	}
}

func TestMarkdownRenderer_NonNilCostFormattedAsDollarsAndCents(t *testing.T) {
	var buf strings.Builder
	findings := []finding.Finding{
		{Kind: "PersistentVolume", Namespace: "data", Name: "pv-1", Detector: "d", CostUSDPerMonth: costPtr(12.5)},
	}
	if err := (render.MarkdownRenderer{}).Render(&buf, findings); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "$12.50") {
		t.Errorf("want $12.50 in output, got:\n%s", out)
	}
}

func TestMarkdownRenderer_EmptyFindingsRendersNoOrphansMessage(t *testing.T) {
	var buf strings.Builder
	if err := (render.MarkdownRenderer{}).Render(&buf, []finding.Finding{}); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	want := "No orphaned resources found.\n"
	if buf.String() != want {
		t.Errorf("want %q, got %q", want, buf.String())
	}
}

func TestMarkdownRenderer_DoesNotMutateCallerSlice(t *testing.T) {
	findings := fixture()
	before := make([]finding.Finding, len(findings))
	copy(before, findings)

	var buf strings.Builder
	if err := (render.MarkdownRenderer{}).Render(&buf, findings); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	for i := range findings {
		if findings[i] != before[i] {
			t.Fatalf("caller slice was mutated at index %d: before=%+v after=%+v", i, before[i], findings[i])
		}
	}
}
