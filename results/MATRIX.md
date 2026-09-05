# Context Propagation Support Matrix

Generated from empirical runs of this harness — every cell is the observed
result of an actual request/message hop, not a reading of vendor docs.
Regenerate with `go run ./matrixgen` after `./run.sh`.

Last generated: 2026-09-04 19:56 UTC

Boundaries are `inbound protocol → outbound protocol` through a single
instrumented service (the relay). Legend: ✅ propagated/preserved ·
🟡 header present but seeded value lost · 🔀 new trace started ·
❌ header absent · 🚫 message was never forwarded (instrumentation or app broke).

## OpenTelemetry

### W3C `baggage`

`propagated` = the seeded `sd-routing-key` baggage member arrived intact.

| variant | http→http | http→grpc | http→kafka | grpc→http | grpc→grpc | grpc→kafka | kafka→http | kafka→grpc | kafka→kafka |
|---|---|---|---|---|---|---|---|---|---|
| java · 2.31.1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 0.80.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| python · 0.65b0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ |
| go · v1.46.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet · 1.16.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ |
| ruby · 1.13.0 | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ |

### W3C `tracestate`

`preserved` = the seeded foreign vendor entry (`sdtest=v1`) survived.

| variant | http→http | http→grpc | http→kafka | grpc→http | grpc→grpc | grpc→kafka | kafka→http | kafka→grpc | kafka→kafka |
|---|---|---|---|---|---|---|---|---|---|
| java · 2.31.1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 0.80.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| python · 0.65b0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ |
| go · v1.46.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet · 1.16.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ |
| ruby · 1.13.0 | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ |

### W3C `traceparent` (trace continuity)

`same trace` = the seeded trace-id was continued (span-id may differ, as expected).

| variant | http→http | http→grpc | http→kafka | grpc→http | grpc→grpc | grpc→kafka | kafka→http | kafka→grpc | kafka→kafka |
|---|---|---|---|---|---|---|---|---|---|
| java · 2.31.1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 0.80.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| python · 0.65b0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | 🔀 | 🔀 | 🔀 |
| go · v1.46.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet · 1.16.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | 🔀 | 🔀 | 🔀 |
| ruby · 1.13.0 | ✅ | ✅ | ✅ | 🔀 | 🔀 | 🔀 | ✅ | ✅ | ✅ |

### `ot-baggage-*` (OpenTracing legacy)

`propagated` = `ot-baggage-sd-routing-key` arrived with the seeded value.

| variant | http→http | http→grpc | http→kafka | grpc→http | grpc→grpc | grpc→kafka | kafka→http | kafka→grpc | kafka→kafka |
|---|---|---|---|---|---|---|---|---|---|
| java · 2.31.1 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| node · 0.80.0 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python · 0.65b0 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go · v1.46.0 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet · 1.16.0 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby · 1.13.0 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

## Datadog (`dd-trace`)

### W3C `baggage`

`propagated` = the seeded `sd-routing-key` baggage member arrived intact.

| variant | http→http | http→grpc | http→kafka | grpc→http | grpc→grpc | grpc→kafka | kafka→http | kafka→grpc | kafka→kafka |
|---|---|---|---|---|---|---|---|---|---|
| java · 1.65.1 | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| java (defaults) · 1.65.1 | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| node · 6.14.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node (defaults) · 6.14.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| python · 4.14.0 | ✅ | ✅ | ❌ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| python (defaults) · 4.14.0 | ✅ | ✅ | ❌ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| python (kafkaprop) · 4.14.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ |
| go · v2.10.1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| go (defaults) · v2.10.1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet · 3.52.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet (defaults) · 3.52.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| ruby · 2.42.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| ruby (defaults) · 2.42.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

### W3C `tracestate`

`preserved` = the seeded foreign vendor entry (`sdtest=v1`) survived.

