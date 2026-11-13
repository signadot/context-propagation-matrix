# Contributing

Thanks for helping keep the matrix honest. This guide covers running the
harness locally, then the four ways to extend it — a language, a protocol, a
provider, a variant flavour — and how to explain a non-green cell.

Read [`DESIGN.md`](DESIGN.md) first if you have not; it defines the contracts
(relay, driver, sink, carriers) that everything below plugs into. All of the
recipes here are written so they can be handed to a coding agent verbatim;
the PHP and Rust relays and the RabbitMQ column were built that way.

## Running locally

Requirements: Docker with `docker compose`, Go 1.26+ (for the `cpm` CLI), and
the GitHub CLI (`gh`) if you want to pull published results.

```sh
git clone https://github.com/signadot/context-propagation-matrix
cd context-propagation-matrix

./run.sh fetch -raw                # latest published run -> results/latest + results/raw
./run.sh otel-node                 # one variant (~2 min after the first image build)
./run.sh tool:datadog              # every Datadog variant
./run.sh public                    # everything that needs no secret
NEW_RELIC_LICENSE_KEY=... ./run.sh tool:newrelic   # New Relic agents need a (free) key

go run ./cmd/cpm validate          # variants.yaml ↔ docker-compose.yml ↔ annotations/
go run ./cmd/cpm aggregate         # re-render results/latest from results/raw
go run ./cmd/cpm gate results/latest/results.json   # what CI checks before it publishes
```

`run.sh` starts Kafka, RabbitMQ and the sink, builds the relay image for each
variant, runs the driver (16 hops), writes `results/raw/<variant>.json`, and
finally aggregates everything into `results/latest/results.json` and
`MATRIX.md`. Look at `results/latest/MATRIX.md` for your rows.

Results are never committed: `results/raw`, `results/latest` and
`results/MATRIX.md` are gitignored, and every CI run is a GitHub Release
(`run-YYYY-MM-DD[.N]`) carrying `results.json`, `MATRIX.md`, `changes.json`,
`raw.tar.gz` and `logs.tar.gz`. `./run.sh fetch -raw` first means your local
matrix starts complete and a one-variant run only replaces that variant's row.

A run is published only when `cpm gate` passes. It refuses a run in which a
variant measured in that run leaked the canary, delivered a hop over the
wrong protocol, or — for an instrumented variant — showed nothing its
uninstrumented `none-{lang}` baseline does not show as well (the agent did
not attach). When it refuses, the job fails before the release, the
per-version filing and the site rebuild, so nothing from that run is
published; fix the cause and re-run the workflow. A variant carried over from
an earlier release only warns. `cpm aggregate` records the same verdict on
the variant as `invalid`; DESIGN.md ("Results schema") has the exact rules.

The first build of the Java and .NET images is slow (agent downloads); later
builds are cached.

## The golden rule for relays

A relay is a *naive* application: it receives on one protocol and forwards on
another, and **it never reads, writes, or logs headers, metadata, or tracing
APIs on the request path**. The only exceptions are the `in-code` variants
(Go, Rust) where the vendor offers no runtime attach and their documented
wrapper/carrier APIs are the *only* way to propagate; those relays say so in
their README and the registry marks them `setup_tier: in-code`.

The driver seeds a non-tracing canary header (`x-sd-canary`) on every hop and
fails the run if it ever arrives at the sink — a relay that proxies headers
would make every cell green and every cell meaningless.

## Adding a language (a new row)

1. **`relays/{lang}/`** — one app implementing the relay contract: HTTP
   `POST /relay` + `GET /info` on `:8080`; gRPC `cpmatrix.Relay/Handle` on
   `:8081` (`proto/relay.proto`); a Kafka consumer on `relay-in-{variant}`;
   a RabbitMQ consumer on `relay-rmq-{variant}`. Copy the smallest existing
   relay (`relays/node/app.js`) for shape. `/info` must report
   `tracer_version`, `runtime_version`, and `libs` (client library versions)
   — those become the version chips on the site.
