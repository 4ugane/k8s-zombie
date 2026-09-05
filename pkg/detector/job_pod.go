package detector

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/4ugane/k8s-zombie/pkg/finding"
)

// CompletedJobsAndPodsDetector reports Jobs with no active Pods left and Pods sitting
// in a terminal phase — hygiene findings, not cost findings, since these objects don't
// bill directly but do bloat etcd if left uncleaned (spec section 5, detector #9).
type CompletedJobsAndPodsDetector struct{}

// NewCompletedJobsAndPodsDetector builds the detector.
func NewCompletedJobsAndPodsDetector() *CompletedJobsAndPodsDetector {
	return &CompletedJobsAndPodsDetector{}
}

func (d *CompletedJobsAndPodsDetector) Name() string { return "completed-jobs-and-pods" }

func (d *CompletedJobsAndPodsDetector) Scan(ctx context.Context, clientset kubernetes.Interface) ([]finding.Finding, error) {
	jobs, err := clientset.BatchV1().Jobs(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing jobs: %w", err)
	}

	var findings []finding.Finding
	for _, job := range jobs.Items {
		if job.Status.Active == 0 && (job.Status.Succeeded > 0 || job.Status.Failed > 0) && !isIgnored(job.Labels) {
			// Prefer CompletionTime when the API sets it — "completed Nd ago" is more
			// meaningful than the Job's total age for a cleanup decision.
			reason := "completed with no active Pods"
			if job.Status.CompletionTime != nil {
				reason = withAge(reason, "completed", *job.Status.CompletionTime)
			} else {
				reason = withAge(reason, "created", job.CreationTimestamp)
			}
			findings = append(findings, finding.Finding{
				Detector:   d.Name(),
				Kind:       "Job",
				Namespace:  job.Namespace,
				Name:       job.Name,
				Reason:     reason,
				Confidence: finding.ConfidenceHigh,
				Status:     finding.StatusOrphaned,
			})
		}
	}

	pods, err := clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing pods: %w", err)
	}

	// ponytail: deliberately no de-dup between a completed Job and its own completed
	// Pod(s) here — both independently contribute to etcd bloat, so both get reported.
	// Correlating Job -> owned Pods (via OwnerReferences) to collapse them into one
	// finding is a real upgrade if the double-reporting proves noisy in practice.
	for _, pod := range pods.Items {
		if (pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed) && !isIgnored(pod.Labels) {
			findings = append(findings, finding.Finding{
				Detector:   d.Name(),
				Kind:       "Pod",
				Namespace:  pod.Namespace,
				Name:       pod.Name,
				Reason:     withAge(fmt.Sprintf("terminal phase: %s", pod.Status.Phase), "created", pod.CreationTimestamp),
				Confidence: finding.ConfidenceHigh,
				Status:     finding.StatusOrphaned,
			})
		}
	}

	return findings, nil
}
