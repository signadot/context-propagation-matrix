# Findings — root causes and upstream references

Empirical results are in [`results/MATRIX.md`](results/MATRIX.md) (raw header
dumps per test in `results/raw/*.json`). This file documents *why* the notable
cells look the way they do, with upstream citations. Runs dated 2026-09-05;
30 variants × 16 boundaries ({http, grpc, kafka, rabbitmq}²); tracer versions
as recorded per variant.

## 1. Datadog Java: W3C `baggage` survives only HTTP→HTTP — 1 of 16 boundaries

**Observed** (dd-trace-java 1.65.1, `DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage`
and also with defaults): `baggage` propagated on http→http only; absent on the
other **15** boundaries, including every RabbitMQ combination. `traceparent`,
`tracestate`, `ot-baggage-*`, and `x-datadog-*` propagate on **all 16**.

**Root cause**: Kafka/AMQP producer (and non-HTTP client) instrumentation
injects from the *span* rather than the *current context*
(`defaultPropagator().inject(span, ...)`); baggage lives on the thread-local
context, and `AgentSpan.get()` delegates non-span context keys to
`Context.root()`, so the baggage propagator finds nothing. Reported upstream
in May 2026:
[dd-trace-java#11286](https://github.com/DataDog/dd-trace-java/issues/11286).
Datadog's response confirms scoping: "W3C baggage propagation was specifically
scoped to HTTP to begin with … extending it to other transports is a feature
and not a bug", citing the absence of a W3C spec for non-HTTP carriers.
Groundwork exists (context/tracing decoupling in
[PR #8330](https://github.com/DataDog/dd-trace-java/pull/8330) and
[PR #10767](https://github.com/DataDog/dd-trace-java/pull/10767), milestone
1.61.0) but the inject sites were still span-centric at 1.65.1.

**Contrast**: dd-trace-js 6.14.0, dd-trace-go v2.10.1, dd-trace-dotnet 3.52.0
and datadog-opentelemetry (Rust) 0.5.2 all propagate W3C baggage across **all
16** boundaries — the gap is implementation-specific, not a Datadog design
position.

## 2. Datadog Python: Kafka off by default, RabbitMQ (pika) not covered at all

**Observed** (ddtrace 4.14.0): with default config, *no propagation headers of
any kind* are injected into produced Kafka records (`*→kafka`: everything
absent). Setting `DD_KAFKA_PROPAGATION_ENABLED=true` turns on full W3C
injection *including baggage*, but consumer-side continuation is still broken:
`kafka→*` starts a new trace and drops baggage. RabbitMQ is worse: nothing is
injected or extracted on any `*→rabbitmq` / `rabbitmq→*` leg, flag or no flag.

**Root cause / reference**: Kafka injection is a documented default-off —
`ddtrace.config.kafka["distributed_tracing_enabled"]`, "Default: False", env
`DD_KAFKA_PROPAGATION_ENABLED`
([ddtrace integrations docs](https://ddtrace.readthedocs.io/en/stable/integrations.html));
the consumer integration traces `poll()` but does not activate the extracted
context. For RabbitMQ, dd-trace-py simply has **no pika integration** — its
AMQP coverage is the `kombu` library only (same docs), and pika is the
canonical Python client. Uninstrumented library ⇒ silent total loss.

**Also**: dd-trace-py never emits `ot-baggage-*` (nor do dd-trace-dotnet and
dd-trace-rb); only dd-trace-java, dd-trace-js and dd-trace-go carry the
OpenTracing legacy headers.

## 3. OpenTelemetry Python: Kafka-inbound loses everything — but RabbitMQ is perfect

**Observed** (instrumentation 0.65b0): kafka→* all start a new trace; baggage
and tracestate lost. Every other boundary — including all eight RabbitMQ
combinations via `opentelemetry-instrumentation-pika` — propagates everything.

**Root cause**: `opentelemetry-instrumentation-confluent-kafka` wraps `poll()`
and creates a consume span, but never makes the extracted context (or its
baggage) current for the code that processes the message. Long-standing
upstream issues:
[#1674](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/1674),
[#1732](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/1732)
(closed as duplicate of
[#3026](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/3026)),
[#1962](https://github.com/open-telemetry/opentelemetry-python-contrib/issues/1962).
The pika instrumentation demonstrates the correct design in the same
ecosystem: its per-message callback wrapper extracts *and attaches* the
context before invoking the handler. The failure is a per-instrumentation
design choice, not a language limitation.

## 4. OpenTelemetry .NET: links instead of parents — Kafka drops all, RabbitMQ splits the verdict

**Observed** (auto-instrumentation 1.16.0): kafka→* starts a new trace with
baggage/tracestate absent. rabbitmq→* **continues the trace** (traceparent +
tracestate survive) **but baggage is dropped**. All `*→kafka` and
`*→rabbitmq` injection works, baggage included.

**Root cause**: Confluent.Kafka consume instrumentation records the producer
context as a span **link**, not a parent (per current OTel messaging semantic
conventions;
[opentelemetry-dotnet-contrib#3995](https://github.com/open-telemetry/opentelemetry-dotnet-contrib/issues/3995)
closed "working as designed"). RabbitMQ.Client 7.x ships *native*
`ActivitySource` instrumentation (the OTel zero-code agent just enables it),
which does parent the consume activity — hence trace continuity — but nothing
re-attaches the transported `baggage` member to the consumer-side context, so
application-level baggage still dies at the hop.

## 5. OpenTelemetry Ruby: correct on inject, broken on consume (bunny), absent on gRPC server

**Observed** (SDK 1.13.0): http→* and kafka→* (rdkafka) perfect, including
`*→rabbitmq` injection via `opentelemetry-instrumentation-bunny` 0.25.0. But
rabbitmq→* starts a new trace (consume side does not activate the extracted
context), and grpc→* remains a total loss (no maintained gRPC *server*
instrumentation; the community gem covers the client side).

The bunny consume gap is the third instance of the consume-side failure class
(after OTel Python's confluent-kafka and OTel .NET's Kafka links): injection
is easy to get right, *activation of extracted context for user code* is where
messaging instrumentations diverge.

## 6. Datadog Ruby: no bunny integration

**Observed** (datadog gem 2.42.0): all HTTP/gRPC/Kafka boundaries perfect
(incl. W3C baggage). RabbitMQ: nothing injected on produce; consume starts a
fresh trace on the outbound leg. The
[dd-trace-rb integrations list](https://docs.datadoghq.com/tracing/trace_collection/compatibility/ruby/)
has no bunny entry — AMQP coverage is only via the Sneakers/Kicks worker
framework. Same class as dd-py/pika: canonical client, zero coverage.

## 7. PHP: the most fragmented column in the matrix

Three structural findings apply to every PHP variant:

- **No gRPC server in any tracer-supported topology.** The official gRPC PHP
  implementation is client-only
  ([grpc.io quickstart](https://grpc.io/docs/languages/php/quickstart/):
  "You can only create gRPC clients in PHP. Use another language to create a
  gRPC server."). PHP *can* serve gRPC via a separate application server —
  RoadRunner's [gRPC plugin](https://docs.roadrunner.dev/docs/grpc/grpc)
  (Go proxy → PHP workers; the de facto standard) or Swoole-based frameworks
  such as Hyperf — but none of the three tracers' zero-code instrumentation
  covers those paths: dd-trace-php's RoadRunner integration hooks the HTTP
  `PSR7Worker` only (Datadog's own guidance for RoadRunner gRPC is manual
  `dd_trace_method` hooks, see
  [dd-trace-php#805](https://github.com/DataDog/dd-trace-php/issues/805));
  OTel has no roadrunner-grpc PHP instrumentation (RoadRunner's `otel`
  plugin traces in the Go proxy layer, not the PHP worker); Elastic supports
  neither. The harness measures the documented Apache mod_php + CLI worker
  topology, so all grpc→* boundaries are undeliverable (🚫). A
  RoadRunner-based relay with manual instrumentation is a candidate
  follow-up (tracked in the article followups).
- **No tool instruments the gRPC client** (`ext-grpc` + `grpc/grpc`): every
  `*→grpc` cell is header-free for otel, datadog, and elastic alike.
- The relay runs Apache mod_php + two CLI workers — the documented supported
  topology for all three tools (dd-trace-php does not support `php -S`).

Per tool (all on PHP 8.3):

- **Datadog (ddtrace 1.25.0)** is the strongest: HTTP and RabbitMQ
  (php-amqplib) propagate everything including W3C baggage in both directions;
  Kafka *injection* works (`*→kafka` ✅ incl. baggage) but the rdkafka
  *consume* side does not continue context (kafka→*: new trace, baggage
  gone) — the by-now-familiar consume-side class.
- **OpenTelemetry (ext 1.2.1 + auto-instrumentation)**: consume-side rdkafka
  extraction *works* (kafka→http propagates everything: extract + context
  activation are correct), but **producer injection silently no-ops when the
  optional `$headers` argument of `producev()` is omitted** — the naive call
  `producev($partition, 0, $payload)` gets no headers, while adding an
  explicit empty array `producev($partition, 0, $payload, null, [])` makes
  injection (incl. baggage) work. Verified empirically both ways with
  [open-telemetry/opentelemetry-auto-ext-rdkafka](https://github.com/opentelemetry-php/contrib-auto-ext-rdkafka),
  whose README claims producev injection with no mention of the
  argument-presence requirement. RabbitMQ: no php-amqplib instrumentation
  exists (only `ext-amqp`, a different client) — all rabbit legs lose
  everything.
- **Elastic (elastic_apm 1.17.0)**: http→http only; Kafka and RabbitMQ are
  not in its supported-technologies list, and consumed messages start fresh
  traces on the outbound HTTP leg.

## 8. Elastic APM: one perfect agent, six partial ones

The headline: **Elastic's Java agent (1.56.0) propagates `traceparent`,
`tracestate`, and W3C `baggage` across all 16 boundaries** — the only
non-OpenTelemetry agent in the matrix to do so, and on messaging hops it adds
its binary `elasticapmtraceparent` header alongside the W3C set. Elastic's
[agent spec](https://github.com/elastic/apm/blob/main/specs/agents/tracing-distributed-tracing.md)
mandates baggage support; only Java delivers it end-to-end.

Everywhere else, coverage collapses by language:

- **Go (apm-agent-go v2.7.12)**: `apmhttp`/`apmgrpc` wrappers plus the
  documented manual traceparent/tracestate pattern carry *trace identity*
  across all 16 boundaries, but the agent has **no baggage API at all** — the
  only mention in the source is the OpenTracing bridge stating baggage is
  silently dropped. Baggage absent on all 16.
- **Node (elastic-apm-node 4.18.0)**: traceparent + tracestate over
  HTTP/gRPC/kafkajs; **no W3C baggage implementation**; **no amqplib
  support** (absent from the
  [supported technologies](https://www.elastic.co/docs/reference/apm/agents/nodejs/supported-technologies);
  Elastic's docs direct users to manual traceparent passing for queues) — all
  RabbitMQ legs empty.
- **Python (elastic-apm 6.26.2)**: HTTP-inbound works; gRPC-inbound continues
  the trace but **drops foreign `tracestate` entries**, and we captured an
  outright bug: the outbound `tracestate` header emitted after a gRPC hop is
  the literal string `tracestate` (the header *name* as its value). No Kafka
  (confluent-kafka) or RabbitMQ (pika) support.
- **.NET (1.34.7, startup-hook zero-code)**: HTTP + gRPC perfect incl.
  baggage. The agent's own Kafka/RabbitMQ instrumentation exists **only in
  the CLR-profiler deployment** (linux-x64 only — it cannot run on arm64 at
  all), and even the profiler caps at `RabbitMQ.Client <7.0.0` and
  `Confluent.Kafka <3.0.0`; the startup hook covers neither. Measured
  nuance: **RabbitMQ trace context still flows** (`*→rmq` inject and `rmq→*`
  continuation both ✅ for traceparent/tracestate) because RabbitMQ.Client
  7.x ships *native* `ActivitySource` instrumentation that the elastic
  agent's OpenTelemetry bridge picks up — but baggage does not ride along.
  Kafka (Confluent.Kafka has no native ActivitySource) measures fully
  absent: `*→kafka` no inject, `kafka→*` new trace.
- **Ruby (elastic-apm 4.9.0)**: http→http only; gRPC/Kafka/RabbitMQ all
  unsupported (gRPC requires hand-added interceptors; Kafka only via the
  Racecar framework).
- **PHP**: see §7.

Practical summary: outside Java (and HTTP-only stacks on .NET/Node), Elastic
APM cannot currently carry application baggage across protocol boundaries.

## 9. Rust: manual-by-design, and both tools ace it

No auto-instrumentation exists for Rust; the relay uses the documented
carrier/propagator APIs (opentelemetry-rust 0.32 + opentelemetry-http;
carriers over tonic `MetadataMap`, rdkafka headers, lapin `FieldTable`).

- **otel-rust**: all 16 boundaries, all headers.
- **datadog-rust** ([datadog-opentelemetry](https://docs.datadoghq.com/tracing/trace_collection/automatic_instrumentation/dd_libraries/rust/)
  0.5.2, "Preview"): all 16 boundaries — `x-datadog-*`, traceparent,
  tracestate, and W3C baggage. Notable: Datadog's docs footnote claims baggage
  is unsupported in Rust, but 0.5.2's source implements it (delegating to the
  OTel `BaggagePropagator`) and enables it by default — the docs lag the code.
  No `ot-baggage-*` style exists in this SDK.

The catch: none of it happens unless the developer wires the carriers at
every hop — "supported" here means "the API exists", not "your service is
covered by attaching an agent".

## 10. Baseline (no instrumentation): seven clean zeros — and .NET

Control runs: every language's relay with no agent, wrapper, or tracing gem
attached (`none-*` variants, 128 hops). Seven of eight languages behave as
expected — nothing propagates on any of the 16 boundaries, confirming that
everything in the instrumented tables is attributable to the tool under
test, not to the apps or the harness.

The exception is .NET. With **no agent attached**, ASP.NET Core + HttpClient
propagate on all four http/grpc → http/grpc boundaries:

- `traceparent`: same trace-id, fresh span-id — the runtime genuinely
  participates in the trace. ASP.NET Core hosting creates an `Activity` per
  request, and `HttpClient`'s DiagnosticsHandler injects it on the way out
  (gRPC inherits this because `Grpc.Net.Client` rides on HttpClient).
- `tracestate`: preserved verbatim.
- W3C `baggage` in → **`Correlation-Context` out**. The default
  `DistributedContextPropagator` on .NET 8/9 is the pre-W3C "legacy"
  propagator: it reads the standard `baggage` header into `Activity.Baggage`
  but re-emits it under the obsolete draft name, so values survive one hop
  and are then invisible to any W3C-only reader downstream.

Nothing propagates on Kafka/RabbitMQ legs (no built-in messaging
propagation). Note the version dependence: [.NET 10 switches the default to
a strict W3C propagator](https://learn.microsoft.com/en-us/dotnet/core/compatibility/core-libraries/10.0/default-trace-context-propagator)
(`baggage` header out, strict encoding, `CreatePreW3CPropagator()` to opt
back) — an *uninstrumented* .NET service changes its propagation behavior on
a runtime upgrade. Harness runs used .NET 8.

## 11. Positive results worth stating

- **All 16 boundaries, all headers**: OTel Java agent 2.31.1, OTel Node
  auto-instrumentations 0.80.0, OTel Go (otelhttp/otelgrpc/kotel + documented
  propagator plumbing), otel-rust 0.32, dd-trace-js 6.14.0, dd-trace-go
  v2.10.1, dd-trace-dotnet 3.52.0, datadog-rust 0.5.2, and (trace context +
  baggage) Elastic Java 1.56.0.
- **tracestate foreign-entry preservation** was honored by every tracer that
  continued a trace, with one exception: elastic-apm Python's gRPC path (§8).
- **Canary**: `x-sd-canary` never leaked in any of the 608 recorded tests
  (480 instrumented + 128 baseline) — relays re-emit, not proxy.

## 12. Cross-cutting conclusions

1. **The dominant failure class is consume-side context activation.** Five
   independent instrumentations inject correctly but fail to make the
   extracted context current for user code: OTel Python (confluent-kafka),
   OTel .NET (Kafka links; RabbitMQ baggage), OTel Ruby (bunny), dd-py
   (kafka), dd-php (rdkafka). Injection is table stakes; the consumer is
   where propagation actually dies.
2. **Span-centric inject sites** (dd-java, #11286) lose exactly the data that
   lives on context and not on spans — baggage — while trace identity
   survives, making the failure invisible to trace-waterfall debugging.
3. **Coverage is an allowlist, not a protocol feature.** dd-py/pika,
   dd-rb/bunny, otel-php/amqplib, elastic everywhere outside Java: the
   canonical client library simply isn't on the vendor's list, and the
   result is silent, total loss.
4. **Optional-argument traps exist**: OTel PHP injects into Kafka only when
   `producev()` is called with the `$headers` argument present (§7) — the
   difference between ✅ and ❌ is one `[]` in application code.
5. W3C Baggage being HTTP-scoped and still a Candidate Recommendation
   ([spec](https://www.w3.org/TR/baggage/), snapshot 2024-05-30) gives
   vendors cover for these gaps — the spec's silence on non-HTTP carriers is
   where implementations diverge.
6. The OpenTracing-era `ot-baggage-*` headers outlive their deprecation for a
   reason: in dd-trace-java they cross all sixteen boundaries while the
   modern `baggage` header crosses one.
