# Context Propagation Support Matrix

Generated from empirical runs of this harness — every cell is the observed
result of an actual request/message hop, not a reading of vendor docs.
Regenerate with `go run ./matrixgen` after `./run.sh`.

Last generated: 2026-09-07 11:15 UTC

Boundaries are `inbound protocol → outbound protocol` through a single
instrumented service (the relay). Legend: ✅ propagated/preserved ·
🟡 header present but seeded value lost · 🔀 new trace started ·
❌ header absent · 🚫 message was never forwarded (instrumentation or app broke).

The `setup` column says what attaching the tool required: `zero-code`
(runtime attach only — agent flag, preload require, CLR profiler, php.ini;
no source involved) · `init-only` (a boot-file initializer, request-path
code untouched) · `in-code` (wrappers and carriers woven into application
code, contexts threaded by hand). See DESIGN.md and the relay READMEs.

## OpenTelemetry

### W3C `baggage`

`propagated` = the seeded `sd-routing-key` baggage member arrived intact.

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 2.31.1 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 0.80.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| python · 0.65b0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ |
| go · v1.46.0 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet · 1.16.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby · 1.13.0 | init-only | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| php · open-telemetry/sdk 1.15.0 (ext-opentelemetry 1.2.1) | zero-code | ✅ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| rust · 0.32.0 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

### W3C `tracestate`

`preserved` = the seeded foreign vendor entry (`sdtest=v1`) survived.

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 2.31.1 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 0.80.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| python · 0.65b0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ |
| go · v1.46.0 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet · 1.16.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ |
| ruby · 1.13.0 | init-only | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| php · open-telemetry/sdk 1.15.0 (ext-opentelemetry 1.2.1) | zero-code | ✅ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| rust · 0.32.0 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

### W3C `traceparent` (trace continuity)

`same trace` = the seeded trace-id was continued (span-id may differ, as expected).

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 2.31.1 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 0.80.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| python · 0.65b0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | 🔀 | 🔀 | 🔀 | 🔀 | ✅ | ✅ | ✅ | ✅ |
| go · v1.46.0 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet · 1.16.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | 🔀 | 🔀 | 🔀 | 🔀 | ✅ | ✅ | ✅ | ✅ |
| ruby · 1.13.0 | init-only | ✅ | ✅ | ✅ | ✅ | 🔀 | 🔀 | 🔀 | 🔀 | ✅ | ✅ | ✅ | ✅ | 🔀 | 🔀 | 🔀 | 🔀 |
| php · open-telemetry/sdk 1.15.0 (ext-opentelemetry 1.2.1) | zero-code | ✅ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ✅ | ❌ | ❌ | ❌ | 🔀 | ❌ | ❌ | ❌ |
| rust · 0.32.0 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

### `ot-baggage-*` (OpenTracing legacy)

`propagated` = `ot-baggage-sd-routing-key` arrived with the seeded value.

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 2.31.1 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| node · 0.80.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python · 0.65b0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go · v1.46.0 | in-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet · 1.16.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby · 1.13.0 | init-only | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php · open-telemetry/sdk 1.15.0 (ext-opentelemetry 1.2.1) | zero-code | ❌ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| rust · 0.32.0 | in-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

## Datadog (`dd-trace`)

### W3C `baggage`

`propagated` = the seeded `sd-routing-key` baggage member arrived intact.

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
| dotnet · 3.52.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet (defaults) · 3.52.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| ruby · 2.42.0 | init-only | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby (defaults) · 2.42.0 | init-only | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php · ddtrace 1.25.0 | zero-code | ✅ | ❌ | ✅ | ✅ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ✅ | ❌ | ✅ | ✅ |
| rust · 0.5.2 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

### W3C `tracestate`

`preserved` = the seeded foreign vendor entry (`sdtest=v1`) survived.

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
| dotnet · 3.52.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet (defaults) · 3.52.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| ruby · 2.42.0 | init-only | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | 🟡 | 🟡 | 🟡 | ❌ |
| ruby (defaults) · 2.42.0 | init-only | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | 🟡 | 🟡 | 🟡 | ❌ |
| php · ddtrace 1.25.0 | zero-code | ✅ | ❌ | ✅ | ✅ | 🚫 | 🚫 | 🚫 | 🚫 | 🟡 | ❌ | 🟡 | 🟡 | ✅ | ❌ | ✅ | ✅ |
| rust · 0.5.2 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

