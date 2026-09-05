package detector_test

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/4ugane/k8s-zombie/pkg/detector"
	"github.com/4ugane/k8s-zombie/pkg/finding"
)

// fakeDetector lets each test control exactly what a detector returns.
type fakeDetector struct {
	name     string
	findings []finding.Finding
	err      error
	panics   bool
}

func (f fakeDetector) Name() string { return f.name }

func (f fakeDetector) Scan(_ context.Context, _ kubernetes.Interface) ([]finding.Finding, error) {
	if f.panics {
		panic("boom: simulated detector bug")
	}
	return f.findings, f.err
}

func TestRunAll_AggregatesFindingsFromMultipleDetectors(t *testing.T) {
	d1 := fakeDetector{name: "det-a", findings: []finding.Finding{{Detector: "det-a", Name: "r1"}}}
	d2 := fakeDetector{name: "det-b", findings: []finding.Finding{{Detector: "det-b", Name: "r2"}}}

	reg := detector.NewRegistry(d1, d2)
	got := reg.RunAll(context.Background(), k8sfake.NewSimpleClientset())

	if len(got) != 2 {
		t.Fatalf("want 2 findings, got %d: %+v", len(got), got)
	}
}

func TestRunAll_RBACForbiddenErrorBecomesSkippedFinding(t *testing.T) {
	forbiddenErr := apierrors.NewForbidden(
		schema.GroupResource{Group: "", Resource: "persistentvolumeclaims"},
		"", errors.New("user cannot list resource"),
	)
	d := fakeDetector{name: "unattached-pvc", err: forbiddenErr}

	reg := detector.NewRegistry(d)
	got := reg.RunAll(context.Background(), k8sfake.NewSimpleClientset())

	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d: %+v", len(got), got)
	}
	if got[0].Status != finding.StatusSkipped {
		t.Errorf("want Status=%q, got %q", finding.StatusSkipped, got[0].Status)
	}
	if got[0].Detector != "unattached-pvc" {
		t.Errorf("want Detector=%q, got %q", "unattached-pvc", got[0].Detector)
	}
}

func TestRunAll_OneDetectorFailingDoesNotBlockOthers(t *testing.T) {
	ok := fakeDetector{name: "det-ok", findings: []finding.Finding{{Detector: "det-ok", Name: "r1"}}}
	broken := fakeDetector{name: "det-broken", err: errors.New("boom")}

	reg := detector.NewRegistry(ok, broken)
	got := reg.RunAll(context.Background(), k8sfake.NewSimpleClientset())

	if len(got) != 2 {
		t.Fatalf("want 2 findings (1 real + 1 skipped), got %d: %+v", len(got), got)
	}
}

func TestRunAll_PanicInDetectorBecomesSkippedFindingInsteadOfCrashing(t *testing.T) {
	ok := fakeDetector{name: "det-ok", findings: []finding.Finding{{Detector: "det-ok", Name: "r1"}}}
	broken := fakeDetector{name: "det-panics", panics: true}

	reg := detector.NewRegistry(ok, broken)
	got := reg.RunAll(context.Background(), k8sfake.NewSimpleClientset())

	if len(got) != 2 {
		t.Fatalf("want 2 findings (1 real + 1 skipped), got %d: %+v", len(got), got)
	}
	var skipped *finding.Finding
	for i := range got {
		if got[i].Detector == "det-panics" {
			skipped = &got[i]
		}
	}
	if skipped == nil {
		t.Fatal("want a finding for the panicking detector, found none")
	}
	if skipped.Status != finding.StatusSkipped {
		t.Errorf("want Status=%q for panicking detector, got %q", finding.StatusSkipped, skipped.Status)
	}
}
