# ADR-0004: AWS-Only Static Pricing Table

## Status
Accepted

## Context
Cost-waste estimates need a price source. Live pricing APIs (AWS Pricing API) are
accurate but add latency, IAM/auth requirements, and rate limits to every single scan
run — a heavy dependency for a tool meant to be a quick, frequent check.

## Decision
Bundle a versioned YAML pricing table (EBS $/GB-month by volume type, NLB/ALB hourly
rate + estimated LCU cost, idle EIP hourly rate) inside the binary. Support
`--pricing-file <path>` to override it for non-standard pricing or regions.

## Alternatives Considered
- **Live AWS Pricing API calls** — most accurate, but adds auth setup and network
  dependency to every run; deferred to v2.
- **Delegate to OpenCost/Kubecost if installed** — avoids maintaining a pricing table,
  but makes the tool's core value proposition (cost estimates) conditional on an
  optional dependency being present; deferred to v2 as a bonus data source, not a v1
  requirement.

## Consequences
- Positive: scans run offline-capable and fast, no AWS credentials required to get a
  cost estimate.
- Negative: the bundled table drifts from real AWS pricing over time and needs
  periodic manual updates; estimates are directional ("~$340/mo"), not billing-grade.

## Trade-offs
Simplicity, speed, and zero required cloud credentials are prioritized over pricing
accuracy and multi-region/multi-cloud coverage.