### W3C `traceparent` (trace continuity)

`same trace` = the seeded trace-id was continued (span-id may differ, as expected).

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
| dotnet · 3.52.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet (defaults) · 3.52.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| ruby · 2.42.0 | init-only | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | 🔀 | 🔀 | 🔀 | ❌ |
| ruby (defaults) · 2.42.0 | init-only | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | 🔀 | 🔀 | 🔀 | ❌ |
| php · ddtrace 1.25.0 | zero-code | ✅ | ❌ | ✅ | ✅ | 🚫 | 🚫 | 🚫 | 🚫 | 🔀 | ❌ | 🔀 | 🔀 | ✅ | ❌ | ✅ | ✅ |
| rust · 0.5.2 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

### `ot-baggage-*` (OpenTracing legacy)

`propagated` = `ot-baggage-sd-routing-key` arrived with the seeded value.

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
| dotnet · 3.52.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet (defaults) · 3.52.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby · 2.42.0 | init-only | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby (defaults) · 2.42.0 | init-only | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php · ddtrace 1.25.0 | zero-code | ❌ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| rust · 0.5.2 | in-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

## Elastic APM (classic agents)

### W3C `baggage`

`propagated` = the seeded `sd-routing-key` baggage member arrived intact.

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 1.56.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 4.18.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python · 6.26.2 | init-only | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go · v2.7.12 | in-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet · 1.34.7 | zero-code | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby · 4.9.0 | init-only | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php · elastic_apm 1.17.0 | zero-code | ❌ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

### W3C `tracestate`

`preserved` = the seeded foreign vendor entry (`sdtest=v1`) survived.

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 1.56.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 4.18.0 | zero-code | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python · 6.26.2 | init-only | ✅ | ✅ | ❌ | ❌ | 🟡 | 🟡 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go · v2.7.12 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet · 1.34.7 | zero-code | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ | ❌ | 🟡 | ✅ | ✅ | ❌ | ✅ |
| ruby · 4.9.0 | init-only | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php · elastic_apm 1.17.0 | zero-code | ✅ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | 🟡 | ❌ | ❌ | ❌ | 🟡 | ❌ | ❌ | ❌ |

### W3C `traceparent` (trace continuity)

`same trace` = the seeded trace-id was continued (span-id may differ, as expected).

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 1.56.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 4.18.0 | zero-code | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python · 6.26.2 | init-only | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go · v2.7.12 | in-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet · 1.34.7 | zero-code | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | 🔀 | 🔀 | ❌ | 🔀 | ✅ | ✅ | ❌ | ✅ |
| ruby · 4.9.0 | init-only | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php · elastic_apm 1.17.0 | zero-code | ✅ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | 🔀 | ❌ | ❌ | ❌ | 🔀 | ❌ | ❌ | ❌ |

### `ot-baggage-*` (OpenTracing legacy)

`propagated` = `ot-baggage-sd-routing-key` arrived with the seeded value.

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

### W3C `baggage`

`propagated` = the seeded `sd-routing-key` baggage member arrived intact.

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 9.4.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| node · 14.3.10 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python · 13.5.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go · v3.44.2 | in-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet · 10.54.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby · 10.7.1 | init-only | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php · newrelic 12.10.0.39 | zero-code | ❌ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

### W3C `tracestate`

`preserved` = the seeded foreign vendor entry (`sdtest=v1`) survived.

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 9.4.0 | zero-code | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| node · 14.3.10 | zero-code | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ | ❌ | ✅ |
| python · 13.5.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| go · v3.44.2 | in-code | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ |
| dotnet · 10.54.0 | zero-code | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby · 10.7.1 | init-only | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ |
| php · newrelic 12.10.0.39 | zero-code | ✅ | ❌ | ❌ | ✅ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

### W3C `traceparent` (trace continuity)

`same trace` = the seeded trace-id was continued (span-id may differ, as expected).

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 9.4.0 | zero-code | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| node · 14.3.10 | zero-code | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ | ❌ | ✅ |
| python · 13.5.0 | zero-code | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| go · v3.44.2 | in-code | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ |
| dotnet · 10.54.0 | zero-code | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby · 10.7.1 | init-only | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ |
| php · newrelic 12.10.0.39 | zero-code | ✅ | ❌ | ❌ | ✅ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

### `ot-baggage-*` (OpenTracing legacy)

`propagated` = `ot-baggage-sd-routing-key` arrived with the seeded value.

