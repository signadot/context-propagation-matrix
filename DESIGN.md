# Context Propagation Matrix — Harness Design

This harness empirically answers one question:

> When a service instrumented with tool T in language L receives a request over
> protocol A and makes a follow-up call over protocol B, which context
> propagation headers survive the hop?

Every cell of the published support matrix is produced by actually running the
combination, never by quoting vendor documentation.

## Dimensions

- **Tools**: `otel` (OpenTelemetry), `datadog` (dd-trace), `elastic` (Elastic
  APM classic agents), `newrelic` (New Relic agents; requires
  `NEW_RELIC_LICENSE_KEY` in `.env` — free tier), plus a `none` **baseline**
  per language — the same naive app with no agent/wrapper/gem attached,
  proving that nothing propagates unless a tool provides it (and surfacing
  anything the platform itself propagates, e.g. .NET's built-in `Activity`
  machinery). See Roadmap below for further tool candidates.
- **Languages**: `java`, `node`, `python`, `go`, `dotnet`, `ruby`, `php`, `rust`
- **Boundaries** (inbound protocol → outbound protocol): every pair in
  `{http, grpc, kafka, rabbitmq} × {http, grpc, kafka, rabbitmq}`
- **Headers observed** (primary): `traceparent`, `tracestate`, `baggage` (W3C),
  `ot-baggage-*` (OpenTracing legacy). Secondary observations recorded verbatim:
  `x-datadog-*`, `newrelic`, `b3`/`x-b3-*`, and everything else that arrives.

A **variant** is one tool × language pair, named `{tool}-{lang}` (e.g.
`datadog-java`). Config variations get suffixed names (e.g.
`datadog-java-defaults`, `datadog-python-kafkaprop`), and so do alternative
*setup mechanisms*: `otel-go-compiletime` and `datadog-go-orchestrion`
compile unmodified naive Go source with each vendor's build-time weaver
(`relays/go-otelc/`, `relays/go-orchestrion/`), measured alongside the
hand-wired `otel-go`/`datadog-go` rows. Not every tool covers every language
(e.g. Elastic has no Rust agent); `variants.yaml` holds the authoritative
variant list.

## Components

```
driver ──(seeded headers via http|grpc|kafka)──▶ relay-{variant} ──(http|grpc|kafka)──▶ sink
                                                                                        │
driver ◀──────────────── GET /observations?test_id=... ────────────────────────────────┘
```

### Relay (one app per language, one build target per tool)

