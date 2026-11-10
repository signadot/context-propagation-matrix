#!/usr/bin/env bash
# check-toolchains.sh — the language-toolchain pins that live in two places
# must agree. Dependabot bumps each side independently (gomod raises go.mod's
# `go` directive when a dependency needs it; the docker ecosystem bumps FROM
# tags), and the matrix only notices on its next run. Run in CI
# (.github/workflows/toolchains.yml) and locally.
#
#   Go    go.mod `go` (and `toolchain`) directive  <=  FROM golang:<tag>
#         for the root module and each Go relay module. Older images cannot
#         build newer go.mod (GOTOOLCHAIN=auto in the Dockerfiles is only the
#         safety net); newer images are fine.
#         Each relay module `replace`s the root module (../../), so its own
#         `go` directive must also be >= the root's: Go refuses to build a
#         module whose dependency declares a newer Go, and only `go get`/`go
#         mod download` switch toolchains for a dependency — `go build` does
#         not. Dependabot raises the root directive (a kadm bump did) without
#         touching the relay modules.
#   .NET  Relay.csproj <TargetFramework>netN.0</TargetFramework>
#         == dotnet/sdk:N.0 == dotnet/aspnet:N.0. The aspnet image ships only
#         its own major's shared framework, so a mismatch fails at startup.
#   Java  pom.xml <java.version>N</java.version> == every eclipse-temurin-N
#         image in relays/java/Dockerfile, so the measured JVM is the declared one.
#
# Prints one line per pair; exits 1 if any disagree.
set -uo pipefail
cd "$(dirname "$0")/.."

fail=0
ok()   { printf 'ok        %s\n' "$1"; }
bad()  { printf 'MISMATCH  %s\n' "$1"; fail=1; }
minor() { printf '%s' "$1" | sed -E 's/^([0-9]+)\.([0-9]+).*/\1 \2/'; } # "1.26.0" -> "1 26"

# --- Go --------------------------------------------------------------------
# Effective Go requirement of a module: max(go directive, toolchain directive).
gowant() {
  local want tc
  want=$(awk '$1=="go"{print $2}' "$1")
  tc=$(awk '$1=="toolchain"{sub(/^go/,"",$2); print $2}' "$1")
  [[ -n "$tc" ]] && want=$(printf '%s\n%s\n' "$want" "$tc" | sort -V | tail -1)
  printf '%s' "$want"
}
root_want=$(gowant go.mod)
for pair in "go.mod docker/go.Dockerfile" \
            "gen/go.mod docker/go.Dockerfile" \
            "relays/go/internal/go.mod relays/go/Dockerfile" \
            "relays/go/otel/go.mod relays/go/Dockerfile" \
            "relays/go/datadog/go.mod relays/go/Dockerfile" \
            "relays/go/newrelic/go.mod relays/go/Dockerfile" \
            "relays/go/elastic/go.mod relays/go/Dockerfile" \
            "relays/go/none/go.mod relays/go/Dockerfile" \
            "relays/go-otelc/go.mod relays/go-otelc/Dockerfile" \
            "relays/go-orchestrion/go.mod relays/go-orchestrion/Dockerfile"; do
  set -- $pair; mod=$1; df=$2
  want=$(gowant "$mod")
  have=$(grep -E '^FROM golang:' "$df" | head -1 | sed -E 's/^FROM golang:([0-9]+\.[0-9]+).*/\1/')
  if [[ -z "$want" || -z "$have" ]]; then bad "$df: could not read go.mod directive ($want) or golang image tag ($have)"; continue; fi
  read -r wmaj wmin <<<"$(minor "$want")"; read -r hmaj hmin <<<"$(minor "$have")"
  if (( hmaj > wmaj || (hmaj == wmaj && hmin >= wmin) )); then
    ok "$df golang:$have >= $mod go $want"
  else
    bad "$df golang:$have is older than $mod go $want (bump the FROM tag)"
  fi
  # Relay modules depend on in-repo modules (gen, relays/go/internal) through
  # replace directives; those modules' go directives follow the root's, so a
  # relay module must not fall behind the root.
  if [[ "$mod" != go.mod ]] && grep -qE '^\s*(replace\s+)?github.com/signadot/context-propagation-matrix(/\S+)? => \.\./' "$mod"; then
    if [[ "$(printf '%s\n%s\n' "$root_want" "$want" | sort -V | tail -1)" == "$want" ]]; then
      ok "$mod go $want >= go.mod go $root_want (replaces in-repo modules)"
    else
      bad "$mod go $want is older than go.mod go $root_want (run: cd $(dirname "$mod") && go mod edit -go=$root_want && go mod tidy)"
    fi
  fi
done

# --- .NET ------------------------------------------------------------------
csproj=$(ls relays/dotnet/*.csproj | head -1)
tfm=$(sed -nE 's/.*<TargetFramework>net([0-9]+)\.[0-9]+<\/TargetFramework>.*/\1/p' "$csproj" | head -1)
for img in sdk aspnet; do
  have=$(grep -E "^FROM mcr\.microsoft\.com/dotnet/$img:" relays/dotnet/Dockerfile | head -1 | sed -E "s#^FROM mcr\.microsoft\.com/dotnet/$img:([0-9]+)\..*#\1#")
  if [[ -z "$tfm" || -z "$have" ]]; then bad "relays/dotnet: could not read TargetFramework ($tfm) or dotnet/$img tag ($have)"; continue; fi
  if [[ "$have" == "$tfm" ]]; then
    ok "relays/dotnet/Dockerfile dotnet/$img:$have.0 == $csproj net$tfm.0"
  else
    bad "relays/dotnet/Dockerfile dotnet/$img:$have.0 != $csproj net$tfm.0 (bump both together)"
  fi
done

# --- Java ------------------------------------------------------------------
jv=$(sed -nE 's/.*<java\.version>([0-9]+)<\/java\.version>.*/\1/p' relays/java/pom.xml | head -1)
if [[ -z "$jv" ]]; then
  bad "relays/java/pom.xml: no <java.version>"
else
  while read -r line; do
    have=$(printf '%s' "$line" | sed -nE 's/.*temurin[-:]([0-9]+).*/\1/p')
    [[ -z "$have" ]] && continue
    if [[ "$have" == "$jv" ]]; then
      ok "relays/java/Dockerfile '$line' == pom.xml java.version $jv"
    else
      bad "relays/java/Dockerfile '$line' != pom.xml java.version $jv (bump both together)"
    fi
  done < <(grep -E '^FROM .*temurin' relays/java/Dockerfile | sed -E 's/^FROM +//; s/ +AS .*//' | sort -u)
fi

if (( fail )); then
  echo "toolchains: mismatch — see above" >&2
  exit 1
fi
echo "toolchains: all pins agree"
