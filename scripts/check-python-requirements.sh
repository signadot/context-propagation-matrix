#!/usr/bin/env bash
# check-python-requirements.sh — relays/python/requirements.txt must agree with
# relays/python/requirements.in, the file pip-compile makes it from.
#
# The image installs requirements.txt. Nothing reads requirements.in but
# pip-compile, run by a person or by Dependabot. So a version changed in
# requirements.in and not recompiled would build, pass, and install the old
# one. This fails when:
#
#   - a line of requirements.in is not `name==version`;
#   - a pin of requirements.in is missing from requirements.txt, or is there
#     at another version;
#   - requirements.txt says a package comes from requirements.in
#     (`# via -r requirements.in`) and requirements.in does not list it.
#
# It does not recompile, so the packages requirements.txt adds on its own
# (what the pinned ones pull in) are compared with nothing. The image build
# runs `pip check`, which fails on one that is missing or incompatible.
#
#   scripts/check-python-requirements.sh [IN_FILE [COMPILED_FILE]]
#
# Offline; needs bash and awk.
set -euo pipefail

in_file=${1:-}
txt_file=${2:-}
if [[ -z "$in_file" ]]; then
  cd "$(dirname "$0")/.."
  in_file=relays/python/requirements.in
fi
[[ -n "$txt_file" ]] || txt_file=${in_file%.in}.txt
for f in "$in_file" "$txt_file"; do
  [[ -f "$f" ]] || { echo "check-python-requirements: no file $f" >&2; exit 2; }
done

awk -v in_file="$in_file" -v txt_file="$txt_file" -v in_base="$(basename "$in_file")" '
# PEP 503: names compare without case, and runs of - _ . are one separator.
function norm(name) {
  name = tolower(name)
  gsub(/[-_.]+/, "-", name)
  return name
}
# 3.1 and 3.1.0 are the same release.
function release(version) {
  version = tolower(version)
  while (version ~ /\.0$/) sub(/\.0$/, "", version)
  return version
}
# pin(s): is s `name==version` (extras allowed after the name)? Sets NAME, VERSION.
function pin(s,    i) {
  i = index(s, "==")
  if (i < 2) return 0
  NAME = substr(s, 1, i - 1)
  VERSION = substr(s, i + 2)
  sub(/\[[^]]*\]$/, "", NAME)
  if (NAME !~ /^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$/) return 0
  if (VERSION !~ /^[A-Za-z0-9][A-Za-z0-9.!+_-]*$/) return 0
  return 1
}
function problem(text) { problems[++n_problems] = text }

FILENAME == in_file {
  line = $0
  sub(/^[ \t]+/, "", line)
  if (line ~ /^#/) next
  sub(/[ \t]+#.*$/, "", line)
  sub(/[ \t]+$/, "", line)
  if (line == "") next
  if (!pin(line)) {
    problem(in_file ":" FNR ": `" line "` is not `name==version`")
    next
  }
  key = norm(NAME)
  if (key in wanted) {
    problem(in_file ":" FNR ": " NAME " is pinned twice")
    next
  }
  wanted[key] = VERSION
  shown[key] = NAME
  order[++n_wanted] = key
  next
}

# requirements.txt: a pin starts its line; the lines under it say what needs it.
/^[A-Za-z0-9]/ {
  first = $1
  sub(/;$/, "", first)
  current = ""
  if (pin(first)) {
    current = norm(NAME)
    compiled[current] = VERSION
  }
  next
}
/^[ \t]+#/ && current != "" && index($0, "-r " in_base) {
  if (!(current in direct)) direct_order[++n_direct] = current
  direct[current] = 1
}

END {
  if (n_wanted == 0 && n_problems == 0) problem(in_file ": no `name==version` line")
  for (i = 1; i <= n_wanted; i++) {
    key = order[i]
    if (!(key in compiled))
      problem(shown[key] ": " in_file " pins " wanted[key] ", " txt_file " does not have the package")
    else if (release(compiled[key]) != release(wanted[key]))
      problem(shown[key] ": " in_file " pins " wanted[key] ", " txt_file " has " compiled[key])
  }
  for (i = 1; i <= n_direct; i++) {
    key = direct_order[i]
    if (!(key in wanted))
      problem(key ": " txt_file " says it comes from " in_base ", which does not list it")
  }
  if (n_problems) {
    print "check-python-requirements: " txt_file " does not agree with " in_file ":" > "/dev/stderr"
    for (i = 1; i <= n_problems; i++) print "  " problems[i] > "/dev/stderr"
    print "Recompile it with the command at the top of relays/python/Dockerfile and commit both files." > "/dev/stderr"
    exit 1
  }
  print "check-python-requirements: " txt_file " has the " n_wanted " pin(s) of " in_file " at the same versions"
}
' "$in_file" "$txt_file"
