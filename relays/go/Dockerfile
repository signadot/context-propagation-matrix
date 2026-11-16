# syntax=docker/dockerfile:1
# Build context is the repo root (so gen/ is reachable, mirroring
# relays/java/Dockerfile). Select the relay with
# --build-arg TOOL=otel|datadog|newrelic|elastic|none; each tool is its own
# Go module (relays/go/<tool>/go.mod) that `replace`s the two in-repo modules
# it depends on: gen (the stubs) and relays/go/internal (shared plumbing).

# go.mod is the source of truth for the Go version; the image is a bootstrap
# (see docker/go.Dockerfile and scripts/check-toolchains.sh).
FROM golang:1.26 AS build
ENV GOTOOLCHAIN=auto
WORKDIR /src
ARG TOOL=otel
# All three modules' manifests first, for layer-cached dependency downloads.
COPY gen/go.mod gen/go.sum ./gen/
COPY relays/go/internal/go.mod relays/go/internal/go.sum ./relays/go/internal/
COPY relays/go/${TOOL}/go.mod relays/go/${TOOL}/go.sum ./relays/go/${TOOL}/
RUN --mount=type=cache,target=/go/pkg/mod cd "relays/go/${TOOL}" && go mod download
# Now the source.
COPY gen ./gen
COPY relays/go/internal ./relays/go/internal
COPY relays/go/${TOOL} ./relays/go/${TOOL}
RUN --mount=type=cache,target=/go/pkg/mod \
    cd "relays/go/${TOOL}" && CGO_ENABLED=0 go build -o /relay .

FROM debian:bookworm-slim AS runtime
# ca-certificates: the New Relic agent connects to collector.newrelic.com
# over TLS. Harmless for the other relays.
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /relay /usr/local/bin/relay
EXPOSE 8080 8081
ENTRYPOINT ["/usr/local/bin/relay"]
