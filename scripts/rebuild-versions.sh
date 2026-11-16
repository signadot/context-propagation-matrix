#!/usr/bin/env bash
# rebuild-versions.sh — (re)create every variant-<id> release from the
# run-* and backtest-* releases, which remain the record of each job.
#
#   scripts/rebuild-versions.sh                 # every instrumented variant
#   scripts/rebuild-versions.sh otel-java otel-php
#   scripts/rebuild-versions.sh -dry-run        # build locally, upload nothing
#
# Walks the releases oldest first and files each measurement under its
# tracer version with `cpm versions`, so when a version was measured more
# than once the latest measurement stands — the same rule the workflows
# apply incrementally. Then uploads each variant's <version>.json files,
# index.json and versions.json (the bundle) in one go (creating the release
# when missing). Documents from
# before tracer.pinned was recorded are filed under the plain reported
# version; a back-test's version comes from its tag; anything else (PHP
# relays report composite strings) is listed at the end and gets filed by
# the next matrix run that measures it. The one-off back-fill, and the
# repair tool if a variant-* release is ever damaged. Needs `gh` with
# contents:write; GH_REPO overrides the repository.
set -euo pipefail
cd "$(dirname "$0")/.."

dry=false
only=()
for a in "$@"; do
  case "$a" in
    -dry-run|--dry-run) dry=true ;;
    -h|--help) sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    -*) echo "rebuild-versions: unknown flag $a (try -h)" >&2; exit 2 ;;
    *) only+=("$a") ;;
  esac
done
repo=${GH_REPO:-$(gh repo view --json nameWithOwner --jq .nameWithOwner)}
work=$(mktemp -d)
mkdir -p "$work/src" "$work/variants"
go build -o "$work/cpm" ./cmd/cpm

wanted() { # variant id → 0 if selected
  ((${#only[@]} == 0)) && return 0
  local x; for x in "${only[@]}"; do [[ "$x" == "$1" ]] && return 0; done; return 1
}

echo "listing releases…"
# Oldest first by publication time. created_at is the tagged commit's date:
# releases cut from the same commit share it, and the tag name then decides
# (run-D.10 before run-D.9, every backtest-* before every run-*).
gh api --paginate "repos/$repo/releases?per_page=100" \
  --jq '.[] | select(.draft | not) | select(.tag_name | test("^(run|backtest)-")) | "\(.published_at)\t\(.tag_name)"' \
  | LC_ALL=C sort >"$work/releases"
echo "$(wc -l <"$work/releases" | tr -d ' ') run/back-test releases"

skipped=()
while IFS=$'\t' read -r _ tag; do
  doc="$work/src/$tag.json"
  gh release download "$tag" -R "$repo" -p results.json -O "$doc" 2>/dev/null || { echo "  $tag: no results.json"; continue; }
  if [[ "$tag" == backtest-* ]]; then
    v=$(jq -r '.variants[] | select(.missing | not) | .id' "$doc")
    wanted "$v" || continue
    ver=${tag#"backtest-$v-"}
    "$work/cpm" versions -variant "$v" -out "$work/variants/$v" -results "$doc" -version "$ver" >/dev/null \
      || skipped+=("$tag")
  else
    n=0
    while IFS= read -r v; do
      wanted "$v" || continue
      # Measured in this run (not carried over) and with a tracer; cpm refuses the rest.
      if "$work/cpm" versions -variant "$v" -out "$work/variants/$v" -results "$doc" >/dev/null 2>"$work/err"; then
        n=$((n + 1))
      elif grep -q 'no pinned version' "$work/err"; then
        skipped+=("$tag $v")
      elif ! grep -q 'carried over' "$work/err"; then
        cat "$work/err" >&2
      fi
    done < <(jq -r '.variants[] | select((.missing | not) and .tracer != null) | .id' "$doc")
    echo "  $tag: filed $n variant(s)"
  fi
done <"$work/releases"

echo
for dir in "$work"/variants/*/; do
  v=$(basename "$dir")
  n=$(jq '.versions | length' "$dir/index.json")
  if $dry; then
    echo "$v: $n version(s): $(jq -r '[.versions[].version] | join(" ")' "$dir/index.json")"
    continue
  fi
  tag="variant-$v"
  if ! gh release view "$tag" -R "$repo" >/dev/null 2>&1; then
    gh release create "$tag" -R "$repo" --latest=false --title "$v — results by tracer version" \
      --notes "One asset per tracer version ever measured for \`$v\` (\`<version>.json\`, a single-variant results document) plus \`index.json\` listing them and \`versions.json\` bundling the index and every document. Updated by every matrix run and back-test that measures this variant; the run and back-test releases remain the record of each job." >/dev/null
  fi
  gh release upload "$tag" -R "$repo" --clobber "$dir"/*.json >/dev/null
  echo "$tag: $n version(s) uploaded"
done

if ((${#skipped[@]})); then
  echo
  echo "not filed (no canonical version in the document; the next run that measures them will file them):"
  printf '  %s\n' "${skipped[@]}"
fi
$dry && echo "(dry run: nothing uploaded; local copy in $work/variants)" || rm -rf "$work"
