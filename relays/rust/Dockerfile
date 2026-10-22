# syntax=docker/dockerfile:1
# Build context is the repo root (so proto/relay.proto is reachable, mirroring
# relays/go/Dockerfile). Select the relay binary with
# --build-arg TOOL=otel|datadog.
#
# rust:1.93 rather than 1.8x: the committed Cargo.lock is resolved with the
# 2026 dependency tree (some transitive crates' MSRV exceeds 1.8x); it matches
# the toolchain the lockfile was generated with.

FROM rust:1.93-bookworm AS build
# cmake: the rdkafka `cmake-build` feature compiles the vendored librdkafka.
# protobuf-compiler: tonic-prost-build shells out to protoc for the stubs.
RUN apt-get update \
 && apt-get install -y --no-install-recommends cmake protobuf-compiler \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY proto ./proto
# Explicit paths (not `COPY relays/rust`): the repo root has no .dockerignore,
# so a blanket copy would drag any local relays/rust/target into the context.
COPY relays/rust/Cargo.toml relays/rust/Cargo.lock relays/rust/build.rs ./relays/rust/
COPY relays/rust/src ./relays/rust/src
ARG TOOL=otel
ARG TRACER_VERSION
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/src/relays/rust/target \
    cd relays/rust \
 && locked=--locked \
 && if [ -n "$TRACER_VERSION" ]; then \
      case "$TOOL" in \
        otel)    crates="opentelemetry opentelemetry_sdk opentelemetry-http" ;; \
        datadog) crates="datadog-opentelemetry" ;; \
        *)       crates="" ;; \
      esac; \
      for c in $crates; do sed -i -E "s/^($c = )\"[^\"]*\"/\\1\"$TRACER_VERSION\"/" Cargo.toml; done; \
      cargo update; locked=""; \
    fi \
 && cargo build --release $locked --bin "relay-${TOOL}" \
 && cp "target/release/relay-${TOOL}" /relay

FROM debian:bookworm-slim AS runtime
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /relay /usr/local/bin/relay
EXPOSE 8080 8081
ENTRYPOINT ["/usr/local/bin/relay"]
