#!/usr/bin/env bash
# publish-versions.sh — file a measurement under its tracer version.
#
# Every variant has one rolling GitHub Release, `variant-<id>`, whose assets
# are `<version>.json` (a single-variant results document per tracer version
# ever measured) and `index.json` (the list of them, newest first, with
# counts and provenance). Runs and back-tests stay as they are — this is a
# second view over the same measurements, keyed the way people look for
# them: "OTel Java 2.33.1", not "the run of 16 November".
#
#   scripts/publish-versions.sh results.json VARIANT...      # from a run or back-test document
#   scripts/publish-versions.sh -version 1.4.2 results.json VARIANT
#                                                            # old document without tracer.pinned
#   scripts/publish-versions.sh -rebuild VARIANT...          # re-derive index.json from the assets
#
# For each variant: download the release's index.json (if the release exists;
# create it otherwise), let `cpm versions` write <version>.json and upsert the
# entry, upload both with --clobber. The index is reconciled with the asset
# list each time — entries whose asset is gone are dropped, assets the index
# does not know are re-read and added — so two workflows updating the same
# variant seconds apart cannot leave it wrong for longer than the next
# update. Needs `gh` with contents:write; GH_REPO overrides the repository.
set -euo pipefail
cd "$(dirname "$0")/.."

version=""
rebuild=false
results=""
while (($#)); do
  case "$1" in
    -version|--version) version="$2"; shift 2 ;;
    -rebuild|--rebuild) rebuild=true; shift ;;
    -h|--help) sed -n '2,21p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    -*) echo "publish-versions: unknown flag $1 (try -h)" >&2; exit 2 ;;
    *)
      if ! $rebuild && [[ -z "$results" ]]; then results="$1"; else break; fi
      shift ;;
  esac
done
if ! $rebuild && [[ -z "$results" ]]; then echo "publish-versions: results.json required (try -h)" >&2; exit 2; fi
if (($# == 0)); then echo "publish-versions: at least one variant required" >&2; exit 2; fi
repo=${GH_REPO:-$(gh repo view --json nameWithOwner --jq .nameWithOwner)}

failed=()
for v in "$@"; do
  tag="variant-$v"
  if ! $rebuild; then
    # Baselines have no tracer, so no version to file under.
    tracer=$(jq -r --arg v "$v" '.variants[] | select(.id == $v) | .tracer // empty | .package // empty' "$results")
    if [[ -z "$tracer" ]]; then echo "== $v: no tracer, nothing to file"; continue; fi
  fi
  work=$(mktemp -d)
  echo "== $v → $tag"
  if gh release view "$tag" -R "$repo" --json assets --jq '.assets[].name' >"$work/assets" 2>/dev/null; then
    present=$(paste -sd, "$work/assets")
    if grep -qx index.json "$work/assets"; then
      gh release download "$tag" -R "$repo" -p index.json -D "$work"
    fi
  else
    present=""
    : >"$work/assets"
    # Two jobs may race to create it; the loser carries on with the winner's.
    gh release create "$tag" -R "$repo" --latest=false --title "$v — results by tracer version" \
      --notes "One asset per tracer version ever measured for \`$v\` (\`<version>.json\`, a single-variant results document) plus \`index.json\` listing them. Updated by every matrix run and back-test that measures this variant; the run and back-test releases remain the record of each job." >/dev/null \
      && echo "   created $tag" || gh release view "$tag" -R "$repo" >/dev/null
  fi

  # Per-version documents on the release that the index does not list (a
  # rebuild wants all of them; otherwise only strays from a lost race).
  docs=()
  if $rebuild; then rm -f "$work/index.json"; fi
  known=$( [[ -f "$work/index.json" ]] && jq -r '.versions[].asset' "$work/index.json" || true )
  while IFS= read -r a; do
    [[ -z "$a" || "$a" == index.json || "$a" != *.json ]] && continue
    grep -qxF "$a" <<<"$known" && continue
    gh release download "$tag" -R "$repo" -p "$a" -D "$work/docs" >/dev/null
    docs+=("$work/docs/$a")
  done <"$work/assets"

  args=(-variant "$v" -out "$work" -present "$present")
  if $rebuild; then
    if ((${#docs[@]} == 0)); then echo "   nothing on $tag to rebuild from"; rm -rf "$work"; continue; fi
    out=$(go run ./cmd/cpm versions "${args[@]}" "${docs[@]}") || { failed+=("$v"); continue; }
    upload=("$work/index.json")
  else
    [[ -n "$version" ]] && args+=(-version "$version")
    out=$(go run ./cmd/cpm versions "${args[@]}" -results "$results" ${docs[@]+"${docs[@]}"}) || { failed+=("$v"); continue; }
    asset=$(jq -r --arg s "$results" 'select(.source==$s) | .asset' <<<"$out")
    upload=("$work/$asset" "$work/index.json")
  fi
  gh release upload "$tag" -R "$repo" --clobber "${upload[@]}" >/dev/null
  echo "   $(jq -r '"\(.version) (\(.asset))"' <<<"$out" | paste -sd ' ' -) uploaded; index has $(jq '.versions|length' "$work/index.json") version(s)"
  rm -rf "$work"
done
if ((${#failed[@]})); then
  echo "publish-versions: failed for ${failed[*]}" >&2
  exit 1
fi
