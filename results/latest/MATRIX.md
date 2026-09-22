# Context Propagation Support Matrix

Generated from empirical runs of this harness — every cell is the observed
result of an actual request/message hop, not a reading of vendor docs.
Regenerate with `go run ./cmd/cpm aggregate` after `./run.sh`.

Run: `run-2026-09-07` · 47 variants · 752 hops · 3008 cells · finished 2026-09-07 12:30 UTC

Boundaries are `inbound protocol → outbound protocol` through a single
instrumented service (the relay). Legend: ✅ propagated/preserved ·
🟡 header present but seeded value lost · 🔀 new trace started ·
❌ header absent · 🚫 message was never forwarded (instrumentation or app broke).

The `setup` column says what attaching the tool required:
- `zero-code` — Runtime attach only: an agent flag, a preload require, the CLR profiler environment, a php.ini extension. No application source involved.
- `init-only` — A boot-file initializer loads and configures the tool before application code runs (Ruby's require+configure pattern, Elastic Python's middleware). Request-path code is untouched.
- `build-time` — Source is untouched but the build command is wrapped by a compile-time weaver (`orchestrion go build`, `go tool otelc go build`) that injects instrumentation during compilation.
- `in-code` — Instrumentation is woven into application code: wrappers at every client/server construction site and contexts threaded through (Go contribs, Rust propagator API with hand-written carriers).

## OpenTelemetry

### W3C baggage

Did the seeded `sd-routing-key` baggage member arrive intact?

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 2.31.1 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 0.80.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| python · 0.65b0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ |
| go · v1.46.0 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| go (compiletime) · v1.45.0 | build-time | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet · 1.16.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby · 1.13.0 | init-only | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| php · open-telemetry/sdk 1.15.0 (ext-opentelemetry 1.2.1) | zero-code | ✅ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| rust · 0.32.0 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

### W3C tracestate

Did the seeded foreign vendor entry (`sdtest=v1`) survive?

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 2.31.1 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 0.80.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| python · 0.65b0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ |
| go · v1.46.0 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| go (compiletime) · v1.45.0 | build-time | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet · 1.16.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ |
| ruby · 1.13.0 | init-only | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| php · open-telemetry/sdk 1.15.0 (ext-opentelemetry 1.2.1) | zero-code | ✅ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| rust · 0.32.0 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

### W3C traceparent (trace continuity)

Was the seeded trace-id continued (a new span-id is expected)?

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 2.31.1 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 0.80.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| python · 0.65b0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | 🔀 | 🔀 | 🔀 | 🔀 | ✅ | ✅ | ✅ | ✅ |
| go · v1.46.0 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| go (compiletime) · v1.45.0 | build-time | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet · 1.16.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | 🔀 | 🔀 | 🔀 | 🔀 | ✅ | ✅ | ✅ | ✅ |
| ruby · 1.13.0 | init-only | ✅ | ✅ | ✅ | ✅ | 🔀 | 🔀 | 🔀 | 🔀 | ✅ | ✅ | ✅ | ✅ | 🔀 | 🔀 | 🔀 | 🔀 |
| php · open-telemetry/sdk 1.15.0 (ext-opentelemetry 1.2.1) | zero-code | ✅ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ✅ | ❌ | ❌ | ❌ | 🔀 | ❌ | ❌ | ❌ |
| rust · 0.32.0 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

### ot-baggage-* (OpenTracing legacy)

Did `ot-baggage-sd-routing-key` arrive with the seeded value?

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 2.31.1 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| node · 0.80.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python · 0.65b0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go · v1.46.0 | in-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go (compiletime) · v1.45.0 | build-time | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet · 1.16.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby · 1.13.0 | init-only | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php · open-telemetry/sdk 1.15.0 (ext-opentelemetry 1.2.1) | zero-code | ❌ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| rust · 0.32.0 | in-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

## Datadog

### W3C baggage

Did the seeded `sd-routing-key` baggage member arrive intact?

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 1.65.1 | zero-code | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| java (defaults) · 1.65.1 | zero-code | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| node · 6.14.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node (defaults) · 6.14.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| python · 4.14.0 | zero-code | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python (defaults) · 4.14.0 | zero-code | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python (kafkaprop) · 4.14.0 | zero-code | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go · v2.10.1 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| go (defaults) · v2.10.1 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| go (orchestrion) · v2.10.1 | build-time | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet · 3.52.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet (defaults) · 3.52.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| ruby · 2.42.0 | init-only | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby (defaults) · 2.42.0 | init-only | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php · ddtrace 1.25.0 | zero-code | ✅ | ❌ | ✅ | ✅ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ✅ | ❌ | ✅ | ✅ |
| rust · 0.5.2 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

### W3C tracestate

Did the seeded foreign vendor entry (`sdtest=v1`) survive?

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 1.65.1 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| java (defaults) · 1.65.1 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 6.14.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node (defaults) · 6.14.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| python · 4.14.0 | zero-code | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | ❌ | ❌ | 🟡 | 🟡 | ❌ | ❌ | 🟡 | 🟡 | ❌ | ❌ |
| python (defaults) · 4.14.0 | zero-code | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | ❌ | ❌ | 🟡 | 🟡 | ❌ | ❌ | 🟡 | 🟡 | ❌ | ❌ |
| python (kafkaprop) · 4.14.0 | zero-code | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | 🟡 | 🟡 | 🟡 | ❌ | 🟡 | 🟡 | 🟡 | ❌ |
| go · v2.10.1 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| go (defaults) · v2.10.1 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| go (orchestrion) · v2.10.1 | build-time | ✅ | ✅ | 🟡 | ❌ | ✅ | ✅ | 🟡 | ❌ | 🟡 | 🟡 | 🟡 | ❌ | 🟡 | 🟡 | 🟡 | ❌ |
| dotnet · 3.52.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet (defaults) · 3.52.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| ruby · 2.42.0 | init-only | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | 🟡 | 🟡 | 🟡 | ❌ |
| ruby (defaults) · 2.42.0 | init-only | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | 🟡 | 🟡 | 🟡 | ❌ |
| php · ddtrace 1.25.0 | zero-code | ✅ | ❌ | ✅ | ✅ | 🚫 | 🚫 | 🚫 | 🚫 | 🟡 | ❌ | 🟡 | 🟡 | ✅ | ❌ | ✅ | ✅ |
| rust · 0.5.2 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

### W3C traceparent (trace continuity)

Was the seeded trace-id continued (a new span-id is expected)?

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 1.65.1 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| java (defaults) · 1.65.1 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 6.14.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node (defaults) · 6.14.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| python · 4.14.0 | zero-code | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | ❌ | ❌ | 🔀 | 🔀 | ❌ | ❌ | 🔀 | 🔀 | ❌ | ❌ |
| python (defaults) · 4.14.0 | zero-code | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | ❌ | ❌ | 🔀 | 🔀 | ❌ | ❌ | 🔀 | 🔀 | ❌ | ❌ |
| python (kafkaprop) · 4.14.0 | zero-code | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | 🔀 | 🔀 | 🔀 | ❌ | 🔀 | 🔀 | 🔀 | ❌ |
| go · v2.10.1 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| go (defaults) · v2.10.1 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| go (orchestrion) · v2.10.1 | build-time | ✅ | ✅ | 🔀 | ❌ | ✅ | ✅ | 🔀 | ❌ | 🔀 | 🔀 | 🔀 | ❌ | 🔀 | 🔀 | 🔀 | ❌ |
| dotnet · 3.52.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet (defaults) · 3.52.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| ruby · 2.42.0 | init-only | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | 🔀 | 🔀 | 🔀 | ❌ |
| ruby (defaults) · 2.42.0 | init-only | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | 🔀 | 🔀 | 🔀 | ❌ |
| php · ddtrace 1.25.0 | zero-code | ✅ | ❌ | ✅ | ✅ | 🚫 | 🚫 | 🚫 | 🚫 | 🔀 | ❌ | 🔀 | 🔀 | ✅ | ❌ | ✅ | ✅ |
| rust · 0.5.2 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

