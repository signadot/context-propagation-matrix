#!/usr/bin/env bash
# publish-versions.sh — file a measurement under its tracer version.
#
# Every variant has one rolling GitHub Release, `variant-<id>`, whose assets
# are `<version>.json` (a single-variant results document per tracer version
# ever measured), `index.json` (the list of them, newest first, with counts
# and provenance) and `versions.json` (the index plus every document in one
# file, so the site needs one download per variant). Runs and back-tests
# stay as they are — this is a second view over the same measurements, keyed
# the way people look for them: "OTel Java 2.33.1", not "the run of 16
# November".
#
#   scripts/publish-versions.sh results.json VARIANT...      # from a run or back-test document
#   scripts/publish-versions.sh -version 1.4.2 results.json VARIANT
#                                                            # old document without tracer.pinned
#   scripts/publish-versions.sh -rebuild VARIANT...          # re-derive index.json and versions.json
#                                                            # from the <version>.json assets
#   scripts/publish-versions.sh -refresh VARIANT...          # re-annotate versions.json in place
#                                                            # (republish.yml, when annotations/ changes)
#
# For each variant: download the release's index.json and versions.json (if
# the release exists; create it otherwise) plus any <version>.json the bundle
# lacks, let `cpm versions` write <version>.json and upsert the entry into
# both, upload all three with --clobber. The index is reconciled with the
# asset list each time — entries whose asset is gone are dropped, assets the
# index does not know are re-read and added — so two workflows updating the
# same variant seconds apart cannot leave it wrong for longer than the next
# update. Needs `gh` with contents:write; GH_REPO overrides the repository.
#
# Whatever the mode, `cpm versions` re-annotates every document in the bundle
# from annotations/ at this checkout: a <version>.json asset keeps the
# annotations it was filed with, the bundle (what the site renders) carries
# the ones the current YAML attaches to that version's cells.
set -euo pipefail
cd "$(dirname "$0")/.."

version=""
rebuild=false
refresh=false
results=""
while (($#)); do
  case "$1" in
    -version|--version) version="$2"; shift 2 ;;
    -rebuild|--rebuild) rebuild=true; shift ;;
    -refresh|--refresh) refresh=true; shift ;;
    -h|--help) sed -n '2,33p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    -*) echo "publish-versions: unknown flag $1 (try -h)" >&2; exit 2 ;;
    *)
      if ! $rebuild && ! $refresh && [[ -z "$results" ]]; then results="$1"; else break; fi
      shift ;;
  esac
done
if $rebuild && $refresh; then echo "publish-versions: -rebuild and -refresh exclude each other" >&2; exit 2; fi
if ! $rebuild && ! $refresh && [[ -z "$results" ]]; then echo "publish-versions: results.json required (try -h)" >&2; exit 2; fi
if (($# == 0)); then echo "publish-versions: at least one variant required" >&2; exit 2; fi
repo=${GH_REPO:-$(gh repo view --json nameWithOwner --jq .nameWithOwner)}

failed=()
for v in "$@"; do
  tag="variant-$v"
  if ! $rebuild && ! $refresh; then
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
    if grep -qx versions.json "$work/assets"; then
      gh release download "$tag" -R "$repo" -p versions.json -D "$work"
    fi
  elif $refresh; then
    # Baselines and variants never measured have no release; nothing to refresh.
    echo "   no $tag release; nothing to re-annotate"; rm -rf "$work"; continue
  else
    present=""
    : >"$work/assets"
    # Two jobs may race to create it; the loser carries on with the winner's.
    gh release create "$tag" -R "$repo" --latest=false --title "$v — results by tracer version" \
      --notes "One asset per tracer version ever measured for \`$v\` (\`<version>.json\`, a single-variant results document), \`index.json\` listing them and \`versions.json\` bundling the index and every document. Updated by every matrix run and back-test that measures this variant; the run and back-test releases remain the record of each job." >/dev/null \
      && echo "   created $tag" || gh release view "$tag" -R "$repo" >/dev/null
  fi

  # Per-version documents to hand to cpm: those on the release that the
  # index does not list (a rebuild wants all of them; otherwise only strays
  # from a lost race) and those the index lists but the bundle lacks (the
  # first bundle of a release, or a bundle that fell behind).
  docs=()
  if $rebuild; then rm -f "$work/index.json" "$work/versions.json"; fi
  known=$( [[ -f "$work/index.json" ]] && jq -r '.versions[].asset' "$work/index.json" || true )
  bundled=$( [[ -f "$work/versions.json" ]] && jq -r '.documents | keys[] | . + ".json"' "$work/versions.json" || true )
  while IFS= read -r a; do
    [[ -z "$a" || "$a" == index.json || "$a" == versions.json || "$a" != *.json ]] && continue
    if grep -qxF "$a" <<<"$known" && grep -qxF "$a" <<<"$bundled"; then continue; fi
    gh release download "$tag" -R "$repo" -p "$a" -D "$work/docs" >/dev/null
    docs+=("$work/docs/$a")
  done <"$work/assets"

  args=(-variant "$v" -out "$work" -present "$present")
  if $rebuild; then
    if ((${#docs[@]} == 0)); then echo "   nothing on $tag to rebuild from"; rm -rf "$work"; continue; fi
    out=$(go run ./cmd/cpm versions "${args[@]}" "${docs[@]}") || { failed+=("$v"); continue; }
    upload=("$work/index.json" "$work/versions.json")
  elif $refresh; then
    if [[ ! -f "$work/versions.json" ]] && ((${#docs[@]} == 0)); then echo "   nothing on $tag to re-annotate"; rm -rf "$work"; continue; fi
    out=$(go run ./cmd/cpm versions "${args[@]}" ${docs[@]+"${docs[@]}"}) || { failed+=("$v"); continue; }
    upload=("$work/index.json" "$work/versions.json")
  else
    [[ -n "$version" ]] && args+=(-version "$version")
    out=$(go run ./cmd/cpm versions "${args[@]}" -results "$results" ${docs[@]+"${docs[@]}"}) || { failed+=("$v"); continue; }
    asset=$(jq -r --arg s "$results" 'select(.source==$s) | .asset' <<<"$out")
    upload=("$work/$asset" "$work/index.json" "$work/versions.json")
  fi
  gh release upload "$tag" -R "$repo" --clobber "${upload[@]}" >/dev/null
  if $refresh; then
    echo "   re-annotated; index has $(jq '.versions|length' "$work/index.json") version(s)"
  else
    echo "   $(jq -r '"\(.version) (\(.asset))"' <<<"$out" | paste -sd ' ' -) uploaded; index has $(jq '.versions|length' "$work/index.json") version(s)"
  fi
  rm -rf "$work"
done
if ((${#failed[@]})); then
  echo "publish-versions: failed for ${failed[*]}" >&2
  exit 1
fi
