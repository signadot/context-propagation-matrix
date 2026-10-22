#!/usr/bin/env bash
# fetch-latest.sh — restore results/latest (and optionally results/raw) from
# a published run. Results are never committed; every matrix run is a GitHub
# Release `run-YYYY-MM-DD[.N]` with results.json, changes.json, MATRIX.md,
# raw.tar.gz and logs.tar.gz attached.
#
#   scripts/fetch-latest.sh              # newest run-* release -> results/latest/
#   scripts/fetch-latest.sh -raw         # ... and raw.tar.gz -> results/raw/ (replaces *.json there)
#   scripts/fetch-latest.sh run-2026-10-20
#   REPO=you/your-fork scripts/fetch-latest.sh
#
# Exits 0 with a notice when the repository has no run-* release yet, so CI
# before the first publish still passes (`cpm validate` then skips the
# annotation selector check). Needs the GitHub CLI (`gh`), which is on every
# Actions runner; set GH_TOKEN in CI.
set -euo pipefail
cd "$(dirname "$0")/.."

raw=false
tag=""
for a in "$@"; do
  case "$a" in
    -raw|--raw) raw=true ;;
    -h|--help)  sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    -*)         echo "fetch-latest: unknown flag $a (try -h)" >&2; exit 2 ;;
    *)          tag="$a" ;;
  esac
done

repo="${REPO:-${GITHUB_REPOSITORY:-signadot/context-propagation-matrix}}"
if ! command -v gh >/dev/null 2>&1; then
  echo "fetch-latest: the GitHub CLI (gh) is required: https://cli.github.com" >&2
  exit 1
fi

if [[ -z "$tag" ]]; then
  # gh lists newest first; run-* tags sort the same way by construction.
  tag=$(gh release list -R "$repo" --limit 100 --json tagName \
          --jq '[.[] | select(.tagName | startswith("run-"))][0].tagName // empty')
  if [[ -z "$tag" ]]; then
    echo "::notice::fetch-latest: $repo has no run-* release yet; nothing fetched (results/latest left as is)"
    exit 0
  fi
fi

mkdir -p results/latest
echo "fetch-latest: $repo $tag -> results/latest/"
# changes.json is absent on a first release; gh only fails when no pattern matches.
gh release download "$tag" -R "$repo" -D results/latest --clobber \
  -p results.json -p changes.json -p MATRIX.md
printf '%s\n' "$tag" > results/latest/RELEASE

if $raw; then
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  gh release download "$tag" -R "$repo" -D "$tmp" -p raw.tar.gz
  mkdir -p results/raw
  rm -f results/raw/*.json
  tar xzf "$tmp/raw.tar.gz" -C results
  echo "fetch-latest: $(find results/raw -name '*.json' | wc -l | tr -d ' ') raw files -> results/raw/"
fi
