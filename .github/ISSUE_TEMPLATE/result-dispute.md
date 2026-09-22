---
name: Dispute or question a result
about: A cell in the matrix looks wrong, or you can explain a non-success cell
title: "[result] <tool>/<language> <header> <inbound>->  <outbound>"
labels: results
---

## Which cell?

- Tool / provider:
- Language and variant (e.g. `datadog-python-kafkaprop`):
- Header (`baggage`, `tracestate`, `traceparent`, `ot-baggage`):
- Boundary (e.g. `kafka -> http`):
- Run id / date (from the results page or `results/latest/results.json`):
- Tracer version shown for that row:

## What does the matrix say, and what do you believe is correct?

## Evidence

Link to the upstream source, issue, PR, or documentation that explains the
behaviour, or paste a minimal reproduction. If you have a configuration flag
that changes the result, name it exactly: we may add it as a variant flavour.
