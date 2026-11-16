#!/usr/bin/env bash
# run-base-moved.sh — is a pull-request run still testing today's base branch?
#
# A pull_request run tests the pull request merged with its base branch as
# the base was when the run was created, and it keeps that tree however late
# it starts. A run held for approval or run again builds the old one. #260
# was opened at 12:46, before the Ruby relay had lock files. Its held run
# was started at 19:31, built the tree of 12:46, passed, and the pull request
# merged a Gemfile without its lock.
#
#   scripts/run-base-moved.sh BASE_THEN BASE_NOW HEAD
#   scripts/run-base-moved.sh --checkout BASE_NOW
#
# BASE_THEN is the base branch's commit the run tests, BASE_NOW the base
# branch as it is now and HEAD the pull request's head. Prints every path
# the base branch has changed from BASE_THEN to BASE_NOW under a relay
# (relays/<name>) that the pull request changes, one per line, and nothing
# when there is none.
#
# --checkout is what verify.yml runs. It reads BASE_THEN and HEAD off the
# commit checked out, which in a pull_request run is GitHub's merge of the
# pull request (second parent) into the base branch as it was (first
# parent), says in one line what the run tests, and exits 1 with an error
# for the run's summary when anything is listed.
#
# Only relays the pull request changes: that is where a change and the tree
# it was checked in stop fitting together (a manifest and its lock, a pin
# and its Dockerfile). A pull request that changes no relay is never
# stopped, and neither is a run that starts when it is created.
#
# Run in a checkout with history (fetch-depth: 0). Exits 2 when it cannot
# tell, rather than print nothing, which would read as "nothing has moved".
set -euo pipefail

if [[ "${1:-}" == "--checkout" ]]; then
  if (($# != 2)); then
    echo "usage: run-base-moved.sh --checkout BASE_NOW" >&2
    exit 2
  fi
  now=$2
  if ! created=$(git rev-parse -q --verify 'HEAD^1^{commit}') || ! head=$(git rev-parse -q --verify 'HEAD^2^{commit}'); then
    echo "::error::run-base-moved: the commit checked out, $(git rev-parse --short HEAD), is not a merge of a pull request into its base branch; cannot tell which base this run tests"
    exit 2
  fi
  moved=$("$0" "$created" "$now" "$head")
  if [[ -n "$moved" ]]; then
    echo "::error::this run tests the pull request merged with $now at ${created:0:7}. $now has since changed $(wc -l <<<"$moved" | tr -d ' ') file(s) of a relay this pull request changes, the first being $(head -1 <<<"$moved"). Update the branch so the checks run on today's tree."
    exit 1
  fi
  echo "this run tests ${head:0:7} merged with $now at ${created:0:7}; $now is at $(git rev-parse --short "$now^{commit}") and has changed no relay this pull request changes since"
  exit 0
fi

if (($# != 3)); then
  echo "usage: run-base-moved.sh BASE_THEN BASE_NOW HEAD" >&2
  exit 2
fi
created=$1 now=$2 head=$3

for rev in "$created" "$now" "$head"; do
  if ! git rev-parse -q --verify "$rev^{commit}" >/dev/null; then
    echo "run-base-moved: $rev is not a commit in this checkout" >&2
    exit 2
  fi
done
if ! fork=$(git merge-base "$created" "$head"); then
  echo "run-base-moved: $created and $head have no commit in common in this checkout (is it shallow?)" >&2
  exit 2
fi

# The relays the pull request changes: relays/<name> of each changed path.
# A file directly under relays/ belongs to none.
relays=()
while IFS= read -r path; do
  IFS=/ read -r top name rest <<<"$path"
  if [[ -n "$rest" ]]; then
    relays+=("$top/$name")
  fi
done < <(git diff --name-only "$fork" "$head" -- relays)
if ((${#relays[@]} == 0)); then
  exit 0
fi

# A pathspec names a directory by whole components: relays/go does not
# match relays/go-orchestrion.
git diff --name-only "$created" "$now" -- "${relays[@]}"
