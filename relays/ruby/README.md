# Ruby relay

One Sinatra/gRPC/Kafka relay app, three Docker targets (`otel` | `datadog` |
`newrelic`) that differ only in which instrumentation gems are installed
(`Gemfile.{tool}`) and which boot file loads them before the app
(`app/boot_{tool}.rb`). Build context is the repo root; gRPC stubs are
generated from `proto/relay.proto` at image build time with `grpc-tools`.

Application code (`app/main.rb` and everything it requires) never touches
headers, record headers, metadata, or tracing APIs. All instrumentation setup
lives in the boot files.

## Kafka client per tool

No single Ruby Kafka client is instrumented by all three vendors, so the
Kafka adapter is chosen per tool (reflected in the `/info` `libs.kafka`
field):

| tool | client | why |
|---|---|---|
| otel | `rdkafka` | `opentelemetry-instrumentation-rdkafka` (0.10.0, current, requires rdkafka >= 0.18) injects on `Producer#produce` and extracts + activates context around `Consumer#each`. The alternative gems target dead or framework-specific clients (`ruby-kafka` instrumentation 0.25.0 — the ruby-kafka gem itself has been unmaintained since 2022; `racecar` 0.7.0 is a framework wrapper). |
| datadog | `karafka` + `waterdrop` (producer) | dd-trace-rb (v2.42.0) has **no rdkafka integration at all**, and its ruby-kafka `:kafka` integration only creates spans — its sole option is `enabled`; there is no `distributed_tracing` and no header injection/extraction. Datadog's documented Kafka propagation path for Ruby is the `:karafka` (>= 2.3) and `:waterdrop` (>= 2.8.8) integrations with `distributed_tracing: true`. |
| newrelic | `rdkafka` | `newrelic_rpm` >= 9.14 ships rdkafka instrumentation (also ruby-kafka): injects distributed-trace headers on produce, accepts headers and wraps each consumed message in a consume transaction. |

### Documented-pattern exception (datadog)

Datadog's karafka/waterdrop distributed tracing is **off by default** and is
enabled exactly as documented, via integration options in
`app/boot_datadog.rb`:

```ruby
c.tracing.instrument :karafka,   distributed_tracing: true
c.tracing.instrument :waterdrop, distributed_tracing: true
```

(equivalently `DD_TRACE_KARAFKA_DISTRIBUTED_TRACING` /
`DD_TRACE_WATERDROP_DISTRIBUTED_TRACING`). This is instrumentation
configuration, not app code: consuming happens in a plain
`Karafka::BaseConsumer#consume` with `messages.each`, producing via
`Karafka.producer.produce_sync` — no explicit inject/extract calls anywhere.
The karafka server runs embedded in the relay process
(`Karafka::Embedded.start`). Note the propagation *style* still comes from
`DD_TRACE_PROPAGATION_STYLE`, so the `datadog-ruby-defaults` variant measures
default style with the integration able to propagate at all.

## gRPC caveat (otel)

There is no OpenTelemetry Ruby server-side instrumentation for the plain
`grpc` gem. `opentelemetry-instrumentation-grpc` (0.5.2) instruments
**clients only** (it prepends `GRPC::ClientStub`, so outbound calls are
injected automatically); the only server-side OTel gem targets the `gruf`
framework, not `GRPC::RpcServer`. Consequence for the matrix: on
`grpc -> *` boundaries the otel-ruby relay never extracts the inbound
context, so outbound legs carry a **new trace** and seeded baggage is lost.
That is a legitimate empirical finding about OTel Ruby, not a harness bug.

Datadog (`:grpc` integration, client + server interceptors, patched into the
grpc gem automatically) and New Relic (grpc client + server instrumentation
since 8.10) both cover the gRPC server.

## Library versions at time of writing

Resolved fresh at image build (`bundle install`, pessimistic constraints):
sinatra 4.2.x, puma 8.0.x, rackup 2.3.x, grpc 1.83.x, rdkafka 0.29.x,
karafka 2.6.x, waterdrop 2.10.x, opentelemetry-sdk 1.13.x (instrumentation:
rack 0.31.x, sinatra 0.30.x, net_http 0.29.x, grpc 0.5.x, rdkafka 0.10.x),
datadog 2.42.x, newrelic_rpm 10.7.x. Exact versions are reported at runtime
by `GET /info` (`tracer_version`, `libs`) via `Gem.loaded_specs`.

## Process model

`ruby app/boot_{tool}.rb` → requires instrumentation → `app/main.rb`:

- gRPC server (`GRPC::RpcServer`) on `:8081` in a background thread
- Kafka consumer in a background thread (rdkafka `consumer.each`, or the
  embedded karafka server for datadog); group `relay-{VARIANT}`, topic
  `RELAY_TOPIC`, earliest offset
- Sinatra via puma on `:8080` in the main thread (`POST /relay`, `GET /info`)

Env: `VARIANT`, `TOOL`, `RELAY_TOPIC` (default `relay-in-{VARIANT}`),
`KAFKA_BROKERS` (default `kafka:9092`), `SINK_HTTP` (default
`http://sink:9080`), `SINK_GRPC` (default `sink:9081`).
