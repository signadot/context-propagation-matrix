# syntax=docker/dockerfile:1
# Build context is the repo root (so proto/ is reachable).
# Targets: otel | datadog | elastic (same app, different Gemfile + boot
# file).

# grpc-tools ships no linux/arm64 protoc binaries, so stub generation runs in
# an explicit amd64 stage (emulated on Apple Silicon) and the generated .rb
# files are copied into the runtime image.
FROM --platform=linux/amd64 ruby:3.3 AS protogen
WORKDIR /app
COPY proto/relay.proto proto/relay.proto
RUN gem install grpc-tools --no-document \
 && mkdir -p gen \
 && grpc_tools_ruby_protoc -I proto --ruby_out=gen --grpc_out=gen proto/relay.proto

FROM ruby:3.3 AS base
WORKDIR /app

# Build deps for native gems. The rdkafka gem compiles a vendored librdkafka
# from source (works on linux/arm64); grpc ships precompiled binaries for
# aarch64-linux/x86_64-linux.
RUN apt-get update -qq \
 && apt-get install -y -qq --no-install-recommends \
      build-essential libssl-dev libsasl2-dev libzstd-dev liblz4-dev \
 && rm -rf /var/lib/apt/lists/*

# gRPC stubs generated in the amd64 protogen stage above.
COPY --from=protogen /app/gen gen

ENV BUNDLE_JOBS=4 BUNDLE_RETRY=3
EXPOSE 8080 8081

# ---- per-tool targets ----
# Tracer version is read at runtime from Gem.loaded_specs (see app/info.rb),
# so no /agent/version.txt is written.

FROM base AS otel
COPY relays/ruby/Gemfile.otel Gemfile
RUN bundle install
COPY relays/ruby/app app
ENV TOOL=otel
CMD ["ruby", "app/boot_otel.rb"]

FROM base AS datadog
COPY relays/ruby/Gemfile.datadog Gemfile
RUN bundle install
COPY relays/ruby/app app
ENV TOOL=datadog
CMD ["ruby", "app/boot_datadog.rb"]

FROM base AS elastic
COPY relays/ruby/Gemfile.elastic Gemfile
RUN bundle install
COPY relays/ruby/app app
ENV TOOL=elastic
CMD ["ruby", "app/boot_elastic.rb"]