### ot-baggage-* (OpenTracing legacy)

Did `ot-baggage-sd-routing-key` arrive with the seeded value?

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 1.65.1 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| java (defaults) · 1.65.1 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 6.14.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node (defaults) · 6.14.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| python · 4.14.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python (defaults) · 4.14.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python (kafkaprop) · 4.14.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go · v2.10.1 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| go (defaults) · v2.10.1 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| go (orchestrion) · v2.10.1 | build-time | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet · 3.52.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet (defaults) · 3.52.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby · 2.42.0 | init-only | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby (defaults) · 2.42.0 | init-only | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php · ddtrace 1.25.0 | zero-code | ❌ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| rust · 0.5.2 | in-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

## Elastic APM

### W3C baggage

Did the seeded `sd-routing-key` baggage member arrive intact?

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 1.56.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 4.18.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python · 6.26.2 | init-only | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go · v2.7.12 | in-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet · 1.34.7 | zero-code | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby · 4.9.0 | init-only | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php · elastic_apm 1.17.0 | zero-code | ❌ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

### W3C tracestate

Did the seeded foreign vendor entry (`sdtest=v1`) survive?

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 1.56.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 4.18.0 | zero-code | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python · 6.26.2 | init-only | ✅ | ✅ | ❌ | ❌ | 🟡 | 🟡 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go · v2.7.12 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet · 1.34.7 | zero-code | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ | ❌ | 🟡 | ✅ | ✅ | ❌ | ✅ |
| ruby · 4.9.0 | init-only | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php · elastic_apm 1.17.0 | zero-code | ✅ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | 🟡 | ❌ | ❌ | ❌ | 🟡 | ❌ | ❌ | ❌ |

### W3C traceparent (trace continuity)

Was the seeded trace-id continued (a new span-id is expected)?

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 1.56.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 4.18.0 | zero-code | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python · 6.26.2 | init-only | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go · v2.7.12 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet · 1.34.7 | zero-code | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | 🔀 | 🔀 | ❌ | 🔀 | ✅ | ✅ | ❌ | ✅ |
| ruby · 4.9.0 | init-only | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php · elastic_apm 1.17.0 | zero-code | ✅ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | 🔀 | ❌ | ❌ | ❌ | 🔀 | ❌ | ❌ | ❌ |

### ot-baggage-* (OpenTracing legacy)

Did `ot-baggage-sd-routing-key` arrive with the seeded value?

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 1.56.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| node · 4.18.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python · 6.26.2 | init-only | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go · v2.7.12 | in-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet · 1.34.7 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby · 4.9.0 | init-only | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php · elastic_apm 1.17.0 | zero-code | ❌ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

## New Relic

### W3C baggage

Did the seeded `sd-routing-key` baggage member arrive intact?

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 9.4.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| node · 14.3.10 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python · 13.5.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go · v3.44.2 | in-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet · 10.54.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby · 10.7.1 | init-only | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php · newrelic 12.10.0.39 | zero-code | ❌ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

### W3C tracestate

Did the seeded foreign vendor entry (`sdtest=v1`) survive?

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 9.4.0 | zero-code | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| node · 14.3.10 | zero-code | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ | ❌ | ✅ |
| python · 13.5.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| go · v3.44.2 | in-code | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ |
| dotnet · 10.54.0 | zero-code | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby · 10.7.1 | init-only | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ |
| php · newrelic 12.10.0.39 | zero-code | ✅ | ❌ | ❌ | ✅ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

### W3C traceparent (trace continuity)

Was the seeded trace-id continued (a new span-id is expected)?

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 9.4.0 | zero-code | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| node · 14.3.10 | zero-code | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ | ❌ | ✅ |
| python · 13.5.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| go · v3.44.2 | in-code | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ |
| dotnet · 10.54.0 | zero-code | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby · 10.7.1 | init-only | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ |
| php · newrelic 12.10.0.39 | zero-code | ✅ | ❌ | ❌ | ✅ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

### ot-baggage-* (OpenTracing legacy)

Did `ot-baggage-sd-routing-key` arrive with the seeded value?

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 9.4.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| node · 14.3.10 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python · 13.5.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go · v3.44.2 | in-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet · 10.54.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby · 10.7.1 | init-only | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php · newrelic 12.10.0.39 | zero-code | ❌ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

## No instrumentation (baseline)

### W3C baggage

Did the seeded `sd-routing-key` baggage member arrive intact?

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| node | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php | — | ❌ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| rust | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

### W3C tracestate

Did the seeded foreign vendor entry (`sdtest=v1`) survive?

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| node | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet | — | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php | — | ❌ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| rust | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

### W3C traceparent (trace continuity)

Was the seeded trace-id continued (a new span-id is expected)?

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| node | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet | — | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php | — | ❌ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| rust | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

### ot-baggage-* (OpenTracing legacy)

Did `ot-baggage-sd-routing-key` arrive with the seeded value?

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| node | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php | — | ❌ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| rust | — | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

## Why the non-green cells are non-green

Each annotation below is attached to the cells it explains in `results.json`;
the full write-ups live in `annotations/*.yaml` and on contextpropagation.io.