2. **`relays/{lang}/Dockerfile`** — multi-stage, one build target per tool
   (`otel`, `datadog`, `elastic`, `newrelic`, `none`). Targets differ only in
   which agent they install and how the entrypoint attaches it; app source is
   identical across targets. Pin every version exactly (no ranges, no
   `latest`). Add the tracer's pin to `pins.yaml` (which file holds it and
   how it is written); `cpm pins check` in `verify.yml` fails until every
   instrumented variant has one. If an agent is fetched by URL, make the
   version a Dockerfile `ARG` (`kind: arg`); `bump-agents.yml` moves every
   kind of pin and Dependabot ignores them, and `backtest.yml` moves any of
   them backwards through the same entry.
3. **`docker-compose.yml`** — one `relay-{tool}-{lang}` service per target:
   `<<: *relay-common-env`, `profiles: [{variant}]`, `restart: on-failure`,
   `depends_on: *relay-depends`, plus the tool's environment (copy another
   language's block for that tool — propagation-style variables matter).
4. **`variants.yaml`** — add the language under `languages:` and one entry
   per variant under `variants:` (tool, language, `flavour: default`,
   display name, `setup_tier`, one-line `attach`, `tracer` coordinates,
   `source`). The baseline `none-{lang}` variant has `setup_tier: null` and
   no tracer.
   An `in-code` variant also needs `legs`: for every protocol, whether the
   inbound and the outbound side are wired through a `library` or by the
   relay's own code (`app`). No other variant may have them.
5. **Dependabot** — add the manifest directory to `.github/dependabot.yml`
   for the language's ecosystem, and the relay directory to the `docker`
   list.
6. `go run ./cmd/cpm validate`, then `./run.sh language:{lang}`. Explain every
   non-green cell in `annotations/` (below) and summarise in `FINDINGS.md`.
7. Add a `relays/{lang}/README.md` recording library choices and any
   topology caveats (as `relays/php/README.md` does for the mod_php/CLI
   worker split).

`driver/`, `sink/` and the evaluation logic do not change.

## Adding a protocol (a new column)

A protocol touches every component, and boundaries grow quadratically: n
protocols → n² boundaries (4 → 16 today; a fifth makes 25 cells per header
per variant). Open an issue first so we can agree the carrier semantics.

1. Extend the contracts in `DESIGN.md`: how the seeded headers are encoded
   in the new carrier (e.g. SQS message attributes, NATS headers), the relay
   inbound address (`relay-{proto}-{variant}` naming), the sink inbound
   address, declare/warm-up rules. The RabbitMQ sections are the template.
2. `driver/` — seed headers into the new carrier; the boundary list is
   protocols², so adding the protocol adds 2n+1 tests per variant.
3. `sink/` — consume/serve the new protocol and record arriving
   headers/metadata verbatim (base64 for non-UTF-8, as today).
4. Every relay under `relays/*` — one inbound listener plus one
   `forward_via` branch, using the language's idiomatic client. Each relay's
   `/info` should report the new client's version under `libs`.
5. `variants.yaml` — add the protocol under `protocols:` (with its
   `carrier` description, `async: true` if it is a queue, and an optional
   `short_name` for tight grid axes) and every new boundary under
   `boundaries:` in the column order you want. The registry refuses to load
   unless the boundary count is exactly protocols². contextpropagation.io
   derives its grids, heatmaps, OG images and copy from these `dimensions`,
   so no site change is needed for a new column.
6. `docker-compose.yml` — a broker service (with a healthcheck) if needed,
   and any new endpoints in `x-relay-common-env`.
7. Run everything (`./run.sh all`); the new column will be mostly red at
   first — that is the point. Annotate.

## Adding a provider (a new tool)

Only providers with a free, re-runnable attach path belong here: a
contributor must be able to reproduce every cell without a sales
conversation. A free-tier key (as with New Relic) is acceptable; a
commercial-only agent is not.

1. `variants.yaml` — add the tool under `tools:` (display name, vendor, docs
   URL, `native_headers`, `default_propagators`, and `requires_secret:
   NAME_OF_ENV_VAR` if the agent will not propagate until it has connected).
2. One Dockerfile target and one compose service per language you cover
   (you do not need all eight; missing rows simply show as absent on the
   site). Follow the language's existing targets for how agents attach.
