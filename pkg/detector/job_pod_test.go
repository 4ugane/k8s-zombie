package detector_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/4ugane/k8s-zombie/pkg/detector"
	"github.com/4ugane/k8s-zombie/pkg/finding"
)

func jobWithStatus(name string, active, succeeded, failed int32) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Status: batchv1.JobStatus{
			Active:    active,
			Succeeded: succeeded,
			Failed:    failed,
		},
	}
}

func podWithPhase(name string, phase corev1.PodPhase) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Status:     corev1.PodStatus{Phase: phase},
	}
}

func findingsOfKind(findings []finding.Finding, kind string) []finding.Finding {
	var out []finding.Finding
	for _, f := range findings {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

func TestCompletedJobsAndPodsDetector_Name(t *testing.T) {
	d := detector.NewCompletedJobsAndPodsDetector()
	if d.Name() != "completed-jobs-and-pods" {
		t.Fatalf("want Name()=completed-jobs-and-pods, got %q", d.Name())
	}
}

func TestCompletedJobsAndPodsDetector_ActiveJobIsNotReported(t *testing.T) {
	job := jobWithStatus("active-job", 1, 0, 0)
	clientset := k8sfake.NewSimpleClientset(job)
	d := detector.NewCompletedJobsAndPodsDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findingsOfKind(findings, "Job")) != 0 {
		t.Fatalf("want 0 Job findings for an active Job, got %+v", findings)
	}
}

func TestCompletedJobsAndPodsDetector_SucceededJobIsReported(t *testing.T) {
	job := jobWithStatus("succeeded-job", 0, 1, 0)
	clientset := k8sfake.NewSimpleClientset(job)
	d := detector.NewCompletedJobsAndPodsDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	jobFindings := findingsOfKind(findings, "Job")
	if len(jobFindings) != 1 {
		t.Fatalf("want 1 Job finding, got %+v", findings)
	}
	f := jobFindings[0]
	if f.Namespace != "default" || f.Name != "succeeded-job" {
		t.Errorf("unexpected finding identity: %+v", f)
	}
	if f.Status != finding.StatusOrphaned || f.Confidence != finding.ConfidenceHigh {
		t.Errorf("want Status=Orphaned, Confidence=High, got %+v", f)
	}
}

func TestCompletedJobsAndPodsDetector_FailedJobIsReported(t *testing.T) {
	job := jobWithStatus("failed-job", 0, 0, 1)
	clientset := k8sfake.NewSimpleClientset(job)
	d := detector.NewCompletedJobsAndPodsDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findingsOfKind(findings, "Job")) != 1 {
		t.Fatalf("want 1 Job finding for a failed Job, got %+v", findings)
	}
}

func TestCompletedJobsAndPodsDetector_RunningPodIsNotReported(t *testing.T) {
	pod := podWithPhase("running-pod", corev1.PodRunning)
	clientset := k8sfake.NewSimpleClientset(pod)
	d := detector.NewCompletedJobsAndPodsDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findingsOfKind(findings, "Pod")) != 0 {
		t.Fatalf("want 0 Pod findings for a Running Pod, got %+v", findings)
	}
}

func TestCompletedJobsAndPodsDetector_SucceededPodIsReported(t *testing.T) {
	pod := podWithPhase("succeeded-pod", corev1.PodSucceeded)
	clientset := k8sfake.NewSimpleClientset(pod)
	d := detector.NewCompletedJobsAndPodsDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	podFindings := findingsOfKind(findings, "Pod")
	if len(podFindings) != 1 {
		t.Fatalf("want 1 Pod finding, got %+v", findings)
	}
	f := podFindings[0]
	if f.Namespace != "default" || f.Name != "succeeded-pod" {
		t.Errorf("unexpected finding identity: %+v", f)
	}
	if f.Reason != "terminal phase: Succeeded" {
		t.Errorf("want Reason=%q, got %q", "terminal phase: Succeeded", f.Reason)
	}
}

func TestCompletedJobsAndPodsDetector_FailedPodIsReported(t *testing.T) {
	pod := podWithPhase("failed-pod", corev1.PodFailed)
	clientset := k8sfake.NewSimpleClientset(pod)
	d := detector.NewCompletedJobsAndPodsDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findingsOfKind(findings, "Pod")) != 1 {
		t.Fatalf("want 1 Pod finding for a Failed Pod, got %+v", findings)
	}
}

