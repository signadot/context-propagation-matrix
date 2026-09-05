# Context Propagation Matrix — Harness Design

This harness empirically answers one question:

> When a service instrumented with tool T in language L receives a request over
> protocol A and makes a follow-up call over protocol B, which context
> propagation headers survive the hop?

Every cell of the published support matrix is produced by actually running the
combination, never by quoting vendor documentation.

## Dimensions

- **Tools**: `otel` (OpenTelemetry), `datadog` (dd-trace), `elastic` (Elastic
  APM classic agents), `newrelic` (New Relic agents; deferred pending free-tier
  license key)
- **Languages**: `java`, `node`, `python`, `go`, `dotnet`, `ruby`, `php`, `rust`
- **Boundaries** (inbound protocol → outbound protocol): all 16 of
  `{http, grpc, kafka, rabbitmq} × {http, grpc, kafka, rabbitmq}`
- **Headers observed** (primary): `traceparent`, `tracestate`, `baggage` (W3C),
  `ot-baggage-*` (OpenTracing legacy). Secondary observations recorded verbatim:
  `x-datadog-*`, `newrelic`, `b3`/`x-b3-*`, and everything else that arrives.

A **variant** is one tool × language pair, named `{tool}-{lang}` (e.g.
`datadog-java`). Config variations get suffixed names (e.g.
`datadog-java-defaults`, `datadog-python-kafkaprop`). Not every tool covers
every language (e.g. Elastic has no Rust agent); `run.sh` holds the
authoritative variant list.

## Components

```
driver ──(seeded headers via http|grpc|kafka)──▶ relay-{variant} ──(http|grpc|kafka)──▶ sink
                                                                                        │
driver ◀──────────────── GET /observations?test_id=... ────────────────────────────────┘
```

### Relay (one app per language, three instrumentation variants)

The relay is deliberately naive application code. It listens on all four
protocols and, on receiving a message, forwards a new message to the sink over
the protocol named in the payload. **Relay code must never touch headers,
metadata, record headers, or tracing APIs directly** — header propagation is
exclusively the job of the tool's instrumentation, because that is what we are
measuring. The exceptions are Go and Rust, where no auto-instrumentation
exists for any of the tools; those relays use each vendor's documented
contrib libraries / carrier APIs and idiomatic context plumbing, and the
matrix labels their results with the exact libraries used. PHP is a partial
exception of a different kind: it serves HTTP and consumes Kafka/RabbitMQ,
but no tracer-supported PHP topology can serve gRPC (see
`relays/php/README.md`), so its gRPC-inbound cells are undeliverable by
construction.

Endpoints (identical for every variant):

- HTTP: `POST /relay` on `:8080`, body `{"test_id": "...", "forward_via": "http"|"grpc"|"kafka"|"rabbitmq"}`
- gRPC: `cpmatrix.Relay/Handle` on `:8081`, same fields
- Kafka: consume topic `relay-in-{variant}` (JSON value, same fields), group `relay-{variant}`, `auto.offset.reset=earliest`
- RabbitMQ: consume queue `relay-rmq-{variant}` (JSON body, same fields) from
  `RABBIT_URL` (default `amqp://guest:guest@rabbitmq:5672`). Queue
  properties (must match on declare): durable=false, auto-delete=false,
  exclusive=false. The driver declares the queue before publishing, so
  messages published before the relay subscribes are held by the broker — no
  warmup needed, but relays should also declare it defensively on startup.
- `GET /info` on `:8080` →
  `{"variant","tool","language","tracer_version","runtime_version","libs":{...}}`
  (used by the driver to stamp versions into results)

Forwarding targets (fixed):

- http → `POST http://sink:9080/deliver` body `{"test_id": "..."}`
- grpc → `cpmatrix.Sink/Deliver` at `sink:9081`
- kafka → produce to topic `sink-in`, JSON value `{"test_id": "..."}`
- rabbitmq → publish to queue `sink-rmq` (default exchange, routing key
  `sink-rmq`, same queue properties), JSON body `{"test_id": "..."}`

The relay handles the inbound message and performs the outbound call in
whatever way is idiomatic for that platform (inside the HTTP handler / gRPC
method / consumer callback), so the tool's notion of "current context" is
whatever a normal application would have at that point.

### Sink (Go, uninstrumented)

Records exactly what arrives, verbatim:

- HTTP `:9080 POST /deliver` → all request headers
- gRPC `:9081 cpmatrix.Sink/Deliver` → all incoming metadata (`-bin` values base64-encoded)
- Kafka: consume `sink-in` from earliest → all record headers (non-UTF-8 values recorded as `base64:<...>`)
- RabbitMQ: consume `sink-rmq` → all AMQP header-table entries (strings pass
  through; non-UTF-8 bytes as `base64:<...>`; other types via `%v`)

Each arrival is an **observation**:

```json
{
  "test_id": "datadog-java--http-kafka--3f2a9c1e",
  "transport": "kafka",
  "headers": { "baggage": ["sd-routing-key=rk-3f2a9c1e,team=alpha"], ... },
  "received_at": "2026-09-04T19:03:11Z"
}
```

Observations are kept in memory and appended to `/data/observations.jsonl`.
Query: `GET /observations?test_id=X` → `{"observations":[...]}`.

### Driver (Go, uninstrumented)

For each variant it runs all 16 boundaries. Per test:

1. Generate `test_id = {variant}--{in}-{out}--{rand8}`, a random W3C
   `trace_id`/`span_id`, and routing key `rk-{rand8}`.
