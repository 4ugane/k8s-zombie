# ADR-0005: No Generated Delete Commands or `--apply` in v1

## Status
Accepted

## Context
The original project pitch included generating dry-run `kubectl delete` commands
for detected orphans. Generating a wrong or overly broad delete command against a
misidentified resource (especially in production) is the single highest-blast-radius
mistake this tool could make.

## Decision
v1 only reports findings. No command generation, no `--apply` flag, no execution
path of any kind — the tool never suggests or performs a deletion.

## Alternatives Considered
- **Dry-run command generation** (print the `kubectl delete` a user could run) —
  deferred to v2, once detector precision has been validated against real clusters
  and a measured false-positive rate exists to justify the added trust surface.
- **`--apply` flag that actually deletes** — rejected even for v2 discussion at this
  stage; would require a much higher confidence bar (grace periods, exclude labels,
  confirmation flow) than v1's detectors currently provide.

## Consequences
- Positive: zero risk of the tool being responsible for an unwanted production
  deletion; trivially safe to run against any cluster, including production, on day one.
- Negative: lower immediate operational utility than the original pitch — users must
  manually translate a finding into a delete command themselves.

## Trade-offs
Safety and trustworthiness on first release are prioritized over one-shot
convenience.
