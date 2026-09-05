# context-propagation-matrix

An empirical, re-runnable test harness that measures **context propagation**
support — which trace/baggage headers actually survive a hop — across:

- **Tools**: OpenTelemetry, Datadog (`dd-trace-*`), New Relic
- **Languages**: Java, Node.js, Python, Go, .NET, Ruby
- **Protocol boundaries**: every combination of `{HTTP, gRPC, Kafka} → {HTTP, gRPC, Kafka}`
- **Headers**: `traceparent`, `tracestate`, `baggage` (W3C), `ot-baggage-*`
  (OpenTracing legacy), plus whatever else shows up

Results are generated, not transcribed from docs: see the published matrix in
[`results/MATRIX.md`](results/MATRIX.md) and the design/contract in
[`DESIGN.md`](DESIGN.md).

## Run it

Requirements: Docker + docker compose.

```sh
# infra (Kafka + sink)
docker compose up -d kafka sink

# run one variant end to end (builds the relay image on first use)
./run.sh otel-java

# run everything (New Relic variants need a license key)
NEW_RELIC_LICENSE_KEY=... ./run.sh all

# regenerate results/MATRIX.md from results/raw/*.json
go run ./matrixgen
```

Each variant writes `results/raw/{variant}.json` containing the seeded
headers, the verbatim headers observed after the hop, and an evaluated status
per header — every cell of the matrix is auditable down to raw header values.

## Layout

```
DESIGN.md          the harness contract (read this first)
proto/             relay/sink gRPC definitions
sink/              uninstrumented Go recorder (HTTP + gRPC + Kafka)
driver/            uninstrumented Go test driver (seeds headers, evaluates results)
matrixgen/         results/raw/*.json -> results/MATRIX.md
relays/{lang}/     one naive relay app per language, 3 instrumentation variants each
results/raw/       machine-readable results per variant
results/MATRIX.md  the generated support matrix
```
