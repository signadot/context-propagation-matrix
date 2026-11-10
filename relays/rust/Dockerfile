# syntax=docker/dockerfile:1
# Build context is the repo root (so proto/relay.proto is reachable, mirroring
# relays/go/Dockerfile). Select the relay binary with
# --build-arg TOOL=otel|datadog|none.
#
# All three binaries are compiled in ONE cargo invocation, in a stage that
# does not see TOOL, and only the binaries leave that stage. So the expensive
# layer is byte-identical for the three Rust variants (one build serves all,
# and the shared layer cache ref relays-rust never sees three different
# versions of it), and it is a registry-cache hit whenever Cargo.lock, src/
# and proto/ are unchanged — which on a CI runner, where the cargo cache
# mounts start empty, is the difference between seconds and ten minutes.
# TOOL only picks a file in the final stage.
#
# rust:1.93 rather than 1.8x: the committed Cargo.lock is resolved with the
# 2026 dependency tree (some transitive crates' MSRV exceeds 1.8x); it matches
# the toolchain the lockfile was generated with.

FROM rust:1.99-bookworm AS build
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
# target/ lives in a cache mount, so the layer holds just the binaries.
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/src/relays/rust/target \
    cd relays/rust \
 && cargo build --release --locked --bins \
 && mkdir -p /out \
 && cp target/release/relay-otel target/release/relay-datadog target/release/relay-none /out/

FROM debian:bookworm-slim AS runtime
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*
ARG TOOL=otel
COPY --from=build /out/relay-${TOOL} /usr/local/bin/relay
EXPOSE 8080 8081
ENTRYPOINT ["/usr/local/bin/relay"]