3. If the agent needs a secret: add it to `.env.example`, to `run.sh`'s
   skip/prime logic only if its handshake behaviour differs from New Relic's,
   and to `matrix.yml` as a repository secret.
4. One `variants:` entry per tool × language, with `setup_tier` honestly
   classified (see the tier definitions in `variants.yaml`).
   An `in-code` variant also needs `legs` (see the `wirings:` definitions).
5. `annotations/{tool}.yaml` for the cells that do not pass.

## Adding a variant flavour

A flavour is the same tool × language with a different configuration or
attach mechanism — `datadog-python-kafkaprop` (a feature flag),
`datadog-java-defaults` (agent defaults rather than our
`tracecontext,baggage` style), `datadog-go-orchestrion` (build-time weaving).

1. Reuse the language's Dockerfile target where possible; a flavour is
   usually just a different `environment:` block in a new compose service
   `relay-{tool}-{lang}-{flavour}` with `profiles: [{tool}-{lang}-{flavour}]`.
   Build-time weavers get their own relay directory
   (`relays/go-orchestrion/`, `relays/go-otelc/`) because the build command
   is the variant.
2. `variants.yaml` — an entry with `flavour: {flavour}`; the id must be
   `{tool}-{lang}-{flavour}`. Say in `attach` exactly what differs.
3. Run it and annotate. If the flavour *fixes* cells the default flavour
   fails, say so in the default flavour's annotation with `status: config`.

## Explaining a cell: annotations

`annotations/*.yaml` is where root causes live as data. Every non-pass cell
should be matched by at least one annotation. `cpm validate` lists the
failing cells nobody has explained yet as a warning (a GitHub annotation in CI)
that never fails the check, with or without `-strict`; the Slack summary of the
weekly run repeats that list so a new regression gets an explanation written.

```yaml
- id: datadog-java-baggage-non-http
  title: "dd-trace-java injects W3C baggage only on HTTP clients"
  match:
    - tool: datadog
      language: java
      header: baggage
      boundary: ["*->grpc", "*->kafka", "*->rabbitmq"]
  cause: "Baggage injection is wired into the HTTP client instrumentation only; the gRPC, Kafka and AMQP injectors never call it."
  explanation: |
    Markdown. What happens at the code level, what a user can do about it
    (a config flag, an alternate header, manual injection), and what would
    fix it upstream.
  status: open          # open | by-design | config | app-limitation | wontfix | fixed
  links:
    - { title: "dd-trace-java #11286", url: "https://github.com/DataDog/dd-trace-java/issues/11286" }
```

Selector fields: `tool`, `language`, `flavour`, `header` (scalar or list),
`variant` (glob, e.g. `datadog-python-*`), `boundary` (globs such as
`*->kafka`, `kafka->*`), `outcome` (defaults to every non-pass outcome).
Match only what the cause explains — a broad selector that swallows unrelated
failures is worse than no annotation. Cite upstream issues you have actually
read; if none exists, say "no upstream issue yet" and consider filing one
(link back to the cell permalink on contextpropagation.io).

To see the effect of an annotation edit without measuring anything, work
from the latest published raw evidence:

```sh
./run.sh fetch -raw                # results/raw from the latest run-* release
go run ./cmd/cpm aggregate         # re-embed annotations/ -> results/latest/results.json
go run ./cmd/cpm validate -strict  # exactly what PR CI runs
```

Once merged, `republish.yml` does the same on the latest release (replacing
its `results.json` and `MATRIX.md`), re-annotates every variant's per-version
bundle so each version's page shows the note too — attached to its failing
cells on the versions before a fix, as resolved on the versions from
`fixed_in` on — and rebuilds the site. No matrix run.

### Lifecycle: write once, let the data retire it

Annotations are written once and evaluated against every run, so they are
version-specific without saying so. Each selector is matched in two passes:
**structurally** (tool, language, variant, flavour, header, boundary — does
the cell exist?) and then by **outcome** (default: every non-pass outcome).

