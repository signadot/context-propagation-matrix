#!/usr/bin/env bash
# create-release.sh TAG NOTES_FILE [gh release create arguments] — publish a
# release, with its tag on the commit that was measured ($GITHUB_SHA) when
# GitHub allows that and on the tip of main when it does not.
#
# Left to itself GitHub puts a new tag on the tip of main, which is a later
# commit than the one measured when something merged during the run. So the
# tag is asked for at $GITHUB_SHA (--target).
#
# GitHub can refuse that request with "HTTP 403: Resource not accessible by
# integration". Its stated rule: a release that targets a commit which
# changes a workflow file, and which no branch or tag points at, needs the
# `workflows` permission, and a workflow's own token cannot be given it.
# https://github.blog/changelog/2023-11-02-github-actions-enforcing-workflow-scope-when-creating-a-release/
# The run on 58a59c5 (2026-11-13) was refused this way and published nothing:
# that commit changed two workflows, and main had moved on while it ran.
#
# Nothing reads where the tag sits: results.json records the commit
# (run.harness_commit, and measured_commit on each variant). So a 403 on the
# first attempt is answered by creating the release without --target. The
# tag is then on main's tip, the release notes and a warning on the run say
# so, and the measurements are published. Any other failure fails the step.
set -euo pipefail

usage="usage: create-release.sh TAG NOTES_FILE [gh release create arguments]"
tag=${1:?$usage}
notes=${2:?$usage}
shift 2
sha=${GITHUB_SHA:?GITHUB_SHA is not set: it names the commit that was measured}
[[ -f "$notes" ]] || { echo "create-release.sh: no notes file $notes" >&2; exit 2; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

if gh release create "$tag" --target "$sha" --notes-file "$notes" "$@" 2>"$work/err"; then
  exit 0
fi
cat "$work/err" >&2
grep -q 'HTTP 403' "$work/err" || exit 1

echo "::warning title=Release tag is not on the measured commit::GitHub refused to tag $sha for $tag (HTTP 403). Creating the release without --target: its tag will be on the tip of main. results.json records the measured commit."
{
  cat "$notes"
  # shellcheck disable=SC2016 # the backticks are Markdown
  printf '\n\nThe tag `%s` is on the tip of `main` as it was when this was published, not on the commit that was measured (`%s`): GitHub did not let the workflow tag that commit. `results.json` records the measured commit.\n' "$tag" "$sha"
} >"$work/notes"
gh release create "$tag" --notes-file "$work/notes" "$@"
