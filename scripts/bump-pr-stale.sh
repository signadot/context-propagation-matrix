#!/usr/bin/env bash
# bump-pr-stale.sh — has main changed the relay a bump branch was built for?
#
# bump-agents.yml builds bump/<id> as main plus one commit, and the PR's
# checks run against that main. A branch that still names the newest version
# and merges without conflict was left alone however far main had moved, so
# its checks could describe a relay that no longer exists: a bump built
# before the Ruby relay had lock files changed a Gemfile and no lock, and
# was green, approved and set to merge itself into an image that refuses
# exactly that (#260).
#
#   scripts/bump-pr-stale.sh BRANCH FILE...
#
# BRANCH is the bump branch as a ref the checkout has (origin/bump/<id>) and
# FILE... the files the bump writes (`.files` of `cpm pins latest`). Prints
# every path main has changed since BRANCH forked from it under the relay of
# a FILE, one per line, and nothing when there is none. The relay is the
# directory the image is built from, relays/<name>: a tracer shares its
# Dockerfile and its application with the other tools of the language.
# bump-agents.yml rebuilds the branch when anything is printed.
#
# Run in the checkout. MAIN (default origin/main) is the ref to compare
# with. The history has to reach the fork point: on a shallow checkout this
# exits 2 rather than print nothing, which would read as "not stale".
set -euo pipefail

main=${MAIN:-origin/main}
if (($# < 2)); then
  echo "usage: bump-pr-stale.sh BRANCH FILE..." >&2
  exit 2
fi
branch=$1
shift

if ! base=$(git merge-base "$main" "$branch"); then
  echo "bump-pr-stale: $main and $branch have no commit in common in this checkout (is it shallow?)" >&2
  exit 2
fi

relays=()
for f in "$@"; do
  case "$f" in
    relays/*/*) IFS=/ read -r top name _ <<<"$f"; relays+=("$top/$name") ;;
    */*)        relays+=("$(dirname "$f")") ;;
    *)          relays+=("$f") ;;
  esac
done

# A pathspec names a directory by whole components: relays/go does not
# match relays/go-orchestrion.
git diff --name-only "$base" "$main" -- "${relays[@]}"
