# k8s-zombie — Kubernetes Orphan Cleaner Design

**Status:** Draft — pending user review
**Date:** 2026-09-04
**Author:** Ganesh Rajendran (with Claude)
**Type:** New open-source project (v1)

## 1. Problem Statement

Kubernetes clusters accumulate orphaned resources over time — unused namespaces,
LoadBalancer Services with zero live endpoints, unattached PVCs, PVs left behind after
their PVC is deleted, stale ConfigMaps/Secrets, dangling Ingresses, scaled-to-zero
Deployments, HPAs pointing at resources that no longer exist, and completed/failed
Jobs/Pods that never got cleaned up. Several of these keep billing (EBS volumes, load
balancer hours) long after they stopped doing anything useful. Nobody has a single,
narrow tool that (a) detects these deterministically, (b) estimates the dollar cost of
the waste, and (c) reports it safely for a human to act on.

## 2. Market Landscape

This isn't an empty niche — it overlaps two existing categories:

- **Orphan/unused-resource detection**: `kor` (Kubernetes Orphan Resources),
  `kube-janitor`, `popeye` already detect various flavors of "unused" Kubernetes
  objects.
- **Cost visibility**: Kubecost / OpenCost attach cost data to cluster resources, but
  answer "what am I spending," not "what can I safely delete."

**The gap this project targets:** nobody combines deterministic orphan detection +
AWS dollar-cost-of-waste estimation + a report built for the FinOps→SRE handoff, in one
narrow, safety-first tool. That combination — not orphan detection alone — is the
reason this is worth building rather than adopting `kor`.

## 3. Chosen Approach

A standalone Go CLI, `k8s-zombie`, that performs a single read-only scan of one
cluster context, runs a fixed set of deterministic detectors against the live API
state, attaches an AWS cost estimate where one applies, and renders a report. No
mutation of the cluster ever happens from this tool — it never deletes anything and
in v1 does not even generate delete commands.

Two things were deliberately kept out of v1 to keep the first release shippable:

- **No generated `kubectl delete` commands / no `--apply` execution.** The original
  pitch included dry-run cleanup commands; that's deferred to v2 once the detection
  side has real usage and a measured false-positive rate.
- **No grace-period / state tracking.** Knowing "orphaned for N consecutive days"
  requires either persisting state between runs or relying on `creationTimestamp` as
  an imprecise proxy. Both add real complexity (where does state live in CI?). v1
  reports everything orphaned *right now* and lets the user apply judgment; grace
  periods can be added in v2 once it's clear how much noise v1 actually produces.

## 4. Architecture

```
                    +-------------------+
kubeconfig/context->|   k8s-zombie CLI  |
                    +-------------------+
                            |
              +-------------+--------------+
              |                            |
      +-------v-------+           +--------v--------+
      | Detector       |           | Cost Estimator  |
      | Registry       |           | (AWS static     |
      | (client-go,    |           |  pricing table) |
      |  read-only)    |           +-----------------+
      +-------+--------+                    |
              |  []Finding                  |
              +--------------+--------------+
                             |
                    +--------v---------+
                    |    Renderer      |
                    | table / json /   |
                    |   markdown       |
                    +------------------+
```

- **Detector registry**: each orphan type is one `Detector` implementing
  `Name() string` and `Scan(ctx, clientset) ([]Finding, error)`. Detectors run
  concurrently; a failure or RBAC gap in one detector doesn't block the others.
  One detector per file — keeps each under ~200 lines and makes adding a new
  orphan type in v2 a single-file change.
- **Cost estimator**: a separate package holding a bundled, versioned YAML pricing
  table (EBS $/GB-month by volume type, NLB/ALB hourly + estimated LCU, idle EIP
  hourly). AWS-only for v1. Overridable via `--pricing-file` for non-standard
  pricing or regions. Detectors with no direct cost line (see §5) simply omit the
  cost field on their findings rather than forcing a fake estimate.
- **Renderer**: one `Renderer` interface with three implementations. Adding a
  fourth output format later doesn't touch detector code.
- **Statelessness**: no local state file, no database, no cross-run memory. Every
  invocation is a clean snapshot of one cluster context.

## 5. Detectors (v1)

| # | Detector | Signal | Cost estimate? |
|---|----------|--------|-----------------|
| 1 | Unused namespaces | No active Pods/Deployments/StatefulSets/CronJobs, excluding system namespaces | No |
| 2 | Zero-endpoint Services (incl. LoadBalancer) | 0 ready addresses in Endpoints/EndpointSlice | Yes (LB hourly/LCU) |
| 3 | Unattached PVCs | Not referenced by any Pod volume | Yes (EBS $/GB-mo) |
| 4 | Unused ConfigMaps/Secrets | Not referenced by any Pod env/volume/envFrom | No |
| 5 | Orphaned Ingresses | Backend Service missing or has 0 endpoints | No |
| 6 | Idle Deployments | 0 ready replicas, no owning HPA activity | No |
| 7 | Stale HPAs | Target Deployment/resource no longer exists | No |
| 8 | Orphaned PersistentVolumes | `status.phase` in `Released`/`Available` (Retain-policy leftovers still billing for the underlying EBS volume) | Yes (EBS $/GB-mo) |
| 9 | Completed/failed Jobs and Pods | `Job.status.succeeded/failed` with no active Pods; `Pod.status.phase` in `Succeeded`/`Failed` left behind | No (hygiene finding, not a cost finding) |

Detector #4 (ConfigMaps/Secrets) is explicitly lower-confidence — it can't see
references from Helm hooks or external controllers — and its findings are labeled
as such in the report rather than presented with the same confidence as, say, an
unattached PVC.

All detectors respect an opt-out label: `k8s-zombie.io/ignore: "true"`.

## 6. CLI Surface

```
k8s-zombie scan --context <ctx> [--namespace ...] [--exclude-namespace ...] \
                 [--output table|json|markdown] [--pricing-file <path>]
```

One run = one cluster context. No built-in multi-cluster loop — shell scripting
covers it (`for ctx in dev qa prod; do k8s-zombie scan --context $ctx; done`);
building that in is unnecessary complexity for v1.

## 7. Error Handling

- RBAC gap on a resource type (403 on list/get) → that detector's finding set
  reports a `skipped` warning row; the overall run still succeeds and reports
  everything else.
- Bad/missing kubeconfig context → fail fast, before any detector runs, with a
  clear error naming the bad context.
- The tool never writes to the cluster. There is no failure mode where a bug in
  this tool causes cluster state to change.

## 8. Testing

- Unit tests per detector against the `client-go` fake clientset — each detector
  gets fixtures for both a true-positive orphan and a true-negative healthy
  resource, so a broken detector fails a test rather than shipping false
  positives/negatives.
- Golden-file tests for each renderer (table/json/markdown) against a fixed
  finding set.
- One `envtest`/`kind`-based integration test in CI: create real orphaned and
  real healthy resources of each type, run a full scan, assert the report
  matches expectations end-to-end.

## 9. Explicitly Out of Scope (v1)

- Generated `kubectl delete` commands or any `--apply` execution path
- Grace-period / state tracking across runs
- In-cluster CronJob/controller mode (CLI-first; in-cluster mode is a planned v2
  addition using the same detector core)
- Multi-cloud pricing (GCP/Azure) — AWS only
- Multi-cluster orchestration built into the tool itself
- Cloud-API-backed detectors (e.g. correlating orphaned Elastic IPs from failed
  LB teardown) — every v1 detector reads only the Kubernetes API; mixing in direct
  AWS API calls for detection (as opposed to pricing lookups) is a v2 decision,
  not a v1 one
