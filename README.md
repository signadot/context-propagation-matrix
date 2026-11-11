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

Results are generated, not transcribed from docs. Every run is published as a
GitHub Release (`run-YYYY-MM-DD[.N]`) carrying `results.json`, `MATRIX.md`,
`changes.json` and the raw evidence — see the
[latest release](https://github.com/signadot/context-propagation-matrix/releases/latest)
or browse them at **https://contextpropagation.io**. The same measurements
are also filed by tracer version, one rolling release per variant
(`variant-otel-java`: `2.33.1.json`, `2.32.0.json`, …, `index.json`, and
`versions.json` bundling them all), so a
result can be looked up by the version it was measured at. Nothing is committed to
the tree by the harness; the design/contract is in [`DESIGN.md`](DESIGN.md).

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

# start from the latest published run instead of measuring (needs `gh`)
./run.sh fetch          # results/latest/{results,changes}.json + MATRIX.md
./run.sh fetch -raw     # ... and the raw evidence into results/raw/

# re-aggregate results/raw/*.json -> results/latest/{results.json,MATRIX.md}
go run ./cmd/cpm aggregate

# check variants.yaml, docker-compose.yml and annotations/ agree
go run ./cmd/cpm validate
```

Each variant writes `results/raw/{variant}.json` containing the seeded
headers, the verbatim headers observed after the hop, and an evaluated status
per header — every cell of the matrix is auditable down to raw header values.
`cpm aggregate` folds those into `results/latest/results.json` (schema v2:
the contract the website consumes; see DESIGN.md) and renders `MATRIX.md`.
`results/raw`, `results/latest` and `results/MATRIX.md` are local, gitignored
outputs; the published copy of every run is a release.

## How results reach the site

```
merge to main (relays/**, variants.yaml, ...)  ──▶ matrix.yml: cpm affected picks the variants
                                                   the change can alter, runs them in one
                                                   parallel wave, lays the new raw results over
                                                   the previous release's raw.tar.gz, aggregates
                                                   ──▶ Release run-YYYY-MM-DD[.N] ──▶ Netlify hook
Monday 06:17 UTC / workflow_dispatch           ──▶ matrix.yml: every variant, same publish path
merge to main touching annotations/**          ──▶ republish.yml: re-aggregate the latest release's
                                                   raw.tar.gz at this commit, replace its
                                                   results.json + MATRIX.md ──▶ Netlify hook
pull request                                   ──▶ verify.yml: fetch the latest results.json,
                                                   cpm validate -strict, smoke the affected relays
```

A push run publishes the latest known state of every cell: `run.partial` and
`run.reran` in `results.json` say which variants were measured in that run,
and each variant's `ran_at` says when its own cells were last measured.

### Bootstrap (once, repository admin)

1. In Netlify, add a build hook for the site's `main` branch and store its URL
   as the `NETLIFY_BUILD_HOOK_URL` repository secret. Without it runs still
   publish releases; the site simply is not rebuilt.
2. Optional secrets: `NEW_RELIC_LICENSE_KEY` (New Relic variants are skipped
   without it) and `SLACK_WEBHOOK_URL` (a summary when cells change).
3. Settings → Actions → "Allow GitHub Actions to create and approve pull
   requests", so a run can open the PR that marks resolved annotations fixed.
4. Dispatch `matrix.yml` once (Actions → matrix → Run workflow) for the first
   `run-*` release. Until it exists, PR CI validates without results,
   `./run.sh fetch` is a no-op with a notice, and `republish.yml` skips.

## Layout

```
DESIGN.md          the harness contract (read this first)
FINDINGS.md        root causes and upstream references for notable cells
variants.yaml      the registry: tools, languages, protocols, headers, variants
annotations/       per-cell explanations as data (matched into results.json)
proto/             relay/sink gRPC definitions
sink/              uninstrumented Go recorder (HTTP + gRPC + Kafka + RabbitMQ)
driver/            uninstrumented Go test driver (seeds headers, evaluates results)
cmd/cpm/           CLI: list | affected | validate | aggregate | diff | summary | coords | annotations | pins | discover | versions
internal/          registry loader, results schema v2, annotations, affected-variant rules, renderer
matrixgen/         compatibility wrapper around `cpm aggregate` (MATRIX.md only)
scripts/           fetch-latest.sh: restore results/latest (+ results/raw) from a release
relays/{lang}/     one naive relay app per language, one instrumentation variant per tool
relays/go-orchestrion/  naive Go app compiled with Datadog Orchestrion (build-time weaving)
relays/go-otelc/   naive Go app compiled with OTel compile-time instrumentation (otelc)
results/experiments/, results/nokey/   hand-curated evidence referenced from FINDINGS.md
results/raw/, results/latest/          local outputs (gitignored; published as release assets)
```

## Results site

The published, browsable results live at **https://contextpropagation.io**,
which is rebuilt from this repository's GitHub Releases every time the matrix
runs or annotations are republished. Every cell there has a permalink and
links back to the raw evidence (`raw.tar.gz` on the same release).

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for how to add a language, a
protocol boundary, a provider, or a variant flavour, and how to write an
annotation explaining a non-success cell. Please read
[`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md); security concerns go to the
address in [`SECURITY.md`](SECURITY.md).

## License

Apache License 2.0 — see [`LICENSE`](LICENSE). Built and maintained by
[Signadot](https://www.signadot.com).