| variant | http→http | http→grpc | http→kafka | grpc→http | grpc→grpc | grpc→kafka | kafka→http | kafka→grpc | kafka→kafka |
|---|---|---|---|---|---|---|---|---|---|
| java · 1.65.1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| java (defaults) · 1.65.1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 6.14.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node (defaults) · 6.14.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| python · 4.14.0 | ✅ | ✅ | ❌ | ✅ | ✅ | ❌ | 🟡 | 🟡 | ❌ |
| python (defaults) · 4.14.0 | ✅ | ✅ | ❌ | ✅ | ✅ | ❌ | 🟡 | 🟡 | ❌ |
| python (kafkaprop) · 4.14.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | 🟡 | 🟡 | 🟡 |
| go · v2.10.1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| go (defaults) · v2.10.1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet · 3.52.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet (defaults) · 3.52.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| ruby · 2.42.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| ruby (defaults) · 2.42.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

### W3C `traceparent` (trace continuity)

`same trace` = the seeded trace-id was continued (span-id may differ, as expected).

| variant | http→http | http→grpc | http→kafka | grpc→http | grpc→grpc | grpc→kafka | kafka→http | kafka→grpc | kafka→kafka |
|---|---|---|---|---|---|---|---|---|---|
| java · 1.65.1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| java (defaults) · 1.65.1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 6.14.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node (defaults) · 6.14.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| python · 4.14.0 | ✅ | ✅ | ❌ | ✅ | ✅ | ❌ | 🔀 | 🔀 | ❌ |
| python (defaults) · 4.14.0 | ✅ | ✅ | ❌ | ✅ | ✅ | ❌ | 🔀 | 🔀 | ❌ |
| python (kafkaprop) · 4.14.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | 🔀 | 🔀 | 🔀 |
| go · v2.10.1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| go (defaults) · v2.10.1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet · 3.52.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet (defaults) · 3.52.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| ruby · 2.42.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| ruby (defaults) · 2.42.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

### `ot-baggage-*` (OpenTracing legacy)

`propagated` = `ot-baggage-sd-routing-key` arrived with the seeded value.

| variant | http→http | http→grpc | http→kafka | grpc→http | grpc→grpc | grpc→kafka | kafka→http | kafka→grpc | kafka→kafka |
|---|---|---|---|---|---|---|---|---|---|
| java · 1.65.1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| java (defaults) · 1.65.1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node · 6.14.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| node (defaults) · 6.14.0 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| python · 4.14.0 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python (defaults) · 4.14.0 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| python (kafkaprop) · 4.14.0 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| go · v2.10.1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| go (defaults) · v2.10.1 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| dotnet · 3.52.0 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| dotnet (defaults) · 3.52.0 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby · 2.42.0 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| ruby (defaults) · 2.42.0 | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

## New Relic

_No results yet._

## Variant details

