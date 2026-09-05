# Go relays

Go has no auto-instrumentation for any of the tools, so there is one relay
program per tool, each built exclusively from that vendor's documented
contrib/instrumentation libraries with idiomatic `context.Context` plumbing
(one of the languages where explicit context plumbing is allowed — see
DESIGN.md). No relay constructs or copies propagation headers by hand; header
injection/extraction is always done by the vendor's propagator or
instrumentation API.

One Go module (`relays/go/go.mod`) with three main packages (`./otel`,
`./datadog`, `./elastic`) and a shared `./internal/common` package (env
handling, JSON types, `/info` assembly, `/relay` handler and RabbitMQ
dial/declare/consume-loop plumbing — nothing tracing-related). The module
`replace`s the repo root module so it can import the pre-generated gRPC stubs
at `gen/cpmatrix`.

RabbitMQ client for all relays: `github.com/rabbitmq/amqp091-go` v1.14.0.
Queues are declared idempotently on startup with the contract's properties
(durable=false, auto-delete=false, exclusive=false); outbound messages go to
the sink queue via the default exchange.

Build: `relays/go/Dockerfile` with build context = repo root and
`--build-arg TOOL=otel|datadog|elastic` (single Dockerfile, `ARG`-selected
binary; pure Go, `CGO_ENABLED=0`).

