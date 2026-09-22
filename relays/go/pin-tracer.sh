#!/bin/sh
# pin-tracer <tool> <version>: in the current Go module, `go get` every module
# of the tool's version-locked family at <version>, then tidy. No-op when the
# version is empty (the pinned go.mod is used). Used by the Dockerfile for
# back-testing older tracer releases.
#
# Families are the module sets a vendor releases in lock-step. Contrib
# modules with their own version lines (otelhttp/otelgrpc, nr integrations)
# are deliberately left at their pins.
set -eu
tool="$1"; ver="${2:-}"
[ -n "$ver" ] || exit 0
ver="v${ver#v}"
case "$tool" in
  otel)     filter='^go\.opentelemetry\.io/otel(/|$)' ;;
  datadog)  filter='^github\.com/DataDog/dd-trace-go/' ;;
  newrelic) filter='^github\.com/newrelic/go-agent/v3$' ;;
  elastic)  filter='^go\.elastic\.co/apm/' ;;
  *) echo "pin-tracer: unknown tool $tool" >&2; exit 1 ;;
esac
mods=$(go list -m -f '{{if not .Indirect}}{{.Path}}{{end}}' all | grep -E "$filter" || true)
[ -n "$mods" ] || { echo "pin-tracer: no $tool modules in go.mod" >&2; exit 1; }
set -- ; for m in $mods; do set -- "$@" "$m@$ver"; done
echo "pin-tracer: $*" >&2
go get "$@"
go mod tidy
