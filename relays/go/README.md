# Go relays

Go has no auto-instrumentation for any of the three tools, so there are three
separate relay programs, one per tool, each built exclusively from that
vendor's documented contrib/instrumentation libraries with idiomatic
`context.Context` plumbing (the one language where explicit context plumbing
is allowed — see DESIGN.md). No relay constructs or copies propagation
headers by hand; header injection/extraction is always done by the vendor's
propagator or instrumentation API.

One Go module (`relays/go/go.mod`) with three main packages (`./otel`,
`./datadog`, `./newrelic`) and a shared `./internal/common` package (env
handling, JSON types, `/info` assembly, `/relay` handler plumbing — nothing
tracing-related). The module `replace`s the repo root module so it can import
the pre-generated gRPC stubs at `gen/cpmatrix`.

Build: `relays/go/Dockerfile` with build context = repo root and
`--build-arg TOOL=otel|datadog|newrelic` (single Dockerfile, `ARG`-selected
binary; pure Go, `CGO_ENABLED=0`).

## otel (`./otel`)

| leg | library |
|---|---|
| HTTP server / client | `otelhttp` v0.71.0 (`NewHandler` / `NewTransport`) |
| gRPC server / client | `otelgrpc` v0.71.0 (`NewServerHandler` / `NewClientHandler` stats handlers) |
| Kafka | `franz-go` v1.21.6 + `kotel` plugin v1.7.0 (SDK `go.opentelemetry.io/otel` v1.46.0) |

The global propagator is set explicitly to
`propagation.NewCompositeTextMapPropagator(TraceContext{}, Baggage{})` and the
same propagator is handed to kotel via `kotel.TracerPropagator`. The SDK
`TracerProvider` has **no exporter registered**: spans are still created and
sampled (so downstream injection carries fresh span ids instead of
pass-through), but nothing leaves the process. On consume, kotel's documented
pattern `tracer.WithProcessSpan(record)` yields the `context.Context` passed
to the outbound call.

## datadog (`./datadog`)

| leg | library |
|---|---|
| HTTP server / client | `github.com/DataDog/dd-trace-go/contrib/net/http/v2` v2.10.1 (`NewServeMux` / `WrapClient`) |
| gRPC server / client | `github.com/DataDog/dd-trace-go/contrib/google.golang.org/grpc/v2` v2.10.1 (unary interceptors) |
| Kafka | `github.com/DataDog/dd-trace-go/contrib/IBM/sarama/v2` v2.10.1 (`WrapSyncProducer` / `WrapConsumerGroupHandler`) on `github.com/IBM/sarama` v1.60.2 |

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

## newrelic (`./newrelic`)

| leg | library |
|---|---|
| HTTP server / client | `go-agent/v3` v3.44.2 (`newrelic.WrapHandleFunc` / `newrelic.NewRoundTripper`) |
| gRPC server / client | `nrgrpc` v1.4.10 (`UnaryServerInterceptor(app)` / `UnaryClientInterceptor`) |
| Kafka | `nrsarama` v1.0.2 on `github.com/Shopify/sarama` v1.38.1, plus manual `AcceptDistributedTraceHeaders` on consume (see below) |

Caveats:

- **nrsarama requires the archived Shopify/sarama fork** (its API takes
  Shopify types), so this relay uses `github.com/Shopify/sarama` v1.38.1 —
  the last release published under that module path; the repo now redirects
  to IBM/sarama whose tags declare the IBM module path, hence the pin in
  `go.mod`. The Datadog relay independently uses `github.com/IBM/sarama`.
- Producer: `nrsarama.NewProducerWrapper(producer, txn).SendMessage(...)` —
  the integration calls `txn.InsertDistributedTraceHeaders` and copies the
  headers onto the Kafka record.
- Consumer: `nrsarama.NewConsumerHandlerFromApp` starts one transaction per
  message and passes a transaction-bearing context to the message handler —
  but (as of v1.0.2) **it never accepts the inbound record's distributed
  trace headers**, so on its own the trace would not continue across the
  Kafka hop. The message handler therefore applies New Relic's documented
  manual message-queue pattern: copy the record headers into an
  `http.Header` and call
  `txn.AcceptDistributedTraceHeaders(newrelic.TransportKafka, hdrs)`
  (the insert/accept pattern from New Relic's distributed-tracing docs and
  the go-agent GUIDE). Reading inbound headers into the vendor's Accept API
  is the documented approach; the relay never writes headers itself.
- If `NEW_RELIC_LICENSE_KEY` is missing/invalid, `newrelic.NewApplication`
  fails; the relay falls back to `newrelic.ConfigEnabled(false)` (no-op
  agent) so it still starts and serves `/info`. With a valid key the relay
  calls `app.WaitForConnection(15s)` at startup, because NR agents inject
  nothing until the connect handshake completes.

## Shared notes

- Both sarama-based relays set `cfg.Version = sarama.V2_1_0_0` (record
  headers need >= 0.11) and `Consumer.Offsets.Initial = OffsetOldest`;
  consumer group id is `relay-{VARIANT}`. Driver-produced messages carry
  record headers; both sarama consumers and franz-go handle these fine.
- `/info` reports `tracer_version` (and per-lib versions) read from
  `runtime/debug.ReadBuildInfo`, `runtime_version` from `runtime.Version()`,
  and echoes whichever of `OTEL_PROPAGATORS` / `OTEL_TRACES_EXPORTER` /
  `DD_TRACE_PROPAGATION_STYLE` / `NEW_RELIC_DISTRIBUTED_TRACING_ENABLED` are
  set.
