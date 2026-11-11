# syntax=docker/dockerfile:1
# Build context is the repo root (so proto/ is reachable).

# The application's own dependencies (and everything they pull in) are locked:
# requirements.in pins the direct ones exactly and requirements.txt, compiled
# from it, pins every transitive one, so two builds of the same commit contain
# the same code. Dependabot's pip-compile support updates the two files
# together. Regenerate the compiled file with the same Python that installs it
# (the image below); pip-compile keeps the pins already in requirements.txt,
# so this only adds or drops what the .in file changed:
#   docker run --rm -v "$PWD/relays/python:/w" -w /w python:3.12-slim sh -c \
#     'pip install pip-tools && pip-compile --allow-unsafe --output-file=requirements.txt requirements.in'
# --allow-unsafe: grpcio-tools needs setuptools, which this image does not ship.
FROM python:3.12-slim AS app
WORKDIR /app
COPY relays/python/requirements.txt .
# --no-deps + pip check: install exactly what the compiled file lists, and
# fail the build if that is not everything the application needs, instead of
# letting pip resolve the missing package at whatever version is current.
RUN pip install --no-cache-dir --no-deps -r requirements.txt && pip check
COPY proto/relay.proto proto/relay.proto
RUN python -m grpc_tools.protoc -Iproto --python_out=. --grpc_python_out=. proto/relay.proto
COPY relays/python/app.py .
EXPOSE 8080 8081

# Baseline: no instrumentation at all.
FROM app AS none
ENTRYPOINT ["python", "app.py"]

# Tool targets install their agent at the exact version pinned in the
# per-tool requirements file (a standalone file that bump-agents.yml moves).
# The tracer and the packages it pulls in are NOT locked: they resolve at build
# time, because the tracer version is the variable being measured. A tracer
# adds packages of its own; one that needed a different version of a locked
# package would move it, and nothing here stops that (an old tracer in a
# back-test has to stay installable).

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
