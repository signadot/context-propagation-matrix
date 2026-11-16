#!/usr/bin/env bash
# test-run-base-moved.sh — run-base-moved.sh must report a run whose base
# branch has since changed a relay the pull request changes, and no other.
# A miss lets a run that was held or run again pass on a tree main no longer
# has; a false report fails pull requests that did nothing wrong.
#
# Runs the real script in a scratch repository laid out like this one.
# Offline; needs bash and git.
set -euo pipefail
script=$(cd "$(dirname "$0")" && pwd)/run-base-moved.sh

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.invalid GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.invalid
# A commit may leave `git maintenance` running in the background, still
# writing under .git when the next fixture is made or this one is removed.
export GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_0=maintenance.auto GIT_CONFIG_VALUE_0=false GIT_CONFIG_KEY_1=gc.auto GIT_CONFIG_VALUE_1=0

# put FILE... — write each file (its own path plus a counter) and commit
# them on the current branch.
n=0
put() {
  local f
  for f in "$@"; do
    mkdir -p "$(dirname "$f")"
    n=$((n + 1))
    echo "$f $n" >"$f"
  done
  git add -A
  git commit -q -m "change $*"
}

# fresh — a repository with main and pull-request branches forked from it.
# The tag `created` is main when their runs were created.
fresh() {
  local repo
  repo=$(mktemp -d "$work/repo.XXXXXX")
  git init -q -b main "$repo"
  cd "$repo"
  put README.md driver/main.go \
      relays/ruby/Dockerfile relays/ruby/gemfiles/datadog/Gemfile relays/ruby/gemfiles/otel/Gemfile \
      relays/go/internal/app.go relays/go/datadog/go.mod relays/go-orchestrion/go.mod \
      relays/node/package.json relays/python/requirements.txt
  git checkout -q -b pr/ruby main && put relays/ruby/gemfiles/datadog/Gemfile
  git checkout -q -b pr/go main && put relays/go/datadog/go.mod
  git checkout -q -b pr/two main && put relays/node/package.json relays/python/requirements.txt
  git checkout -q -b pr/docs main && put README.md driver/main.go
  git checkout -q main
  git tag created
}

failed=0
# check NAME WANT ARGS... — WANT is what the script prints, lines joined by
# a space; "(exit N)" when it fails.
check() {
  local name=$1 want=$2 got
  shift 2
  if got=$("$script" "$@" 2>/dev/null); then
    got=$(tr '\n' ' ' <<<"$got")
    got=${got% }
  else
    got="(exit $?)"
  fi
  if [[ "$got" == "$want" ]]; then
    echo "ok    $name"
  else
    echo "FAIL  $name: want '$want', got '$got'"
    failed=1
  fi
}

# check_run NAME STATUS TEXT — the --checkout form, in the current checkout
# and against main: its exit status, and a string its output must contain.
check_run() {
  local name=$1 status=$2 text=$3 out rc=0
  out=$("$script" --checkout main 2>&1) || rc=$?
  if [[ "$rc" == "$status" && "$out" == *"$text"* ]]; then
    echo "ok    $name"
  else
    echo "FAIL  $name: want exit $status and '$text', got exit $rc and '$out'"
    failed=1
  fi
}

fresh
check "the run starts when it is created" "" created main pr/ruby

put README.md driver/main.go relays/node/package.json
check "main changed another relay and shared code" "" created main pr/ruby

# What happened to #260: the locks arrived on main while its run was held.
put relays/ruby/gemfiles/datadog/Gemfile.lock relays/ruby/gemfiles/otel/Gemfile.lock
check "a lock for the manifest arrived on main after the run was created" \
  "relays/ruby/gemfiles/datadog/Gemfile.lock relays/ruby/gemfiles/otel/Gemfile.lock" created main pr/ruby
check "... which is nothing to a pull request for another relay" "" created main pr/go
check "... or to one that changes no relay" "" created main pr/docs
check "... and is reported for one of the two relays a pull request changes" "relays/node/package.json" created main pr/two

fresh
put relays/ruby/Dockerfile
check "main changed the relay's Dockerfile" "relays/ruby/Dockerfile" created main pr/ruby

fresh
put relays/go/internal/app.go
check "main changed the application the Go tools share" "relays/go/internal/app.go" created main pr/go

fresh
put relays/go-orchestrion/go.mod
check "relays/go-orchestrion is not relays/go" "" created main pr/go

# The difference from "is the branch behind main": a relay change that was
# on main before the run was created is in the tree the run tests.
fresh
put relays/ruby/Dockerfile
git tag -f created >/dev/null
check "main changed the relay before the run was created" "" created main pr/ruby
put relays/ruby/gemfiles/otel/Gemfile
check "... and again after it" "relays/ruby/gemfiles/otel/Gemfile" created main pr/ruby

# A pull request that only deletes or adds files in a relay still changes it.
fresh
git checkout -q -b pr/new main && put relays/ruby/gemfiles/elastic/Gemfile && git checkout -q main
put relays/ruby/Dockerfile
check "a pull request that adds a file to the relay" "relays/ruby/Dockerfile" created main pr/new

# A file directly under relays/ is no relay's.
fresh
git checkout -q -b pr/readme main && put relays/README.md && git checkout -q main
put relays/ruby/Dockerfile relays/README.md
check "a pull request that changes only relays/README.md" "" created main pr/readme

# What verify.yml runs. The checkout of a pull_request run is GitHub's merge
# of the pull request into the base branch as it was when the run was
# created, and stays that commit however late the run starts.
fresh
git checkout -q --detach created && git merge -q --no-ff -m "Merge pr/ruby into main" pr/ruby && git tag run
check_run "in a run's checkout: the run starts when it is created" 0 "has changed no relay"
git checkout -q main && put README.md relays/node/package.json && git checkout -q run
check_run "... it starts after main changed another relay" 0 "has changed no relay"
git checkout -q main && put relays/ruby/gemfiles/datadog/Gemfile.lock relays/ruby/gemfiles/otel/Gemfile.lock && git checkout -q run
check_run "... it starts after the lock arrived on main" 1 \
  "::error::this run tests the pull request merged with main at $(git rev-parse --short=7 created). main has since changed 2 file(s) of a relay this pull request changes, the first being relays/ruby/gemfiles/datadog/Gemfile.lock."
git checkout -q main
check_run "a checkout that is not the merge of a pull request" 2 "::error::"
git checkout -q run
check "a base branch the checkout does not have" "(exit 2)" --checkout origin/absent
check "--checkout without the base branch" "(exit 2)" --checkout

# No answer is not "nothing has moved".
fresh
check "an argument missing" "(exit 2)" created main
check "a commit the checkout does not have" "(exit 2)" created main pr/absent
check "... as the base of the event" "(exit 2)" 0123456789012345678901234567890123456789 main pr/ruby
git checkout -q --orphan elsewhere && git rm -q -rf . && put relays/ruby/Dockerfile && git checkout -q main
check "no commit in common, as in a shallow checkout" "(exit 2)" created main elsewhere

exit "$failed"
