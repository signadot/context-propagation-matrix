# syntax=docker/dockerfile:1
# Build context is the repo root (so proto/ is reachable).

FROM python:3.12-slim AS app
WORKDIR /app
COPY relays/python/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt grpcio-tools
COPY proto/relay.proto proto/relay.proto
RUN python -m grpc_tools.protoc -Iproto --python_out=. --grpc_python_out=. proto/relay.proto
COPY relays/python/app.py .
EXPOSE 8080 8081

FROM app AS otel
RUN pip install --no-cache-dir \
      opentelemetry-distro \
      opentelemetry-instrumentation-flask \
      opentelemetry-instrumentation-requests \
      opentelemetry-instrumentation-grpc \
      opentelemetry-instrumentation-confluent-kafka \
      opentelemetry-instrumentation-pika
ENTRYPOINT ["opentelemetry-instrument", "python", "app.py"]

FROM app AS datadog
RUN pip install --no-cache-dir ddtrace
ENTRYPOINT ["ddtrace-run", "python", "app.py"]

FROM app AS elastic
# No zero-code runner: app.py conditionally applies the documented Flask
# middleware + elasticapm.instrument() when TOOL=elastic.
RUN pip install --no-cache-dir elastic-apm
ENTRYPOINT ["python", "app.py"]
