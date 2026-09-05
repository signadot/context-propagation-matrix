#!/usr/bin/env bash
# Run one or more variants end to end.
#
#   ./run.sh otel-java datadog-node
#   ./run.sh all
#   NEW_RELIC_LICENSE_KEY=... ./run.sh newrelic-java
set -uo pipefail
cd "$(dirname "$0")"

ALL_VARIANTS=(
  otel-java otel-node otel-python otel-go otel-dotnet otel-ruby otel-php otel-rust
  datadog-java datadog-node datadog-python datadog-go datadog-dotnet datadog-ruby
  datadog-php datadog-rust
  datadog-java-defaults datadog-node-defaults datadog-python-defaults
  datadog-go-defaults datadog-dotnet-defaults datadog-ruby-defaults
  datadog-python-kafkaprop
  elastic-java elastic-node elastic-python elastic-go elastic-dotnet elastic-ruby
  elastic-php
  newrelic-java newrelic-node newrelic-python newrelic-go newrelic-dotnet newrelic-ruby
  newrelic-php
  none-java none-node none-python none-go none-dotnet none-ruby none-php none-rust
)

# Load .env (gitignored) so the newrelic skip check below sees the license
# key; docker compose reads the same file for ${NEW_RELIC_LICENSE_KEY}.
if [[ -f .env ]]; then
  set -a; source .env; set +a
fi

if [[ $# -eq 0 ]]; then
  echo "usage: $0 <variant>...|all" >&2
  echo "variants: ${ALL_VARIANTS[*]}" >&2
  exit 2
fi

if [[ "$1" == "all" ]]; then
  variants=("${ALL_VARIANTS[@]}")
else
  variants=("$@")
fi

echo "==> starting infra (kafka, rabbitmq, sink)"
docker compose up -d --build kafka rabbitmq sink || exit 1
docker compose build driver || exit 1

failures=()
for v in "${variants[@]}"; do
  if [[ "$v" == newrelic-* && -z "${NEW_RELIC_LICENSE_KEY:-}" ]]; then
    echo "==> SKIP $v (NEW_RELIC_LICENSE_KEY not set)"
    continue
  fi
  echo "==> running variant $v"
  # Pre-create topics: some Kafka clients (e.g. kafkajs) crash at startup if
  # the subscribed topic does not exist yet.
  docker compose exec -T kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 \
    --create --if-not-exists --topic "relay-in-$v" --partitions 1 --replication-factor 1 >/dev/null 2>&1
  docker compose exec -T kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 \
    --create --if-not-exists --topic sink-in --partitions 1 --replication-factor 1 >/dev/null 2>&1
  if ! docker compose up -d --build "relay-$v"; then
    echo "==> BUILD/START FAILED: $v"
    failures+=("$v")
    continue
  fi
  # New Relic agents create DT headers only after their async collector
  # handshake (connect) completes, and some (python, php) only *start*
  # connecting on first traffic. Prime with a full discarded driver pass,
  # then let the handshake settle, so the recorded run measures steady state.
  if [[ "$v" == newrelic-* ]]; then
    echo "==> priming $v (NR agents connect lazily; first pass discarded)"
    sleep 5
    docker compose run --rm driver -variant "$v" >/dev/null 2>&1 || true
    sleep "${NEWRELIC_SETTLE_SECONDS:-10}"
  fi
  if ! docker compose run --rm driver -variant "$v"; then
    echo "==> DRIVER FAILED: $v"
    failures+=("$v")
  fi
  docker compose stop -t 2 "relay-$v" >/dev/null 2>&1
  docker compose rm -f "relay-$v" >/dev/null 2>&1
done

echo
if [[ ${#failures[@]} -gt 0 ]]; then
  echo "failed variants: ${failures[*]}"
  exit 1
fi
echo "all requested variants completed; see results/raw/"
