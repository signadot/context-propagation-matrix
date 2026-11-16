# Go relay — OpenTelemetry compile-time instrumentation (build-time weaving)

Variant `otel-go-compiletime`, setup tier **`build-time`**: the naive app in
`main.go` imports no tracing API whatsoever (compare
`relays/go/none/main.go`), and every propagation behaviour is woven in during
compilation by
[otelc](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation)
— `go tool otelc go build` instead of `go build` is the only delta from an
uninstrumented build. otelc is a Go 1.24+ tool dependency, pinned in `go.mod`
via the `tool` directive (Dependabot bumps it); with no committed config it
analyzes the dependency graph and enables every instrumentation it has for
the libraries the app imports.

| leg | library | woven? |
|---|---|---|
| http | net/http (server + client) | yes |
| grpc | google.golang.org/grpc | yes |
| kafka | github.com/segmentio/kafka-go v0.4.51 | yes — the **only** Kafka client otelc v1 supports (the hand-wired `otel-go` relay uses franz-go + kotel, so the rows also compare client coverage) |
| rabbitmq | github.com/rabbitmq/amqp091-go | yes, since otelc 1.2.0: publish injects, so `http->rabbitmq` and `grpc->rabbitmq` pass; nothing continues a trace after a consume, so `rabbitmq->*` starts a new one. otelc 1.1.0 and earlier had no AMQP instrumentation and those cells were dark |

Measured result (see `MATRIX.md` on the latest release and FINDINGS §11): every
http/grpc-inbound leg propagates **everything — traceparent, tracestate, and
W3C baggage — including into Kafka** (kafka-go's `WriteMessages(ctx, …)`
gives the weaver a context to inject from). Every Kafka-inbound leg loses
everything: kafka-go's `Reader` is a pull API with no callback boundary, so
the woven consumer has nowhere to hand the extracted context back to the
app's loop (upstream is tracking this in
[otelc#695](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/695)).

Gotchas encoded in the Dockerfile:

- Woven code is injected at the toolchain level, so the build **asserts OTel
  SDK + kafka-go instrumentation symbols with `go tool nm`** — an un-woven
  binary fails the image build.
- otelc's dependency sync edits `go.mod`/`go.sum` during the build (inside
  the image only). Its local scratch directory `.otelc-build/` is gitignored.
- otelc is a `tool` dependency, not a dependency of the binary, so
  `debug.ReadBuildInfo` cannot see its version. The Dockerfile stamps it with
  `-ldflags "-X main.tracerVersion=$(go list -m -f '{{.Version}}' go.opentelemetry.io/otelc)"`
  and `/info` reports **that** as `tracer_version` — the version this variant
  is pinned by and filed under (`pins.yaml` `otelc`). The OTel SDK otelc
  weaves in is visible in build info and is reported under `libs.sdk`.
  `backtest.yml` fails a back-test whose relay reports a version other than
  the one requested, so the two must not drift apart.
