# syntax=docker/dockerfile:1
# Build context is the repo root (so proto/ is reachable).

# TRACER_VERSION (build-arg) overrides the pinned tracer/agent version for the
# target being built; empty means "use the pin". Used by backtest.yml to
# measure older releases without editing this file.
ARG TRACER_VERSION=

FROM python:3.12-slim AS app
WORKDIR /app
COPY relays/python/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY proto/relay.proto proto/relay.proto
RUN python -m grpc_tools.protoc -Iproto --python_out=. --grpc_python_out=. proto/relay.proto
COPY relays/python/app.py .
EXPOSE 8080 8081

# Baseline: no instrumentation at all.
FROM app AS none
ENTRYPOINT ["python", "app.py"]

# Tool targets install their agent at the exact version pinned in the
# per-tool requirements file (kept separate so Dependabot can bump each one).

FROM app AS otel
COPY relays/python/requirements-otel.txt .
ARG TRACER_VERSION
RUN if [ -n "$TRACER_VERSION" ]; then sed -i -E "s/==.*/==$TRACER_VERSION/" requirements-otel.txt; fi \
 && pip install --no-cache-dir -r requirements-otel.txt
ENTRYPOINT ["opentelemetry-instrument", "python", "app.py"]

FROM app AS datadog
COPY relays/python/requirements-datadog.txt .
ARG TRACER_VERSION
RUN if [ -n "$TRACER_VERSION" ]; then sed -i -E "s/==.*/==$TRACER_VERSION/" requirements-datadog.txt; fi \
 && pip install --no-cache-dir -r requirements-datadog.txt
ENTRYPOINT ["ddtrace-run", "python", "app.py"]

FROM app AS newrelic
COPY relays/python/requirements-newrelic.txt .
ARG TRACER_VERSION
RUN if [ -n "$TRACER_VERSION" ]; then sed -i -E "s/==.*/==$TRACER_VERSION/" requirements-newrelic.txt; fi \
 && pip install --no-cache-dir -r requirements-newrelic.txt
ENTRYPOINT ["newrelic-admin", "run-program", "python", "app.py"]

FROM app AS elastic
# No zero-code runner: app.py conditionally applies the documented Flask
# middleware + elasticapm.instrument() when TOOL=elastic.
COPY relays/python/requirements-elastic.txt .
ARG TRACER_VERSION
RUN if [ -n "$TRACER_VERSION" ]; then sed -i -E "s/==.*/==$TRACER_VERSION/" requirements-elastic.txt; fi \
 && pip install --no-cache-dir -r requirements-elastic.txt
ENTRYPOINT ["python", "app.py"]
