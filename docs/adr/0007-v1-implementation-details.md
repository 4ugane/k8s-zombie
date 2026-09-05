# ADR-0007: v1 Implementation Details (Finding Schema, Pricing Region, Volume-Type Lookup, Module Path)

## Status
Accepted

## Context
Before starting TDD on the first detectors (unattached PVCs, orphaned PVs — build
order step 2), four concrete implementation decisions were needed that the design
spec and ADR-0001..0006 left open.

## Decisions

**Finding struct schema:**
```go
type Finding struct {
    Detector    string   // e.g. "unattached-pvc"
    Kind        string   // e.g. "PersistentVolumeClaim"
    Namespace   string   // empty for cluster-scoped resources
    Name        string
    Reason      string   // human-readable explanation
    Confidence  string   // "High" | "Medium" | "Low"
    CostUSDPerMonth *float64 // nil when the detector has no direct cost line
    Status      string   // "Orphaned" | "Skipped" (RBAC gap on this detector)
}
```

**Pricing table region scope:** single default region (us-east-1) baked into the
bundled table, documented in the README as an approximation. `--pricing-file`
(ADR-0004) already lets users supply their own rates for their actual region — no
region-detection logic is built in v1.

**EBS volume type resolution:** the cost estimator reads the PVC/PV's `StorageClass`
object and infers volume type from its provisioner/parameters (gp2/gp3/io1/io2). If
the StorageClass can't be read or its type is unrecognized, the estimator falls back
to a configurable default type (gp3) and the resulting `CostUSDPerMonth` is still
populated but based on an assumed type rather than a confirmed one.

**Go module path:** placeholder (`github.com/TODO/k8s-zombie`) until a GitHub
location is chosen. Renaming later is a mechanical find-replace across the module,
not an architectural change.

## Alternatives Considered
- Finding schema without a `Confidence` field (folding the caveat into `Reason` text)
  — rejected: a structured field lets renderers sort/filter by confidence later
  without a text-parsing hack.
- Multi-region pricing table with auto-detected cluster region — deferred to v2;
  real scope increase (table design + region-detection + more fixtures) not
  justified before v1 has any usage data.
- Flat default volume type with no StorageClass lookup — rejected: understates cost
  for io1/io2-backed volumes, which are exactly the higher-cost orphans this tool
  exists to surface.

## Consequences
- Positive: unblocks writing tests for the PVC/PV detectors and the cost estimator
  with a concrete, agreed schema.
- Negative: cost estimates carry two documented approximations (single-region
  pricing, and a default-type fallback when StorageClass lookup fails) — both are
  called out in the report/README rather than presented as billing-grade figures.
