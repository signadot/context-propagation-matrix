#!/usr/bin/env bash
# test-fetch-latest.sh — `fetch-latest.sh -tag` must name the newest run-*
# release whatever order the releases API lists them in. matrix.yml carries
# results over from that release and diffs against it, and republish.yml
# rewrites it, so a wrong answer republishes old measurements as current.
#
# Runs the real script against a stand-in `gh` that answers
# `gh api … --jq <filter>` from a fixture through jq, so the filter and the
# ordering are both under test. Offline; needs bash and jq.
set -euo pipefail
cd "$(dirname "$0")/.."

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"
cat >"$work/bin/gh" <<'EOF'
#!/usr/bin/env bash
# The fixture holds one JSON array per page; like `gh api --paginate --jq`,
# the filter is applied to each page in turn.
filter=""
while (($#)); do
  case "$1" in
    --jq) filter=$2; shift ;;
  esac
  shift
done
exec jq -r "$filter" "$FIXTURE"
EOF
chmod +x "$work/bin/gh"

# page TAG... — one page of releases, in the order given. `draft:TAG` is a draft.
page() {
  jq -cn '$ARGS.positional | map({tag_name: ltrimstr("draft:"), draft: startswith("draft:")})' --args "$@"
}

failed=0
# check NAME WANT — the pages on stdin are the API's answer.
check() {
  local name=$1 want=$2 got
  cat >"$work/releases.json"
  got=$(PATH="$work/bin:$PATH" FIXTURE="$work/releases.json" REPO=test/test scripts/fetch-latest.sh -tag) \
    || got="(exit $?) $got"
  if [[ "$got" == "$want" ]]; then
    echo "ok    $name"
  else
    echo "FAIL  $name: want '$want', got '$got'"
    failed=1
  fi
}

# The order the API returned on 2026-11-11, back-test and variant releases
# left in: .9 comes first, .22 is the newest.
check "API order of 2026-11-11" run-2026-11-10.22 < <(
  page backtest-otel-node-0.77.0 variant-otel-node \
       run-2026-11-10.9 run-2026-11-10.8 run-2026-11-10.7 run-2026-11-10.6 run-2026-11-10.5 \
       run-2026-11-10.4 run-2026-11-10.3 run-2026-11-10.22 run-2026-11-10.21 run-2026-11-10.20 \
       run-2026-11-10.2 run-2026-11-10.19 run-2026-11-10.18 run-2026-11-10.17 run-2026-11-10.16 \
       run-2026-11-10.15 run-2026-11-10.14 run-2026-11-10.13 run-2026-11-10.12 run-2026-11-10.11 \
       run-2026-11-10.10 run-2026-11-10.1 run-2026-11-10 run-2026-11-09.7 run-2026-11-09 \
       run-2026-11-02 run-2026-10-23.1 run-2026-10-23
)
check ".10 is newer than .9" run-2026-11-10.10 < <(page run-2026-11-10.9 run-2026-11-10.10)
check "a day's first run has no suffix" run-2026-11-10.1 < <(page run-2026-11-10 run-2026-11-10.1)
check "a later date beats a higher suffix" run-2026-11-11 < <(page run-2026-11-10.22 run-2026-11-11 run-2026-11-09.7)
check "across a year boundary" run-2027-01-04 < <(page run-2026-12-28.3 run-2027-01-04)
check "the newest is on a later page" run-2026-11-10.22 < <(
  page run-2026-11-10.9 backtest-otel-java-2.20.0
  page backtest-otel-java-2.19.0 run-2026-11-10.22
)
check "drafts are not published runs" run-2026-11-10.9 < <(page draft:run-2026-11-10.10 run-2026-11-10.9)
check "only run-YYYY-MM-DD[.N] tags count" run-2026-11-10 < <(
  page run-2026-11-10 run-2026-11-11-rc1 run-2026-11-12.x runner-2026-11-13 backtest-otel-go-1.40.0 variant-otel-go
)
check "no run release yet" "" < <(page backtest-otel-go-1.40.0 variant-otel-go)

exit "$failed"
