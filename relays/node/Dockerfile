# syntax=docker/dockerfile:1
# Build context is the repo root (so proto/ is reachable).

# The application's own dependencies (and everything they pull in) are locked:
# package.json pins the direct ones exactly and package-lock.json pins every
# transitive one, so two builds of the same commit contain the same code.
# `npm ci` installs exactly the lock and fails if it and package.json disagree.
# Dependabot updates package.json and package-lock.json together. Regenerate
# the lock with the same npm that reads it (the image below):
#   docker run --rm -v "$PWD/relays/node:/w" -w /w node:26-slim \
#     npm install --package-lock-only --omit=dev
FROM node:26-slim AS app
WORKDIR /app
COPY relays/node/package.json relays/node/package-lock.json ./
RUN npm ci --omit=dev
COPY relays/node/app.js ./
COPY proto/relay.proto ./relay.proto
EXPOSE 8080 8081
ENTRYPOINT ["node", "app.js"]

# Each tool target installs its agent at the exact version pinned in
# relays/node/tools/<tool>/package.json (a standalone manifest that
# bump-agents.yml moves). --no-save keeps the app's own package.json and
# package-lock.json untouched. The tracer and the packages it pulls in are NOT
# locked: they resolve at build time, because the tracer version is the
# variable being measured. The application's locked packages above must not
# move when the tracer is installed on top (a tracer may add packages of its
# own, or nest its own copy of a shared one).

FROM app AS otel
COPY relays/node/tools/otel/package.json ./tool.package.json
RUN npm install --no-save $(node -p "Object.entries(require('./tool.package.json').dependencies).map(e=>e.join('@')).join(' ')")
ENV NODE_OPTIONS="--require @opentelemetry/auto-instrumentations-node/register"

FROM app AS datadog
COPY relays/node/tools/datadog/package.json ./tool.package.json
RUN npm install --no-save $(node -p "Object.entries(require('./tool.package.json').dependencies).map(e=>e.join('@')).join(' ')")
ENV NODE_OPTIONS="--require dd-trace/init"

FROM app AS newrelic
COPY relays/node/tools/newrelic/package.json ./tool.package.json
RUN npm install --no-save $(node -p "Object.entries(require('./tool.package.json').dependencies).map(e=>e.join('@')).join(' ')")
ENV NODE_OPTIONS="--require newrelic"

FROM app AS elastic
COPY relays/node/tools/elastic/package.json ./tool.package.json
RUN npm install --no-save $(node -p "Object.entries(require('./tool.package.json').dependencies).map(e=>e.join('@')).join(' ')")
ENV NODE_OPTIONS="--require elastic-apm-node/start"
