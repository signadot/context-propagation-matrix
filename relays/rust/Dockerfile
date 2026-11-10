# syntax=docker/dockerfile:1
# Build context is the repo root (so proto/relay.proto is reachable, mirroring
# relays/go/Dockerfile). Select the relay with --build-arg TOOL=otel|datadog|none:
# each tool is its own crate, relays/rust/<tool>/Cargo.toml + Cargo.lock,
# building the shared source in relays/rust/src (see README.md). The
# lockfiles differ per tool (each resolves its own tracer crates), so the
# build layer is per tool too; it is a registry-cache hit whenever that
# tool's Cargo.lock, src/ and proto/ are unchanged — which on a CI runner,
# where the cargo cache mounts start empty, is the difference between
# seconds and ten minutes.
#
# rust:1.9x rather than 1.8x: the committed lockfiles are resolved with the
# 2026 dependency tree (some transitive crates' MSRV exceeds 1.8x); it matches
# the toolchain they were generated with.

FROM rust:1.99-bookworm AS build
# cmake: the rdkafka `cmake-build` feature compiles the vendored librdkafka.
# protobuf-compiler: tonic-prost-build shells out to protoc for the stubs.
RUN apt-get update \
 && apt-get install -y --no-install-recommends cmake protobuf-compiler \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY proto ./proto
# Explicit paths (not `COPY relays/rust`): the repo root has no .dockerignore,
# so a blanket copy would drag any local target/ into the context.
COPY relays/rust/build.rs ./relays/rust/
COPY relays/rust/src ./relays/rust/src
ARG TOOL=otel
COPY relays/rust/${TOOL}/Cargo.toml relays/rust/${TOOL}/Cargo.lock ./relays/rust/${TOOL}/
# target/ lives in a cache mount, so the layer holds just the binary. The
# three tools share it (cargo reuses the dependency artefacts they have in
# common — librdkafka above all), and sharing=locked serialises concurrent
# builds of two tools instead of letting them race on the registry cache,
# whose lock file cargo keeps outside the mounted directory.
ENV CARGO_TARGET_DIR=/target
RUN --mount=type=cache,target=/usr/local/cargo/registry,sharing=locked \
    --mount=type=cache,target=/target,sharing=locked \
    cd "relays/rust/${TOOL}" \
 && cargo build --release --locked --bin "relay-${TOOL}" \
 && mkdir -p /out \
 && cp "/target/release/relay-${TOOL}" /out/relay

FROM debian:bookworm-slim AS runtime
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/relay /usr/local/bin/relay
EXPOSE 8080 8081
ENTRYPOINT ["/usr/local/bin/relay"]
