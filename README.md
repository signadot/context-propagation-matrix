# context-propagation-matrix

An empirical, re-runnable test harness that measures **context propagation**
support — which trace/baggage headers actually survive a hop — across:

- **Tools**: OpenTelemetry, Datadog (`dd-trace-*`), Elastic APM, New Relic
  (needs `NEW_RELIC_LICENSE_KEY` in `.env` — free tier) — plus a
  no-instrumentation `none` baseline per language (control runs; see
  FINDINGS.md §12 for the one language that propagates anyway)
- **Languages**: Java, Node.js, Python, Go, .NET, Ruby, PHP, Rust — Go twice
  over: hand-wired contribs *and* both compile-time weavers (Datadog
  Orchestrion, OTel otelc) as separate `build-time` variants (FINDINGS.md
  §11)
- **Protocol boundaries**: every combination of
  `{HTTP, gRPC, Kafka, RabbitMQ} → {HTTP, gRPC, Kafka, RabbitMQ}`
- **Headers**: `traceparent`, `tracestate`, `baggage` (W3C), `ot-baggage-*`
  (OpenTracing legacy), plus whatever else shows up

Results are generated, not transcribed from docs: see the published matrix in
[`results/MATRIX.md`](results/MATRIX.md) and the design/contract in
[`DESIGN.md`](DESIGN.md).

## Run it

Requirements: Docker + docker compose.

```sh
# infra (Kafka + RabbitMQ + sink)
docker compose up -d kafka rabbitmq sink

# run one variant end to end (builds the relay image on first use)
./run.sh otel-java

# run everything (New Relic variants need a license key)
NEW_RELIC_LICENSE_KEY=... ./run.sh all

# or: ./run.sh public | tool:datadog | language:python

# re-aggregate results/raw/*.json -> results/latest/{results.json,MATRIX.md}
go run ./cmd/cpm aggregate -matrix-md results/MATRIX.md

# check variants.yaml, docker-compose.yml and annotations/ agree
go run ./cmd/cpm validate
```

Each variant writes `results/raw/{variant}.json` containing the seeded
headers, the verbatim headers observed after the hop, and an evaluated status
per header — every cell of the matrix is auditable down to raw header values.
`cpm aggregate` folds those into `results/latest/results.json` (schema v2:
the contract the website consumes; see DESIGN.md) and renders `MATRIX.md`.

## Layout

```
DESIGN.md          the harness contract (read this first)
FINDINGS.md        root causes and upstream references for notable cells
variants.yaml      the registry: tools, languages, protocols, headers, variants
annotations/       per-cell explanations as data (matched into results.json)
proto/             relay/sink gRPC definitions
sink/              uninstrumented Go recorder (HTTP + gRPC + Kafka + RabbitMQ)
driver/            uninstrumented Go test driver (seeds headers, evaluates results)
cmd/cpm/           CLI: list | validate | aggregate | diff | summary
internal/          registry loader, results schema v2, annotations, renderer
matrixgen/         compatibility wrapper around `cpm aggregate` (MATRIX.md only)
relays/{lang}/     one naive relay app per language, one instrumentation variant per tool
relays/go-orchestrion/  naive Go app compiled with Datadog Orchestrion (build-time weaving)
relays/go-otelc/   naive Go app compiled with OTel compile-time instrumentation (otelc)
results/raw/       machine-readable results per variant (internal format)
results/latest/    results.json (schema v2) + MATRIX.md + changes.json from the last run
results/MATRIX.md  mirror of results/latest/MATRIX.md
```

## Results site

The published, browsable results live at **https://contextpropagation.io**,
which is rebuilt from this repository's GitHub Releases every time the matrix
runs. Every cell there has a permalink and links back to the raw evidence here.

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for how to add a language, a
protocol boundary, a provider, or a variant flavour, and how to write an
annotation explaining a non-success cell. Please read
[`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md); security concerns go to the
address in [`SECURITY.md`](SECURITY.md).

## License

Apache License 2.0 — see [`LICENSE`](LICENSE). Built and maintained by
[Signadot](https://www.signadot.com).
