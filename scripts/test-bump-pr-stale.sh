#!/usr/bin/env bash
# test-bump-pr-stale.sh — bump-pr-stale.sh must report a bump branch whose
# relay main has changed since the branch was built, and no other. A miss
# leaves a bump with checks from an older main free to merge itself; a false
# report rebuilds a branch every six hours and asks for its checks again.
#
# Runs the real script in a scratch repository laid out like this one.
# Offline; needs bash and git.
set -euo pipefail
script=$(cd "$(dirname "$0")" && pwd)/bump-pr-stale.sh

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.invalid GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.invalid
# A commit may leave `git maintenance` running in the background, still
# writing under .git when the next fixture is made or this one is removed.
export GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_0=maintenance.auto GIT_CONFIG_VALUE_0=false GIT_CONFIG_KEY_1=gc.auto GIT_CONFIG_VALUE_1=0

# put FILE... — write each file (its own path as content plus a counter) and
# commit them on the current branch.
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

# fresh — a repository with main and, forked from it, one bump branch per
# kind of pin: a Gemfile, a go.mod family and a Dockerfile ARG.
fresh() {
  local repo
  repo=$(mktemp -d "$work/repo.XXXXXX")
  git init -q -b main "$repo"
  cd "$repo"
  put README.md pins.yaml \
      relays/ruby/Dockerfile relays/ruby/app.rb \
      relays/ruby/gemfiles/datadog/Gemfile relays/ruby/gemfiles/otel/Gemfile \
      relays/go/Dockerfile relays/go/internal/app.go relays/go/datadog/go.mod relays/go/datadog/go.sum \
      relays/go-orchestrion/go.mod relays/java/Dockerfile relays/node/package.json
  git checkout -q -b bump/datadog-ruby main && put relays/ruby/gemfiles/datadog/Gemfile
  git checkout -q -b bump/dd-trace-go main && put relays/go/datadog/go.mod relays/go/datadog/go.sum
  git checkout -q -b bump/dd-java-agent main && put relays/java/Dockerfile
  git checkout -q main
}

ruby=(bump/datadog-ruby relays/ruby/gemfiles/datadog/Gemfile relays/ruby/gemfiles/datadog/Gemfile.lock)
go=(bump/dd-trace-go relays/go/datadog/go.mod relays/go/datadog/go.sum)
java=(bump/dd-java-agent relays/java/Dockerfile)

failed=0
# check NAME WANT ARGS... — WANT is what the script prints, lines joined by
# a space; "(exit N)" when it fails.
check() {
  local name=$1 want=$2 got
  shift 2
  if got=$(MAIN=main "$script" "$@" 2>/dev/null); then
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

fresh
check "main has not moved" "" "${ruby[@]}"
check "the branch's own change is not main's" "" "${go[@]}"

put README.md pins.yaml relays/node/package.json
check "main changed another relay and files outside relays/" "" "${ruby[@]}"

# What happened to #260: the locks arrived on main after the bump was built.
put relays/ruby/gemfiles/datadog/Gemfile.lock relays/ruby/gemfiles/otel/Gemfile.lock
check "a file the bump now has to write appeared on main" \
  "relays/ruby/gemfiles/datadog/Gemfile.lock relays/ruby/gemfiles/otel/Gemfile.lock" "${ruby[@]}"
check "... which is nothing to a bump of another relay" "" "${java[@]}"

fresh
put relays/ruby/gemfiles/datadog/Gemfile
check "main changed the file the bump changes" "relays/ruby/gemfiles/datadog/Gemfile" "${ruby[@]}"

fresh
put relays/ruby/Dockerfile
check "main changed the relay's Dockerfile" "relays/ruby/Dockerfile" "${ruby[@]}"

fresh
put relays/ruby/gemfiles/otel/Gemfile
check "main changed another tool of the same relay" "relays/ruby/gemfiles/otel/Gemfile" "${ruby[@]}"

fresh
put relays/go/internal/app.go
check "main changed the application the Go tools share" "relays/go/internal/app.go" "${go[@]}"

fresh
put relays/go-orchestrion/go.mod
check "relays/go-orchestrion is not relays/go" "" "${go[@]}"

fresh
put relays/java/Dockerfile relays/ruby/app.rb
check "a pin written as a Dockerfile ARG" "relays/java/Dockerfile" "${java[@]}"

# Rebuilt as bump-agents.yml does it: main plus one commit.
fresh
put relays/ruby/Dockerfile
git checkout -q -B bump/datadog-ruby main && put relays/ruby/gemfiles/datadog/Gemfile && git checkout -q main
check "a branch rebuilt on today's main" "" "${ruby[@]}"
put README.md
check "... and still not stale after an unrelated merge" "" "${ruby[@]}"

fresh
put relays/ruby/Dockerfile
check "a file outside relays/ is compared by its directory" "" bump/datadog-ruby docs/pin.txt
put docs/other.txt
check "... and reported when that directory changed" "docs/other.txt" bump/datadog-ruby docs/pin.txt

# No answer is not "not stale".
fresh
check "no file named" "(exit 2)" bump/datadog-ruby
check "a branch the checkout does not have" "(exit 2)" bump/absent relays/ruby/Dockerfile
git checkout -q --orphan elsewhere && git rm -q -rf . && put relays/ruby/Dockerfile && git checkout -q main
check "no commit in common, as in a shallow checkout" "(exit 2)" elsewhere relays/ruby/Dockerfile

exit "$failed"