| variant | setup | http→http | http→grpc | http→kafka | http→rmq | grpc→http | grpc→grpc | grpc→kafka | grpc→rmq | kafka→http | kafka→grpc | kafka→kafka | kafka→rmq | rmq→http | rmq→grpc | rmq→kafka | rmq→rmq |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| java · 9.4.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| node · 14.3.10 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python · 13.5.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go · v3.44.2 | in-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet · 10.54.0 | zero-code | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby · 10.7.1 | init-only | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| php · newrelic 12.10.0.39 | zero-code | ❌ | ❌ | ❌ | ❌ | 🚫 | 🚫 | 🚫 | 🚫 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

## Baseline (no instrumentation attached)

### W3C `baggage`

`propagated` = the seeded `sd-routing-key` baggage member arrived intact.

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

### W3C `tracestate`

`preserved` = the seeded foreign vendor entry (`sdtest=v1`) survived.

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

### W3C `traceparent` (trace continuity)

`same trace` = the seeded trace-id was continued (span-id may differ, as expected).

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

### `ot-baggage-*` (OpenTracing legacy)

`propagated` = `ot-baggage-sd-routing-key` arrived with the seeded value.

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

## Variant details

- **datadog-java** — tracer 1.65.1, java 17.0.20; libs: grpc grpc-java, http-client RestTemplate (HttpURLConnection), kafka spring-kafka, kafka-clients 3.7.2, rabbitmq spring-rabbit (amqp-client 5.21.0), spring-boot 3.3.13; config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-07
- **datadog-java-defaults** — tracer 1.65.1, java 17.0.20; libs: grpc grpc-java, http-client RestTemplate (HttpURLConnection), kafka spring-kafka, kafka-clients 3.7.2, rabbitmq spring-rabbit (amqp-client 5.21.0), spring-boot 3.3.13; config: ; ran 2026-09-07
- **datadog-node** — tracer 6.14.0, node v22.23.2; libs: express 4.22.2, grpc-js 1.14.4, http_client axios 1.20.0, kafkajs 2.2.4, rabbitmq amqplib 0.10.9; config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-07
- **datadog-node-defaults** — tracer 6.14.0, node v22.23.2; libs: express 4.22.2, grpc-js 1.14.4, http_client axios 1.20.0, kafkajs 2.2.4, rabbitmq amqplib 0.10.9; config: ; ran 2026-09-07
- **datadog-python** — tracer 4.14.0, python 3.12.14; libs: confluent-kafka 2.15.0, flask 3.1.3, grpcio 1.83.1, rabbitmq pika 1.4.4, requests 2.34.2; config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-07
- **datadog-python-defaults** — tracer 4.14.0, python 3.12.14; libs: confluent-kafka 2.15.0, flask 3.1.3, grpcio 1.83.1, rabbitmq pika 1.4.4, requests 2.34.2; config: ; ran 2026-09-07
- **datadog-python-kafkaprop** — tracer 4.14.0, python 3.12.14; libs: confluent-kafka 2.15.0, flask 3.1.3, grpcio 1.83.1, rabbitmq pika 1.4.4, requests 2.34.2; config: DD_KAFKA_PROPAGATION_ENABLED=true DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-07
- **datadog-go** — tracer v2.10.1, go1.25.14; libs: grpc dd contrib google.golang.org/grpc v2.10.1, http dd contrib net/http v2.10.1, kafka dd contrib IBM/sarama v2.10.1 (sarama v1.60.2), rabbitmq amqp091-go v1.14.0 (manual tracer.Inject/Extract carrier; no dd contrib); config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-07
- **datadog-go-defaults** — tracer v2.10.1, go1.25.14; libs: grpc dd contrib google.golang.org/grpc v2.10.1, http dd contrib net/http v2.10.1, kafka dd contrib IBM/sarama v2.10.1 (sarama v1.60.2), rabbitmq amqp091-go v1.14.0 (manual tracer.Inject/Extract carrier; no dd contrib); config: ; ran 2026-09-07
- **datadog-dotnet** — tracer 3.52.0, .NET 8.0.30 (CLR 8.0.30); libs: grpc Grpc.AspNetCore + Grpc.Net.Client 2.83.0, http HttpClient, kafka Confluent.Kafka 2.14.2, rabbitmq RabbitMQ.Client 7.2.2; config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-07
- **datadog-dotnet-defaults** — tracer 3.52.0, .NET 8.0.30 (CLR 8.0.30); libs: grpc Grpc.AspNetCore + Grpc.Net.Client 2.83.0, http HttpClient, kafka Confluent.Kafka 2.14.2, rabbitmq RabbitMQ.Client 7.2.2; config: ; ran 2026-09-07
- **datadog-ruby** — tracer 2.42.0, ruby 3.3.12; libs: grpc grpc 1.83.1, http-client net/http (stdlib), http-server sinatra 4.2.1 (puma 8.0.2), kafka karafka 2.6.1 + waterdrop 2.10.4, rabbitmq bunny 3.3.0; config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-07
- **datadog-ruby-defaults** — tracer 2.42.0, ruby 3.3.12; libs: grpc grpc 1.83.1, http-client net/http (stdlib), http-server sinatra 4.2.1 (puma 8.0.2), kafka karafka 2.6.1 + waterdrop 2.10.4, rabbitmq bunny 3.3.0; config: ; ran 2026-09-07
- **datadog-php** — tracer ddtrace 1.25.0, php 8.3.33; libs: grpc ext-grpc 1.83.1 / grpc/grpc 1.82.0, http_client ext-curl 8.14.1, kafka ext-rdkafka 6.0.5, rabbitmq php-amqplib v3.7.4, server apache2 mod_php / slim 4.15.3; config: DD_INSTRUMENTATION_TELEMETRY_ENABLED=false DD_REMOTE_CONFIG_ENABLED=false DD_SERVICE=relay-datadog-php DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage DD_TRACE_STARTUP_LOGS=false; ran 2026-09-07
- **datadog-rust** — tracer 0.5.2, rustc 1.93.1 (01f6ddf75 2026-02-11); libs: grpc tonic 0.14.6, http_client reqwest 0.13.4, http_server axum 0.8.9, kafka rust-rdkafka 0.39.0, propagation opentelemetry 0.32.0 + opentelemetry_sdk 0.32.1 + opentelemetry-http 0.32.0, rabbitmq lapin 4.10.0; config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-07
- **elastic-java** — tracer 1.56.0, java 17.0.20; libs: grpc grpc-java, http-client RestTemplate (HttpURLConnection), kafka spring-kafka, kafka-clients 3.7.2, rabbitmq spring-rabbit (amqp-client 5.21.0), spring-boot 3.3.13; config: ; ran 2026-09-07
- **elastic-node** — tracer 4.18.0, node v22.23.2; libs: express 4.22.2, grpc-js 1.14.4, http_client axios 1.20.0, kafkajs 2.2.4, rabbitmq amqplib 0.10.9; config: ; ran 2026-09-07
- **elastic-python** — tracer 6.26.2, python 3.12.14; libs: confluent-kafka 2.15.0, flask 3.1.3, grpcio 1.83.1, rabbitmq pika 1.4.4, requests 2.34.2; config: ; ran 2026-09-07
- **elastic-go** — tracer v2.7.12, go1.25.14; libs: grpc apmgrpc v2.7.12 (unary interceptors), http apmhttp v2.7.12 (Wrap + WrapClient), kafka franz-go v1.21.6 + manual apmhttp trace headers (no elastic messaging module), rabbitmq amqp091-go v1.14.0 + manual apmhttp trace headers (no elastic messaging module); config: ; ran 2026-09-07
- **elastic-dotnet** — tracer 1.34.7, .NET 8.0.30 (CLR 8.0.30); libs: grpc Grpc.AspNetCore + Grpc.Net.Client 2.83.0, http HttpClient, kafka Confluent.Kafka 2.14.2, rabbitmq RabbitMQ.Client 7.2.2; config: ; ran 2026-09-07
- **elastic-ruby** — tracer 4.9.0, ruby 3.3.12; libs: grpc grpc 1.83.1, http-client net/http (stdlib), http-server sinatra 4.2.1 (puma 8.0.2), kafka rdkafka 0.29.1, rabbitmq bunny 3.3.0; config: ; ran 2026-09-07
- **elastic-php** — tracer elastic_apm 1.17.0, php 8.3.33; libs: grpc ext-grpc 1.83.1 / grpc/grpc 1.82.0, http_client ext-curl 8.14.1, kafka ext-rdkafka 6.0.5, rabbitmq php-amqplib v3.7.4, server apache2 mod_php / slim 4.15.3; config: ELASTIC_APM_SERVICE_NAME=relay-elastic-php; ran 2026-09-07
- **newrelic-java** — tracer 9.4.0, java 17.0.20; libs: grpc grpc-java, http-client RestTemplate (HttpURLConnection), kafka spring-kafka, kafka-clients 3.7.2, rabbitmq spring-rabbit (amqp-client 5.21.0), spring-boot 3.3.13; config: NEW_RELIC_DISTRIBUTED_TRACING_ENABLED=true; ran 2026-09-07
- **newrelic-node** — tracer 14.3.10, node v22.23.2; libs: express 4.22.2, grpc-js 1.14.4, http_client axios 1.20.0, kafkajs 2.2.4, rabbitmq amqplib 0.10.9; config: NEW_RELIC_DISTRIBUTED_TRACING_ENABLED=true; ran 2026-09-07
- **newrelic-python** — tracer 13.5.0, python 3.12.14; libs: confluent-kafka 2.15.0, flask 3.1.3, grpcio 1.83.1, rabbitmq pika 1.4.4, requests 2.34.2; config: NEW_RELIC_DISTRIBUTED_TRACING_ENABLED=true; ran 2026-09-07
- **newrelic-go** — tracer v3.44.2, go1.25.14; libs: grpc nrgrpc v1.4.10, http newrelic.WrapHandleFunc + newrelic.NewRoundTripper (go-agent v3.44.2), kafka nrsarama v1.0.2 + manual AcceptDistributedTraceHeaders (Shopify/sarama v1.38.1), rabbitmq nramqp v1.0.3 (amqp091-go v1.14.0); config: NEW_RELIC_DISTRIBUTED_TRACING_ENABLED=true; ran 2026-09-07
- **newrelic-dotnet** — tracer 10.54.0, .NET 8.0.30 (CLR 8.0.30); libs: grpc Grpc.AspNetCore + Grpc.Net.Client 2.83.0, http HttpClient, kafka Confluent.Kafka 2.14.2, rabbitmq RabbitMQ.Client 7.2.2; config: NEW_RELIC_DISTRIBUTED_TRACING_ENABLED=true; ran 2026-09-07
- **newrelic-ruby** — tracer 10.7.1, ruby 3.3.12; libs: grpc grpc 1.83.1, http-client net/http (stdlib), http-server sinatra 4.2.1 (puma 8.0.2), kafka rdkafka 0.29.1, rabbitmq bunny 3.3.0; config: NEW_RELIC_DISTRIBUTED_TRACING_ENABLED=true; ran 2026-09-07
- **newrelic-php** — tracer newrelic 12.10.0.39, php 8.3.33; libs: grpc ext-grpc 1.83.1 / grpc/grpc 1.82.0, http_client ext-curl 8.14.1, kafka ext-rdkafka 6.0.5, rabbitmq php-amqplib v3.7.4, server apache2 mod_php / slim 4.15.3; config: NEW_RELIC_APP_NAME=relay-newrelic-php NEW_RELIC_DISTRIBUTED_TRACING_ENABLED=true; ran 2026-09-07
- **none-java** — tracer unknown, java 17.0.20; libs: grpc grpc-java, http-client RestTemplate (HttpURLConnection), kafka spring-kafka, kafka-clients 3.7.2, rabbitmq spring-rabbit (amqp-client 5.21.0), spring-boot 3.3.13; config: ; ran 2026-09-07
- **none-node** — tracer unknown, node v22.23.2; libs: express 4.22.2, grpc-js 1.14.4, http_client axios 1.20.0, kafkajs 2.2.4, rabbitmq amqplib 0.10.9; config: ; ran 2026-09-07
- **none-python** — tracer unknown, python 3.12.14; libs: confluent-kafka 2.15.0, flask 3.1.3, grpcio 1.83.1, rabbitmq pika 1.4.4, requests 2.34.2; config: ; ran 2026-09-07
- **none-go** — tracer none, go1.25.14; libs: grpc google.golang.org/grpc (plain), http net/http (plain), kafka franz-go v1.21.6 (plain), rabbitmq amqp091-go v1.14.0 (plain); config: ; ran 2026-09-07
- **none-dotnet** — tracer unknown, .NET 8.0.30 (CLR 8.0.30); libs: grpc Grpc.AspNetCore + Grpc.Net.Client 2.83.0, http HttpClient, kafka Confluent.Kafka 2.14.2, rabbitmq RabbitMQ.Client 7.2.2; config: ; ran 2026-09-07
- **none-ruby** — tracer none, ruby 3.3.12; libs: grpc grpc 1.83.1, http-client net/http (stdlib), http-server sinatra 4.2.1 (puma 8.0.2), kafka rdkafka 0.29.1, rabbitmq bunny 3.3.0; config: ; ran 2026-09-07
- **none-php** — tracer none, php 8.3.33; libs: grpc ext-grpc 1.83.1 / grpc/grpc 1.82.0, http_client ext-curl 8.14.1, kafka ext-rdkafka 6.0.5, rabbitmq php-amqplib v3.7.4, server apache2 mod_php / slim 4.15.3; config: ; ran 2026-09-07
- **none-rust** — tracer none, rustc 1.93.1 (01f6ddf75 2026-02-11); libs: grpc tonic 0.14.6, http_client reqwest 0.13.4, http_server axum 0.8.9, kafka rust-rdkafka 0.39.0, propagation opentelemetry 0.32.0 + opentelemetry_sdk 0.32.1 + opentelemetry-http 0.32.0, rabbitmq lapin 4.10.0; config: ; ran 2026-09-07
- **otel-java** — tracer 2.31.1, java 17.0.20; libs: grpc grpc-java, http-client RestTemplate (HttpURLConnection), kafka spring-kafka, kafka-clients 3.7.2, rabbitmq spring-rabbit (amqp-client 5.21.0), spring-boot 3.3.13; config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-07
- **otel-node** — tracer 0.80.0, node v22.23.2; libs: express 4.22.2, grpc-js 1.14.4, http_client axios 1.20.0, kafkajs 2.2.4, rabbitmq amqplib 0.10.9; config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-07
- **otel-python** — tracer 0.65b0, python 3.12.14; libs: confluent-kafka 2.15.0, flask 3.1.3, grpcio 1.83.1, rabbitmq pika 1.4.4, requests 2.34.2; config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-07
- **otel-go** — tracer v1.46.0, go1.25.14; libs: grpc otelgrpc v0.71.0, http otelhttp v0.71.0, kafka franz-go v1.21.6 + kotel v1.7.0, rabbitmq amqp091-go v1.14.0 (manual otel.GetTextMapPropagator inject/extract; no official contrib); config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-07
- **otel-dotnet** — tracer 1.16.0, .NET 8.0.30 (CLR 8.0.30); libs: grpc Grpc.AspNetCore + Grpc.Net.Client 2.83.0, http HttpClient, kafka Confluent.Kafka 2.14.2, rabbitmq RabbitMQ.Client 7.2.2; config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-07
- **otel-ruby** — tracer 1.13.0, ruby 3.3.12; libs: grpc grpc 1.83.1, http-client net/http (stdlib), http-server sinatra 4.2.1 (puma 8.0.2), kafka rdkafka 0.29.1, rabbitmq bunny 3.3.0; config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-07
- **otel-php** — tracer open-telemetry/sdk 1.15.0 (ext-opentelemetry 1.2.1), php 8.3.33; libs: grpc ext-grpc 1.83.1 / grpc/grpc 1.82.0, http_client ext-curl 8.14.1, kafka ext-rdkafka 6.0.5, rabbitmq php-amqplib v3.7.4, server apache2 mod_php / slim 4.15.3; config: OTEL_LOGS_EXPORTER=none OTEL_METRICS_EXPORTER=none OTEL_PHP_AUTOLOAD_ENABLED=true OTEL_PROPAGATORS=tracecontext,baggage OTEL_SERVICE_NAME=relay-otel-php OTEL_TRACES_EXPORTER=none; ran 2026-09-07
- **otel-rust** — tracer 0.32.0, rustc 1.93.1 (01f6ddf75 2026-02-11); libs: grpc tonic 0.14.6, http_client reqwest 0.13.4, http_server axum 0.8.9, kafka rust-rdkafka 0.39.0, propagation opentelemetry 0.32.0 + opentelemetry_sdk 0.32.1 + opentelemetry-http 0.32.0, rabbitmq lapin 4.10.0; config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-07

Canary check: in all recorded runs the non-tracing control header
(`x-sd-canary`) was verified absent after the hop, proving relays re-emit
rather than proxy. Raw per-test header dumps live in `results/raw/*.json`.
