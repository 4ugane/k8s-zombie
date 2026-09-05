# ADR-0006: CLI-First Execution Model

## Status
Accepted

## Context
Adoption depends on how easy the tool is to try. An in-cluster controller/CronJob
requires a Helm chart or manifests, RBAC setup, and a namespace to live in before a
user sees any value. A CLI requires only a kubeconfig.

## Decision
Ship v1 as a standalone CLI (`kubectl`-plugin-style, kubeconfig-based). Design the
detector registry and cost estimator as an importable library package (separate from
the `cmd/` CLI entrypoint) so a v2 in-cluster CronJob/controller mode can reuse the
same core logic rather than forking or reimplementing it.

## Alternatives Considered
- **CronJob in-cluster from day one** — provides continuous/scheduled monitoring
  without a human running it, but adds real setup cost (manifests, RBAC, a home
  namespace) before anyone can evaluate the tool; deferred to v2.

## Consequences
- Positive: zero-install evaluation path (`go install` or a downloaded binary +
  existing kubeconfig); the core logic is already structured for reuse when v2's
  in-cluster mode is built.
- Negative: v1 has no "always-on" monitoring story — someone has to run it, whether
  manually or via their own cron/CI schedule. This is an explicit, documented gap,
  not an oversight.

## Trade-offs
Fast, frictionless adoption is prioritized over built-in continuous monitoring for v1.
