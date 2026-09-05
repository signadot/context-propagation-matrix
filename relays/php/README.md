# PHP relay

One Slim application, three Docker targets (`otel` | `datadog` | `elastic`)
that differ only in which instrumentation is installed. Build context is the
repo root; gRPC stubs are generated from `proto/relay.proto` at image build
time.

Application code (`public/index.php`, `src/`, `workers/`) never touches
propagation headers, record headers, metadata, or tracing APIs. All
instrumentation is installed and enabled at the Dockerfile/environment level.

## Process model (one container, three processes)

`docker/entrypoint.sh` starts:

- **Apache mod_php on `:8080`** — `POST /relay`, `GET /info` via a Slim 4
  front controller (`FallbackResource /index.php`).
- **Kafka consumer** (`workers/kafka_consumer.php`) — long-running PHP CLI,
  ext-rdkafka high-level consumer, group `relay-{VARIANT}`, topic
  `RELAY_TOPIC` (default `relay-in-{VARIANT}`), `auto.offset.reset=earliest`,
  explicit per-message commits.
- **RabbitMQ consumer** (`workers/rabbit_consumer.php`) — long-running PHP
  CLI, php-amqplib, queue `RELAY_RABBIT_QUEUE` (default
  `relay-rmq-{VARIANT}`, declared defensively with durable=false,
  exclusive=false, auto_delete=false), manual acks.

Both consumers retry/reconnect forever; unreachable brokers never crash the
container (verified: the container starts and serves `/info` with no Kafka or
RabbitMQ present). The entrypoint additionally restarts a worker if its
process ever dies.

**Why Apache mod_php for HTTP?** dd-trace-php's supported-SAPI list covers
`apache2handler`, `fpm-fcgi` and `cli` — but not `cli-server`, so PHP's
built-in dev server is out. Elastic's agent likewise documents Apache/FPM
web SAPIs. OTel's extension works under any SAPI. Apache mod_php is the one
single-process option all three tools support; FPM+nginx would add moving
parts for no instrumentation-coverage gain.

**Long-running CLI workers under Datadog** use Datadog's documented
long-running-process configuration, set as per-process environment in the
entrypoint (not code): `DD_TRACE_CLI_ENABLED=1`,
`DD_TRACE_AUTO_FLUSH_ENABLED=1`, `DD_TRACE_GENERATE_ROOT_SPAN=0` (one trace
per consumed message, flushed when its spans close). These are scoped to the
workers because `DD_TRACE_GENERATE_ROOT_SPAN=0` container-wide would also
disable root spans for Apache web requests. The vars are inert in the
otel/elastic images.

## gRPC server: skipped (deliberate)

PHP cannot serve gRPC natively — there is no server in ext-grpc, only a
client. The only realistic route is RoadRunner's gRPC plugin, which was
rejected: it would replace the HTTP runtime with RoadRunner PHP workers,
i.e. a different SAPI with *worse* tool coverage (Elastic does not support
RoadRunner at all; OTel/Datadog RoadRunner support paths differ from the
Apache paths measured here), so the two server boundaries would no longer be
measuring the same application. Nothing listens on `:8081`; the driver
records all `grpc → *` boundaries for PHP as undelivered. That is a
legitimate empirical finding: no ready auto-instrumented gRPC server story
exists for PHP.

## Client libraries per protocol (and who instruments them)