(A New Relic relay previously lived at `./newrelic` — removed for now because
runs require a license key; see the Roadmap section in the repo DESIGN.md and
this repo's git history to restore it.)

## otel (`./otel`)

| leg | library |
|---|---|
| HTTP server / client | `otelhttp` v0.71.0 (`NewHandler` / `NewTransport`) |
| gRPC server / client | `otelgrpc` v0.71.0 (`NewServerHandler` / `NewClientHandler` stats handlers) |
| Kafka | `franz-go` v1.21.6 + `kotel` plugin v1.7.0 (SDK `go.opentelemetry.io/otel` v1.46.0) |
| RabbitMQ | `amqp091-go` v1.14.0 + global propagator inject/extract (no official contrib, see below) |

The global propagator is set explicitly to
`propagation.NewCompositeTextMapPropagator(TraceContext{}, Baggage{})` and the
same propagator is handed to kotel via `kotel.TracerPropagator`. The SDK
`TracerProvider` has **no exporter registered**: spans are still created and
sampled (so downstream injection carries fresh span ids instead of
pass-through), but nothing leaves the process. On consume, kotel's documented
pattern `tracer.WithProcessSpan(record)` yields the `context.Context` passed
to the outbound call.

Caveats:

- **There is no official OTel Go instrumentation for amqp091**
  ([registry](https://opentelemetry.io/ecosystem/registry/?language=go&component=instrumentation)),
  so the RabbitMQ leg applies the documented propagator pattern directly
  ([OTel Go propagation docs](https://opentelemetry.io/docs/languages/go/propagation/)):
  a `propagation.TextMapCarrier` adapter over the AMQP header table,
  `otel.GetTextMapPropagator().Inject(ctx, carrier)` inside a producer span
  on publish, and `.Extract` followed by a consumer span on consume (the
  consumer span makes the context current for the forward call, mirroring
  kotel's process-span pattern). The relay never names or formats a header
  itself — the propagator owns the keys and values.

## datadog (`./datadog`)

| leg | library |
|---|---|
| HTTP server / client | `github.com/DataDog/dd-trace-go/contrib/net/http/v2` v2.10.1 (`NewServeMux` / `WrapClient`) |
| gRPC server / client | `github.com/DataDog/dd-trace-go/contrib/google.golang.org/grpc/v2` v2.10.1 (unary interceptors) |
| Kafka | `github.com/DataDog/dd-trace-go/contrib/IBM/sarama/v2` v2.10.1 (`WrapSyncProducer` / `WrapConsumerGroupHandler`) on `github.com/IBM/sarama` v1.60.2 |
| RabbitMQ | `amqp091-go` v1.14.0 + `tracer.Inject`/`tracer.Extract` (no dd contrib, see below) |

Tracer: `github.com/DataDog/dd-trace-go/v2` v2.10.1, started with plain
`tracer.Start()`; all configuration (`DD_TRACE_PROPAGATION_STYLE`, …) comes
from the environment. dd-trace-go **v2 worked fine — no v1 fallback needed.**

Caveats:

- sarama's `SendMessage` takes no `context.Context`, so the produce span is
  parented by injecting the active span context into the message with the
  contrib's own carrier —
  `tracer.Inject(span.Context(), saramatrace.NewProducerMessageCarrier(msg))` —
  which is exactly where the wrapped producer looks for a parent
  (`startProducerSpan` extracts from the message before starting its span and
  then re-injects). This is the vendor propagator API, not manual headers.
- Consumer → outbound uses the pattern documented in the contrib's
  `ExampleWrapConsumer`: `tracer.Extract(saramatrace.NewConsumerMessageCarrier(msg))`
  then `tracer.ChildOf(spanctx)` + `tracer.ContextWithSpan`.
  `WrapConsumerGroupHandler` has already traced the consume and re-injected
  its span context into the record headers by the time the handler sees the
  message.
- **dd-trace-go has no contrib for rabbitmq/amqp091**
  (`contrib/rabbitmq/...` does not exist on the module proxy as of v2.10.1),
  so the RabbitMQ leg uses the vendor's documented propagator API directly
  ([Datadog custom trace-context propagation docs](https://docs.datadoghq.com/tracing/trace_collection/trace_context_propagation/custom_format/?code-lang=go),
  [`tracer.Inject`/`tracer.Extract`](https://pkg.go.dev/github.com/DataDog/dd-trace-go/v2/ddtrace/tracer#Inject)):
  a `tracer.TextMapWriter`/`TextMapReader` carrier over the AMQP header
  table — the same carrier shape the sarama contrib documents. Produce starts
  a span from the inbound context and injects its span context; consume does
  `tracer.Extract` + `tracer.ChildOf` + `tracer.ContextWithSpan`, exactly the
  documented consume pattern above. The tracer's configured propagation
  styles decide which headers are written; the relay writes none itself.

## elastic (`./elastic`)

| leg | library |
|---|---|
| HTTP server / client | `apmhttp` v2.7.12 (`Wrap` / `WrapClient`) |
| gRPC server / client | `apmgrpc` v2.7.12 (`NewUnaryServerInterceptor` / `NewUnaryClientInterceptor`) |
| Kafka | `franz-go` v1.21.6 (plain) + documented manual trace-context pattern (see below) |
| RabbitMQ | `amqp091-go` v1.14.0 (plain) + the same documented manual pattern |

Tracer: `go.elastic.co/apm/v2` v2.7.12 via `apm.DefaultTracer()`, configured
entirely from the environment. `DefaultTracer` initializes with
continue-on-error semantics (`TracerOptions.initDefaults(true)` in
`tracer.go`), and an unreachable `ELASTIC_APM_SERVER_URL` only affects the
background sender (events are buffered, then dropped) — transactions,
sampling and header injection keep working and the process does not crash.

Caveats:

- **Elastic APM Go has no messaging instrumentation modules at all**: the
  complete `module/` list of
  [apm-agent-go](https://github.com/elastic/apm-agent-go/tree/main/module)
  contains HTTP frameworks, gRPC, databases and loggers, but nothing for
  Kafka, sarama, franz-go, AMQP or RabbitMQ (see also the agent's
  [supported technologies](https://www.elastic.co/docs/reference/apm/agents/go/supported-technologies)).
- Elastic **does** document a manual distributed-tracing pattern for
  "different, unsupported protocols"
  ([APM guide → Manual distributed tracing, Go tab](https://www.elastic.co/docs/solutions/observability/apm/traces)):
  the sender formats the active `TraceContext` with
  `apmhttp.FormatTraceparentHeader(tc)` (+ `tc.State.String()` for
  tracestate) and the receiver parses with
  `apmhttp.ParseTraceparentHeader` / `apmhttp.ParseTracestateHeader` and
  continues via
  `apm.DefaultTracer().StartTransactionOptions(..., apm.TransactionOptions{TraceContext: tc})`.
  Both messaging legs apply exactly this pattern, writing the values under
  the header-name constants exported by apmhttp (`Traceparent`,
  `Elastic-Apm-Traceparent`, `Tracestate` — the legacy name matches the
  agent's own HTTP behavior per
  [`ELASTIC_APM_USE_ELASTIC_TRACEPARENT_HEADER`](https://www.elastic.co/docs/reference/apm/agents/go/configuration)).
  No header value is hand-built beyond those documented helpers, so the legs
  are instrumented rather than left plain.
- What Elastic Go **cannot** propagate: baggage. The agent has no W3C
  `baggage` API at all (its manual pattern and its HTTP/gRPC modules carry
  only traceparent/tracestate; even the agent's OpenTracing bridge docs state
  "`Span.SetBaggageItem` is a no-op; baggage items are silently dropped"),
  so `baggage` / `ot-baggage-*` can never survive an elastic-go hop —
  expected `absent` in the matrix on every boundary.
- Inbound message headers are matched case-insensitively when reading
  (`traceparent` on a Kafka record vs the canonicalized constant), because
  record/AMQP header keys are raw bytes, not canonicalized HTTP headers.

## Shared notes

- The sarama-based relay sets `cfg.Version = sarama.V2_1_0_0` (record
  headers need >= 0.11) and `Consumer.Offsets.Initial = OffsetOldest`;
  consumer group id is `relay-{VARIANT}`. Driver-produced messages carry
  record headers; both sarama and franz-go consumers handle these fine.
- RabbitMQ: all relays consume `RELAY_RABBIT_QUEUE` (default
  `relay-rmq-{VARIANT}`) with auto-ack from `RABBIT_URL`, publish to
  `SINK_RABBIT_QUEUE` (default `sink-rmq`) via the default exchange, and
  declare both queues idempotently on startup (durable=false,
  auto-delete=false, exclusive=false). The consume loop redials on
  connection loss (`internal/common/rabbit.go`).
- `/info` reports `tracer_version` (and per-lib versions, including a
  `rabbitmq` entry naming the amqp091-go version and instrumentation
  approach) read from `runtime/debug.ReadBuildInfo`, `runtime_version` from
  `runtime.Version()`, and echoes whichever of `OTEL_PROPAGATORS` /
  `OTEL_TRACES_EXPORTER` / `DD_TRACE_PROPAGATION_STYLE` /
  `ELASTIC_APM_SERVER_URL` are set.
