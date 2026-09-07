# syntax=docker/dockerfile:1
# Build context is the repo root (so proto/ is reachable).

# Agent versions are pinned for repeatability. Dependabot does not cover
# curl-fetched jars, so bumps are manual (or via the refresh checklist in
# DESIGN.md "Version pinning"): update the ARG, rebuild, re-run the variant.
ARG OTEL_JAVA_AGENT_VERSION=2.31.1
ARG DD_JAVA_AGENT_VERSION=1.65.1
ARG NEWRELIC_JAVA_AGENT_VERSION=9.4.0
ARG ELASTIC_APM_JAVA_AGENT_VERSION=1.56.0

FROM maven:3.9-eclipse-temurin-17 AS build
WORKDIR /app
COPY relays/java/pom.xml .
COPY relays/java/src ./src
COPY proto/relay.proto ./src/main/proto/relay.proto
RUN --mount=type=cache,target=/root/.m2 mvn -q -DskipTests package \
 && cp target/relay-*.jar /relay.jar

# ---- agent fetch stages (version-pinned URLs; version.txt from the ARG) ----

FROM maven:3.9-eclipse-temurin-17 AS fetch-otel
ARG OTEL_JAVA_AGENT_VERSION
RUN mkdir -p /agent \
 && (command -v curl >/dev/null 2>&1 || (apt-get update -qq && apt-get install -y -qq curl)) \
 && curl -fsSL -o /agent/agent.jar \
    "https://github.com/open-telemetry/opentelemetry-java-instrumentation/releases/download/v${OTEL_JAVA_AGENT_VERSION}/opentelemetry-javaagent.jar" \
 && echo "${OTEL_JAVA_AGENT_VERSION}" > /agent/version.txt \
 && cat /agent/version.txt

FROM maven:3.9-eclipse-temurin-17 AS fetch-datadog
ARG DD_JAVA_AGENT_VERSION
RUN mkdir -p /agent \
 && (command -v curl >/dev/null 2>&1 || (apt-get update -qq && apt-get install -y -qq curl)) \
 && curl -fsSL -o /agent/agent.jar \
    "https://repo1.maven.org/maven2/com/datadoghq/dd-java-agent/${DD_JAVA_AGENT_VERSION}/dd-java-agent-${DD_JAVA_AGENT_VERSION}.jar" \
 && echo "${DD_JAVA_AGENT_VERSION}" > /agent/version.txt \
 && cat /agent/version.txt

FROM maven:3.9-eclipse-temurin-17 AS fetch-newrelic
ARG NEWRELIC_JAVA_AGENT_VERSION
RUN mkdir -p /agent \
 && (command -v curl >/dev/null 2>&1 || (apt-get update -qq && apt-get install -y -qq curl)) \
 && curl -fsSL -o /agent/agent.jar \
    "https://repo1.maven.org/maven2/com/newrelic/agent/java/newrelic-agent/${NEWRELIC_JAVA_AGENT_VERSION}/newrelic-agent-${NEWRELIC_JAVA_AGENT_VERSION}.jar" \
 && echo "${NEWRELIC_JAVA_AGENT_VERSION}" > /agent/version.txt \
 && cat /agent/version.txt

FROM maven:3.9-eclipse-temurin-17 AS fetch-elastic
ARG ELASTIC_APM_JAVA_AGENT_VERSION
RUN mkdir -p /agent \
 && (command -v curl >/dev/null 2>&1 || (apt-get update -qq && apt-get install -y -qq curl)) \
 && curl -fsSL -o /agent/agent.jar \
    "https://repo1.maven.org/maven2/co/elastic/apm/elastic-apm-agent/${ELASTIC_APM_JAVA_AGENT_VERSION}/elastic-apm-agent-${ELASTIC_APM_JAVA_AGENT_VERSION}.jar" \
 && echo "${ELASTIC_APM_JAVA_AGENT_VERSION}" > /agent/version.txt \
 && cat /agent/version.txt

# ---- runtime targets ----

FROM eclipse-temurin:17-jre AS base
WORKDIR /app
COPY --from=build /relay.jar /app/relay.jar
ENV TRACER_VERSION_FILE=/agent/version.txt
EXPOSE 8080 8081
ENTRYPOINT ["java", "-jar", "/app/relay.jar"]

FROM base AS otel
COPY --from=fetch-otel /agent /agent
ENV JAVA_TOOL_OPTIONS="-javaagent:/agent/agent.jar"

FROM base AS datadog
COPY --from=fetch-datadog /agent /agent
ENV JAVA_TOOL_OPTIONS="-javaagent:/agent/agent.jar"

FROM base AS newrelic
COPY --from=fetch-newrelic /agent /agent
ENV JAVA_TOOL_OPTIONS="-javaagent:/agent/agent.jar"

FROM base AS elastic
COPY --from=fetch-elastic /agent /agent
ENV JAVA_TOOL_OPTIONS="-javaagent:/agent/agent.jar"
