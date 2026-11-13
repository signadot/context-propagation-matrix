# Ruby relay

One Sinatra/gRPC/Kafka/RabbitMQ relay app, four Docker targets (`otel` |
`datadog` | `newrelic` | `elastic`) that differ only in which instrumentation
gems are installed (`gemfiles/{tool}/Gemfile`) and which boot file loads them before
the app (`app/boot_{tool}.rb`). Build context is the repo root; gRPC stubs
are generated from `proto/relay.proto` at image build time with `grpc-tools`.

Application code (`app/main.rb` and everything it requires) never touches
headers, record headers, metadata, or tracing APIs. All instrumentation setup
lives in the boot files.

## Kafka client per tool

No single Ruby Kafka client is instrumented by all three vendors, so the
Kafka adapter is chosen per tool (reflected in the `/info` `libs.kafka`
field):

| tool | client | why |
|---|---|---|
| otel | `rdkafka` | `opentelemetry-instrumentation-rdkafka` (requires rdkafka >= 0.18) injects on `Producer#produce` and extracts + activates context around `Consumer#each`. The alternative gems target dead or framework-specific clients (`ruby-kafka` instrumentation 0.25.0 — the ruby-kafka gem itself has been unmaintained since 2022; `racecar` 0.7.0 is a framework wrapper). |
| datadog | `karafka` + `waterdrop` (producer) | dd-trace-rb (observed at v2.42.0; the pinned version is in `gemfiles/datadog/Gemfile`) has **no rdkafka integration at all**, and its ruby-kafka `:kafka` integration only creates spans — its sole option is `enabled`; there is no `distributed_tracing` and no header injection/extraction. Datadog's documented Kafka propagation path for Ruby is the `:karafka` (>= 2.3) and `:waterdrop` (>= 2.8.8) integrations with `distributed_tracing: true`. |
| newrelic | `rdkafka` | `newrelic_rpm` >= 9.14 ships rdkafka instrumentation (also ruby-kafka): injects distributed-trace headers on produce, accepts headers and wraps each consumed message in a consume transaction. |
| elastic | `rdkafka` | elastic-apm has **no Kafka client instrumentation at all** — the closest thing is [Racecar framework instrumentation](https://www.elastic.co/docs/release-notes/apm/agents/ruby) (added in agent 4.2, and not even listed on the [supported-technologies page](https://www.elastic.co/docs/reference/apm/agents/ruby/supported-technologies)). No rdkafka/ruby-kafka support exists, so the client matches otel's and the leg runs **uninstrumented** — a measured finding. |

## RabbitMQ client (bunny, all tools)

All four targets use the same client, [`bunny`](https://rubygems.org/gems/bunny)
(version pinned in each `gemfiles/<tool>/Gemfile`): consume
`RELAY_RABBIT_QUEUE` (default `relay-rmq-{VARIANT}`) via
`queue.subscribe`, publish to `SINK_RABBIT_QUEUE` (default `sink-rmq`) via the
default exchange. Queue declares use durable=false, auto-delete=false,
exclusive=false everywhere (idempotent; the driver/sink declare the same).
Instrumentation coverage differs sharply per tool:

| tool | bunny instrumentation |
|---|---|
| otel | **yes** — [`opentelemetry-instrumentation-bunny`](https://rubygems.org/gems/opentelemetry-instrumentation-bunny) (requires ruby >= 3.3) prepends into `Bunny::Channel` (publish-side inject), `Bunny::Consumer`/`Bunny::Queue`/`Bunny::ReaderLoop` (receive/process-side extract). Enabled via `c.use 'OpenTelemetry::Instrumentation::Bunny'` in `boot_otel.rb`. |
| datadog | **no** — dd-trace-rb 2.x has no bunny integration; the [Ruby integrations list](https://docs.datadoghq.com/tracing/trace_collection/compatibility/ruby/) has nothing for RabbitMQ except the Sneakers/Kicks *worker framework* (consume-side only, and we don't use that framework). A third-party gem (`datadog-json-logger`) adds a `:bunny` integration but requires the consumer block to call `continue_trace!` by hand — app code touching tracing APIs, which the harness contract forbids. The leg runs **plain uninstrumented bunny**; absence is the finding. |
| newrelic | **yes** — built into `newrelic_rpm` [since agent 4.3.0](https://docs.newrelic.com/docs/apm/agents/ruby-agent/features/message-queues/), automatic, no configuration: prepends into `Bunny::Exchange#publish` (header insert), `Bunny::Queue#pop` and `Bunny::Consumer#call` (push-style consume wrapped in a message transaction that accepts DT headers). |
| elastic | **no** — RabbitMQ is absent from elastic-apm's [supported technologies](https://www.elastic.co/docs/reference/apm/agents/ruby/supported-technologies); the only AMQP-adjacent entry is Sneakers 2.12.0 (experimental, framework-level). Plain uninstrumented bunny; absence is the finding. |

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
`grpc` gem. `opentelemetry-instrumentation-grpc` (observed at 0.5.2; the
pinned version is in `gemfiles/otel/Gemfile`) instruments **clients only** (it prepends `GRPC::ClientStub`, so outbound calls are
injected automatically); the only server-side OTel gem targets the `gruf`
framework, not `GRPC::RpcServer`. Consequence for the matrix: on
`grpc -> *` boundaries the otel-ruby relay never extracts the inbound
context, so outbound legs carry a **new trace** and seeded baggage is lost.
That is a legitimate empirical finding about OTel Ruby, not a harness bug.

Datadog (`:grpc` integration, client + server interceptors, patched into the
grpc gem automatically) and New Relic (grpc client + server instrumentation
since 8.10) both cover the gRPC server. Elastic covers neither side
automatically — see the next section.

## Elastic variant (elastic-apm)

`gemfiles/elastic/Gemfile` + `app/boot_elastic.rb` + Docker target `elastic`. The
[elastic-apm](https://rubygems.org/gems/elastic-apm) gem is started
with its documented Sinatra integration — `HttpApp.use ElasticAPM::Middleware`
plus `ElasticAPM::Sinatra.start(HttpApp)` in the boot file (app code
untouched); all other configuration comes from `ELASTIC_APM_*` env vars. An
unreachable `ELASTIC_APM_SERVER_URL` does not crash the agent (default
behavior: the transport logs errors and keeps running; `Sinatra.start`
additionally rescues startup errors), and header injection is independent of
export success.

What their [supported-technologies docs](https://www.elastic.co/docs/reference/apm/agents/ruby/supported-technologies)
claim for our four protocols:

| protocol | documented coverage |
|---|---|
| http inbound | Rails/Rack/Sinatra via `ElasticAPM::Middleware`. The docs' tested matrix says Sinatra >= 1.0 **<= 2.2**, but the same page claims "automatic support for … all Rack compatible web frameworks" and Rails up to 8.1 (i.e. Rack 3), so we keep sinatra 4.x like every other target and let the run decide. |
| http outbound | `net/http` auto-instrumented "with distributed tracing" — the only leg with documented propagation. |
| grpc | Ships `ElasticAPM::GRPC::ClientInterceptor` / `ServerInterceptor` (marked experimental), but both must be **added by hand** at stub/server construction — app code touching tracing APIs, which the harness contract forbids. Nothing is configured, so both gRPC directions run uninstrumented. |
| kafka | Racecar framework only (release notes, agent 4.2); no rdkafka/ruby-kafka support. Uninstrumented. |
| rabbitmq | Not supported at all (Sneakers 2.12.0 experimental worker support is the only AMQP-adjacent entry). Uninstrumented. |

Expected matrix outcome: only `http -> http` has a documented propagation
path; everything else measures what actually happens with no elastic
instrumentation on at least one side of the hop.

## Library versions

Each `gemfiles/<tool>/Gemfile` pins the gems the relay uses directly, and the
`Gemfile.lock` beside it pins everything they pull in (see
[Gem locks](#gem-locks)). The versions a running relay loaded are reported by
`GET /info` (`tracer_version`, `libs`) via `Gem.loaded_specs`.

## Gem locks

Each `gemfiles/<tool>/Gemfile` pins its gems exactly and has a `Gemfile.lock`
beside it that pins everything those gems pull in. The image installs with
`BUNDLE_FROZEN=true`: `bundle install` installs exactly the lock, and the
build fails if the lock does not satisfy the Gemfile. The tracer gem is in
the lock too, because Bundler resolves a Gemfile as a whole, so the lock
moves whenever the tracer does:

- `cpm pins set` and `cpm pins latest -apply` (`backtest.yml`,
  `bump-agents.yml`) rewrite the tracer's `gem` line and run `bundle lock
  --update <tracer gem>`. That resolves the tracer and what it depends on
  again and leaves the other gems where the lock has them, unless one
  conflicts with the tracer. `cpm pins check` fails when a lock does not hold
  the tracer version its Gemfile pins.
- Dependabot updates a Gemfile and its lock in the same PR.

The locks cover the two platforms the image is built for, `aarch64-linux`
and `x86_64-linux`, with a gem's precompiled build for each where it ships
one (grpc, google-protobuf, ffi, rdkafka, libdatadog, libddwaf). Run Bundler
in the image the relay is built from, so the lock is written by the Ruby and
Bundler that install it and the host's platform is not added to it:

```sh
# After editing a Gemfile by hand: resolves what changed, keeps the rest.
docker run --rm -v "$PWD/relays/ruby/gemfiles/<tool>:/w" -w /w ruby:3.3 bundle lock

# Every gem again, at the newest version the Gemfile allows.
docker run --rm -v "$PWD/relays/ruby/gemfiles/<tool>:/w" -w /w ruby:3.3 bundle lock --update

# A new Gemfile. Starting from an empty lock keeps Bundler from adding every
# platform the gems ship for.
docker run --rm -v "$PWD/relays/ruby/gemfiles/<tool>:/w" -w /w ruby:3.3 \
  sh -c ': > Gemfile.lock && bundle lock --add-platform aarch64-linux x86_64-linux'
```

## Process model

`ruby app/boot_{tool}.rb` → requires instrumentation → `app/main.rb`:

- gRPC server (`GRPC::RpcServer`) on `:8081` in a background thread
- Kafka consumer in a background thread (rdkafka `consumer.each`, or the
  embedded karafka server for datadog); group `relay-{VARIANT}`, topic
  `RELAY_TOPIC`, earliest offset
- RabbitMQ consumer in a background thread (bunny `queue.subscribe`,
  restarting on error; deliveries run on bunny's channel work pool)
- Sinatra via puma on `:8080` in the main thread (`POST /relay`, `GET /info`)

Env: `VARIANT`, `TOOL`, `RELAY_TOPIC` (default `relay-in-{VARIANT}`),
`KAFKA_BROKERS` (default `kafka:9092`), `RABBIT_URL` (default
`amqp://guest:guest@rabbitmq:5672/`), `RELAY_RABBIT_QUEUE` (default
`relay-rmq-{VARIANT}`), `SINK_RABBIT_QUEUE` (default `sink-rmq`), `SINK_HTTP`
(default `http://sink:9080`), `SINK_GRPC` (default `sink:9081`).