The relay is deliberately naive application code. It listens on all four
protocols and, on receiving a message, forwards a new message to the sink over
the protocol named in the payload. **Relay code must never touch headers,
metadata, record headers, or tracing APIs directly** — header propagation is
exclusively the job of the tool's instrumentation, because that is what we are
measuring. The exceptions are Go and Rust, where no *runtime* attach exists
for any of the tools; those relays use each vendor's documented contrib
libraries / carrier APIs and idiomatic context plumbing, and the matrix
labels their results with the exact libraries used. Go additionally has two
**build-time weaving** variants (`otel-go-compiletime`,
`datadog-go-orchestrion`) whose application source is fully naive — the
weaver injects instrumentation during compilation, so the no-touching rule
holds absolutely for them. PHP is a partial
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
  "headers": { "baggage": ["routing-key=rk-3f2a9c1e,team=alpha"], ... },
  "received_at": "2026-09-04T19:03:11Z"
}
```

Observations are kept in memory and appended to `/data/observations.jsonl`.
Query: `GET /observations?test_id=X` → `{"observations":[...]}`.

### Driver (Go, uninstrumented)

For each variant it runs every boundary. Per test:

1. Generate `test_id = {variant}--{in}-{out}--{rand8}`, a random W3C
   `trace_id`/`span_id`, and routing key `rk-{rand8}`.
2. Seed the inbound call with exactly these headers (HTTP headers, gRPC
   metadata, Kafka record headers, or AMQP header-table entries,
   respectively):

   | header | value |
   |---|---|
   | `traceparent` | `00-{trace_id}-{span_id}-01` |
   | `tracestate` | `cptest=v1` |
   | `baggage` | `routing-key={rk},team=alpha` |
   | `ot-baggage-routing-key` | `{rk}` |
   | `x-cp-canary` | `{test_id}` |

   `x-cp-canary` is a control: no instrumentation forwards arbitrary headers,
   so if it ever reaches the sink the relay is proxying rather than
   re-emitting, and the whole variant is invalid.

   The names are constants in `internal/eval`, which the driver seeds from.
   They name no vendor and no product. Every result records the headers it
   was seeded with, and it is judged against those.

   Until 16 November 2026 the same values were seeded under other names.
   Renaming them did not change the contract, so `matrix_version` stayed
   at 1: `run-2026-11-16.5` measured all 47 variants under the new names,
   and none of its 3,008 cells differs from the variant's last result under
   the old ones. The results the site reads were then relabelled, so that
   they show the names above whenever they were measured: every document
   in the `variant-*` releases, and `results.json` of the twelve runs from
   `run-2026-11-13.7` to `run-2026-11-16.5`. Nothing else in them was
   changed. The `backtest-*` releases, the older runs, every `raw.tar.gz`
   and the files under `results/` are as they were recorded, under the
   names of their time; a result is judged against the names it records,
   so they evaluate as before.
3. Poll the sink for an observation with `test_id` (45s timeout; before the
   first Kafka-inbound test the driver sends a warm-up message and waits up to
   120s for the consumer group to join).
4. Evaluate the observation into per-header statuses (below) and write raw +
   evaluated data to `results/raw/{variant}.json`.

Kafka notes: the driver produces directly to `relay-in-{variant}` and never
consumes; topics are auto-created; every message is self-describing via
`test_id` so duplicate redelivery after restarts is harmless.

## Per-header status semantics

The evaluation lives in `internal/eval` (unit-tested, and replayed against
recorded raw results). The driver calls it for every delivered hop and writes
the statuses below to the raw file. `tracestate` and `baggage` are list
headers: when a carrier holds several fields of the same name, their values
are joined with `,` before evaluating.

- `traceparent`: `same_trace` (arrived with seeded trace-id; span-id will and
  should differ) | `new_trace` (present, different trace-id) | `absent`
- `tracestate`: `preserved` (our foreign entry `cptest=v1` still listed —
  vendors may prepend their own entries, that's fine) | `present_entry_lost`
  (header present, our entry gone) | `absent`
- `baggage`: `propagated` (`routing-key={rk}` parseable from the W3C list)
  | `partial` (header present, our key missing) | `absent`
- `ot_baggage`: `propagated` (`ot-baggage-routing-key == {rk}`) | `partial`
  (header present, different value) | `absent`
- `datadog_native`: informational; `present_same_trace` if
  `x-datadog-trace-id` equals the low 64 bits of the seeded trace-id (decimal)
  | `present_new_trace` | `absent`. Recorded for every tool and kept in the
  raw file for compatibility; nothing published reads it, `vendor_native`
  (below) is evaluated per tool instead
- `canary_leak`: boolean, must be `false`
- `delivered`: boolean; `false` means the relay never forwarded anything —
  itself a reportable finding (e.g. instrumentation broke the app or the
  library isn't supported)

`cpm aggregate` adds three observations to each hop of `results.json`. It
evaluates them itself from the observed headers stored in the raw file, so
re-aggregating raw results that predate them fills them in. They are
informational: no cell outcome and no count depends on them.

- `vendor_native`: whether the tool's own trace header carried the seeded
  trace, judged on the tool's `native_headers` in `variants.yaml`:
  `present_same_trace` | `present_new_trace` (another trace-id, or a value
  that cannot be decoded) | `absent`. Datadog: `x-datadog-trace-id` equals the
  low 64 bits of the seeded trace-id (decimal). Elastic: the trace-id in
  `elastic-apm-traceparent`. New Relic: `d.tr` in the base64 JSON `newrelic`
  header. Omitted for tools without native headers (`otel`, `none`)
- `traceparent_span`: only when `traceparent` is `same_trace`. `new_span`
  (the span-id differs from the seeded one, as it must when a tracer took
  part) | `same_span` (the seeded span-id came back out: the relay passed the
  header through verbatim, which scores as a `traceparent` pass although no
  instrumentation was involved; see the otel note under Tool configuration).
  `same_span` hops are also listed in `summary.traceparent_passthroughs` as
  `"{variant} {boundary}"`, the way `summary.canary_leaks` lists leaks
- `traceparent_flags`: only when `traceparent` is `same_trace`. The observed
  trace-flags field. The driver seeds `01` (sampled); `00` continues the
  trace but marks it not sampled, which is what the services after the relay
  go by when deciding whether to record it

These keys are optional: left out where they do not apply, and absent from
documents aggregated before they existed. Adding them therefore leaves
`schema_version` at 2.

## Tool configuration (kept deliberately bounded)

- **otel**: SDK/agent defaults — `OTEL_PROPAGATORS=tracecontext,baggage`,
  exporters disabled/none (`OTEL_TRACES_EXPORTER=none` where applicable).
  No collector needed. Header injection is independent of export for most
  SDKs, but not all: `@opentelemetry/sdk-node` (otel-node) and the otelc
  runtime (otel-go-compiletime) register no `TracerProvider` when no trace
  exporter is configured, so with `none` they either forward the inbound
  headers verbatim or inject nothing — every boundary passes or none does,
  which says nothing about the instrumentation. Those two run with
  `OTEL_TRACES_EXPORTER=console` (spans to stdout, no network). A variant
  whose observed `traceparent` carries the seeded span-id on every delivered
  hop is the signature of this problem: an active tracer always starts a new
  span on the inbound side.
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
  need `NEW_RELIC_LICENSE_KEY` in `.env` (free-tier key is fine; `run.sh`
  skips newrelic variants when it is unset). Some agents connect lazily on
  first traffic (python, php), so `run.sh` primes each newrelic variant with
  a discarded driver pass and measures the steady state. See FINDINGS.md §9
  for the no-key and `Enabled: false` control runs (`results/nokey/`).

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

These per-variant files are the harness's *internal* format. The published
contract is **results schema v2**, produced by `go run ./cmd/cpm aggregate`
from `results/raw/*.json` plus the registry (`variants.yaml`):

- `results/latest/results.json` — one document per run: a run manifest
  (`run_id`, `harness_commit`, broker images, timestamps, and for a push-
  triggered run `partial: true` plus `reran: [ids]` naming the variants
  measured in that run; everything else was carried over from the previous
  release and keeps its own `ran_at`), the dimensions
  (tools, languages, protocols, boundaries, headers, setup tiers, verbatim
  from the registry so consumers never need the YAML), and one entry per
  variant with tracer coordinates + observed version, runtime, client
  library versions, config, one entry in `hops` per boundary (evidence:
  seeded and observed headers, delivery, canary) and one in `cells` per
  header per boundary (16 and 64 today). Each cell has a stable id
  `{variant}/{header}/{inbound}-{outbound}` and a normalised `outcome`
  (`pass` · `partial` · `new_trace` · `fail` · `undelivered` · `not_run`)
  next to the driver's raw status. Annotations (below) are embedded, and a
  `summary` carries per-header totals and a tool × language scorecard.

  A measurement is identified independently of the run that contains it.
  Each measured variant carries `tracer.version` (what the relay reported
  at run time), `measured_in` (the run id of the release whose job ran the
  hops), `measured_commit` (the harness commit that job ran) and
  `matrix_version` (the measurement contract, from `variants.yaml`, bumped
  by hand when a dimension or an outcome's meaning changes; the run manifest
  carries the current value as `run.matrix_version`). A variant measured in
  this run has `measured_in == run.run_id`; a carried-over one keeps the
  values it had in the release it came from, so a (variant, tracer version)
  pair resolves to the same measurement in every run that carries it. The
  site uses these to present results per tracer version and to say where
  and when each one was measured.

  `cpm validate` checks the part of the bump rule it can see. It fails when
  the protocols, the boundaries, the headers or a header's scoring lists in
  `variants.yaml` differ from the `dimensions` of the latest release and
  `matrix_version` has not moved. A change in what an outcome means that
  leaves those alone still has to be bumped by hand.

  An `in-code` variant also carries `legs`, copied from the registry: per
  protocol, `inbound` and `outbound`, each one of the `dimensions.wirings`
  ids. Variants without legs have no `legs` key, and `dimensions.wirings` is
  omitted when the registry defines none.
- `results/latest/MATRIX.md` — the human-readable tables, rendered from the
  same document.
- `results/latest/changes.json` — when a previous run is given
  (`-prev`), every cell whose outcome differed, tracer version bumps, and
  added/removed variants. CI uses it to decide whether to post a summary.

None of these are tracked in git (`results/raw`, `results/latest` and
`results/MATRIX.md` are ignored). The published copy of a run is a GitHub
Release tagged `run-YYYY-MM-DD[.N]` with five assets:

| Asset | Contents |
|---|---|
| `results.json` | the schema v2 document above |
| `MATRIX.md` | the rendered tables |
| `changes.json` | diff against the previous `run-*` release (absent on the first) |
| `raw.tar.gz` | `raw/{variant}.json` for every variant in the document — the evidence, and the input the next run carries over from |
| `logs.tar.gz` | relay and sink container logs for the variants measured in that run |

Back-tests of older tracer versions publish `backtest-{variant}-{version}`
pre-releases with a single-variant `results.json`, `MATRIX.md` and the raw
file.

### What counts as a measurement: `invalid` and `cpm gate`

A variant can run all its hops and still not be a measurement of its tool.
`cpm aggregate` judges every measured variant from its hops and, when the
measurement is not valid, records why on the variant in `results.json` as
`invalid`, a list of `{kind, boundaries, detail}`. The field is absent on a
valid variant and in documents written before it existed. The kinds:

- `canary_leak`: `x-cp-canary` reached the sink on the listed `boundaries`,
  so the relay copies inbound headers itself and a `pass` is not the tool's
  doing.
- `transport_mismatch`: on the listed `boundaries` the hop was delivered
  over a protocol other than the boundary's outbound one.
- `agent_inactive` (instrumented variants only, no `boundaries`): the relay
  showed no sign of its tool, so its `fail` cells mean "did not attach", not
  "does not propagate". An old agent on a runtime it does not support is the
  usual cause.

The `agent_inactive` rule: on every delivered hop, the tracing header names
the sink observed are a subset of what the uninstrumented baseline of the
same language (`none-{language}`) shows on the same boundary.

- Tracing header names are each `headers[].wire_name`, anything starting
  `ot-baggage-`, and every tool's `native_headers`, read from the document's
  own dimensions. Any other header is ignored.
- The baseline is needed because a runtime can send tracing headers with no
  agent installed: .NET does, on its HTTP and gRPC clients. The comparison
  is per boundary because on `http->http` a working OpenTelemetry .NET agent
  sends the same names as the bare runtime.
- The baseline is the document's own `none-{language}` or, for a back-test,
  the one in the latest run (`cpm aggregate -baseline`). With neither, the
  comparison is with nothing ("no tracing header at all") and `detail` says
  so.
- It is a floor, not a health check. An agent that half-works shows headers
  and is not flagged: the New Relic Node agent without a licence key still
  sends bare W3C headers on six boundaries (`results/nokey/`, FINDINGS.md
  §9).

Nothing is published until `cpm gate` has judged the document. It recomputes
the verdict from the hops, so a document written before `invalid` existed is
gated the same way.

- A matrix run publishes nothing (no release, no per-version filing, no site
  rebuild) when a variant measured in that run is `invalid` for any of the
  three reasons. A variant carried over from an earlier release only warns,
  because the run that measured it was the one to judge it.
- A back-test publishes nothing for `canary_leak` or `transport_mismatch`.
  For `agent_inactive` it is published and filed with a warning, carrying
  the verdict. That a version does not attach on today's runtime is worth
  recording once, and a pair with neither a release nor an index entry would
  be proposed again by every `cpm discover -skip-published`.
- Either publishes nothing when a variant measured there has a boundary no
  hop ran on (`boundary_not_run` in the gate's output). It is not an
  `invalid` kind, because the boundary's `not_run` cells already say it.

That last rule is one of three that keep the driver and the registry in
step. The driver runs every pair of its own protocol list and does not read
`variants.yaml`, which lists the boundaries a variant is scored on.

- `go test ./driver` fails when the driver's pairs and the registry's
  `boundaries` are not the same set.
- `cpm aggregate` stops on a raw hop whose boundary the registry does not
  list. It has no cells to be filed under.
- `cpm gate` refuses a registry boundary the driver did not run, as above.

Runs and back-tests are the record of each *job*. The same measurements are
also filed the way people look for them — by tracer version — in one rolling
release per instrumented variant, `variant-{id}`:

| Asset | Contents |
|---|---|
| `{version}.json` | a single-variant schema v2 document for that tracer version: the variant, the run manifest it was measured in, the dimensions, the annotations that touch it and a summary for it alone |
| `index.json` | every version filed, newest first, with its asset name, `released_at` (when the tracer's registry published the version; left out when unknown), `measured_in` / `measured_at` / `measured_commit` / `matrix_version`, the reported version string and per-header counts, plus the document's `invalid` when the measurement is not valid (so an agent that never attached can be told from one that propagates nothing without opening the document) |
| `versions.json` | the bundle: `{schema_version, index, documents}` — `index` is `index.json`, `documents` maps each listed version to its `{version}.json` byte for byte. One download gives a consumer the variant's whole history; the site reads only this asset |

Byte for byte as filed, with one exception. Cells are the measurement and
never change; annotations are the explanation and belong to the YAML. So
every publish of a variant — a run, a back-test, or `republish.yml` after a
merge to `annotations/` — re-derives the annotations of every document in the
bundle from the current `annotations/` (`annotations.Reannotate`: the same
evaluation a back-test aggregate makes, judged on that variant alone), while
each `{version}.json` asset keeps the annotations it was filed with. A note
written or corrected after a version was measured thus reaches that version's
page: attached to its failing cells on the versions before the fix, resolved
with `fixed_in` on the versions from it.

`released_at` is the one field of an index entry that does not come from a
measurement. It is a property of the version: the time the tracer's registry
(the variant's `tracer` coordinate in `variants.yaml`) published it. `cpm
versions` looks it up at publish for the entries that have none, and an entry
keeps it from then on, also when the version is measured again. The field is
optional. A lookup that fails is a warning, the entry is published without a
date and tried again at the next publish of the variant. Where each registry
states the date is in `internal/versions/dates.go`; for a Go module it is the
time of the tagged commit, and PECL and New Relic's download index name no
time zone, so theirs are read as UTC. The per-version documents do not carry
the date.

`{version}` is the *pinned* tracer version (`tracer.pinned`: what `cpm pins`
reads from the manifest the relay was built from, the value a bump PR names
and a back-test tag carries), not the string the relay reports —
`open-telemetry/sdk 1.15.0 (ext-opentelemetry 1.2.1)` is filed as `1.2.1`.
Every matrix run files the variants it measured and every back-test files
its one pair (`scripts/publish-versions.sh`, wrapping `cpm versions`); when
a version is measured again the latest measurement replaces the earlier one.
`scripts/rebuild-versions.sh` recreates all of them from the run and
back-test releases; `scripts/publish-versions.sh -rebuild` re-derives
`index.json` and `versions.json` from the documents already on a release;
`scripts/publish-versions.sh -refresh` re-annotates `versions.json` in place.
The site reads one `versions.json` per variant — ~50 downloads per build,
each of which must succeed for the build to pass — instead of every run
release or every per-version document.
A back-test does not build differently: `backtest.yml` rewrites the pin
behind the variant in its checkout (`cpm pins set -variant V -version X`,
the edit a Dependabot PR would make, pointed backwards) and builds the
unchanged Dockerfile, so the measured image is produced exactly the way the
matrix produces it. Two checks make sure the pin took, because a document
filed under a version its relay did not run is worse than no document
(shared Go modules once built otel-go 1.40–1.43 all at 1.44.0; `go mod tidy`
once pulled otel-go 1.27.0–1.30.0 back to 1.47.0, see `companions` in
`pins.yaml`). `cpm pins set` reads the manifest back after writing it and
fails if it does not hold the requested version. Then, before publishing,
the job checks that the version the relay reported contains the requested
one as a token (`v1.5.0` and `open-telemetry/sdk 1.15.0 (ext-opentelemetry
1.5.0)` both satisfy `1.5.0`) and fails otherwise. This is why every relay
must report the version of the package it is pinned by — the compile-time
Go relays stamp it with `-ldflags "-X main.tracerVersion=…"` at build, since
a weaver's module is not among the binary's build-info dependencies.
`scripts/fetch-latest.sh` (or `./run.sh fetch [-raw]`) restores a release
into `results/latest` and `results/raw`, and `republish.yml` re-renders the
latest release in place and re-annotates every per-version bundle when
`annotations/` changes.

A run's release, and a back-test's when it is first published, is tagged on
the commit that was measured. GitHub refuses that when the commit changes a
workflow file and `main` has since moved on. The release is then created
with its tag on `main`'s tip, and its notes say so
(`scripts/create-release.sh`). Nothing reads where a tag sits: the commit is
in the document (`run.harness_commit`, and `measured_commit` on each
variant).

contextpropagation.io consumes only `results.json` (from GitHub Releases), so
the schema is versioned (`schema_version: 2`) and changes to it are breaking
changes for the site.

## The registry: `variants.yaml`

Everything that used to be hardcoded across `run.sh`, `docker-compose.yml`
profiles and `matrixgen` now lives in one file. Each variant entry names its
tool, language, flavour (`default`, `defaults`, `kafkaprop`, `orchestrion`,
`compiletime`, …), display name, setup tier, one-line attach mechanism,
tracer package coordinates (registry + package + GitHub repo — what version
discovery reads) and the relay source directory. `go run ./cmd/cpm validate`
enforces that docker-compose.yml has exactly one `relay-<id>` service with
`profiles: [<id>]` per registered variant and no unregistered relays, and
that every annotation selector matches at least one real cell.

An `in-code` variant must also give `legs`: for each protocol, whose code
moves the context on the relay's inbound and outbound side, a `library` the
application wraps its client or server with, or `app` code that calls the
tracer's API itself (the `wirings:` block defines both). Variants in any other
tier, and baselines, must not have legs, because their application has no
propagation code to describe. A leg records how the relay is wired, not
whether it works; the cells measure that.

## Annotations: `annotations/*.yaml`

Root causes live as data next to the cells they explain, not as prose that
drifts. Each annotation has a `match` list of selectors (tool, language,
variant glob, flavour, header list, boundary glob such as `*->kafka`), a
one-line `cause`, a markdown `explanation`, upstream `links`, and a `status`
(`open`, `by-design`, `config`, `app-limitation`, `wontfix`, `fixed` +
`fixed_in`). The aggregator resolves selectors against the run's cells and
embeds the matched cell ids in `results.json`; a selector that matches
nothing fails CI, and one whose cells all pass is flagged so it can be
marked fixed. FINDINGS.md and REFERENCES.md remain the long-form narrative.

## Library choices per relay

Chosen to be each vendor's documented happy path; recorded in `/info` and in
the matrix footnotes.

| language | server / http client | grpc | kafka | rabbitmq |
|---|---|---|---|---|
| java | Spring Boot 4 (Tomcat) / RestTemplate (HttpURLConnection) | grpc-java | spring-kafka (kafka-clients) | spring-rabbit (amqp-client) |
| node | express / axios (node http) | @grpc/grpc-js + proto-loader | kafkajs | amqplib |
| python | flask / requests | grpcio | confluent-kafka | pika |
| go | net/http | google.golang.org/grpc | per-tool contrib (kotel / dd contrib / nrsarama) | amqp091-go (per-tool wiring) |
| go (build-time) | net/http | google.golang.org/grpc | IBM/sarama (orchestrion) · segmentio/kafka-go (otelc) — each weaver's supported client | amqp091-go (otelc instruments it; Orchestrion has no integration) |
| dotnet | ASP.NET Core minimal API / HttpClient | Grpc.AspNetCore + Grpc.Net.Client | Confluent.Kafka | RabbitMQ.Client |
| ruby | sinatra (puma) / net-http | grpc gem | per-tool (rdkafka / karafka), see relay README | bunny |
| php | Apache mod_php + Slim 4 / ext-curl | ext-grpc (client only; no server — see relay README) | ext-rdkafka | php-amqplib |
| rust | axum / reqwest | tonic | rdkafka | lapin |

## Version pinning & updates

Nothing in the harness installs "latest": every dependency manifest pins
exact versions and every curl-fetched agent is a version ARG in its
Dockerfile, so a rebuild months from now produces the same images and the
matrix stays attributable to exact tracer versions (each table row carries
the version that produced it). The Node, PHP and Python relays also commit a
lockfile for the application's own dependencies (`relays/node/package-lock.json`,
`relays/php/composer.lock`, `relays/python/requirements.txt` compiled from
`requirements.in`), installed with `npm ci` / `composer install` / `pip install`, so
their transitive packages are fixed too. pip installs the compiled file without
looking at `requirements.in`, so `verify.yml` compares the two
(`scripts/check-python-requirements.sh`). The tracer installed on top of it
(and whatever the tracer pulls in) is not locked: it resolves at build time,
because the tracer version is the variable being measured.

The Ruby relay is locked differently. It commits a `Gemfile.lock` beside
each tool's Gemfile (`relays/ruby/gemfiles/<tool>/`), installed with
`BUNDLE_FROZEN`. That lock holds the tracer and what it pulls in as well,
because the tracer is a line of the same Gemfile and Bundler resolves a
Gemfile as a whole. Whatever moves the tracer moves the lock with it:
`cpm pins` runs `bundle lock --update <tracer gem>`, which leaves the
application's gems where the lock has them.

Two bots move versions, and they do not overlap:

- **Dependabot** (`.github/dependabot.yml`) keeps the *application*
  dependencies and toolchain images current: gomod (root module, one module
  per hand-wired Go tool under `relays/go/<tool>`, and the two build-time
  weaving modules `relays/go-orchestrion` and `relays/go-otelc`), npm (app +
  one single-dependency manifest per Node
  tool under `relays/node/tools/`), pip (`requirements.in`, compiled
  to `requirements.txt`, + `requirements-<tool>.txt`), bundler (one Gemfile per tool under
  `relays/ruby/gemfiles/`), composer, maven, nuget, cargo (one crate per
  Rust tool under `relays/rust/`), and docker base images — one grouped
  `app-deps` PR per ecosystem, across every tool directory of a language, so
  the variants of one language are never measured on different versions of
  the same application library (Ruby has a second group,
  `otel-instrumentation`, for the OpenTelemetry instrumentation gems, which
  are neither the pin nor a carrier). Every tracer package
  that `pins.yaml` lists is in that ecosystem's `ignore` list, so Dependabot
  never proposes a tracer bump (it also cannot see the Dockerfile ARGs).
  `dependabot-automerge.yml` requests squash auto-merge on each of its PRs,
  so a patch or minor bump merges once `verify.yml` and the review pass;
  a major is labelled `major` and left to a person, and a `blocked` label
  cancels auto-merge. As for tracer bumps, the merge starts no matrix run
  and the bumped relays are measured by the next scheduled one.
- **bump-agents.yml** moves the *tracers*. `pins.yaml` lists where every
  tracer's pin lives — Dockerfile ARG, go.mod module family, package.json,
  requirements file, Gemfile, Cargo.toml — keyed by the same
  registry/package coordinates as `variants.yaml`'s `tracer`. Every 6 hours
  `cpm pins latest` asks each pin's registry (Maven Central, GitHub
  Releases, PECL, Packagist, New Relic's download index, the Go module
  proxy, npm, PyPI, RubyGems, crates.io) for the newest stable version and,
  for each pin that is behind, `cpm pins latest -only <id> -apply` writes it
  the way the ecosystem expects (`go get` + `go mod tidy` for the whole
  module family, `cargo update` for the crate family, a JSON key, every
  `==` line, a `gem` line with `bundle lock`, an `ARG`). There is exactly
  one PR per pin: branch
  `bump/<id>`, title `bump(<id>): <from> → <to>`, labels `dependencies` +
  `tracer-bump` + `auto-merge` + the language label Dependabot gives that
  language's PRs (`python`, `javascript`, `.NET`, …, so `label:python`
  lists every PR that moved the Python relays, tracer or library), squash
  auto-merge requested. A newer
  upstream release rewrites that branch (current `main` + one commit) and
  PR in place and closes any older PR for the same pin as superseded, so
  the queue never holds two PRs for one tracer, and a PR that has gone
  conflicting (a sibling pin in the same manifest merged first) is rebuilt
  on the next run. For PRs made with the workflow's own token GitHub holds
  the `pull_request` run of `verify.yml` for a maintainer's approval
  ("Approve and run" on the PR); once approved it reports like any other
  check and auto-merge completes after review. A held run still tests the
  `main` it was created for, so `verify.yml` fails one approved after
  `main` has changed the relay the PR bumps; updating the branch starts a
  run on today's tree. With the optional `BUMP_PAT`
  secret set, the bot pushes and opens PRs as that user instead and the
  runs start unprompted.

The same `cpm pins` write path serves `backtest.yml`, which runs `cpm pins
set` to move any pin *backwards* in a scratch checkout, and `verify.yml`
runs `cpm pins check` so every instrumented variant has exactly one pin,
every `Gemfile.lock` holds the tracer version its Gemfile pins, and
every pinned or followed cargo crate has one copy in its `Cargo.lock`. That
last rule is the Rust relays' safety net: `datadog-opentelemetry` requires an
exact `opentelemetry` version, and a second copy of the API crate in the
datadog relay's lock means the SDK's propagator and the relay's calls live in
different crates — the build, boot and delivery checks all pass while the
relay injects nothing. Each Rust tool is therefore its own crate with its own
lock (`relays/rust/<tool>`), and the datadog pin `follow`s its SDK: moving
`datadog-opentelemetry` sets the relay's `opentelemetry` requirement to the
one that release declares.
`cpm discover` lists the older versions a back-test should cover, from the
same registry clients (`internal/versions`), counting back from each
variant's `tracer.pinned` and leaving out what a pin's
`backtest: { min, skip }` policy rules out (versions that cannot be built or
whose family never shipped together — each with its reason in a comment).
The monthly back-test fills gaps only; dispatching it with `refresh`
measures every candidate pair again, which is how older versions catch up
after a `matrix_version` bump or a relay change. The versions themselves
stay in the manifests.

After merging any bump that touches a tracer or instrumentation library, CI
(`matrix.yml`) re-runs the variants that bump can affect — `cpm affected`
maps the changed paths to variants through each variant's `source` directory
and diffs `variants.yaml` and `docker-compose.yml` structurally; changes to
the driver, sink, protobuf contract, shared images or the workflow re-run
everything — carries every other variant's raw result over from the previous
release, publishes a new Release with the resulting `results.json`, and
diffs it against the previous release into `changes.json`. A Rust tracer
bump therefore measures two variants in a few minutes rather than 47.

A push is not the only thing a run has to account for. A run queued behind
another can be cancelled by a newer one, a job can fail, and a merge made
with a workflow's own token (how `bump-agents.yml` lands tracer bumps) starts
no run at all. So the same step also diffs from each variant's own
`measured_commit` in the latest release (`cpm affected -since results.json`)
and adds every variant whose inputs changed since then; a 6-hourly schedule
runs just that selection, and starts nothing when every variant is measured
at its current inputs. `republish.yml` re-renders a release without touching
any variant's `measured_commit`, so that record stays true. The
Monday schedule and manual dispatches run the full matrix. Locally:
`./run.sh <variants>` re-runs and re-aggregates; update `annotations/` (and
FINDINGS/REFERENCES) if a cell changed.

## Extending the matrix

The harness is contract-shaped: rows (languages) and columns (protocols) are
added against the contracts above without touching the measurement machinery.
Both checklists below are written to be handed to a coding agent as-is (the
PHP and Rust relays and the RabbitMQ column were built that way).

### Adding a language (a new row)

1. `relays/{lang}/` — one naive app implementing the relay contract: HTTP
   `POST /relay` + `GET /info` on `:8080`, gRPC `cpmatrix.Relay/Handle` on
   `:8081` (from `proto/relay.proto`), a Kafka consumer on
   `relay-in-{variant}`, and a RabbitMQ consumer on `relay-rmq-{variant}`.
   Copy the shape of an existing relay (`relays/node/app.js` is the
   smallest). The golden rule: **no header or tracing-API access in app
   code**. If the language has no auto-instrumentation for a tool, wire the
   vendor's documented contrib/carrier APIs (Go/Rust style) and say so in a
   relay README.
2. `relays/{lang}/Dockerfile` — multi-stage, one build target per variant
   (`otel`, `datadog`, `elastic`, `newrelic`, `none`). Targets differ only in
   which agent they install and how the entrypoint attaches it; app source is
   identical.
3. `docker-compose.yml` — one `relay-{tool}-{lang}` service per target:
   `x-relay-common-env` anchor, `profiles: [{variant}]`,
   `restart: on-failure`, `depends_on: *relay-depends`, plus the tool's env
   (copy an existing language's entries).
4. `variants.yaml` — add the language under `languages:` (with its display
   name and order) and one entry per variant under `variants:` — tool,
   language, flavour, display name, setup tier, attach mechanism, tracer
   coordinates, source directory. `run.sh` and the aggregator pick them up
   from there; there is nothing to edit in Go.
5. `go run ./cmd/cpm validate` — confirms compose and registry agree.
6. `./run.sh <the new variants>` (which re-aggregates `results/latest/`);
   record root causes for any non-green cell as `annotations/*.yaml`
   entries, and expand on them in `FINDINGS.md`/`REFERENCES.md`.

`driver/`, `sink/`, and the evaluation logic do not change.

### Adding a protocol (a new column)

A protocol touches every component, and the boundary count grows
quadratically (n protocols → n² boundaries; 4 → 16 today, a fifth makes 25).

1. Extend the contracts in this document first: how the five seeded headers
   are encoded in the new carrier, the relay-inbound address
   (`relay-{proto}-{variant}` naming), the sink-inbound address, and any
   declare/warm-up rules — see the RabbitMQ sections above for the shape.
2. `driver/` — seed headers into the new carrier; extend the boundary list
   (16 → 25 tests per variant). `go test ./driver` fails until the
   registry (step 5) lists the same pairs.
3. `sink/` — consume/serve the new protocol and record arriving
   headers/metadata verbatim, with the same base64 rules for non-UTF-8.
4. Every relay in `relays/*` — one inbound listener plus one `forward_via`
   branch, using the language's idiomatic client.
5. `variants.yaml` — add the protocol under `protocols:` and the new
   `boundaries:` (the registry requires exactly protocols² of them, in the
   column order you want), and bump `matrix_version`; `cpm validate` fails
   until it is bumped.
6. `docker-compose.yml` — a broker/infra service (with healthcheck) if the
   protocol needs one, plus any new endpoints in `x-relay-common-env`.
7. Re-read the annotations, `FINDINGS.md` and `REFERENCES.md`: they state
   results against the boundaries that existed when they were written, and
   nothing derives those numbers (see `CONTRIBUTING.md`).

## Roadmap / contributions welcome

Tool columns worth adding next (the 2026 provider landscape has largely
converged on OpenTelemetry; proprietary propagation stacks are now the
exception, which is what makes the remaining ones worth measuring):

- **Sentry**: open-source SDKs, free tier; propagates `sentry-trace` plus
  `sentry-*` keys inside a W3C-syntax `baggage` header — the only non-APM
  vendor actively using baggage.
- **A vendor OpenTelemetry distro** (e.g. `splunk-otel-*`): tests the claim
  that distros inherit upstream propagation unchanged (defaults can differ).
- **Dynatrace OneAgent**: proprietary deep instrumentation + `x-dynatrace`
  headers; commercial-only licensing is the blocker for a re-runnable
  harness.