- **Baseline: with no agent attached, nothing propagates** — These relays run with no agent, wrapper or tracing library; no runtime in the matrix carries context on its own except .NET over HTTP/gRPC. _[by-design; 484 cells]_
- **Baseline .NET 8: the runtime reads W3C baggage but re-emits it as Correlation-Context** — On .NET 8/9 the default DistributedContextPropagator is the pre-W3C legacy one: it reads `baggage` but re-emits it as the obsolete Correlation-Context header. _[by-design; 4 cells]_
  - [.NET 10 breaking change: default trace context propagator](https://learn.microsoft.com/en-us/dotnet/core/compatibility/core-libraries/10.0/default-trace-context-propagator)
- **PHP: no gRPC server exists in any tracer-supported topology, so gRPC-inbound is undeliverable** — The official gRPC PHP implementation is client-only; the harness's Apache mod_php + CLI worker topology cannot accept gRPC calls, so grpc->* hops never run. _[app-limitation; 80 cells]_
  - [grpc.io PHP quickstart (clients only)](https://grpc.io/docs/languages/php/quickstart/)
  - [grpc/grpc #38481](https://github.com/grpc/grpc/issues/38481)
  - [RoadRunner gRPC plugin](https://docs.roadrunner.dev/docs/grpc/grpc)
  - [dd-trace-php #805 (RoadRunner gRPC guidance)](https://github.com/DataDog/dd-trace-php/issues/805)
- **PHP: no tool instruments the ext-grpc client, so every *->grpc leg is header-free** — Neither OTel PHP, dd-trace-php, Elastic PHP nor New Relic PHP hooks ext-grpc + grpc/grpc client calls; no metadata is injected on any *->grpc boundary. _[open; 30 cells]_
  - [dd-trace-php #916 (gRPC request, open since 2020)](https://github.com/DataDog/dd-trace-php/issues/916)
  - [Datadog PHP compatibility docs](https://docs.datadoghq.com/tracing/trace_collection/compatibility/php/)
  - [opentelemetry-php-contrib instrumentation tree (no gRPC)](https://github.com/open-telemetry/opentelemetry-php-contrib/tree/main/src/Instrumentation)
  - [grpc.io PHP quickstart](https://grpc.io/docs/languages/php/quickstart/)
- **Go build-time weavers: Kafka consumers never hand the extracted context back to application code** — otelc's kafka-go Reader has no callback to hand back the context; Orchestrion's sarama consumer weave does not continue the trace seeded in record headers. _[open; 28 cells]_
  - [opentelemetry-go-compile-instrumentation](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation)
  - [opentelemetry-go-compile-instrumentation #695 (messaging semconv workstream)](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/695)
  - [opentelemetry-go-compile-instrumentation PR #1076 (kafka.Reader.FetchMessage)](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/1076)
  - [Orchestrion](https://github.com/DataDog/orchestrion)
  - [dd-trace-go contrib IBM/sarama (manual pattern)](https://pkg.go.dev/github.com/DataDog/dd-trace-go/contrib/IBM/sarama/v2)
- **Go build-time weavers: neither otelc nor dd-trace-go has an AMQP client integration to weave** — otelc's supported-libraries list has no AMQP client and dd-trace-go has no amqp091 contrib, so RabbitMQ publish injects nothing and consume extracts nothing. _[open; 49 cells]_
  - [otelc getting-started (supported libraries)](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/blob/main/docs/getting-started.md)
  - [Orchestrion](https://github.com/DataDog/orchestrion)
  - [Datadog Go compile-time instrumentation docs](https://docs.datadoghq.com/tracing/trace_collection/automatic_instrumentation/dd_libraries/go/?tab=compiletimeinstrumentation)
- **OpenTelemetry, Elastic APM and New Relic never implemented the OpenTracing-era ot-baggage-* headers** — ot-baggage-* is a legacy OpenTracing convention; OpenTelemetry, Elastic APM and New Relic each define their propagation header set without it. _[by-design; 356 cells]_
  - [OpenTracing trace identifiers RFC (ot-baggage-*)](https://github.com/opentracing/specification/blob/master/rfc/trace_identifiers.md)
  - [Elastic agent spec — distributed tracing](https://github.com/elastic/apm/blob/main/specs/agents/tracing-distributed-tracing.md)
  - [How New Relic distributed tracing works](https://docs.newrelic.com/docs/distributed-tracing/concepts/how-new-relic-distributed-tracing-works/)
- **dd-trace-java injects W3C baggage only on HTTP clients** — Non-HTTP client and messaging producer instrumentations inject from the span, not the current context, so the context-scoped baggage propagator finds nothing. _[open; 30 cells]_
  - [dd-trace-java #11286](https://github.com/DataDog/dd-trace-java/issues/11286)
  - [dd-trace-java PR #11330 (community fix, closed unmerged)](https://github.com/DataDog/dd-trace-java/pull/11330)
  - [dd-trace-java #11510 (same class on Spring WebClient, fixed)](https://github.com/DataDog/dd-trace-java/issues/11510)
  - [dd-trace-java PR #12027 (context-centric fix, async paths)](https://github.com/DataDog/dd-trace-java/pull/12027)
  - [dd-trace-java PR #12033](https://github.com/DataDog/dd-trace-java/pull/12033)
  - [dd-trace-java PR #8330 (context/tracing decoupling)](https://github.com/DataDog/dd-trace-java/pull/8330)
  - [dd-trace-java PR #10767](https://github.com/DataDog/dd-trace-java/pull/10767)
- **dd-trace-py: Kafka header injection is disabled by default** — ddtrace's Kafka integration ships distributed_tracing_enabled=False; nothing is injected into produced records until DD_KAFKA_PROPAGATION_ENABLED=true. _[config; 24 cells]_
  - [ddtrace Kafka integration docs (DD_KAFKA_PROPAGATION_ENABLED)](https://ddtrace.readthedocs.io/en/stable/integrations.html#kafka)
  - [dd-trace-py PR #7434 (flag introduced default-off)](https://github.com/DataDog/dd-trace-py/pull/7434)
- **dd-trace-py: consumed Kafka context is not activated for the code that processes the message** — ddtrace's kafka.consume span is created without using the extracted context, so downstream calls start a new trace and baggage is dropped. _[open; 36 cells]_
  - [dd-trace-py PR #7345](https://github.com/DataDog/dd-trace-py/pull/7345)
  - [dd-trace-py #8967 (manual activate workaround)](https://github.com/DataDog/dd-trace-py/issues/8967)
  - [dd-trace-py #17980](https://github.com/DataDog/dd-trace-py/issues/17980)
  - [dd-trace-py PR #18667](https://github.com/DataDog/dd-trace-py/pull/18667)
  - [dd-trace-py PR #19858](https://github.com/DataDog/dd-trace-py/pull/19858)
- **dd-trace-py has no pika integration; RabbitMQ legs lose everything** — ddtrace's only AMQP coverage is the kombu library; pika is not instrumented, so nothing is injected on publish or extracted on consume. _[open; 63 cells]_
  - [Datadog Python compatibility docs](https://docs.datadoghq.com/tracing/trace_collection/compatibility/python/)
  - [ddtrace integrations docs](https://ddtrace.readthedocs.io/en/stable/integrations.html)
  - [dd-trace-py #2047 (pika request, not planned)](https://github.com/DataDog/dd-trace-py/issues/2047)
  - [dd-trace-py #17225 (aio-pika request, open)](https://github.com/DataDog/dd-trace-py/issues/17225)
  - [dd-trace-py #2048](https://github.com/DataDog/dd-trace-py/issues/2048)
- **dd-trace-py: ot-baggage-* inject and extract are gated behind a default-off flag** — ddtrace has ot-baggage-* code, but both inject and extract require DD_TRACE_PROPAGATION_HTTP_BAGGAGE_ENABLED=true (default False). _[config; 48 cells]_
  - [dd-trace-py propagation/http.py](https://github.com/DataDog/dd-trace-py/blob/main/ddtrace/propagation/http.py)
  - [ddtrace configuration docs (DD_TRACE_PROPAGATION_HTTP_BAGGAGE_ENABLED)](https://ddtrace.readthedocs.io/en/stable/configuration.html)
  - [dd-trace-py PR #6072](https://github.com/DataDog/dd-trace-py/pull/6072)
  - [dd-trace-py v3.0.0 (OpenTracing bridge removed)](https://github.com/DataDog/dd-trace-py/releases/tag/v3.0.0)
  - [Datadog trace context propagation reference](https://docs.datadoghq.com/tracing/trace_collection/trace_context_propagation/)
- **dd-trace .NET, Ruby, Rust and PHP do not emit the legacy ot-baggage-* headers** — ot-baggage-* is undocumented legacy behaviour of dd-trace java/js/go; .NET never had it, Ruby removed it with OpenTracing, Rust and PHP do not emit it. _[by-design; 92 cells]_
  - [Datadog trace context propagation reference](https://docs.datadoghq.com/tracing/trace_collection/trace_context_propagation/)
  - [dd-trace-rb PR #3443 (OpenTracing removed in 2.0)](https://github.com/DataDog/dd-trace-rb/pull/3443)
  - [dd-trace-rb #4055](https://github.com/DataDog/dd-trace-rb/issues/4055)
  - [dd-trace-rb PR #4493 (W3C baggage)](https://github.com/DataDog/dd-trace-rb/pull/4493)
  - [dd-trace-rb PR #4505](https://github.com/DataDog/dd-trace-rb/pull/4505)
  - [dd-trace-php #3614](https://github.com/DataDog/dd-trace-php/issues/3614)
- **dd-trace-rb has no bunny integration; RabbitMQ legs lose everything** — The datadog gem's AMQP coverage is the Sneakers/Kicks worker framework only; the bunny client is not instrumented on publish or consume. _[open; 42 cells]_
  - [Datadog Ruby compatibility docs](https://docs.datadoghq.com/tracing/trace_collection/compatibility/ruby/)
  - [dd-trace-rb contrib directory (no bunny)](https://github.com/DataDog/dd-trace-rb/tree/master/lib/datadog/tracing/contrib)
- **dd-trace-php closes the rdkafka consume span inside consume(), before user code runs** — The Kafka consume hook attaches the extracted headers to a span it closes before returning the message, so outbound calls run outside the extracted context. _[open; 12 cells]_
  - [dd-trace-php KafkaIntegration.php](https://github.com/DataDog/dd-trace-php/blob/master/src/DDTrace/Integrations/Kafka/KafkaIntegration.php)
  - [dd-trace-php configuration.h (DD_TRACE_KAFKA_DISTRIBUTED_TRACING default true)](https://github.com/DataDog/dd-trace-php/blob/master/tracer/configuration.h)
  - [dd-trace-php #639 (rdkafka request, still open)](https://github.com/DataDog/dd-trace-php/issues/639)
  - [Datadog PHP compatibility docs](https://docs.datadoghq.com/tracing/trace_collection/compatibility/php/)
- **Orchestrion: sarama SendMessage has no context parameter, so the woven produce span is a new root** — Orchestrion finds a context.Context only in the woven function's parameters; sarama's SendMessage(msg) has none, so the produce span is a new root. _[open; 8 cells]_
  - [Orchestrion](https://github.com/DataDog/orchestrion)
  - [Datadog Go compile-time instrumentation docs](https://docs.datadoghq.com/tracing/trace_collection/automatic_instrumentation/dd_libraries/go/?tab=compiletimeinstrumentation)
  - [orchestrion #871 (context discovery limitation)](https://github.com/DataDog/orchestrion/issues/871)
  - [dd-trace-go contrib IBM/sarama (manual pattern)](https://pkg.go.dev/github.com/DataDog/dd-trace-go/contrib/IBM/sarama/v2)
- **apm-agent-go has no baggage API, by explicit decision** — Elastic's Go agent carries trace identity on all 16 boundaries but implements no W3C baggage at all; the request was closed not-planned in 2019. _[wontfix; 16 cells]_
  - [apm-agent-go #537 (closed not-planned)](https://github.com/elastic/apm-agent-go/issues/537)
  - [apmot/span.go (SetBaggageItem is a no-op)](https://github.com/elastic/apm-agent-go/blob/main/module/apmot/span.go#L127-L136)
  - [apmot/doc.go](https://github.com/elastic/apm-agent-go/blob/main/module/apmot/doc.go)
  - [Elastic agent spec — distributed tracing](https://github.com/elastic/apm/blob/main/specs/agents/tracing-distributed-tracing.md)
- **Elastic Node, Python, Ruby and PHP agents do not propagate W3C baggage on any boundary** — Only Elastic's Java agent (and .NET over HTTP/gRPC) implements the baggage propagation the agent spec mandates; these agents drop it on every boundary. _[open; 60 cells]_
  - [Elastic agent spec — distributed tracing](https://github.com/elastic/apm/blob/main/specs/agents/tracing-distributed-tracing.md)
  - [Elastic Node.js agent — distributed tracing docs](https://www.elastic.co/docs/reference/apm/agents/nodejs/distributed-tracing)
- **elastic-apm-node does not instrument amqplib; RabbitMQ legs carry nothing** — amqplib is absent from the Node agent's supported technologies; long-standing requests (#1188, #1791) have been open since 2019/2020. _[open; 14 cells]_
  - [Elastic Node.js agent — supported technologies](https://www.elastic.co/docs/reference/apm/agents/nodejs/supported-technologies)
  - [Elastic Node.js agent — distributed tracing docs](https://www.elastic.co/docs/reference/apm/agents/nodejs/distributed-tracing)
  - [apm-agent-nodejs #1188](https://github.com/elastic/apm-agent-nodejs/issues/1188)
  - [apm-agent-nodejs #1791](https://github.com/elastic/apm-agent-nodejs/issues/1791)
- **elastic-apm Python: gRPC server interceptor assigns the tracestate header name instead of its value** — server_interceptor.py has `tracestate = metadata.key`, so after a gRPC hop the outbound tracestate is the literal string 'tracestate'; foreign entries are lost. _[open; 2 cells]_
  - [apm-agent-python server_interceptor.py @ v6.26.2 L66-L73](https://github.com/elastic/apm-agent-python/blob/v6.26.2/elasticapm/contrib/grpc/server_interceptor.py#L66-L73)
  - [apm-agent-python server_interceptor.py @ main (still unfixed)](https://github.com/elastic/apm-agent-python/blob/main/elasticapm/contrib/grpc/server_interceptor.py#L70-L71)
- **elastic-apm Python has no confluent-kafka or pika instrumentation** — The Python agent's only messaging client is kafka-python; confluent-kafka and pika are uninstrumented: nothing is injected, consumers start no transaction. _[open; 24 cells]_
  - [Elastic Python agent — supported technologies](https://www.elastic.co/docs/reference/apm/agents/python/supported-technologies)
  - [apm-agent-python #678 (RabbitMQ request, open since 2019)](https://github.com/elastic/apm-agent-python/issues/678)
- **Elastic .NET: Kafka instrumentation exists only in the CLR-profiler deployment, not the startup hook** — The startup-hook zero-code deployment covers neither Kafka nor RabbitMQ; the profiler that does is x64-only and caps at Confluent.Kafka <3.0.0. _[by-design; 21 cells]_
  - [Elastic .NET profiler auto-instrumentation docs](https://www.elastic.co/docs/reference/apm/agents/dotnet/setup-auto-instrumentation)
- **Elastic .NET: RabbitMQ trace context flows via the client's native ActivitySource, but baggage does not** — The Elastic agent has no RabbitMQ.Client 7 instrumentation; the client's own default propagation carries trace id and tracestate only, never baggage. _[open; 5 cells]_
  - [Elastic .NET profiler auto-instrumentation docs](https://www.elastic.co/docs/reference/apm/agents/dotnet/setup-auto-instrumentation)
  - [rabbitmq-dotnet-client PR #1261 (native ActivitySource)](https://github.com/rabbitmq/rabbitmq-dotnet-client/pull/1261)
  - [rabbitmq-dotnet-client PR #1528 (opt-in baggage package)](https://github.com/rabbitmq/rabbitmq-dotnet-client/pull/1528)
  - [rabbitmq-dotnet-client #1728](https://github.com/rabbitmq/rabbitmq-dotnet-client/issues/1728)
- **Elastic Ruby: gRPC support is experimental and manual, with no zero-code client or server path** — The Ruby agent instruments gRPC only via a hand-added ClientInterceptor at stub creation; the init-only setup adds none, and there is no server-side path. _[open; 14 cells]_
  - [Elastic Ruby agent — supported technologies](https://www.elastic.co/docs/reference/apm/agents/ruby/supported-technologies)
- **Elastic Ruby: no bunny or client-level Kafka instrumentation, and the Racecar spy never reads headers** — The Ruby agent covers Kafka only via Racecar (whose spy never reads a trace_parent) and RabbitMQ only via Sneakers; rdkafka and bunny are uninstrumented. _[open; 24 cells]_
  - [Elastic Ruby agent — supported technologies](https://www.elastic.co/docs/reference/apm/agents/ruby/supported-technologies)
  - [apm-agent-ruby racecar.rb spy (no trace_parent)](https://github.com/elastic/apm-agent-ruby/blob/main/lib/elastic_apm/spies/racecar.rb)
  - [apm-agent-ruby PR #1284](https://github.com/elastic/apm-agent-ruby/pull/1284)
- **Elastic PHP: Kafka and RabbitMQ are not supported technologies; only http->http propagates** — elastic_apm instruments neither rdkafka nor php-amqplib: nothing is injected on produce/publish and consumed messages start a fresh trace on the outbound leg. _[open; 20 cells]_
- **New Relic agents carry no W3C baggage on any boundary** — Baggage is not part of New Relic's distributed-tracing model (traceparent, tracestate, newrelic); no agent forwards the header and no feature request exists. _[by-design; 108 cells]_
  - [How New Relic distributed tracing works](https://docs.newrelic.com/docs/distributed-tracing/concepts/how-new-relic-distributed-tracing-works/)
- **New Relic Java: Kafka DT header injection is opt-in via a class_transformer setting** — kafka-clients instrumentation is telemetry-only by default; header injection requires enabling the kafka-clients-spans-0.11.0.0 module in newrelic.yml. _[config; 8 cells]_
  - [New Relic Java agent — instrument Kafka message queues](https://docs.newrelic.com/docs/apm/agents/java-agent/instrumentation/java-agent-instrument-kafka-message-queues/)
  - [kafka-clients-spans-0.11.0.0 instrumentation module](https://github.com/newrelic/newrelic-java-agent/tree/main/instrumentation/kafka-clients-spans-0.11.0.0)
- **New Relic Java: Kafka consumers and spring-rabbit listeners never accept inbound DT headers** — No spring-rabbit module: DT accept runs on the AMQP connection thread, not the listener's thread; the Kafka consumer instrumentation never accepts DT headers. _[open; 16 cells]_
  - [newrelic-java-agent rabbit-amqp-5.0.0 Consumer_Instrumentation.java](https://github.com/newrelic/newrelic-java-agent/blob/main/instrumentation/rabbit-amqp-5.0.0/src/main/java/com/rabbitmq/client/Consumer_Instrumentation.java)
- **New Relic Node: kafkajs instrumentation is behind a default-off feature flag** — feature_flags.js ships kafkajs_instrumentation: false; with it off the agent skips kafkajs entirely, so no headers are injected or accepted on Kafka. _[config; 14 cells]_
  - [node-newrelic feature_flags.js @ v14.3.10](https://github.com/newrelic/node-newrelic/blob/v14.3.10/lib/feature_flags.js)
  - [node-newrelic kafkajs client-constructor.js (skip log)](https://github.com/newrelic/node-newrelic/blob/v14.3.10/lib/subscribers/kafkajs/client-constructor.js#L56-L64)
  - [node-newrelic feature-flags.md @ v14.3.10](https://github.com/newrelic/node-newrelic/blob/v14.3.10/documentation/feature-flags.md)
  - [node-newrelic v11.19.0 release](https://github.com/newrelic/node-newrelic/releases/tag/v11.19.0)
  - [node-newrelic compatibility.md](https://github.com/newrelic/node-newrelic/blob/main/compatibility.md)
- **New Relic Go: nrsarama's producer injects DT headers into a throwaway map** — nrsarama calls InsertDistributedTraceHeaders on the carrier's embedded http.Header, bypassing the Set method that would copy headers into msg.Headers. _[open; 8 cells]_
  - [go-agent nrsarama/producer.go](https://github.com/newrelic/go-agent/blob/master/v3/integrations/nrsarama/producer.go)
  - [go-agent nrsarama/consumer.go](https://github.com/newrelic/go-agent/blob/master/v3/integrations/nrsarama/consumer.go)
  - [go-agent nrsarama consumer example (no accept call)](https://github.com/newrelic/go-agent/blob/master/v3/integrations/nrsarama/example/consumer/consumerexample.go)
  - [go-agent #98](https://github.com/newrelic/go-agent/issues/98)
  - [go-agent #160](https://github.com/newrelic/go-agent/issues/160)
- **New Relic .NET: RabbitMQ.Client 7's async IChannel API is not instrumented by the classic agent** — The agent instruments the classic IModel API that RabbitMQ.Client 7 removed; v7 support exists only via the default-off hybrid agent, which emits span links. _[open; 14 cells]_
  - [newrelic-dotnet-agent PR #2890 (closed unmerged)](https://github.com/newrelic/newrelic-dotnet-agent/pull/2890)
  - [newrelic-dotnet-agent #2885](https://github.com/newrelic/newrelic-dotnet-agent/issues/2885)
  - [New Relic OpenTelemetry API support (hybrid agent)](https://docs.newrelic.com/docs/apm/agents/manage-apm-agents/opentelemetry-api-support/)
  - [New Relic .NET agent compatibility and requirements](https://docs.newrelic.com/docs/apm/agents/net-agent/getting-started/net-agent-compatibility-requirements/)
- **New Relic .NET: Confluent.Kafka produce injects, but consumed records are never accepted** — Confluent.Kafka instrumentation injects on produce but never accepts record headers into a transaction on consume, so outbound calls carry no headers. _[open; 8 cells]_
- **New Relic Ruby: bunny publish injects only the legacy newrelic header, not W3C headers** — newrelic_rpm's bunny publish instrumentation emits the vendor newrelic header only; traceparent and tracestate are absent on every *->rabbitmq leg. _[open; 8 cells]_
- **New Relic PHP does not instrument rdkafka; Kafka produce injects nothing** — The PHP agent's messaging support is php-amqplib publish only; rdkafka is unsupported, so no headers are injected into produced records. _[open; 6 cells]_
- **New Relic PHP: long-running CLI consumers never accept inbound context** — The PHP agent has no rdkafka consume or php-amqplib consume instrumentation, so the CLI worker processes never start a transaction from message headers. _[open; 16 cells]_
- **OTel Python: confluent-kafka consumer links the producer context but never attaches it** — opentelemetry-instrumentation-confluent-kafka starts the consume span with the producer context as a link and never calls context.attach(). _[open; 12 cells]_
  - [opentelemetry-python-contrib #1674](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/1674)
  - [opentelemetry-python-contrib #1962 (maintainer meta-issue)](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/1962)
  - [opentelemetry-python-contrib #1732 (kafka-python sibling)](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/1732)
  - [opentelemetry-python-contrib #3026 (aiokafka sibling)](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/3026)
  - [opentelemetry-python-contrib #3465 (maintainer position)](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/3465)
  - [opentelemetry-python-contrib PR #1678 (closed unmerged)](https://github.com/open-telemetry/opentelemetry-python-contrib/pull/1678)
- **OTel .NET: Confluent.Kafka consumer spans link to the producer instead of continuing it** — Since 1.7.0 the .NET auto-instrumentation deliberately stops using the message-creation context as the consumer span's parent, per OTel messaging semconv. _[by-design; 12 cells]_
  - [opentelemetry-dotnet-instrumentation #3434](https://github.com/open-telemetry/opentelemetry-dotnet-instrumentation/issues/3434)
  - [opentelemetry-dotnet-instrumentation PR #3438](https://github.com/open-telemetry/opentelemetry-dotnet-instrumentation/pull/3438)
  - [OTel messaging semantic conventions (span structure)](https://github.com/open-telemetry/semantic-conventions/blob/main/docs/messaging/messaging-spans.md)
  - [OTEP-0220](https://github.com/open-telemetry/opentelemetry-specification/blob/main/oteps/trace/0220-messaging-semantic-conventions-span-structure.md)
  - [semantic-conventions PR #1414 (parent-child re-allowed as option)](https://github.com/open-telemetry/semantic-conventions/pull/1414)
  - [opentelemetry-dotnet-contrib #3995](https://github.com/open-telemetry/opentelemetry-dotnet-contrib/issues/3995)
  - [opentelemetry-dotnet-contrib #2351](https://github.com/open-telemetry/opentelemetry-dotnet-contrib/issues/2351)
- **OTel .NET: RabbitMQ.Client's native propagation continues the trace but carries no baggage** — RabbitMQ.Client 7's native ActivitySource extractor reads trace id and tracestate only; baggage lives in an opt-in package that never shipped stable. _[open; 4 cells]_
  - [rabbitmq-dotnet-client PR #1261 (native ActivitySource)](https://github.com/rabbitmq/rabbitmq-dotnet-client/pull/1261)
  - [rabbitmq-dotnet-client PR #1528 (opt-in baggage package)](https://github.com/rabbitmq/rabbitmq-dotnet-client/pull/1528)
  - [rabbitmq-dotnet-client #1728 (ship RabbitMQ.Client.OpenTelemetry 1.0.0)](https://github.com/rabbitmq/rabbitmq-dotnet-client/issues/1728)
  - [opentelemetry-dotnet-instrumentation #3662](https://github.com/open-telemetry/opentelemetry-dotnet-instrumentation/issues/3662)
  - [rabbitmq-dotnet-client PR #1779 (UsePublisherAsParent default, 7.2.0)](https://github.com/rabbitmq/rabbitmq-dotnet-client/pull/1779)
  - [rabbitmq-dotnet-client #1967](https://github.com/rabbitmq/rabbitmq-dotnet-client/issues/1967)
- **OTel Ruby: bunny consumer drops the producer context and starts a new trace** — opentelemetry-instrumentation-bunny parents the process span on its own re-injected local headers and demotes the producer context to a span link. _[open; 12 cells]_
  - [opentelemetry-ruby-contrib #526](https://github.com/open-telemetry/opentelemetry-ruby-contrib/issues/526)
  - [opentelemetry-ruby discussion #1098](https://github.com/open-telemetry/opentelemetry-ruby/discussions/1098)
  - [bunny patch_helpers.rb](https://github.com/open-telemetry/opentelemetry-ruby-contrib/blob/main/instrumentation/bunny/lib/opentelemetry/instrumentation/bunny/patch_helpers.rb)
  - [opentelemetry-ruby PR #566 (design lineage)](https://github.com/open-telemetry/opentelemetry-ruby/pull/566)
- **OTel Ruby: the gRPC instrumentation is client-only, so gRPC-inbound requests start a new trace** — opentelemetry-instrumentation-grpc only instruments outbound calls; there is no server-side instrumentation for the plain grpc gem. _[open; 12 cells]_
  - [opentelemetry-ruby-contrib gRPC README (client-only warning)](https://github.com/open-telemetry/opentelemetry-ruby-contrib/blob/main/instrumentation/grpc/README.md)
  - [opentelemetry-ruby-contrib #10 (original 2020 request)](https://github.com/open-telemetry/opentelemetry-ruby-contrib/issues/10)
  - [opentelemetry-ruby-contrib #1041](https://github.com/open-telemetry/opentelemetry-ruby-contrib/issues/1041)
  - [opentelemetry-ruby-contrib PR #1164 (donation)](https://github.com/open-telemetry/opentelemetry-ruby-contrib/pull/1164)
  - [hibachrach/opentelemetry-instrumentation-grpc](https://github.com/hibachrach/opentelemetry-instrumentation-grpc)
  - [opentelemetry-ruby-contrib gruf README (server spans for Gruf only)](https://github.com/open-telemetry/opentelemetry-ruby-contrib/blob/main/instrumentation/gruf/README.md)
- **OTel PHP: rdkafka producev() injection silently no-ops unless the $headers argument is present** — ext-opentelemetry drops arguments a pre-hook adds to a native function call, so the rdkafka instrumentation's injected $headers never reach producev(). _[config; 6 cells]_
  - [ExtRdKafkaInstrumentation.php](https://github.com/open-telemetry/opentelemetry-php-contrib/blob/main/src/Instrumentation/ExtRdKafka/src/ExtRdKafkaInstrumentation.php)
  - [opentelemetry-php-instrumentation PR #84 (disable argument expansion)](https://github.com/open-telemetry/opentelemetry-php-instrumentation/pull/84)
  - [opentelemetry-php-instrumentation PR #120](https://github.com/open-telemetry/opentelemetry-php-instrumentation/pull/120)
  - [opentelemetry-php #1155 (original segfault)](https://github.com/open-telemetry/opentelemetry-php/issues/1155)
  - [ext-opentelemetry README (allow_stack_extension)](https://github.com/open-telemetry/opentelemetry-php-instrumentation/blob/main/README.md)
  - [ExtRdKafka README](https://github.com/open-telemetry/opentelemetry-php-contrib/blob/main/src/Instrumentation/ExtRdKafka/README.md)
  - [opentelemetry-php-contrib PR #284 (instrumentation origin)](https://github.com/open-telemetry/opentelemetry-php-contrib/pull/284)
  - [php-rdkafka topic.stub.php ($headers optional)](https://github.com/arnaud-lb/php-rdkafka/blob/6.x/topic.stub.php)
- **OTel PHP: no php-amqplib instrumentation exists, so every RabbitMQ leg loses context** — opentelemetry-php-contrib has no instrumentation for php-amqplib; its only AMQP coverage is the PECL ext-amqp client, a different library. _[open; 18 cells]_
  - [opentelemetry-php-contrib instrumentation tree](https://github.com/open-telemetry/opentelemetry-php-contrib/tree/main/src/Instrumentation)
  - [ExtAmqp README (ext-amqp only)](https://github.com/open-telemetry/opentelemetry-php-contrib/blob/main/src/Instrumentation/ExtAmqp/README.md)
  - [opentelemetry-php #937 (cannot hook closures)](https://github.com/open-telemetry/opentelemetry-php/issues/937)

## Variant details

- **otel-java** — tracer opentelemetry-javaagent 2.31.1, java 17.0.20; libs: grpc grpc-java, http-client RestTemplate (HttpURLConnection), kafka spring-kafka, kafka-clients 3.7.2, rabbitmq spring-rabbit (amqp-client 5.21.0), spring-boot 3.3.13; config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-07
- **otel-node** — tracer @opentelemetry/auto-instrumentations-node 0.80.0, node v22.23.2; libs: express 4.22.2, grpc-js 1.14.4, http_client axios 1.20.0, kafkajs 2.2.4, rabbitmq amqplib 0.10.9; config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-07
- **otel-python** — tracer opentelemetry-distro 0.65b0, python 3.12.14; libs: confluent-kafka 2.15.0, flask 3.1.3, grpcio 1.83.1, rabbitmq pika 1.4.4, requests 2.34.2; config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-07
- **otel-go** — tracer go.opentelemetry.io/otel v1.46.0, go1.25.14; libs: grpc otelgrpc v0.71.0, http otelhttp v0.71.0, kafka franz-go v1.21.6 + kotel v1.7.0, rabbitmq amqp091-go v1.14.0 (manual otel.GetTextMapPropagator inject/extract; no official contrib); config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-07
- **otel-go-compiletime** — tracer go.opentelemetry.io/otelc v1.45.0, go1.25.14; libs: grpc google.golang.org/grpc (plain source, woven), http net/http (plain source, woven), kafka segmentio/kafka-go v0.4.51 (plain source, woven), rabbitmq amqp091-go v1.14.0 (plain source, NO instrumentation exists), weaver otelc (compile-time, no source changes); config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-07
- **otel-dotnet** — tracer OpenTelemetry.AutoInstrumentation 1.16.0, .NET 8.0.30 (CLR 8.0.30); libs: grpc Grpc.AspNetCore + Grpc.Net.Client 2.83.0, http HttpClient, kafka Confluent.Kafka 2.14.2, rabbitmq RabbitMQ.Client 7.2.2; config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-07
- **otel-ruby** — tracer opentelemetry-sdk 1.13.0, ruby 3.3.12; libs: grpc grpc 1.83.1, http-client net/http (stdlib), http-server sinatra 4.2.1 (puma 8.0.2), kafka rdkafka 0.29.1, rabbitmq bunny 3.3.0; config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-07
- **otel-php** — tracer opentelemetry (ext) open-telemetry/sdk 1.15.0 (ext-opentelemetry 1.2.1), php 8.3.33; libs: grpc ext-grpc 1.83.1 / grpc/grpc 1.82.0, http_client ext-curl 8.14.1, kafka ext-rdkafka 6.0.5, rabbitmq php-amqplib v3.7.4, server apache2 mod_php / slim 4.15.3; config: OTEL_LOGS_EXPORTER=none OTEL_METRICS_EXPORTER=none OTEL_PHP_AUTOLOAD_ENABLED=true OTEL_PROPAGATORS=tracecontext,baggage OTEL_SERVICE_NAME=relay-otel-php OTEL_TRACES_EXPORTER=none; ran 2026-09-07
- **otel-rust** — tracer opentelemetry 0.32.0, rustc 1.93.1 (01f6ddf75 2026-02-11); libs: grpc tonic 0.14.6, http_client reqwest 0.13.4, http_server axum 0.8.9, kafka rust-rdkafka 0.39.0, propagation opentelemetry 0.32.0 + opentelemetry_sdk 0.32.1 + opentelemetry-http 0.32.0, rabbitmq lapin 4.10.0; config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-07
- **datadog-java** — tracer dd-java-agent 1.65.1, java 17.0.20; libs: grpc grpc-java, http-client RestTemplate (HttpURLConnection), kafka spring-kafka, kafka-clients 3.7.2, rabbitmq spring-rabbit (amqp-client 5.21.0), spring-boot 3.3.13; config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-07
- **datadog-java-defaults** — tracer dd-java-agent 1.65.1, java 17.0.20; libs: grpc grpc-java, http-client RestTemplate (HttpURLConnection), kafka spring-kafka, kafka-clients 3.7.2, rabbitmq spring-rabbit (amqp-client 5.21.0), spring-boot 3.3.13; config: ; ran 2026-09-07
- **datadog-node** — tracer dd-trace 6.14.0, node v22.23.2; libs: express 4.22.2, grpc-js 1.14.4, http_client axios 1.20.0, kafkajs 2.2.4, rabbitmq amqplib 0.10.9; config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-07
- **datadog-node-defaults** — tracer dd-trace 6.14.0, node v22.23.2; libs: express 4.22.2, grpc-js 1.14.4, http_client axios 1.20.0, kafkajs 2.2.4, rabbitmq amqplib 0.10.9; config: ; ran 2026-09-07
- **datadog-python** — tracer ddtrace 4.14.0, python 3.12.14; libs: confluent-kafka 2.15.0, flask 3.1.3, grpcio 1.83.1, rabbitmq pika 1.4.4, requests 2.34.2; config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-07
- **datadog-python-defaults** — tracer ddtrace 4.14.0, python 3.12.14; libs: confluent-kafka 2.15.0, flask 3.1.3, grpcio 1.83.1, rabbitmq pika 1.4.4, requests 2.34.2; config: ; ran 2026-09-07
- **datadog-python-kafkaprop** — tracer ddtrace 4.14.0, python 3.12.14; libs: confluent-kafka 2.15.0, flask 3.1.3, grpcio 1.83.1, rabbitmq pika 1.4.4, requests 2.34.2; config: DD_KAFKA_PROPAGATION_ENABLED=true DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-07
- **datadog-go** — tracer dd-trace-go v2.10.1, go1.25.14; libs: grpc dd contrib google.golang.org/grpc v2.10.1, http dd contrib net/http v2.10.1, kafka dd contrib IBM/sarama v2.10.1 (sarama v1.60.2), rabbitmq amqp091-go v1.14.0 (manual tracer.Inject/Extract carrier; no dd contrib); config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-07
- **datadog-go-defaults** — tracer dd-trace-go v2.10.1, go1.25.14; libs: grpc dd contrib google.golang.org/grpc v2.10.1, http dd contrib net/http v2.10.1, kafka dd contrib IBM/sarama v2.10.1 (sarama v1.60.2), rabbitmq amqp091-go v1.14.0 (manual tracer.Inject/Extract carrier; no dd contrib); config: ; ran 2026-09-07
- **datadog-go-orchestrion** — tracer dd-trace-go v2.10.1, go1.25.14; libs: grpc google.golang.org/grpc (plain source, woven), http net/http (plain source, woven), kafka IBM/sarama v1.60.2 (plain source, woven), rabbitmq amqp091-go v1.14.0 (plain source, NO integration exists), weaver orchestrion (compile-time, no source changes); config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-07
- **datadog-dotnet** — tracer dd-trace-dotnet 3.52.0, .NET 8.0.30 (CLR 8.0.30); libs: grpc Grpc.AspNetCore + Grpc.Net.Client 2.83.0, http HttpClient, kafka Confluent.Kafka 2.14.2, rabbitmq RabbitMQ.Client 7.2.2; config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-07
- **datadog-dotnet-defaults** — tracer dd-trace-dotnet 3.52.0, .NET 8.0.30 (CLR 8.0.30); libs: grpc Grpc.AspNetCore + Grpc.Net.Client 2.83.0, http HttpClient, kafka Confluent.Kafka 2.14.2, rabbitmq RabbitMQ.Client 7.2.2; config: ; ran 2026-09-07
- **datadog-ruby** — tracer datadog (gem) 2.42.0, ruby 3.3.12; libs: grpc grpc 1.83.1, http-client net/http (stdlib), http-server sinatra 4.2.1 (puma 8.0.2), kafka karafka 2.6.1 + waterdrop 2.10.4, rabbitmq bunny 3.3.0; config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-07
- **datadog-ruby-defaults** — tracer datadog (gem) 2.42.0, ruby 3.3.12; libs: grpc grpc 1.83.1, http-client net/http (stdlib), http-server sinatra 4.2.1 (puma 8.0.2), kafka karafka 2.6.1 + waterdrop 2.10.4, rabbitmq bunny 3.3.0; config: ; ran 2026-09-07
- **datadog-php** — tracer dd-trace-php ddtrace 1.25.0, php 8.3.33; libs: grpc ext-grpc 1.83.1 / grpc/grpc 1.82.0, http_client ext-curl 8.14.1, kafka ext-rdkafka 6.0.5, rabbitmq php-amqplib v3.7.4, server apache2 mod_php / slim 4.15.3; config: DD_INSTRUMENTATION_TELEMETRY_ENABLED=false DD_REMOTE_CONFIG_ENABLED=false DD_SERVICE=relay-datadog-php DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage DD_TRACE_STARTUP_LOGS=false; ran 2026-09-07
- **datadog-rust** — tracer datadog-opentelemetry 0.5.2, rustc 1.93.1 (01f6ddf75 2026-02-11); libs: grpc tonic 0.14.6, http_client reqwest 0.13.4, http_server axum 0.8.9, kafka rust-rdkafka 0.39.0, propagation opentelemetry 0.32.0 + opentelemetry_sdk 0.32.1 + opentelemetry-http 0.32.0, rabbitmq lapin 4.10.0; config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-07
- **elastic-java** — tracer elastic-apm-agent 1.56.0, java 17.0.20; libs: grpc grpc-java, http-client RestTemplate (HttpURLConnection), kafka spring-kafka, kafka-clients 3.7.2, rabbitmq spring-rabbit (amqp-client 5.21.0), spring-boot 3.3.13; config: ; ran 2026-09-07
- **elastic-node** — tracer elastic-apm-node 4.18.0, node v22.23.2; libs: express 4.22.2, grpc-js 1.14.4, http_client axios 1.20.0, kafkajs 2.2.4, rabbitmq amqplib 0.10.9; config: ; ran 2026-09-07
- **elastic-python** — tracer elastic-apm 6.26.2, python 3.12.14; libs: confluent-kafka 2.15.0, flask 3.1.3, grpcio 1.83.1, rabbitmq pika 1.4.4, requests 2.34.2; config: ; ran 2026-09-07
- **elastic-go** — tracer go.elastic.co/apm v2.7.12, go1.25.14; libs: grpc apmgrpc v2.7.12 (unary interceptors), http apmhttp v2.7.12 (Wrap + WrapClient), kafka franz-go v1.21.6 + manual apmhttp trace headers (no elastic messaging module), rabbitmq amqp091-go v1.14.0 + manual apmhttp trace headers (no elastic messaging module); config: ; ran 2026-09-07
- **elastic-dotnet** — tracer elastic-apm-dotnet 1.34.7, .NET 8.0.30 (CLR 8.0.30); libs: grpc Grpc.AspNetCore + Grpc.Net.Client 2.83.0, http HttpClient, kafka Confluent.Kafka 2.14.2, rabbitmq RabbitMQ.Client 7.2.2; config: ; ran 2026-09-07
- **elastic-ruby** — tracer elastic-apm (gem) 4.9.0, ruby 3.3.12; libs: grpc grpc 1.83.1, http-client net/http (stdlib), http-server sinatra 4.2.1 (puma 8.0.2), kafka rdkafka 0.29.1, rabbitmq bunny 3.3.0; config: ; ran 2026-09-07
- **elastic-php** — tracer elastic-apm-php elastic_apm 1.17.0, php 8.3.33; libs: grpc ext-grpc 1.83.1 / grpc/grpc 1.82.0, http_client ext-curl 8.14.1, kafka ext-rdkafka 6.0.5, rabbitmq php-amqplib v3.7.4, server apache2 mod_php / slim 4.15.3; config: ELASTIC_APM_SERVICE_NAME=relay-elastic-php; ran 2026-09-07
- **newrelic-java** — tracer newrelic-agent 9.4.0, java 17.0.20; libs: grpc grpc-java, http-client RestTemplate (HttpURLConnection), kafka spring-kafka, kafka-clients 3.7.2, rabbitmq spring-rabbit (amqp-client 5.21.0), spring-boot 3.3.13; config: NEW_RELIC_DISTRIBUTED_TRACING_ENABLED=true; ran 2026-09-07
- **newrelic-node** — tracer newrelic (npm) 14.3.10, node v22.23.2; libs: express 4.22.2, grpc-js 1.14.4, http_client axios 1.20.0, kafkajs 2.2.4, rabbitmq amqplib 0.10.9; config: NEW_RELIC_DISTRIBUTED_TRACING_ENABLED=true; ran 2026-09-07
- **newrelic-python** — tracer newrelic (pypi) 13.5.0, python 3.12.14; libs: confluent-kafka 2.15.0, flask 3.1.3, grpcio 1.83.1, rabbitmq pika 1.4.4, requests 2.34.2; config: NEW_RELIC_DISTRIBUTED_TRACING_ENABLED=true; ran 2026-09-07
- **newrelic-go** — tracer go-agent v3.44.2, go1.25.14; libs: grpc nrgrpc v1.4.10, http newrelic.WrapHandleFunc + newrelic.NewRoundTripper (go-agent v3.44.2), kafka nrsarama v1.0.2 + manual AcceptDistributedTraceHeaders (Shopify/sarama v1.38.1), rabbitmq nramqp v1.0.3 (amqp091-go v1.14.0); config: NEW_RELIC_DISTRIBUTED_TRACING_ENABLED=true; ran 2026-09-07
- **newrelic-dotnet** — tracer newrelic-dotnet-agent 10.54.0, .NET 8.0.30 (CLR 8.0.30); libs: grpc Grpc.AspNetCore + Grpc.Net.Client 2.83.0, http HttpClient, kafka Confluent.Kafka 2.14.2, rabbitmq RabbitMQ.Client 7.2.2; config: NEW_RELIC_DISTRIBUTED_TRACING_ENABLED=true; ran 2026-09-07
- **newrelic-ruby** — tracer newrelic_rpm 10.7.1, ruby 3.3.12; libs: grpc grpc 1.83.1, http-client net/http (stdlib), http-server sinatra 4.2.1 (puma 8.0.2), kafka rdkafka 0.29.1, rabbitmq bunny 3.3.0; config: NEW_RELIC_DISTRIBUTED_TRACING_ENABLED=true; ran 2026-09-07
- **newrelic-php** — tracer newrelic-php-agent newrelic 12.10.0.39, php 8.3.33; libs: grpc ext-grpc 1.83.1 / grpc/grpc 1.82.0, http_client ext-curl 8.14.1, kafka ext-rdkafka 6.0.5, rabbitmq php-amqplib v3.7.4, server apache2 mod_php / slim 4.15.3; config: NEW_RELIC_APP_NAME=relay-newrelic-php NEW_RELIC_DISTRIBUTED_TRACING_ENABLED=true; ran 2026-09-07
- **none-java** — tracer none, java 17.0.20; libs: grpc grpc-java, http-client RestTemplate (HttpURLConnection), kafka spring-kafka, kafka-clients 3.7.2, rabbitmq spring-rabbit (amqp-client 5.21.0), spring-boot 3.3.13; config: ; ran 2026-09-07
- **none-node** — tracer none, node v22.23.2; libs: express 4.22.2, grpc-js 1.14.4, http_client axios 1.20.0, kafkajs 2.2.4, rabbitmq amqplib 0.10.9; config: ; ran 2026-09-07
- **none-python** — tracer none, python 3.12.14; libs: confluent-kafka 2.15.0, flask 3.1.3, grpcio 1.83.1, rabbitmq pika 1.4.4, requests 2.34.2; config: ; ran 2026-09-07
- **none-go** — tracer none, go1.25.14; libs: grpc google.golang.org/grpc (plain), http net/http (plain), kafka franz-go v1.21.6 (plain), rabbitmq amqp091-go v1.14.0 (plain); config: ; ran 2026-09-07
- **none-dotnet** — tracer none, .NET 8.0.30 (CLR 8.0.30); libs: grpc Grpc.AspNetCore + Grpc.Net.Client 2.83.0, http HttpClient, kafka Confluent.Kafka 2.14.2, rabbitmq RabbitMQ.Client 7.2.2; config: ; ran 2026-09-07
- **none-ruby** — tracer none, ruby 3.3.12; libs: grpc grpc 1.83.1, http-client net/http (stdlib), http-server sinatra 4.2.1 (puma 8.0.2), kafka rdkafka 0.29.1, rabbitmq bunny 3.3.0; config: ; ran 2026-09-07
- **none-php** — tracer none, php 8.3.33; libs: grpc ext-grpc 1.83.1 / grpc/grpc 1.82.0, http_client ext-curl 8.14.1, kafka ext-rdkafka 6.0.5, rabbitmq php-amqplib v3.7.4, server apache2 mod_php / slim 4.15.3; config: ; ran 2026-09-07
- **none-rust** — tracer none, rustc 1.93.1 (01f6ddf75 2026-02-11); libs: grpc tonic 0.14.6, http_client reqwest 0.13.4, http_server axum 0.8.9, kafka rust-rdkafka 0.39.0, propagation opentelemetry 0.32.0 + opentelemetry_sdk 0.32.1 + opentelemetry-http 0.32.0, rabbitmq lapin 4.10.0; config: ; ran 2026-09-07

Canary check: in all recorded runs the non-tracing control header
(`x-sd-canary`) was verified absent after the hop, proving relays re-emit
rather than proxy. Raw per-test header dumps live in `results/raw/*.json`
and, per hop, under `variants[].hops[].observed_headers` in `results.json`.