| protocol | library | otel | datadog | elastic |
|---|---|---|---|---|
| HTTP out | ext-curl (raw `curl_exec`) | ✓ `opentelemetry-auto-curl` (injects `traceparent` on `curl_exec`/`curl_multi_exec`) | ✓ curl integration (all supported PHP versions) | ✓ curl is the agent's documented outgoing-HTTP instrumentation |
| gRPC out | ext-grpc (PECL) + `grpc/grpc`, `google/protobuf` (composer) | ✗ no auto-instr package exists | ✗ no grpc integration in dd-trace-php | ✗ not in supported technologies |
| Kafka | ext-rdkafka (PECL, pinned **6.0.5**) | ✓ `opentelemetry-auto-ext-rdkafka` (extracts + activates on `KafkaConsumer::consume`, injects on `ProducerTopic::producev`, ends span on `commit`) | ✓ Kafka integration (ext-rdkafka; 6.0.5 is the version in Datadog's tested matrix — hence the pin) | ✗ no Kafka support |
| RabbitMQ | php-amqplib (composer) | ✗ no php-amqplib package (only `opentelemetry-auto-ext-amqp`, a different client, publish-side only) | ✓ AMQP integration (php-amqplib 2.x–3.x, publish inject + consume extract) | ✗ no AMQP support |

Notes on the choices:

- **HTTP client — ext-curl over Guzzle.** Both are triple-covered on paper
  (dd: Guzzle 5–7; elastic: Guzzle 6–7; otel: `auto-guzzle`), but raw curl is
  each tool's lowest-level, primary-documented hook with exactly one
  instrumentation point — with Guzzle, dd/elastic can hit both the
  Guzzle-level and the underlying curl-level hooks, which muddies what is
  being measured. The relay uses one plain `curl_exec` per forward.
- **Kafka producer uses `producev`**, the headers-capable produce API — the
  method both the OTel and Datadog instrumentations hook for injection (the
  legacy `produce` cannot carry record headers at all). The relay passes no
  headers itself.
- **The instrumentation gaps in the table are findings, not bugs**: expect
  `* → grpc` to carry no propagation for all three tools, `kafka|rabbitmq`
  boundaries to be dark for elastic, and `rabbitmq` boundaries to be dark
  for otel.

## Tool install mechanisms

- **otel**: `opentelemetry` PECL extension (via `install-php-extensions`),
  plus composer packages `open-telemetry/sdk`,
  `opentelemetry-auto-slim` (inbound HTTP extraction — plain PHP has no
  generic inbound auto-instr, which is why the front controller is Slim),
  `opentelemetry-auto-curl`, `opentelemetry-auto-ext-rdkafka`. SDK
  bootstrapped by env only: `OTEL_PHP_AUTOLOAD_ENABLED=true` with
  `OTEL_TRACES_EXPORTER=none` (image defaults; compose can override).
  Propagation (`tracecontext,baggage`) is the SDK default and independent of
  export.
- **datadog**: the documented `datadog-setup.php` installer from the latest
  dd-trace-php release, `--php-bin=all`. Version reported at runtime via
  `phpversion('ddtrace')` in `/info`.
- **elastic**: `apm-agent-php` **v1.17.0** `.deb` (arch-selected:
  amd64/arm64 — arm64 packages are marked experimental by Elastic) via the
  documented `dpkg -i` install. The agent tolerates an unreachable
  `ELASTIC_APM_SERVER_URL` (logs errors, keeps serving; verified in the
  smoke test with no APM server present).

## gRPC stubs

Generated at image build time (not checked in) into `/app/gen` using
Debian bookworm's `protobuf-compiler` + `protobuf-compiler-grpc`
(`grpc_php_plugin`), then autoloaded via composer PSR-4 (`Cpmatrix\`,
`GPBMetadata\`). The composer `google/protobuf` runtime is `^4.27` (the 3.x
line is blocked by a Composer security advisory); the 4.x PHP runtime still
accepts bookworm's protoc 3.21 proto3 gencode — verified in the smoke test
by round-tripping a `SinkRequest` in the built image. Building from proto at
image build time keeps the stubs in lockstep with `proto/relay.proto`, same
as the python/ruby relays.

## Environment

`VARIANT`, `TOOL`, `RELAY_TOPIC` (default `relay-in-{VARIANT}`),
`KAFKA_BROKERS` (default `kafka:9092`), `RELAY_RABBIT_QUEUE` (default
`relay-rmq-{VARIANT}`), `RABBIT_URL` (default
`amqp://guest:guest@rabbitmq:5672/`), `SINK_HTTP` (default
`http://sink:9080`), `SINK_GRPC` (default `sink:9081`),
`SINK_RABBIT_QUEUE` (default `sink-rmq`), plus each tool's own
`OTEL_*` / `DD_*` / `ELASTIC_*` config. `GET /info` reports
`tracer_version`, lib versions, and every `DD_*`/`OTEL_*`/`ELASTIC_*` env
var under `config`.

## Build

```
docker build -f relays/php/Dockerfile --target otel    .
docker build -f relays/php/Dockerfile --target datadog .
docker build -f relays/php/Dockerfile --target elastic .
```
