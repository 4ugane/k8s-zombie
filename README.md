# k8s-zombie

[![CI](https://github.com/4ugane/k8s-zombie/actions/workflows/ci.yml/badge.svg)](https://github.com/4ugane/k8s-zombie/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/4ugane/k8s-zombie?include_prereleases&sort=semver)](https://github.com/4ugane/k8s-zombie/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/4ugane/k8s-zombie.svg)](https://pkg.go.dev/github.com/4ugane/k8s-zombie)
[![Go Report Card](https://goreportcard.com/badge/github.com/4ugane/k8s-zombie)](https://goreportcard.com/report/github.com/4ugane/k8s-zombie)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Find the Kubernetes resources nobody remembers, and see exactly what they're costing you.**

k8s-zombie is a read-only CLI that scans a Kubernetes cluster for orphaned
resources — the LoadBalancer nobody tore down, the PVC left behind after its
Pod was deleted, the namespace a demo forgot about eight months ago — and
reports them with a dollar-cost estimate where one applies. It never deletes
anything. It never modifies anything. It only looks and reports.

```
$ k8s-zombie scan
DETECTOR               KIND                   NAMESPACE  NAME                REASON                                                                                         CONFIDENCE  COST/MO  STATUS
idle-deployment        Deployment             checkout   legacy-worker       0 ready replicas, not managed by any HorizontalPodAutoscaler                                   High        -        Orphaned
unused-namespace       Namespace                         demo-2024-q3        no active Pods/Deployments/StatefulSets/CronJobs                                               High        -        Orphaned
orphaned-pv            PersistentVolume                  pvc-a1b2c3d4        PersistentVolume is Released (Retain-policy leftover still billing for the underlying volume)  High        $40.00   Orphaned
unattached-pvc         PersistentVolumeClaim  payments   old-migration-data  not referenced by any Pod volume                                                               High        $16.00   Orphaned
zero-endpoint-service  Service                checkout   checkout-lb         0 ready endpoints                                                                              High        $16.43   Orphaned
```

## Why this exists

Two categories of tool already partly solve this problem:

- **Orphan detectors** ([kor](https://github.com/yonahd/kor), `kube-janitor`,
  `popeye`) find unused Kubernetes objects, but don't tell you what they cost.
- **Cost tools** (Kubecost, OpenCost) tell you what you're spending, but don't
  tell you what's safe to delete.

k8s-zombie sits in the gap: deterministic orphan detection **plus** a dollar
figure, in one report built for the moment a FinOps or SRE conversation
actually needs it — "here's $340/month of orphaned volumes, here's exactly
which ones." Nothing here is a guess dressed up as a fact: every finding
traces back to a concrete, checkable condition on a specific Kubernetes API
object.

## What it checks

| Detector | What it flags | Cost estimate |
|---|---|---|
| `unattached-pvc` | PersistentVolumeClaims not referenced by any Pod volume | ✅ EBS $/GB-month |
| `orphaned-pv` | PersistentVolumes stuck `Released`/`Available` after their claim is gone (still billing) | ✅ EBS $/GB-month |
| `zero-endpoint-service` | Services (any type except `ExternalName`) with no ready endpoints | ✅ LoadBalancer-type only |
| `unused-namespace` | Namespaces with no Pods, Deployments, StatefulSets, or CronJobs | — |
| `unused-configmap-secret` | ConfigMaps/Secrets not referenced by any Pod's env, volume, projected volume, or image pull secrets | — |
| `orphaned-ingress` | Ingresses whose backend Services are *all* missing or dead | — |
| `idle-deployment` | Deployments at 0 ready replicas that aren't managed by an HPA | — |
| `stale-hpa` | HorizontalPodAutoscalers targeting a Deployment/StatefulSet that no longer exists | — |
| `completed-jobs-and-pods` | Jobs with no active Pods left, and Pods sitting in a terminal phase | — |

Every detector is deterministic — no heuristics, no ML, no "probably." Each
finding names the exact condition that triggered it (`Reason`), how confident
the detector is (`Confidence`), and whether it's an actual orphan or just a
detector that couldn't finish (`Status: Skipped` — see [Safety](#safety)
below).

### Opting a resource out

Label anything with `k8s-zombie.io/ignore: "true"` and every detector will
skip it, no matter how orphaned it looks.

## Installation

**Download a prebuilt binary** (no Go toolchain needed) from the
[latest release](https://github.com/4ugane/k8s-zombie/releases/latest) —
archives are published for macOS, Linux, and Windows on both amd64 and arm64.

```sh
# macOS (Apple Silicon), adjust os/arch for your platform
curl -sL https://github.com/4ugane/k8s-zombie/releases/latest/download/k8s-zombie_darwin_arm64.tar.gz \
  | tar xz k8s-zombie
sudo mv k8s-zombie /usr/local/bin/
```

**Or, with Go installed** (requires Go 1.27+):

```sh
go install github.com/4ugane/k8s-zombie/cmd/k8s-zombie@latest
```

**Or build from source:**

```sh
git clone https://github.com/4ugane/k8s-zombie.git
cd k8s-zombie
go build -o k8s-zombie ./cmd/k8s-zombie
```

A Homebrew tap and a `krew` plugin manifest (`kubectl krew install zombie`)
are on the roadmap — see [Project status](#project-status).

## Usage

```
k8s-zombie scan [flags]
```

| Flag | Default | What it does |
|---|---|---|
| `--context` | current kubeconfig context | Which cluster context to scan, same semantics as `kubectl --context` |
| `--namespace` | (all namespaces) | Only report findings in this namespace (cluster-scoped findings, like `unused-namespace`, are always shown regardless) |
| `--exclude-namespace` | (none) | Namespace to drop from the report — repeatable, e.g. `--exclude-namespace kube-system --exclude-namespace argocd` |
| `--output` | `table` | `table`, `json`, or `markdown` |
| `--pricing-file` | bundled AWS pricing | Path to a YAML file overriding the built-in cost table (see [Cost estimates](#how-cost-estimates-work)) |

```sh
# Default: scan the current context, human-readable table
k8s-zombie scan

# A specific cluster, JSON for scripting/CI
k8s-zombie scan --context prod-cluster --output json | jq '.[] | select(.cost_usd_per_month != null)'

# Skip infra namespaces, paste the result into a Slack message or PR comment
k8s-zombie scan --exclude-namespace kube-system --exclude-namespace argocd --output markdown
```

Markdown output looks like this:

```
| DETECTOR | KIND | NAMESPACE | NAME | REASON | CONFIDENCE | COST/MO | STATUS |
|---|---|---|---|---|---|---|---|
| idle-deployment | Deployment | checkout | legacy-worker | 0 ready replicas, not managed by any HorizontalPodAutoscaler | High | - | Orphaned |
| unused-namespace | Namespace |  | demo-2024-q3 | no active Pods/Deployments/StatefulSets/CronJobs | High | - | Orphaned |
| orphaned-pv | PersistentVolume |  | pvc-a1b2c3d4 | PersistentVolume is Released (Retain-policy leftover still billing for the underlying volume) | High | $40.00 | Orphaned |
```

## How cost estimates work

Cost estimates are **directional**, not billing-grade — good enough to tell
you "$340/month" is worth investigating, not good enough to reconcile against
an AWS invoice. Two approximations, both documented rather than hidden:

- **Single region.** The bundled pricing table assumes one region
  (us-east-1). Use `--pricing-file` to supply your own rates for your actual
  region.
- **EBS volume type fallback.** Cost lookups resolve a PVC/PV's EBS volume
  type from its StorageClass; if that lookup fails, the estimate falls back
  to a default type (`gp3`) rather than silently reporting `$0.00`.

The full reasoning is in [`docs/adr/0004-aws-only-static-pricing-table.md`](docs/adr/0004-aws-only-static-pricing-table.md)
and [`docs/adr/0007-v1-implementation-details.md`](docs/adr/0007-v1-implementation-details.md).

## Safety

k8s-zombie is designed to be trivially safe to run against production:

- **Every API call is `List` or `Get`.** There is no `Create`, `Update`,
  `Patch`, or `Delete` anywhere in the codebase — verified by both an
  internal code review and an independent security review.
- **No generated delete commands, no `--apply` flag.** v1 only reports. See
  [`docs/adr/0005-no-delete-commands-in-v1.md`](docs/adr/0005-no-delete-commands-in-v1.md)
  for why.
- **A misbehaving detector can't take down the scan.** If a detector hits an
  RBAC gap, any other error, or even panics, it degrades to a single
  `Status: Skipped` finding — the rest of the report still comes back intact.
- **Secrets stay secret.** The `unused-configmap-secret` detector reads
  Secret *metadata* (name, namespace, type) to decide whether it's
  referenced — it never reads or surfaces Secret `data`/`stringData`.

## Project status

**Working and packaged.** The detection engine, cost estimation, all four
output formats, and the CLI itself are built and tested (98+ tests across
the whole repo, `-race`-clean, 86–100% coverage per package), verified
against a real cluster, and released as prebuilt binaries via `goreleaser`
on every tagged version. What's still ahead:

- An `envtest`/`kind`-based integration test exercising a full scan
  end-to-end against a real (if ephemeral) API server, wired into CI
- A Homebrew tap (`brew install 4ugane/tap/k8s-zombie`)
- A `krew` plugin manifest (`kubectl krew install zombie`)

See [`docs/architecture.md`](docs/architecture.md) and [`docs/adr/`](docs/adr/)
for the full design history and every non-obvious decision behind this
project, and [`docs/superpowers/specs/`](docs/superpowers/specs/) for the
original design spec.

## Development

```sh
go build ./...
go vet ./...
go test ./... -race -cover
gofmt -l .
```

Every detector was built test-first (RED confirmed against the unmodified
code before each fix or feature landed) — new detectors or fixes are expected
to follow the same discipline. `pkg/detector/*_test.go` is the best reference
for the project's test style.

## License

[MIT](LICENSE)
