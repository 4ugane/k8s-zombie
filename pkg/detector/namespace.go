package detector

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/4ugane/k8s-zombie/pkg/finding"
)

// excludedNamespaces are cluster-critical namespaces that must never be reported as
// unused, regardless of whether they currently run any workloads. User-configurable
// additional excludes are wired in at the CLI layer, not here.
var excludedNamespaces = map[string]bool{
	"kube-system":     true,
	"kube-public":     true,
	"kube-node-lease": true,
	"default":         true,
}

// UnusedNamespaceDetector reports Namespaces that run no active Pods, Deployments,
// StatefulSets, or CronJobs (spec section 5, detector #1).
type UnusedNamespaceDetector struct{}

// NewUnusedNamespaceDetector builds the detector. It has no cost estimation, unlike
// PVC/PV/Service detectors: there's no per-namespace cost line to attach.
func NewUnusedNamespaceDetector() *UnusedNamespaceDetector {
	return &UnusedNamespaceDetector{}
}

func (d *UnusedNamespaceDetector) Name() string { return "unused-namespace" }

func (d *UnusedNamespaceDetector) Scan(ctx context.Context, clientset kubernetes.Interface) ([]finding.Finding, error) {
	namespaces, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing namespaces: %w", err)
	}

	pods, err := clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing pods: %w", err)
	}
	deployments, err := clientset.AppsV1().Deployments(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing deployments: %w", err)
	}
	statefulSets, err := clientset.AppsV1().StatefulSets(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing statefulsets: %w", err)
	}
	cronJobs, err := clientset.BatchV1().CronJobs(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing cronjobs: %w", err)
	}

	// hasWorkloads buckets every namespace with at least one Pod, Deployment,
	// StatefulSet, or CronJob by namespace name, so membership in each namespace
	// is a single cluster-wide list per resource kind rather than a List per
	// namespace (mirrors storageClassVolumeTypes in ebs.go).
	hasWorkloads := make(map[string]bool)
	for _, p := range pods.Items {
		hasWorkloads[p.Namespace] = true
	}
	for _, d := range deployments.Items {
		hasWorkloads[d.Namespace] = true
	}
	for _, s := range statefulSets.Items {
		hasWorkloads[s.Namespace] = true
	}
	for _, c := range cronJobs.Items {
		hasWorkloads[c.Namespace] = true
	}

	var findings []finding.Finding
	for _, ns := range namespaces.Items {
		if excludedNamespaces[ns.Name] {
			continue
		}

		if hasWorkloads[ns.Name] {
			continue
		}
		if isIgnored(ns.Labels) {
			continue
		}

		findings = append(findings, finding.Finding{
			Detector:   d.Name(),
			Kind:       "Namespace",
			Namespace:  "", // cluster-scoped resource
			Name:       ns.Name,
			Reason:     withAge("no active Pods/Deployments/StatefulSets/CronJobs", "created", ns.CreationTimestamp),
			Confidence: finding.ConfidenceHigh,
			Status:     finding.StatusOrphaned,
		})
	}
	return findings, nil
}
