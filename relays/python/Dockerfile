# syntax=docker/dockerfile:1
# Build context is the repo root (so proto/ is reachable).

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
RUN pip install --no-cache-dir -r requirements-otel.txt
ENTRYPOINT ["opentelemetry-instrument", "python", "app.py"]

FROM app AS datadog
COPY relays/python/requirements-datadog.txt .
RUN pip install --no-cache-dir -r requirements-datadog.txt
ENTRYPOINT ["ddtrace-run", "python", "app.py"]

FROM app AS newrelic
COPY relays/python/requirements-newrelic.txt .
RUN pip install --no-cache-dir -r requirements-newrelic.txt
ENTRYPOINT ["newrelic-admin", "run-program", "python", "app.py"]

FROM app AS elastic
# No zero-code runner: app.py conditionally applies the documented Flask
# middleware + elasticapm.instrument() when TOOL=elastic.
COPY relays/python/requirements-elastic.txt .
RUN pip install --no-cache-dir -r requirements-elastic.txt
ENTRYPOINT ["python", "app.py"]