- **datadog-java** — tracer 1.65.1, java 17.0.20; libs: grpc grpc-java, http-client RestTemplate (HttpURLConnection), kafka spring-kafka, kafka-clients 3.7.2, spring-boot 3.3.13; config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-04
- **datadog-java-defaults** — tracer 1.65.1, java 17.0.20; libs: grpc grpc-java, http-client RestTemplate (HttpURLConnection), kafka spring-kafka, kafka-clients 3.7.2, spring-boot 3.3.13; config: ; ran 2026-09-04
- **datadog-node** — tracer 6.14.0, node v22.23.2; libs: express 4.22.2, grpc-js 1.14.4, http_client axios 1.20.0, kafkajs 2.2.4; config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-04
- **datadog-node-defaults** — tracer 6.14.0, node v22.23.2; libs: express 4.22.2, grpc-js 1.14.4, http_client axios 1.20.0, kafkajs 2.2.4; config: ; ran 2026-09-04
- **datadog-python** — tracer 4.14.0, python 3.12.14; libs: confluent-kafka 2.15.0, flask 3.1.3, grpcio 1.83.1, requests 2.34.2; config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-04
- **datadog-python-defaults** — tracer 4.14.0, python 3.12.14; libs: confluent-kafka 2.15.0, flask 3.1.3, grpcio 1.83.1, requests 2.34.2; config: ; ran 2026-09-04
- **datadog-python-kafkaprop** — tracer 4.14.0, python 3.12.14; libs: confluent-kafka 2.15.0, flask 3.1.3, grpcio 1.83.1, requests 2.34.2; config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-04
- **datadog-go** — tracer v2.10.1, go1.25.14; libs: grpc dd contrib google.golang.org/grpc v2.10.1, http dd contrib net/http v2.10.1, kafka dd contrib IBM/sarama v2.10.1 (sarama v1.60.2); config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-04
- **datadog-go-defaults** — tracer v2.10.1, go1.25.14; libs: grpc dd contrib google.golang.org/grpc v2.10.1, http dd contrib net/http v2.10.1, kafka dd contrib IBM/sarama v2.10.1 (sarama v1.60.2); config: ; ran 2026-09-04
- **datadog-dotnet** — tracer 3.52.0, .NET 8.0.30 (CLR 8.0.30); libs: grpc Grpc.AspNetCore + Grpc.Net.Client 2.83.0, http HttpClient, kafka Confluent.Kafka 2.14.2; config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-04
- **datadog-dotnet-defaults** — tracer 3.52.0, .NET 8.0.30 (CLR 8.0.30); libs: grpc Grpc.AspNetCore + Grpc.Net.Client 2.83.0, http HttpClient, kafka Confluent.Kafka 2.14.2; config: ; ran 2026-09-04
- **datadog-ruby** — tracer 2.42.0, ruby 3.3.12; libs: grpc grpc 1.83.1, http-client net/http (stdlib), http-server sinatra 4.2.1 (puma 8.0.2), kafka karafka 2.6.1 + waterdrop 2.10.4; config: DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage; ran 2026-09-04
- **datadog-ruby-defaults** — tracer 2.42.0, ruby 3.3.12; libs: grpc grpc 1.83.1, http-client net/http (stdlib), http-server sinatra 4.2.1 (puma 8.0.2), kafka karafka 2.6.1 + waterdrop 2.10.4; config: ; ran 2026-09-04
- **otel-java** — tracer 2.31.1, java 17.0.20; libs: grpc grpc-java, http-client RestTemplate (HttpURLConnection), kafka spring-kafka, kafka-clients 3.7.2, spring-boot 3.3.13; config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-04
- **otel-node** — tracer 0.80.0, node v22.23.2; libs: express 4.22.2, grpc-js 1.14.4, http_client axios 1.20.0, kafkajs 2.2.4; config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-04
- **otel-python** — tracer 0.65b0, python 3.12.14; libs: confluent-kafka 2.15.0, flask 3.1.3, grpcio 1.83.1, requests 2.34.2; config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-04
- **otel-go** — tracer v1.46.0, go1.25.14; libs: grpc otelgrpc v0.71.0, http otelhttp v0.71.0, kafka franz-go v1.21.6 + kotel v1.7.0; config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-04
- **otel-dotnet** — tracer 1.16.0, .NET 8.0.30 (CLR 8.0.30); libs: grpc Grpc.AspNetCore + Grpc.Net.Client 2.83.0, http HttpClient, kafka Confluent.Kafka 2.14.2; config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-04
- **otel-ruby** — tracer 1.13.0, ruby 3.3.12; libs: grpc grpc 1.83.1, http-client net/http (stdlib), http-server sinatra 4.2.1 (puma 8.0.2), kafka rdkafka 0.29.1; config: OTEL_PROPAGATORS=tracecontext,baggage OTEL_TRACES_EXPORTER=none; ran 2026-09-04

Canary check: in all recorded runs the non-tracing control header
(`x-sd-canary`) was verified absent after the hop, proving relays re-emit
rather than proxy. Raw per-test header dumps live in `results/raw/*.json`.
