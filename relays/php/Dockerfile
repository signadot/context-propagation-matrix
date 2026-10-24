# syntax=docker/dockerfile:1
# Build context is the repo root (so proto/ is reachable).
#
# Shared "app" stage: Apache mod_php on :8080 (Slim front controller) plus
# two long-running CLI consumers (Kafka via ext-rdkafka, RabbitMQ via
# php-amqplib) started by the entrypoint. gRPC stubs are generated from
# proto/relay.proto at build time with Debian's protoc + grpc_php_plugin.
# Tool targets differ only in which instrumentation is installed.

FROM php:8.3-apache AS app

COPY --from=mlocati/php-extension-installer:2 /usr/bin/install-php-extensions /usr/local/bin/

RUN apt-get update \
 && apt-get install -y --no-install-recommends protobuf-compiler protobuf-compiler-grpc unzip \
 && rm -rf /var/lib/apt/lists/*

# rdkafka pinned to 6.0.5 — the exact version in dd-trace-php's tested
# integration matrix (and covered by OTel's ext-rdkafka instrumentation).
# sockets is required by php-amqplib.
RUN install-php-extensions grpc rdkafka-6.0.5 sockets @composer

# Apache: listen on :8080, serve the Slim front controller from /app/public.
RUN sed -ri 's/^Listen 80$/Listen 8080/' /etc/apache2/ports.conf
COPY relays/php/docker/vhost.conf /etc/apache2/sites-available/000-default.conf
COPY relays/php/docker/relay.ini /usr/local/etc/php/conf.d/zz-relay.ini

WORKDIR /app
COPY relays/php/composer.json ./
RUN composer install --no-dev --no-interaction --no-autoloader

COPY relays/php/public ./public
COPY relays/php/src ./src
COPY relays/php/workers ./workers
COPY relays/php/docker/entrypoint.sh /entrypoint.sh
COPY proto/relay.proto proto/relay.proto

RUN mkdir -p gen \
 && protoc -Iproto \
      --php_out=gen \
      --grpc_out=gen \
      --plugin=protoc-gen-grpc=/usr/bin/grpc_php_plugin \
      proto/relay.proto \
 && composer dump-autoload -o --no-interaction \
 && chmod +x /entrypoint.sh

EXPOSE 8080
ENTRYPOINT ["/entrypoint.sh"]

# --- OpenTelemetry: PECL extension + composer auto-instrumentation.
# Coverage: slim (inbound http), curl (outbound http), ext-rdkafka (kafka
# consume/produce). No packages exist for php-amqplib or ext-grpc clients —
# recorded as findings in README.md.
FROM app AS otel
# Extension + instrumentation packages pinned exactly (composer requires in
# a Dockerfile are not Dependabot-visible; bump manually per DESIGN.md).
RUN install-php-extensions opentelemetry-1.2.1
RUN composer require --no-interaction \
      open-telemetry/sdk:1.15.0 \
      open-telemetry/opentelemetry-auto-slim:1.5.0 \
      open-telemetry/opentelemetry-auto-curl:0.3.0 \
      open-telemetry/opentelemetry-auto-ext-rdkafka:0.0.5
ENV OTEL_PHP_AUTOLOAD_ENABLED=true \
    OTEL_TRACES_EXPORTER=none \
    OTEL_METRICS_EXPORTER=none \
    OTEL_LOGS_EXPORTER=none

# --- Datadog: documented datadog-setup.php installer, pinned to a release
# (each release's setup script installs that release's tracer). Tracer
# version is read at runtime via phpversion('ddtrace') for /info.
FROM app AS datadog
ARG DD_PHP_TRACER_VERSION=1.25.0
RUN curl -fLo /tmp/datadog-setup.php \
      "https://github.com/DataDog/dd-trace-php/releases/download/${DD_PHP_TRACER_VERSION}/datadog-setup.php" \
 && php /tmp/datadog-setup.php --php-bin=all \
 && rm /tmp/datadog-setup.php

# --- New Relic: official PHP agent, version-pinned tarball (multi-arch:
# agent/{x64,aarch64} + newrelic-install handle arch detection). The license
# key cannot be baked at build time, so a wrapper entrypoint writes it from
# NEW_RELIC_* env into an ini before handing off to the shared entrypoint;
# the agent auto-spawns its local daemon on first request.
# Coverage (see README.md): inbound HTTP (mod_php) and outbound curl/Guzzle
# are auto-instrumented for distributed tracing; php-amqplib has agent
# instrumentation (agent >= 11.6); ext-rdkafka and ext-grpc are unsupported.
FROM app AS newrelic
ARG NEWRELIC_PHP_AGENT_VERSION=12.12.0.41
RUN set -eux; \
    ver="${NEWRELIC_PHP_AGENT_VERSION}"; \
    curl -fsSL "https://download.newrelic.com/php_agent/archive/${ver}/newrelic-php5-${ver}-linux.tar.gz" \
      | tar -xz -C /tmp; \
    NR_INSTALL_SILENT=1 NR_INSTALL_USE_CP_NOT_LN=1 \
      "/tmp/newrelic-php5-${ver}-linux/newrelic-install" install; \
    rm -rf /tmp/newrelic-php5-* /tmp/nrinstall*; \
    php -m | grep -qi newrelic
COPY relays/php/docker/newrelic-entrypoint.sh /newrelic-entrypoint.sh
RUN chmod +x /newrelic-entrypoint.sh
ENTRYPOINT ["/newrelic-entrypoint.sh"]

# --- Elastic: APM PHP agent via the documented .deb package (per-arch).
# Must tolerate an unreachable ELASTIC_APM_SERVER_URL (it logs and drops).
FROM app AS elastic
ARG ELASTIC_APM_PHP_VERSION=1.17.0
RUN set -eux; \
    arch="$(dpkg --print-architecture)"; \
    curl -fLo /tmp/apm-agent-php.deb \
      "https://github.com/elastic/apm-agent-php/releases/download/v${ELASTIC_APM_PHP_VERSION}/apm-agent-php_${ELASTIC_APM_PHP_VERSION}_${arch}.deb"; \
    dpkg -i /tmp/apm-agent-php.deb; \
    rm /tmp/apm-agent-php.deb
