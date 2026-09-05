# syntax=docker/dockerfile:1
# Build context is the repo root (so proto/ is reachable).

FROM node:22-slim AS app
WORKDIR /app
COPY relays/node/package.json ./
RUN npm install --omit=dev
COPY relays/node/app.js ./
COPY proto/relay.proto ./relay.proto
EXPOSE 8080 8081
ENTRYPOINT ["node", "app.js"]

FROM app AS otel
RUN npm install @opentelemetry/auto-instrumentations-node
ENV NODE_OPTIONS="--require @opentelemetry/auto-instrumentations-node/register"

FROM app AS datadog
RUN npm install dd-trace
ENV NODE_OPTIONS="--require dd-trace/init"

FROM app AS elastic
RUN npm install elastic-apm-node
ENV NODE_OPTIONS="--require elastic-apm-node/start"
