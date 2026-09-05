# Findings — root causes and upstream references

Empirical results are in [`results/MATRIX.md`](results/MATRIX.md) (raw header
dumps per test in `results/raw/*.json`). This file documents *why* the notable
cells look the way they do, with upstream citations. Runs dated 2026-09-04;
tracer versions as recorded per variant.

## 1. Datadog Java: W3C `baggage` survives only HTTP→HTTP

**Observed** (dd-trace-java 1.65.1, `DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage`
and also with defaults): `baggage` propagated on http→http only; absent on the
other 8 boundaries. `traceparent`, `tracestate`, `ot-baggage-*`, and
`x-datadog-*` propagate on **all 9**.

**Root cause**: Kafka producer (and non-HTTP client) instrumentation injects
from the *span* rather than the *current context*
(`defaultPropagator().inject(span, ...)`); baggage lives on the thread-local
context, and `AgentSpan.get()` delegates non-span context keys to
`Context.root()`, so the baggage propagator finds nothing. Reported by us:
[dd-trace-java#11286](https://github.com/DataDog/dd-trace-java/issues/11286).
Datadog's response confirms scoping: "W3C baggage propagation was specifically
scoped to HTTP to begin with … extending it to other transports is a feature
and not a bug", citing the absence of a W3C spec for non-HTTP carriers.
Groundwork exists (context/tracing decoupling in
[PR #8330](https://github.com/DataDog/dd-trace-java/pull/8330) and
[PR #10767](https://github.com/DataDog/dd-trace-java/pull/10767), milestone
1.61.0) but the inject sites were still span-centric at 1.65.1.

**Contrast**: dd-trace-js 6.14.0, dd-trace-go v2.10.1, dd-trace-dotnet 3.52.0
and dd-trace-rb 2.42.0 all propagate W3C baggage across **all 9** boundaries —
the gap is implementation-specific, not a Datadog design position.

## 2. Datadog Python: no Kafka header injection at all by default

**Observed** (ddtrace 4.14.0): with default config, *no propagation headers of
any kind* are injected into produced Kafka records (`*→kafka` cells:
everything absent). Setting `DD_KAFKA_PROPAGATION_ENABLED=true` turns on full
W3C injection *including baggage* (`*→kafka` all ✅), but consumer-side
continuation is still broken: `kafka→*` starts a new trace and drops baggage.

**Root cause / reference**: documented default —
`ddtrace.config.kafka["distributed_tracing_enabled"]`, "Default: False", env
`DD_KAFKA_PROPAGATION_ENABLED`
([ddtrace integrations docs](https://ddtrace.readthedocs.io/en/stable/integrations.html)).
The consumer integration traces `poll()` but does not activate the extracted
context for processing code.

**Also**: dd-trace-py never emits `ot-baggage-*` (nor do dd-trace-dotnet and
dd-trace-rb); only dd-trace-java and dd-trace-js carry the OpenTracing legacy
headers.

## 3. OpenTelemetry Python: all Kafka-inbound boundaries lose context

**Observed** (instrumentation 0.65b0, confluent-kafka): kafka→http,
kafka→grpc, kafka→kafka all start a new trace; baggage and tracestate lost.
HTTP/gRPC-inbound boundaries are perfect.

**Root cause**: `opentelemetry-instrumentation-confluent-kafka` wraps `poll()`
and creates a consume span, but never makes the extracted context (or its
baggage) current for the code that processes the message. Long-standing
upstream issues:
[#1674](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/1674),
[#1732](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/1732)
(closed as duplicate of
[#3026](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/3026)),
[#1962](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/1962)
(item 5: "Not correct extraction of remote context for Consumer").
Workaround: manual `propagate.extract()` + `context.attach()` in the consumer
loop.

## 4. OpenTelemetry .NET: Kafka consumer links instead of continuing — and baggage goes with it

**Observed** (auto-instrumentation 1.16.0, Confluent.Kafka): kafka→* starts a
new trace; baggage/tracestate absent downstream.

**Root cause**: intentional per current OTel *messaging semantic conventions*
— the consumer span records the producer context as a span **link**, not a
parent (`StartProcessActivity(..., parentContext: default)`); see
[opentelemetry-dotnet-contrib#3995](https://github.com/open-telemetry/opentelemetry-dotnet-contrib/issues/3995)
(closed "working as designed") and
[opentelemetry-dotnet-instrumentation#3438](https://github.com/open-telemetry/opentelemetry-dotnet-instrumentation/pull/3438).
Note the divergence *within* OpenTelemetry: the Java agent (2.31.1) and
kafkajs/rdkafka instrumentations continue the trace and re-attach baggage at
the consumer, while .NET follows the newer link-based convention and drops
both. Trace *links* preserve observability correlation but do nothing for
applications that read baggage downstream.

## 5. OpenTelemetry Ruby: no gRPC server instrumentation

**Observed** (SDK 1.13.0): grpc→* boundaries start a new trace and lose
baggage; http→* and kafka→* (rdkafka) are perfect, including consumer-side
continuation.

**Root cause**: there is no maintained OTel Ruby gRPC *server* instrumentation
(the community gem covers the client side); inbound gRPC metadata is never
extracted. Recorded in `relays/ruby/README.md`.

## 6. Positive results worth stating

- **OTel Java agent 2.31.1, OTel Node auto-instrumentations 0.80.0, OTel Go
  (otelhttp/otelgrpc/kotel, manual plumbing)**: every header, every boundary.
- **dd-trace-js 6.14.0 and dd-trace-go v2.10.1**: every header (incl. W3C
  baggage AND `ot-baggage-*`), every boundary.
- **dd-trace-dotnet 3.52.0, dd-trace-rb 2.42.0 (karafka)**: full W3C support
  on every boundary (no `ot-baggage-*` emission).
- **tracestate foreign-entry preservation** was honored by every tracer that
  emitted tracestate at all — no vendor dropped our `sdtest=v1` entry when a
  trace was continued.
- **Canary**: `x-sd-canary` never leaked in any run (relays re-emit, not
  proxy), and no tracer forwarded arbitrary non-tracing headers.

## 7. Cross-cutting conclusions

1. The failure boundary is not "Kafka" per se: it is **any inject/extract site
   written against span APIs instead of context APIs** (dd-java), plus **any
   consumer model that doesn't activate context for processing** (OTel
   Python/.NET, dd-py consumers).
2. W3C Baggage being HTTP-scoped and still a Candidate Recommendation
   ([spec](https://www.w3.org/TR/baggage/), snapshot 2024-05-30) gives vendors
   cover for exactly these gaps — the spec's silence on non-HTTP carriers is
   where implementations diverge.
3. The OpenTracing-era `ot-baggage-*` headers outlive their deprecation for a
   reason: in dd-trace-java they cross all nine boundaries while the modern
   `baggage` header crosses one.
