#!/usr/bin/env bash
# Run one or more variants end to end, then aggregate results.
#
#   ./run.sh otel-java datadog-node      # explicit variants
#   ./run.sh all                         # everything in variants.yaml
#   ./run.sh public                      # everything that needs no secret
#   ./run.sh tool:datadog                # every variant of one tool
#   ./run.sh language:python             # every variant of one language
#   NEW_RELIC_LICENSE_KEY=... ./run.sh newrelic-java
#
# Variants come from variants.yaml (the registry); docker-compose.yml must
# define relay-<id> with profile <id> for each (checked by `cpm validate`).
# Requires Docker + docker compose, and Go (for the registry-driven CLI).
set -uo pipefail
cd "$(dirname "$0")"

cpm() { go run ./cmd/cpm "$@"; }

# Load .env (gitignored) so the secret check below sees the license key;
# docker compose reads the same file for ${NEW_RELIC_LICENSE_KEY}.
if [[ -f .env ]]; then
  set -a; source .env; set +a
fi

if [[ $# -eq 0 ]]; then
  echo "usage: $0 <variant>...|all|public|tool:<tool>|language:<lang>" >&2
  echo "variants:" >&2
  cpm list | sed 's/^/  /' >&2
  exit 2
fi

variants=()
for arg in "$@"; do
  case "$arg" in
    all)         variants+=($(cpm list)) ;;
    public)      variants+=($(cpm list -no-secret)) ;;
    tool:*)      variants+=($(cpm list -tool "${arg#tool:}")) ;;
    language:*)  variants+=($(cpm list -language "${arg#language:}")) ;;
    *)
      if ! cpm list | grep -qx "$arg"; then
        echo "unknown variant: $arg (see: $0 with no args)" >&2
        exit 2
      fi
      variants+=("$arg") ;;
  esac
done

# Variants whose tool needs a secret (currently New Relic): skipped when the
# secret is absent, and primed before measuring because their agents only
# emit propagation headers after an async backend handshake.
secret_variants=" $(cpm list -secret | tr '\n' ' ') "
needs_secret() { [[ "$secret_variants" == *" $1 "* ]]; }

echo "==> validating registry against docker-compose.yml"
cpm validate >/dev/null || exit 1

echo "==> starting infra (kafka, rabbitmq, sink)"
docker compose up -d --build kafka rabbitmq sink || exit 1
docker compose build driver || exit 1

failures=()
for v in "${variants[@]}"; do
  if needs_secret "$v" && [[ -z "${NEW_RELIC_LICENSE_KEY:-}" ]]; then
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
  if needs_secret "$v"; then
    echo "==> priming $v (agent connects lazily; first pass discarded)"
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
echo "==> aggregating results/raw -> results/latest (results.json, MATRIX.md)"
cpm aggregate -kind local -matrix-md results/MATRIX.md || failures+=("aggregate")

if [[ ${#failures[@]} -gt 0 ]]; then
  echo "failed: ${failures[*]}"
  exit 1
fi
echo "all requested variants completed; see results/latest/"
