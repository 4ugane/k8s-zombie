# ADR-0003: Stateless v1 — No Grace-Period Tracking

## Status
Accepted

## Context
"Orphaned for N consecutive days" is more actionable than "orphaned right now," but
computing it requires either persisting state between runs (where does that state
live in an ephemeral CI runner?) or relying on `creationTimestamp` as an imprecise
proxy (a Service created a year ago whose endpoints emptied five minutes ago looks
identical to one that's been empty for a year).

## Decision
v1 is fully stateless: no local state file, no database, no cross-run memory. Every
invocation reports whatever is orphaned in that single snapshot.

## Alternatives Considered
- **Local state file** (`~/.k8s-zombie/state.json`, keyed by cluster + resource UID)
  recording first-seen-orphaned timestamps — rejected for v1: breaks when run from a
  fresh CI job each time, and introduces a new failure mode (corrupt or missing state
  file) that a v1 tool doesn't need yet.

## Consequences
- Positive: works identically from a laptop or a fresh CI container; no state-file
  format to design, version, or migrate.
- Negative: some noise on transient states — e.g. a Service briefly shows zero
  endpoints mid-rollout and gets reported as orphaned even though it's healthy a
  minute later. Users are expected to apply judgment; this is a known, documented
  limitation, not an oversight.

## Trade-offs
Simplicity and CI-friendliness are prioritized over precision, pending real-world data
on how much noise v1 actually produces.
