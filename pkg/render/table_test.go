package render_test

import (
	"strings"
	"testing"

	"github.com/4ugane/k8s-zombie/pkg/finding"
	"github.com/4ugane/k8s-zombie/pkg/render"
)

func costPtr(v float64) *float64 { return &v }

// fixture returns findings deliberately out of Kind/Namespace/Name order so
// tests can prove the renderer sorts them.
func fixture() []finding.Finding {
	return []finding.Finding{
		{
			Detector:   "unused-service",
			Kind:       "Service",
			Namespace:  "web",
			Name:       "orphan-svc",
			Reason:     "no endpoints",
			Confidence: finding.ConfidenceMedium,
			Status:     finding.StatusOrphaned,
			// nil cost
		},
		{
			Detector:        "unattached-pvc",
			Kind:            "PersistentVolume",
			Namespace:       "data",
			Name:            "pv-1",
			Reason:          "unbound for 30d",
			Confidence:      finding.ConfidenceHigh,
			CostUSDPerMonth: costPtr(12.5),
			Status:          finding.StatusOrphaned,
		},
		{
			Detector:   "rbac-gap",
			Kind:       "PersistentVolume",
			Namespace:  "data",
			Name:       "pv-1",
			Reason:     "rbac forbidden",
			Confidence: finding.ConfidenceLow,
			Status:     finding.StatusSkipped,
			// nil cost, same Kind/Namespace/Name as above, different Detector
		},
		{
			Detector:        "idle-deployment",
			Kind:            "Deployment",
			Namespace:       "batch",
			Name:            "cron-runner",
			Reason:          "zero replicas for 90d",
			Confidence:      finding.ConfidenceHigh,
			CostUSDPerMonth: costPtr(3),
			Status:          finding.StatusOrphaned,
		},
	}
}

func TestTableRenderer_SortsByKindThenNamespaceThenNameThenDetector(t *testing.T) {
	var buf strings.Builder
	if err := (render.TableRenderer{}).Render(&buf, fixture()); err != nil {
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

	// Within the two PersistentVolume/pv-1 rows (same Kind/Namespace/Name),
	// Detector "rbac-gap" sorts before "unattached-pvc".
	idxRbacGap := strings.Index(out, "rbac-gap")
	idxUnattached := strings.Index(out, "unattached-pvc")
	if idxRbacGap == -1 || idxUnattached == -1 {
		t.Fatalf("expected both PV detectors present, got:\n%s", out)
	}
	if !(idxRbacGap < idxUnattached) {
		t.Errorf("want Detector tie-break rbac-gap before unattached-pvc, got:\n%s", out)
	}
}

func TestTableRenderer_NilCostRendersAsDash(t *testing.T) {
	var buf strings.Builder
	findings := []finding.Finding{
		{Kind: "Service", Namespace: "web", Name: "orphan-svc", Detector: "d"},
	}
	if err := (render.TableRenderer{}).Render(&buf, findings); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "-") {
		t.Errorf("want dash for nil cost, got:\n%s", out)
	}
}

func TestTableRenderer_NonNilCostFormattedAsDollarsAndCents(t *testing.T) {
	var buf strings.Builder
	findings := []finding.Finding{
		{Kind: "PersistentVolume", Namespace: "data", Name: "pv-1", Detector: "d", CostUSDPerMonth: costPtr(12.5)},
	}
	if err := (render.TableRenderer{}).Render(&buf, findings); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "$12.50") {
		t.Errorf("want $12.50 in output, got:\n%s", out)
	}
}

func TestTableRenderer_EmptyFindingsRendersNoOrphansMessage(t *testing.T) {
	var buf strings.Builder
	if err := (render.TableRenderer{}).Render(&buf, []finding.Finding{}); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	want := "No orphaned resources found.\n"
	if buf.String() != want {
		t.Errorf("want %q, got %q", want, buf.String())
	}
}

func TestTableRenderer_GroupsByKindWithHeaderAndCount(t *testing.T) {
	var buf strings.Builder
	if err := (render.TableRenderer{}).Render(&buf, fixture()); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()

	// The Detector name moves into the group header (it's redundant per-row —
	// every Kind maps to exactly one Detector), joined if a group ever mixes
	// detectors (as the PersistentVolume group in this fixture deliberately does).
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

func TestTableRenderer_RowsHaveNoDetectorColumn(t *testing.T) {
	var buf strings.Builder
	if err := (render.TableRenderer{}).Render(&buf, fixture()); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "DETECTOR") {
		t.Errorf("want no DETECTOR column header (Detector now lives in the group header), got:\n%s", out)
	}
}

func TestTableRenderer_ClusterScopedNamespaceRendersAsDash(t *testing.T) {
	var buf strings.Builder
	findings := []finding.Finding{
		{Kind: "Namespace", Namespace: "", Name: "empty-ns", Detector: "unused-namespace"},
	}
	if err := (render.TableRenderer{}).Render(&buf, findings); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "empty-ns") {
		t.Fatalf("want the finding present, got:\n%s", out)
	}
}

func TestTableRenderer_SummaryLineShowsTotalAndCost(t *testing.T) {
	var buf strings.Builder
	if err := (render.TableRenderer{}).Render(&buf, fixture()); err != nil {
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

func TestTableRenderer_DoesNotMutateCallerSlice(t *testing.T) {
	findings := fixture()
	before := make([]finding.Finding, len(findings))
	copy(before, findings)

	var buf strings.Builder
	if err := (render.TableRenderer{}).Render(&buf, findings); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	for i := range findings {
		if findings[i] != before[i] {
			t.Fatalf("caller slice was mutated at index %d: before=%+v after=%+v", i, before[i], findings[i])
		}
	}
}
