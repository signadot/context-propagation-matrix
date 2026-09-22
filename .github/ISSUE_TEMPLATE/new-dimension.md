---
name: Propose a new language, protocol, provider or variant
about: Extend the matrix along one of its dimensions
title: "[extend] <language | protocol | provider | variant>: <name>"
labels: enhancement
---

## What should be added?

- Dimension: language / protocol boundary / provider (tool) / variant flavour
- Name and version(s):
- Which existing rows or columns does it interact with?

## Why does it matter?

Who uses this in production, and what propagation question would the new
cells answer that the current matrix cannot?

## Feasibility notes

For a **language**: is there a zero-code, init-only, build-time, or in-code
attach path for each provider? Which idiomatic HTTP, gRPC, Kafka and AMQP
clients would the relay use?

For a **protocol**: which carrier holds headers (e.g. SQS message attributes,
NATS headers)? Is there a broker container we can run in CI? Note that
boundaries grow quadratically: a fifth protocol takes the matrix from 16 to 25
cells per header per variant.

For a **provider**: is there a free, re-runnable way to obtain any credentials
the agent needs before it will propagate?

See `CONTRIBUTING.md` for the step-by-step recipe.
