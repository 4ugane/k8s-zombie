# ADR-0001: Go as Implementation Language

## Status
Accepted

## Context
k8s-zombie needs to be trivially installable (Homebrew, `krew`, GitHub releases) and
needs direct, first-class access to the Kubernetes API for read-only scanning.

## Decision
Implement k8s-zombie in Go, using `client-go` for all cluster interaction.

## Alternatives Considered
- **Python** (`kubernetes` client) — faster to prototype and lower barrier for some
  contributors, but distribution is materially worse: it needs a packaged interpreter
  or a `pip install` step rather than a single static binary.

## Consequences
- Positive: single static binary, cross-compiles trivially, matches the convention set
  by `kor`, `popeye`, and `kube-janitor` (contributors familiar with those will find
  this codebase familiar too).
- Negative: contributors need Go familiarity; slightly more ceremony than Python for
  quick scripting-style contributions.

## Trade-offs
Distribution simplicity and ecosystem convention are prioritized over prototyping speed.
