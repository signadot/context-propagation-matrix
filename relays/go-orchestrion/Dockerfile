# syntax=docker/dockerfile:1
# datadog-go-orchestrion: the naive Go app in this directory compiled with
# Datadog Orchestrion (compile-time auto-instrumentation). Build context is
# the repo root (so gen/ and the root go.mod are reachable, mirroring
# relays/go/Dockerfile).
#
# The build command is the only thing that differs from an uninstrumented
# build: `go build` becomes `go run github.com/DataDog/orchestrion go build`.
# Orchestrion's own version and dd-trace-go's are pinned in go.mod (Dependabot
# bumps them).

FROM golang:1.25 AS build
WORKDIR /src
# Both modules' manifests first, for layer-cached dependency downloads. The
# relays/go-orchestrion module has a `replace
# github.com/signadot/context-propagation-matrix => ../../` directive, so the
# root go.mod must be in place.
COPY go.mod go.sum ./
COPY relays/go-orchestrion/go.mod relays/go-orchestrion/go.sum ./relays/go-orchestrion/
RUN --mount=type=cache,target=/go/pkg/mod cd relays/go-orchestrion && go mod download
# Now the source.
COPY gen ./gen
COPY relays/go-orchestrion ./relays/go-orchestrion
# CGO_ENABLED=0 matches the hand-wired Go relays; dd-trace-go v2 supports it.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    cd relays/go-orchestrion && \
    # Stamp the tracer version into the binary: the woven dd-trace-go is
    # invisible to debug.ReadBuildInfo, so /info reports it via this ldflag.
    DDVER=$(go list -m -f '{{.Version}}' github.com/DataDog/dd-trace-go/v2) && \
    CGO_ENABLED=0 GOFLAGS=-buildvcs=false go run github.com/DataDog/orchestrion \
      go build -ldflags "-X main.tracerVersion=$DDVER" -o /relay . && \
    # Fail the build if weaving silently did not happen: the woven binary must
    # contain dd-trace-go symbols (they are injected at link time and do NOT
    # appear in `go version -m` output).
    go tool nm /relay | grep -q 'dd-trace-go/contrib/IBM/sarama' && \
    go tool nm /relay | grep -q 'dd-trace-go/contrib/net/http'

FROM debian:bookworm-slim AS runtime
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /relay /usr/local/bin/relay
EXPOSE 8080 8081
ENTRYPOINT ["/usr/local/bin/relay"]
