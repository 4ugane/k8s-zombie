# ADR-0002: Detector Registry Pattern

## Status
Accepted

## Context
v1 ships 9 orphan detectors and more are expected in v2. Detectors must be able to
fail independently (e.g. an RBAC gap on one resource type) without blocking the rest
of the scan, and adding a new orphan type shouldn't require touching existing
detector code.

## Decision
Define a common `Detector` interface:

```go
type Detector interface {
    Name() string
    Scan(ctx context.Context, clientset kubernetes.Interface) ([]Finding, error)
}
```

Each orphan type is implemented in its own file, registered in a central registry,
and run concurrently. Findings are aggregated after all detectors complete (or skip).

## Alternatives Considered
- **One monolithic scan function** handling all resource types inline — rejected: as
  detector count grows this becomes an unreviewable file, and a bug or panic in one
  resource type's logic risks taking down the whole scan.

## Consequences
- Positive: adding a v2 detector is additive — one new file plus one registry line,
  no diff to existing detectors. Unit tests are naturally scoped per detector.
- Negative: slightly more boilerplate (interface + registration) than a single
  function for a v1 with only 9 checks.

## Trade-offs
Isolation and extensibility are prioritized over minimal boilerplate for the initial
9 detectors.
