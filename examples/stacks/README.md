# Stack files for `cpm try`

A stack file describes the tracer setup you run in production as a change to
one registered variant. `cpm try` applies it in a throwaway git worktree, builds
and measures that one relay against the same 16 boundaries as the matrix, and
compares every cell with the latest published run.

```sh
go run ./cmd/cpm try examples/stacks/otel-go-older-sdk.yaml            # measure + compare
go run ./cmd/cpm try examples/stacks/otel-python-tracecontext-only.yaml -dry-run   # show the edits only
go run ./cmd/cpm try mystack.yaml -fail-on-regression                  # CI: exit 1 if a published pass fails for you
```

Output goes to `results/try/<name>/` (gitignored): `report.md`, the
aggregated `results.json`, and the raw driver result.

## Fields

| field | meaning |
|---|---|
| `variant` | base variant id (`cpm list`), or `tool` + `language` (+ `flavour`, default `default`) |
| `tracer` | tracer/agent version; moved with the same `cpm pins set` edit back-tests use |
| `libs` | package → version, rewritten in every manifest of the relay that pins the package (requirements, package.json, Gemfile, go.mod, Cargo.toml, .csproj, composer.json, pom.xml incl. `${property}` versions) |
| `env` | extra relay environment, layered over docker-compose.yml with an override file |
| `protocols` | only report boundaries that touch these protocols |
| `name` | output directory name (default `<variant>-<tracer>`) |

## What it does and does not tell you

It measures the tracer, library versions and configuration you give it on the
harness's naive relay: inbound request, forward over a second protocol, no
manual context handling. That isolates what the instrumentation itself carries.
It does not run your application code. A library the relay does not use (say
`aiokafka` instead of `confluent-kafka`) needs relay code first; `cpm try`
says so instead of guessing.

`cpm try` uses the GitHub CLI (`gh`) to fetch the published run for the
comparison; without it (or with `-baseline results.json`) it still reports
your stack's own results.
