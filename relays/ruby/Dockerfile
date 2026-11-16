# syntax=docker/dockerfile:1
# Build context is the repo root (so proto/ is reachable).
# Targets: none | otel | datadog | newrelic | elastic (same app, different
# Gemfile + boot file). Gem versions are pinned exactly in
# relays/ruby/gemfiles/<tool>/Gemfile (one directory per tool so Dependabot
# can watch each).
#
# Every gem is locked: the Gemfile pins the direct ones exactly and the
# Gemfile.lock beside it pins every transitive one, so two builds of the same
# commit contain the same code. BUNDLE_FROZEN makes `bundle install` install
# exactly the lock and fail if it does not satisfy the Gemfile. Unlike the
# Node and PHP relays, where the tracer is installed on top of a locked
# application and resolves at build time, the tracer and what it pulls in are
# in the lock here: the tracer is a line of the same Gemfile, and Bundler
# resolves a Gemfile as a whole. Its version is still the variable being
# measured, so whatever rewrites that line moves the lock in the same change:
# `cpm pins` (bump-agents.yml, backtest.yml) runs `bundle lock --update
# <tracer gem>`, and Dependabot updates a Gemfile and its lock together.
# The locks cover the two platforms the image is built for, aarch64-linux and
# x86_64-linux, with a gem's precompiled build for each where it ships one.
# After editing a Gemfile by hand, move its lock with the Ruby and Bundler
# that read it (the image below; README.md has the command for a new Gemfile):
#   docker run --rm -v "$PWD/relays/ruby/gemfiles/<tool>:/w" -w /w ruby:3.3 \
#     bundle lock

# grpc-tools ships no linux/arm64 protoc binaries, so stub generation runs in
# an explicit amd64 stage (emulated on Apple Silicon) and the generated .rb
# files are copied into the runtime image.
FROM --platform=linux/amd64 ruby:4.0 AS protogen
WORKDIR /app
COPY proto/relay.proto proto/relay.proto
RUN gem install grpc-tools --no-document \
 && mkdir -p gen \
 && grpc_tools_ruby_protoc -I proto --ruby_out=gen --grpc_out=gen proto/relay.proto

FROM ruby:4.0 AS base
WORKDIR /app

# Build deps for gems with native extensions. grpc and rdkafka do not use
# them: the locks hold their precompiled builds for aarch64-linux and
# x86_64-linux.
RUN apt-get update -qq \
 && apt-get install -y -qq --no-install-recommends \
      build-essential libssl-dev libsasl2-dev libzstd-dev liblz4-dev \
 && rm -rf /var/lib/apt/lists/*

# gRPC stubs generated in the amd64 protogen stage above.
COPY --from=protogen /app/gen gen

ENV BUNDLE_JOBS=4 BUNDLE_RETRY=3 BUNDLE_FROZEN=true
EXPOSE 8080 8081

# ---- per-tool targets ----
# Tracer version is read at runtime from Gem.loaded_specs (see app/info.rb),
# so no /agent/version.txt is written.

FROM base AS none
COPY relays/ruby/gemfiles/none/Gemfile relays/ruby/gemfiles/none/Gemfile.lock ./
RUN bundle install
COPY relays/ruby/app app
ENV TOOL=none
CMD ["ruby", "app/boot_none.rb"]

FROM base AS otel
COPY relays/ruby/gemfiles/otel/Gemfile relays/ruby/gemfiles/otel/Gemfile.lock ./
RUN bundle install
COPY relays/ruby/app app
ENV TOOL=otel
CMD ["ruby", "app/boot_otel.rb"]

FROM base AS datadog
COPY relays/ruby/gemfiles/datadog/Gemfile relays/ruby/gemfiles/datadog/Gemfile.lock ./
RUN bundle install
COPY relays/ruby/app app
ENV TOOL=datadog
CMD ["ruby", "app/boot_datadog.rb"]

FROM base AS newrelic
COPY relays/ruby/gemfiles/newrelic/Gemfile relays/ruby/gemfiles/newrelic/Gemfile.lock ./
RUN bundle install
COPY relays/ruby/app app
ENV TOOL=newrelic
CMD ["ruby", "app/boot_newrelic.rb"]

FROM base AS elastic
COPY relays/ruby/gemfiles/elastic/Gemfile relays/ruby/gemfiles/elastic/Gemfile.lock ./
RUN bundle install
COPY relays/ruby/app app
ENV TOOL=elastic
CMD ["ruby", "app/boot_elastic.rb"]