| Run shows… | `cpm aggregate` | `cpm validate` (PR CI, `-strict`) | Site |
|---|---|---|---|
| some described cells still fail | attaches to those cells | ok | Explanations card |
| some described cells now pass in a variant while others still fail (partially resolved) | attaches to the failing cells only; **warns** once per variant, naming the cells that now pass | warns (a GitHub annotation in CI) and never fails: narrow the selector, or split the annotation, so it claims only what still fails | Explanations card on the failing cells |
| every described cell now passes | attaches to the green cells with `resolved: true`; fills `fixed_in` from the variant's reported tracer version if the YAML has none; **warns** | fails until you record `status: fixed` and `fixed_in` | "resolved" card, kept for history |
| a selector names a cell that does not exist (variant, header or boundary removed) | **fails** — the selector is stale or mistyped | fails | — |
| a selector names only variants that are registered but have no results in this release (e.g. the New Relic variants when `NEW_RELIC_LICENSE_KEY` is unset, before any release carried their cells over) | notes it, attaches nothing | ok — nothing to check yet | — |
| `status: fixed` but cells fail again | attaches; warns "regression?" | fails | card shows the regression |

Marking something fixed is two lines — `cpm annotations mark-fixed <id>
-fixed-in <version>` rewrites them for you — and the Slack summary names the
id and version to use. Do not delete a fixed annotation: the version history
page uses it to say what was broken in which release. Release notes for each
`run-YYYY-MM-DD` list annotations that resolved in that run.

## Pull requests

- CI (`verify.yml`) runs `go vet`, `cpm validate -strict` against the latest
  published `results.json`, and boots the relays the change affects; it needs
  no secrets, so fork PRs work.
- Include the relevant rows from your local `results/latest/MATRIX.md` in
  the PR description. Results themselves are gitignored and cannot be
  committed; CI measures and publishes on merge.
- Version bumps come from Dependabot and `bump-agents.yml`; if you bump
  manually, keep one agent per PR so cell changes stay attributable.
- Merging a change runs only the variants it can affect (`cpm affected`):
  a file under `relays/rust/src/` re-runs every Rust variant and one under
  `relays/rust/otel/` only `otel-rust`, `relays/node/Dockerfile`
  every Node variant, a changed `relay-<id>` service or variant definition that
  variant; anything under `driver/` (and `internal/eval/`, `internal/raw/`,
  which it is built from), `sink/`, `proto/`, `gen/`, `docker/`,
  `go.mod`/`go.sum` or `matrix.yml` itself re-runs everything. Cells not
  re-measured are carried over from the previous release, and the new release
  records `run.partial` / `run.reran`. The same run also measures any variant
  whose inputs changed since it was last measured (`cpm affected -since
  results/latest/results.json`), which covers a run that was cancelled or
  failed and merges that start no run, such as the bot's tracer bumps; a
  6-hourly schedule does only that. Annotation-only merges go through
  `republish.yml` instead. The Monday schedule and `workflow_dispatch` run
  the full matrix by default. Try it locally:
  `go run ./cmd/cpm affected -base origin/main`.

### Repository secrets (maintainers)

`matrix.yml`, `republish.yml` and `backtest.yml` publish releases with the
built-in `GITHUB_TOKEN`; these three are configured under *Settings → Secrets
and variables → Actions* and are optional in the sense that the workflow
degrades rather than fails without them:

| Secret | Used by | Without it |
|---|---|---|
| `NEW_RELIC_LICENSE_KEY` | New Relic relays (`tool:newrelic`) | New Relic variants are skipped (they appear in `results.json` as registered but not run); every other cell is unaffected. A free-tier key is sufficient. |
| `NETLIFY_BUILD_HOOK_URL` | `matrix.yml`, `republish.yml` and `backtest.yml` after publishing | contextpropagation.io is not rebuilt until its next scheduled build; the release itself is unaffected. |
| `SLACK_WEBHOOK_URL` | `matrix.yml` summary step | No Slack summary is posted. |

The step that opens a PR marking resolved annotations fixed also needs
*Settings → Actions → "Allow GitHub Actions to create and approve pull
requests"*; without it the branch is pushed and the step warns.

## Reporting a wrong cell

Use the "Dispute or question a result" issue template. Include the cell id
(`{variant}/{header}/{inbound}-{outbound}`), the run id, and what you believe
is correct with a link to the evidence.
