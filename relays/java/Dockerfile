# syntax=docker/dockerfile:1
# Build context is the repo root (so proto/ is reachable).

FROM maven:3.9-eclipse-temurin-17 AS build
WORKDIR /app
COPY relays/java/pom.xml .
COPY relays/java/src ./src
COPY proto/relay.proto ./src/main/proto/relay.proto
RUN --mount=type=cache,target=/root/.m2 mvn -q -DskipTests package \
 && cp target/relay-*.jar /relay.jar

# ---- agent fetch stages (JDK image so `jar` is available for manifest reads) ----

FROM maven:3.9-eclipse-temurin-17 AS fetch-otel
RUN mkdir -p /agent /tmp/mf \
 && (command -v curl >/dev/null 2>&1 || (apt-get update -qq && apt-get install -y -qq curl)) \
 && curl -fsSL -o /agent/agent.jar \
    "https://github.com/open-telemetry/opentelemetry-java-instrumentation/releases/latest/download/opentelemetry-javaagent.jar" \
 && cd /tmp/mf && jar xf /agent/agent.jar META-INF/MANIFEST.MF \
 && (grep -i '^Implementation-Version' META-INF/MANIFEST.MF | head -1 | cut -d: -f2- | tr -d ' \r' > /agent/version.txt) \
 && cat /agent/version.txt

FROM maven:3.9-eclipse-temurin-17 AS fetch-datadog
RUN mkdir -p /agent /tmp/mf \
 && (command -v curl >/dev/null 2>&1 || (apt-get update -qq && apt-get install -y -qq curl)) \
 && curl -fsSL -o /agent/agent.jar "https://dtdg.co/latest-java-tracer" \
 && cd /tmp/mf && jar xf /agent/agent.jar META-INF/MANIFEST.MF \
 && (grep -i '^Implementation-Version' META-INF/MANIFEST.MF | head -1 | cut -d: -f2- | tr -d ' \r' > /agent/version.txt) \
 && cat /agent/version.txt

FROM maven:3.9-eclipse-temurin-17 AS fetch-elastic
RUN mkdir -p /agent \
 && (command -v curl >/dev/null 2>&1 || (apt-get update -qq && apt-get install -y -qq curl)) \
 && VER=$(curl -fsSL "https://repo1.maven.org/maven2/co/elastic/apm/elastic-apm-agent/maven-metadata.xml" \
      | sed -n 's:.*<release>\(.*\)</release>.*:\1:p') \
 && curl -fsSL -o /agent/agent.jar \
    "https://repo1.maven.org/maven2/co/elastic/apm/elastic-apm-agent/${VER}/elastic-apm-agent-${VER}.jar" \
 && echo "${VER}" > /agent/version.txt \
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

FROM base AS elastic
COPY --from=fetch-elastic /agent /agent
ENV JAVA_TOOL_OPTIONS="-javaagent:/agent/agent.jar"
