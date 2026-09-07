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
| `unattached-pvc` | PersistentVolumeClaims not referenced by any Pod volume (PVCs a StatefulSet is retaining past a scale-down for a future scale-up are excluded) | ✅ EBS $/GB-month |
| `orphaned-pv` | PersistentVolumes stuck `Released`/`Available` after their claim is gone (still billing) | ✅ EBS $/GB-month |
| `zero-endpoint-service` | Services (any type except `ExternalName`) with no ready endpoints | ✅ LoadBalancer-type only |
| `unused-namespace` | Namespaces with no Pods, Deployments, StatefulSets, or CronJobs | — |
| `unused-configmap-secret` | ConfigMaps/Secrets not referenced by any Pod's env, volume, projected volume, or image pull secrets (Helm hook resources — `helm.sh/hook` annotation — are excluded) | — |
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
url="$(curl -s https://api.github.com/repos/4ugane/k8s-zombie/releases/latest \
  | grep -o 'https://[^"]*k8s-zombie_[^"]*_darwin_arm64\.tar\.gz')"
curl -sL "$url" | tar xz k8s-zombie
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
go build -o bin/k8s-zombie ./cmd/k8s-zombie
```

**Or via Homebrew** (this is a custom tap — it won't show up on brew.sh or
in a plain `brew search`; installing it directly, or tapping it first,
both work):

```sh
brew install 4ugane/tap/k8s-zombie

# equivalently:
brew tap 4ugane/tap
brew install k8s-zombie
```

A `krew` plugin manifest is generated on every release too — see
[Project status](#project-status) for what's left before
`kubectl krew install zombie` works directly.

## Usage

```
k8s-zombie scan [flags]
```

| Flag | Default | What it does |
|---|---|---|
| `--context` | current kubeconfig context | Which cluster context to scan, same semantics as `kubectl --context` |
| `--namespace` | (all namespaces) | Only report findings in this namespace (cluster-scoped findings, like `unused-namespace`, are always shown regardless) |
| `--exclude-namespace` | (none) | Namespace to drop from the report — repeatable, e.g. `--exclude-namespace kube-system --exclude-namespace argocd` |
| `--output` | `table` | `table`, `json`, `markdown`, or `html` |
| `--pricing-file` | bundled AWS pricing | Path to a YAML file overriding the built-in cost table (see [Cost estimates](#how-cost-estimates-work)) |
| `--min-age` | disabled | Ignore resources created more recently than this (e.g. `1h`, `30m`) — a grace period to avoid false positives on a resource still initializing mid-rollout. Disabled (`0`) by default; opt in for CI/CD pipelines where scans can run seconds after a deploy |

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

## GitHub Action

Run k8s-zombie as a step in your own workflow — it installs the matching
release binary (checksum-verified against `checksums.txt`) and runs a scan,
with no Go toolchain or manual download needed:

```yaml
- uses: 4ugane/k8s-zombie@v0.1.5
  with:
    output: markdown
    exclude-namespace: kube-system,cert-manager
    fail-on-findings: "true"   # fail this step (and the PR check) on any finding
```

The action assumes kubectl/kubeconfig access to the target cluster is already
set up earlier in the job (e.g. via `aws eks update-kubeconfig`, `az aks
get-credentials`, or a self-hosted runner) — same as running the CLI
directly.

| Input | Default | What it does |
|---|---|---|
| `version` | `latest` | Which k8s-zombie release to install |
| `context` | current kubeconfig context | Same as `--context` |
| `namespace` | (all namespaces) | Same as `--namespace` |
| `exclude-namespace` | (none) | Comma-separated, e.g. `kube-system,cert-manager` |
| `output` | `table` | Format written to the `report-path` output |
| `pricing-file` | bundled AWS pricing | Same as `--pricing-file` |
| `min-age` | disabled | Same as `--min-age` |
| `fail-on-findings` | `false` | Fail the step (and the PR check) if any orphaned resource is found |

**Outputs:** `report-path` (file path to the generated report) and
`findings-count` (number of orphaned findings), for a later step to read,
attach as a PR comment, or upload as a workflow artifact.

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
output formats, and the CLI itself are built and tested (160+ tests across
the whole repo, `-race`-clean, 89–100% coverage per package), verified
against a real cluster, and released as prebuilt binaries via `goreleaser`
on every tagged version. Distribution is live on two channels, plus a
[GitHub Action](#github-action) for running scans directly in CI/PR
pipelines:

- **Homebrew** — `brew install 4ugane/tap/k8s-zombie` (verified end-to-end,
  including the macOS Gatekeeper quarantine fix)
- **krew** — plugin manifest is generated and pushed to a staging fork
  (`4ugane/krew-index`) on every tagged release; opening the PR from that
  fork to upstream `kubernetes-sigs/krew-index` is still a manual,
  per-release step until `kubectl krew install zombie` works directly

A `kind`-based integration test exercises a full scan end-to-end against a
real (if ephemeral) API server with real controllers, and runs in CI on every
push and PR.

See [`docs/architecture.md`](docs/architecture.md) and [`docs/adr/`](docs/adr/)
for the full design history and every non-obvious decision behind this
project, and [`docs/specs/`](docs/specs/) for the
original design spec.

## Development

```sh
go build ./...
go vet ./...
go test ./... -race -cover
gofmt -l .
```

The integration test needs a real cluster and is excluded from the default
`go test ./...` run via a build tag:

```sh
go test -tags=integration -v -timeout=5m ./test/integration/...
```

It targets the kubeconfig's current context by default (`kind` in CI, e.g.
`docker-desktop` locally); set `INTEGRATION_KUBE_CONTEXT` to target a
different one. It creates and tears down its own `k8s-zombie-test*`
namespaces and a `retain-test` StorageClass — safe to run against any
cluster you're comfortable applying test manifests to, but never a
production one.

Every detector was built test-first (RED confirmed against the unmodified
code before each fix or feature landed) — new detectors or fixes are expected
to follow the same discipline. `pkg/detector/*_test.go` is the best reference
for the project's test style.

## License

[MIT](LICENSE)
