#!/usr/bin/env bash
# test-create-release.sh — create-release.sh must publish a run whether or
# not GitHub lets the workflow tag the measured commit, and must not hide any
# other failure. matrix.yml and backtest.yml publish through it; a refusal
# that fails the step drops every measurement of the run.
#
# Runs the real script against a stand-in `gh` that records each call (its
# arguments, one per line, and the notes it was given) and answers as
# $GH_ANSWERS says, one word per call: ok, 403 or 422. Offline; needs bash.
set -euo pipefail
cd "$(dirname "$0")/.."

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"
cat >"$work/bin/gh" <<'EOF'
#!/usr/bin/env bash
n=$(($(cat "$CALLS/count") + 1))
echo "$n" >"$CALLS/count"
printf '%s\n' "$@" >"$CALLS/$n.args"
while (($#)); do
  [[ "$1" == --notes-file ]] && cp "$2" "$CALLS/$n.notes"
  shift
done
read -ra answers <<<"$GH_ANSWERS"
case "${answers[n-1]:-ok}" in
  ok) echo "https://github.com/test/test/releases/tag/stand-in" ;;
  403) echo "HTTP 403: Resource not accessible by integration (https://api.github.com/repos/test/test/releases)" >&2; exit 1 ;;
  422) echo "HTTP 422: Validation Failed (https://api.github.com/repos/test/test/releases)" >&2; exit 1 ;;
esac
EOF
chmod +x "$work/bin/gh"
printf 'Summary of the run.\n' >"$work/summary.md"

sha=58a59c5000000000000000000000000000000000
failed=0
fail() { echo "FAIL  $name: $*"; failed=1; bad=1; }

# run ANSWERS [create-release.sh arguments] — a fresh call log, then the
# script. Sets $status, $out (stdout and stderr) and $calls.
run() {
  local answers=$1
  shift
  rm -rf "$work/calls" && mkdir "$work/calls" && echo 0 >"$work/calls/count"
  status=0
  out=$(PATH="$work/bin:$PATH" CALLS="$work/calls" GH_ANSWERS="$answers" GITHUB_SHA="$sha" \
    scripts/create-release.sh "$@" 2>&1) || status=$?
  calls=$(cat "$work/calls/count")
}
# args N — the arguments of call N, space separated on one line; nothing
# when there was no call N.
args() { [[ -f "$work/calls/$1.args" ]] && tr '\n' ' ' <"$work/calls/$1.args" || true; }
done_() { [[ $bad == 0 ]] && echo "ok    $name"; return 0; }

name="the tag is asked for on the measured commit"; bad=0
run "ok" run-2026-11-13 "$work/summary.md" --title run-2026-11-13 results.json raw.tar.gz
[[ $status == 0 ]] || fail "exit $status: $out"
[[ $calls == 1 ]] || fail "$calls calls to gh, want 1"
want="release create run-2026-11-13 --target $sha --notes-file $work/summary.md --title run-2026-11-13 results.json raw.tar.gz "
[[ "$(args 1)" == "$want" ]] || fail "gh was called with: $(args 1)"
[[ "$out" != *"::warning"* ]] || fail "warned without a refusal: $out"
done_

name="a 403 on the measured commit still publishes, on main's tip"; bad=0
run "403 ok" run-2026-11-13 "$work/summary.md" --title run-2026-11-13 results.json raw.tar.gz
[[ $status == 0 ]] || fail "exit $status: $out"
[[ $calls == 2 ]] || fail "$calls calls to gh, want 2"
[[ "$(args 1)" == *"--target $sha"* ]] || fail "the first call did not ask for the measured commit: $(args 1)"
[[ "$(args 2)" != *"--target"* ]] || fail "the second call still has --target: $(args 2)"
[[ "$(args 2)" == "release create run-2026-11-13 --notes-file "*" --title run-2026-11-13 results.json raw.tar.gz " ]] \
  || fail "the second call lost an argument: $(args 2)"
grep -qs '^Summary of the run\.$' "$work/calls/2.notes" || fail "the notes lost the summary"
grep -qs "$sha" "$work/calls/2.notes" || fail "the notes do not name the measured commit"
[[ "$out" == *"::warning title="*"$sha"* ]] || fail "no warning naming the commit: $out"
[[ "$out" == *"HTTP 403"* ]] || fail "GitHub's error was not shown: $out"
[[ "$(cat "$work/summary.md")" == "Summary of the run." ]] || fail "the caller's notes file was changed"
done_

name="arguments with spaces and asset labels pass through both calls"; bad=0
run "403 ok" backtest-otel-go-1.40.0 "$work/summary.md" --title "backtest: otel-go @ 1.40.0" --prerelease "raw/otel-go.json#raw-otel-go.json"
[[ $status == 0 ]] || fail "exit $status: $out"
for n in 1 2; do
  grep -qsx 'backtest: otel-go @ 1.40.0' "$work/calls/$n.args" || fail "call $n split or lost the title"
  grep -qsx -- '--prerelease' "$work/calls/$n.args" || fail "call $n lost --prerelease"
  grep -qsx 'raw/otel-go.json#raw-otel-go.json' "$work/calls/$n.args" || fail "call $n lost the labelled asset"
done
done_

name="any other failure fails, with no second attempt"; bad=0
run "422 ok" run-2026-11-13 "$work/summary.md" --title run-2026-11-13 results.json
[[ $status != 0 ]] || fail "exit 0 after HTTP 422"
[[ $calls == 1 ]] || fail "$calls calls to gh, want 1"
[[ "$out" == *"HTTP 422"* ]] || fail "GitHub's error was not shown: $out"
done_

name="a 403 without --target as well fails"; bad=0
run "403 403" run-2026-11-13 "$work/summary.md" --title run-2026-11-13 results.json
[[ $status != 0 ]] || fail "exit 0 though nothing was published"
[[ $calls == 2 ]] || fail "$calls calls to gh, want 2"
done_

name="no measured commit, nothing published"; bad=0
rm -rf "$work/calls" && mkdir "$work/calls" && echo 0 >"$work/calls/count"
status=0
out=$(env -u GITHUB_SHA PATH="$work/bin:$PATH" CALLS="$work/calls" GH_ANSWERS=ok \
  scripts/create-release.sh run-2026-11-13 "$work/summary.md" results.json 2>&1) || status=$?
[[ $status != 0 ]] || fail "exit 0 without GITHUB_SHA"
[[ "$(cat "$work/calls/count")" == 0 ]] || fail "gh was called without a commit to tag"
done_

exit "$failed"
