# syntax=docker/dockerfile:1
# otel-go-compiletime: the naive Go app in this directory compiled with the
# OpenTelemetry Go Compile-Time Instrumentation tool ("otelc"). Build context
# is the repo root (so gen/ and the root go.mod are reachable, mirroring
# relays/go/Dockerfile).
#
# The build command is the only thing that differs from an uninstrumented
# build: `go build` becomes `go tool otelc go build` (otelc is a Go 1.24+ tool
# dependency, pinned in go.mod, so Dependabot bumps it). With no committed
# otel.instrumentation.go file, otelc analyzes the dependency graph and
# enables every instrumentation it has for the libraries the app imports
# (net/http, grpc, segmentio/kafka-go — it has none for amqp091/RabbitMQ,
# which the rabbitmq legs of the matrix measure).

FROM golang:1.25 AS build
WORKDIR /src
# Both modules' manifests first, for layer-cached dependency downloads. The
# relays/go-otelc module has a `replace
# github.com/signadot/context-propagation-matrix => ../../` directive, so the
# root go.mod must be in place.
COPY go.mod go.sum ./
COPY relays/go-otelc/go.mod relays/go-otelc/go.sum ./relays/go-otelc/
RUN --mount=type=cache,target=/go/pkg/mod cd relays/go-otelc && go mod download
# Now the source.
COPY gen ./gen
COPY relays/go-otelc ./relays/go-otelc
# CGO_ENABLED=0 matches the hand-wired Go relays. otelc's dependency
# synchronization edits go.mod/go.sum during the build (adding its
# instrumentation packages), which is fine inside the image.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    cd relays/go-otelc && \
    CGO_ENABLED=0 GOFLAGS=-buildvcs=false go tool otelc go build -o /relay . && \
    # Fail the build if weaving silently did not happen: the woven binary must
    # contain OTel SDK + kafka-go instrumentation symbols (injected at link
    # time; they do NOT appear in `go version -m` output).
    go tool nm /relay | grep -q 'go.opentelemetry.io/otel/sdk/trace' && \
    go tool nm /relay | grep -q 'otelc/instrumentation/github.com/segmentio/kafka-go'

FROM debian:bookworm-slim AS runtime
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /relay /usr/local/bin/relay
EXPOSE 8080 8081
ENTRYPOINT ["/usr/local/bin/relay"]
