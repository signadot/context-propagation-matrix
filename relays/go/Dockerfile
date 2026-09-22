# syntax=docker/dockerfile:1
# Build context is the repo root (so gen/ and the root go.mod are reachable,
# mirroring relays/java/Dockerfile). Select the relay binary with
# --build-arg TOOL=otel|datadog|newrelic|elastic.

FROM golang:1.25 AS build
WORKDIR /src
# Both modules' manifests first, for layer-cached dependency downloads. The
# relays/go module has a `replace github.com/signadot/context-propagation-matrix
# => ../../` directive, so the root go.mod must be in place.
COPY go.mod go.sum ./
COPY relays/go/go.mod relays/go/go.sum ./relays/go/
RUN --mount=type=cache,target=/go/pkg/mod cd relays/go && go mod download
# Now the source.
COPY gen ./gen
COPY relays/go ./relays/go
ARG TOOL=otel
ARG TRACER_VERSION
COPY relays/go/pin-tracer.sh /usr/local/bin/pin-tracer
RUN --mount=type=cache,target=/go/pkg/mod \
    cd relays/go && pin-tracer "$TOOL" "$TRACER_VERSION" \
 && CGO_ENABLED=0 go build -o /relay "./${TOOL}"

FROM debian:bookworm-slim AS runtime
# ca-certificates: the New Relic agent connects to collector.newrelic.com
# over TLS. Harmless for the other relays.
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /relay /usr/local/bin/relay
EXPOSE 8080 8081
ENTRYPOINT ["/usr/local/bin/relay"]
