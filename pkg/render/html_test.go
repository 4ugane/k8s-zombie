package render_test

import (
	"strings"
	"testing"

	"github.com/4ugane/k8s-zombie/pkg/finding"
	"github.com/4ugane/k8s-zombie/pkg/render"
)

func TestHTMLRenderer_SortsByKindThenNamespaceThenNameThenDetector(t *testing.T) {
	var buf strings.Builder
	if err := (render.HTMLRenderer{}).Render(&buf, fixture()); err != nil {
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

func TestHTMLRenderer_IsWellFormedHTMLDocument(t *testing.T) {
	var buf strings.Builder
	if err := (render.HTMLRenderer{}).Render(&buf, fixture()); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"<!DOCTYPE html>", "<html", "<head>", "<body>", "</html>", "<table"} {
		if !strings.Contains(out, want) {
			t.Errorf("want output to contain %q for a valid standalone HTML document, got:\n%s", want, out)
		}
	}
}

func TestHTMLRenderer_NilCostRendersAsDash(t *testing.T) {
	var buf strings.Builder
	findings := []finding.Finding{
		{Kind: "Service", Namespace: "web", Name: "orphan-svc", Detector: "d"},
	}
	if err := (render.HTMLRenderer{}).Render(&buf, findings); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, ">-<") {
		t.Errorf("want a dash cell for nil cost, got:\n%s", out)
	}
}

func TestHTMLRenderer_NonNilCostFormattedAsDollarsAndCents(t *testing.T) {
	var buf strings.Builder
	findings := []finding.Finding{
		{Kind: "PersistentVolume", Namespace: "data", Name: "pv-1", Detector: "d", CostUSDPerMonth: costPtr(12.5)},
	}
	if err := (render.HTMLRenderer{}).Render(&buf, findings); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "$12.50") {
		t.Errorf("want $12.50 in output, got:\n%s", out)
	}
}

func TestHTMLRenderer_EmptyFindingsRendersNoOrphansMessageInsideValidDocument(t *testing.T) {
	var buf strings.Builder
	if err := (render.HTMLRenderer{}).Render(&buf, []finding.Finding{}); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "No orphaned resources found.") {
		t.Errorf("want the no-orphans message, got:\n%s", out)
	}
	if !strings.Contains(out, "<!DOCTYPE html>") {
		t.Errorf("want a valid HTML document even for empty findings, got:\n%s", out)
	}
	if strings.Contains(out, "<table") {
		t.Errorf("want no table element when there are no findings, got:\n%s", out)
	}
}

func TestHTMLRenderer_EscapesUntrustedFieldContent(t *testing.T) {
	var buf strings.Builder
	findings := []finding.Finding{
		{
			Kind: "Service", Namespace: "web", Name: "orphan-svc", Detector: "d",
			Reason: `<script>alert("xss")</script>`,
		},
	}
	if err := (render.HTMLRenderer{}).Render(&buf, findings); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "<script>alert(") {
		t.Errorf("want Reason content HTML-escaped, got raw script tag in:\n%s", out)
	}
}

func TestHTMLRenderer_GroupsByKindWithHeaderAndCount(t *testing.T) {
	var buf strings.Builder
	if err := (render.HTMLRenderer{}).Render(&buf, fixture()); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"Deployment", "idle-deployment", "(1)",
		"PersistentVolume", "rbac-gap, unattached-pvc", "(2)",
		"Service", "unused-service",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q present in a group header, got:\n%s", want, out)
		}
	}
	idxDeployment := strings.Index(out, "Deployment")
	idxPV := strings.Index(out, "PersistentVolume")
	idxService := strings.Index(out, "Service")
	if !(idxDeployment < idxPV && idxPV < idxService) {
		t.Errorf("want group headers in Kind order, got:\n%s", out)
	}
}

func TestHTMLRenderer_RowsHaveNoDetectorColumn(t *testing.T) {
	var buf strings.Builder
	if err := (render.HTMLRenderer{}).Render(&buf, fixture()); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "<th>Detector</th>") {
		t.Errorf("want no Detector column header (Detector now lives in the group header), got:\n%s", out)
	}
}

func TestHTMLRenderer_StatTilesShowTotalAndCost(t *testing.T) {
	var buf strings.Builder
	if err := (render.HTMLRenderer{}).Render(&buf, fixture()); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "FINDINGS") {
		t.Errorf("want a findings-count stat tile, got:\n%s", out)
	}
	if !strings.Contains(out, "$15.50") {
		t.Errorf("want the combined cost (12.50 + 3.00) in a stat tile, got:\n%s", out)
	}
}

func TestHTMLRenderer_HasSearchFilterAndCostOnlyToggle(t *testing.T) {
	var buf strings.Builder
	if err := (render.HTMLRenderer{}).Render(&buf, fixture()); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `id="search"`) {
		t.Errorf("want a search filter input, got:\n%s", out)
	}
	if !strings.Contains(out, `id="cost-only"`) {
		t.Errorf("want a cost-only filter checkbox, got:\n%s", out)
	}
}

func TestHTMLRenderer_DoesNotMutateCallerSlice(t *testing.T) {
	findings := fixture()
	before := make([]finding.Finding, len(findings))
	copy(before, findings)

	var buf strings.Builder
	if err := (render.HTMLRenderer{}).Render(&buf, findings); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	for i := range findings {
		if findings[i] != before[i] {
			t.Fatalf("caller slice was mutated at index %d: before=%+v after=%+v", i, before[i], findings[i])
		}
	}
}