2. Seed the inbound call with exactly these headers (HTTP headers, gRPC
   metadata, Kafka record headers, or AMQP header-table entries,
   respectively):

   | header | value |
   |---|---|
   | `traceparent` | `00-{trace_id}-{span_id}-01` |
   | `tracestate` | `sdtest=v1` |
   | `baggage` | `sd-routing-key={rk},team=alpha` |
   | `ot-baggage-sd-routing-key` | `{rk}` |
   | `x-sd-canary` | `{test_id}` |

   `x-sd-canary` is a control: no instrumentation forwards arbitrary headers,
   so if it ever reaches the sink the relay is proxying rather than
   re-emitting, and the whole variant is invalid.
3. Poll the sink for an observation with `test_id` (45s timeout; before the
   first Kafka-inbound test the driver sends a warm-up message and waits up to
   120s for the consumer group to join).
4. Evaluate the observation into per-header statuses (below) and write raw +
   evaluated data to `results/raw/{variant}.json`.

Kafka notes: the driver produces directly to `relay-in-{variant}` and never
consumes; topics are auto-created; every message is self-describing via
`test_id` so duplicate redelivery after restarts is harmless.

## Per-header status semantics

- `traceparent`: `same_trace` (arrived with seeded trace-id; span-id will and
  should differ) | `new_trace` (present, different trace-id) | `absent`
- `tracestate`: `preserved` (our foreign entry `sdtest=v1` still listed —
  vendors may prepend their own entries, that's fine) | `present_entry_lost`
  (header present, our entry gone) | `absent`
- `baggage`: `propagated` (`sd-routing-key={rk}` parseable from the W3C list)
  | `partial` (header present, our key missing) | `absent`
- `ot_baggage`: `propagated` (`ot-baggage-sd-routing-key == {rk}`) | `absent`
- `datadog_native`: informational; `present_same_trace` if
  `x-datadog-trace-id` equals the low 64 bits of the seeded trace-id (decimal)
  | `present_new_trace` | `absent`
- `canary_leak`: boolean, must be `false`
- `delivered`: boolean; `false` means the relay never forwarded anything —
  itself a reportable finding (e.g. instrumentation broke the app or the
  library isn't supported)

## Tool configuration (kept deliberately bounded)

- **otel**: SDK/agent defaults — `OTEL_PROPAGATORS=tracecontext,baggage`,
  exporters disabled/none (`OTEL_TRACES_EXPORTER=none` where applicable).
  No collector needed: header injection is independent of export.
- **datadog**: primary run with
  `DD_TRACE_PROPAGATION_STYLE=datadog,tracecontext,baggage` (the configuration
  from [dd-trace-java#11286](https://github.com/DataDog/dd-trace-java/issues/11286));
  a secondary run with library defaults (unset) recorded as variant
  `datadog-{lang}-defaults`. No Datadog agent container: tracers buffer and
  drop when the agent is unreachable, header injection is unaffected
  (`DD_TRACE_STARTUP_LOGS=false` to keep logs quiet).
- **newrelic**: distributed tracing enabled (`NEW_RELIC_DISTRIBUTED_TRACING_ENABLED=true`),
  which is the agents' default W3C mode. New Relic agents require a successful
  connect handshake (license key) before they create traces, so these variants
  need `NEW_RELIC_LICENSE_KEY` in the environment (free-tier key is fine).
  Without a key the driver records the variant as `not_run`.

## Results schema

`results/raw/{variant}.json`:

```json
{
  "variant": "datadog-java",
  "tool": "datadog",
  "language": "java",
  "config": { "DD_TRACE_PROPAGATION_STYLE": "datadog,tracecontext,baggage" },
  "info": { "tracer_version": "1.x.y", "runtime_version": "temurin 17.0.x", "libs": {"kafka": "spring-kafka x.y"} },
  "ran_at": "RFC3339",
  "tests": [
    {
      "boundary": "http->kafka",
      "test_id": "...",
      "delivered": true,
      "seeded": { "trace_id": "...", "span_id": "...", "routing_key": "...", "headers": { ... } },
      "observed": { "transport": "kafka", "headers": { ... } },
      "status": {
        "traceparent": "same_trace",
        "tracestate": "preserved",
        "baggage": "absent",
        "ot_baggage": "propagated",
        "datadog_native": "present_same_trace",
        "canary_leak": false
      }
    }
  ]
}
```

`matrixgen` renders `results/MATRIX.md` (per-tool tables: language × boundary,
one glyph per primary header) directly from these files. Raw observations stay
in the JSON so any cell can be audited down to the exact header values.

## Library choices per relay

Chosen to be each vendor's documented happy path; recorded in `/info` and in
the matrix footnotes.

| language | server / http client | grpc | kafka | rabbitmq |
|---|---|---|---|---|
| java | Spring Boot 3 (Tomcat) / RestTemplate (HttpURLConnection) | grpc-java | spring-kafka (kafka-clients) | spring-rabbit (amqp-client) |
| node | express / axios (node http) | @grpc/grpc-js + proto-loader | kafkajs | amqplib |
| python | flask / requests | grpcio | confluent-kafka | pika |
| go | net/http | google.golang.org/grpc | per-tool contrib (kotel / dd contrib / nrsarama) | amqp091-go (per-tool wiring) |
| dotnet | ASP.NET Core minimal API / HttpClient | Grpc.AspNetCore + Grpc.Net.Client | Confluent.Kafka | RabbitMQ.Client |
| ruby | sinatra (puma) / net-http | grpc gem | per-tool (rdkafka / ruby-kafka), see relay README | bunny |
| php | Apache mod_php + Slim 4 / Guzzle | ext-grpc (client only; no server — see relay README) | ext-rdkafka | php-amqplib |
| rust | axum / reqwest | tonic | rdkafka | lapin |