func TestCompletedJobsAndPodsDetector_CompletedJobAndItsPodAreBothReportedWithoutDedup(t *testing.T) {
	job := jobWithStatus("batch-job", 0, 1, 0)
	pod := podWithPhase("batch-job-abcde", corev1.PodSucceeded)
	clientset := k8sfake.NewSimpleClientset(job, pod)
	d := detector.NewCompletedJobsAndPodsDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("want 2 findings (Job + Pod, no dedup), got %+v", findings)
	}
	if len(findingsOfKind(findings, "Job")) != 1 || len(findingsOfKind(findings, "Pod")) != 1 {
		t.Fatalf("want exactly one Job finding and one Pod finding, got %+v", findings)
	}
}

func TestCompletedJobsAndPodsDetector_JobsListErrorIsReturned(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset()
	clientset.PrependReactor("list", "jobs", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("simulated api error")
	})
	d := detector.NewCompletedJobsAndPodsDetector()

	_, err := d.Scan(context.Background(), clientset)
	if err == nil {
		t.Fatal("want an error when listing Jobs fails, got nil")
	}
}

func TestCompletedJobsAndPodsDetector_PodsListErrorIsReturned(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset()
	clientset.PrependReactor("list", "pods", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("simulated api error")
	})
	d := detector.NewCompletedJobsAndPodsDetector()

	_, err := d.Scan(context.Background(), clientset)
	if err == nil {
		t.Fatal("want an error when listing Pods fails, got nil")
	}
}

func TestCompletedJobsAndPodsDetector_JobReasonPrefersCompletionTimeOverCreationTimestamp(t *testing.T) {
	completedAt := metav1.NewTime(time.Now().Add(-2 * 24 * time.Hour))
	job := jobWithStatus("batch-job", 0, 1, 0)
	job.CreationTimestamp = metav1.NewTime(time.Now().Add(-30 * 24 * time.Hour))
	job.Status.CompletionTime = &completedAt

	clientset := k8sfake.NewSimpleClientset(job)
	d := detector.NewCompletedJobsAndPodsDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	jobFindings := findingsOfKind(findings, "Job")
	if len(jobFindings) != 1 {
		t.Fatalf("want 1 Job finding, got %+v", findings)
	}
	reason := jobFindings[0].Reason
	if !strings.Contains(reason, "completed 2d ago") {
		t.Errorf(`want Reason to prefer CompletionTime ("completed 2d ago"), got %q`, reason)
	}
	if strings.Contains(reason, "created") {
		t.Errorf("want Reason to NOT mention creation age when CompletionTime is set, got %q", reason)
	}
}

func TestCompletedJobsAndPodsDetector_JobReasonFallsBackToCreationTimestampWithoutCompletionTime(t *testing.T) {
	job := jobWithStatus("batch-job", 0, 1, 0)
	job.CreationTimestamp = metav1.NewTime(time.Now().Add(-5 * 24 * time.Hour))
	// Status.CompletionTime left nil.

	clientset := k8sfake.NewSimpleClientset(job)
	d := detector.NewCompletedJobsAndPodsDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	jobFindings := findingsOfKind(findings, "Job")
	if len(jobFindings) != 1 {
		t.Fatalf("want 1 Job finding, got %+v", findings)
	}
	if !strings.Contains(jobFindings[0].Reason, "created 5d ago") {
		t.Errorf(`want Reason to fall back to creation age ("created 5d ago"), got %q`, jobFindings[0].Reason)
	}
}

func TestCompletedJobsAndPodsDetector_IgnoreLabelExcludesJob(t *testing.T) {
	job := jobWithStatus("succeeded-job", 0, 1, 0)
	job.Labels = map[string]string{"k8s-zombie.io/ignore": "true"}

	clientset := k8sfake.NewSimpleClientset(job)
	d := detector.NewCompletedJobsAndPodsDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findingsOfKind(findings, "Job")) != 0 {
		t.Fatalf("want 0 Job findings for an ignore-labeled Job, got %+v", findings)
	}
}

func TestCompletedJobsAndPodsDetector_IgnoreLabelExcludesPod(t *testing.T) {
	pod := podWithPhase("succeeded-pod", corev1.PodSucceeded)
	pod.Labels = map[string]string{"k8s-zombie.io/ignore": "true"}

	clientset := k8sfake.NewSimpleClientset(pod)
	d := detector.NewCompletedJobsAndPodsDetector()

	findings, err := d.Scan(context.Background(), clientset)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(findingsOfKind(findings, "Pod")) != 0 {
		t.Fatalf("want 0 Pod findings for an ignore-labeled Pod, got %+v", findings)
	}
}
