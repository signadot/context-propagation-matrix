# syntax=docker/dockerfile:1
# Build context is the repo root (so proto/ is reachable).

# TRACER_VERSION (build-arg) overrides the pinned tracer/agent version for the
# target being built; empty means "use the pin". Used by backtest.yml to
# measure older releases without editing this file.
ARG TRACER_VERSION=

FROM node:22-slim AS app
WORKDIR /app
COPY relays/node/package.json ./
RUN npm install --omit=dev
COPY relays/node/app.js ./
COPY proto/relay.proto ./relay.proto
EXPOSE 8080 8081
ENTRYPOINT ["node", "app.js"]

# Each tool target installs its agent at the exact version pinned in
# relays/node/tools/<tool>/package.json (a standalone manifest so Dependabot
# can bump it). --no-save keeps the app's own package.json untouched.

FROM app AS otel
COPY relays/node/tools/otel/package.json ./tool.package.json
ARG TRACER_VERSION
RUN npm install --no-save $(node -p "Object.entries(require('./tool.package.json').dependencies).map(([n,v])=>n+'@'+(process.env.TRACER_VERSION||v)).join(' ')")
ENV NODE_OPTIONS="--require @opentelemetry/auto-instrumentations-node/register"

FROM app AS datadog
COPY relays/node/tools/datadog/package.json ./tool.package.json
ARG TRACER_VERSION
RUN npm install --no-save $(node -p "Object.entries(require('./tool.package.json').dependencies).map(([n,v])=>n+'@'+(process.env.TRACER_VERSION||v)).join(' ')")
ENV NODE_OPTIONS="--require dd-trace/init"

FROM app AS newrelic
COPY relays/node/tools/newrelic/package.json ./tool.package.json
ARG TRACER_VERSION
RUN npm install --no-save $(node -p "Object.entries(require('./tool.package.json').dependencies).map(([n,v])=>n+'@'+(process.env.TRACER_VERSION||v)).join(' ')")
ENV NODE_OPTIONS="--require newrelic"

FROM app AS elastic
COPY relays/node/tools/elastic/package.json ./tool.package.json
ARG TRACER_VERSION
RUN npm install --no-save $(node -p "Object.entries(require('./tool.package.json').dependencies).map(([n,v])=>n+'@'+(process.env.TRACER_VERSION||v)).join(' ')")
ENV NODE_OPTIONS="--require elastic-apm-node/start"
