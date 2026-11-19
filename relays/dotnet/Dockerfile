# syntax=docker/dockerfile:1
# Build context is the repo root (so proto/ is reachable).
#
# Agent versions are pinned for repeatability. Dependabot does not cover
# these curl-fetched installers/tarballs, so bumps are manual (see DESIGN.md
# "Version pinning"): update the ARG, rebuild, re-run the variant.
ARG OTEL_DOTNET_AUTO_VERSION=1.19.1
ARG DD_DOTNET_TRACER_VERSION=3.58.0
ARG NEWRELIC_DOTNET_AGENT_VERSION=10.58.0
ARG ELASTIC_APM_DOTNET_VERSION=1.36.0

# The SDK and runtime image majors must match <TargetFramework> in Relay.csproj
# (checked by scripts/check-toolchains.sh): the aspnet image only ships its own
# major's shared framework, so a net8.0 build does not start on aspnet:10.0.
# Bump the three together, after checking each agent's support matrix for the
# new runtime. .NET 8 LTS support ends 2026-11-10.
FROM mcr.microsoft.com/dotnet/sdk:10.0 AS build
WORKDIR /src/relays/dotnet
# Mirror the repo layout so Relay.csproj's ../../proto/relay.proto resolves.
COPY proto/relay.proto /src/proto/relay.proto
COPY relays/dotnet/Relay.csproj relays/dotnet/Program.cs ./
RUN --mount=type=cache,target=/root/.nuget/packages \
    dotnet publish -c Release -o /out Relay.csproj

# ---- base runtime (uninstrumented) ----

FROM mcr.microsoft.com/dotnet/aspnet:10.0 AS base
WORKDIR /app
COPY --from=build /out /app
ENV TRACER_VERSION_FILE=/agent/version.txt
EXPOSE 8080 8081
ENTRYPOINT ["dotnet", "/app/Relay.dll"]

# ---- otel: OpenTelemetry .NET automatic (zero-code) instrumentation ----
# https://opentelemetry.io/docs/zero-code/dotnet/
# Kafka (Confluent.Kafka) is a default bytecode instrumentation; HTTP, ASP.NET
# Core and Grpc.Net.Client are default source instrumentations.

FROM base AS otel
ARG OTEL_DOTNET_AUTO_VERSION
ENV OTEL_DOTNET_AUTO_HOME=/otel-dotnet-auto
RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends curl unzip \
 && mkdir -p /agent \
 && OTEL_VERSION="${OTEL_DOTNET_AUTO_VERSION}" \
 && curl -fsSL -o /tmp/otel-dotnet-auto-install.sh \
      "https://github.com/open-telemetry/opentelemetry-dotnet-instrumentation/releases/download/v${OTEL_VERSION}/otel-dotnet-auto-install.sh" \
 # 1.19+ installers verify the release asset with `gh release verify-asset`
 # and `gh attestation verify`, which need the GitHub CLI and an API token
 # inside the build; neither is available here. The asset still comes over
 # TLS from the project's GitHub release. Older installers ignore the flag.
 && SKIP_RELEASE_VERIFICATION=true sh /tmp/otel-dotnet-auto-install.sh \
 && echo "${OTEL_VERSION}" > /agent/version.txt \
 && cat /agent/version.txt \
 && rm -f /tmp/otel-dotnet-auto-install.sh \
 && apt-get purge -y -qq unzip curl && apt-get autoremove -y -qq && rm -rf /var/lib/apt/lists/*
# instrument.sh exports the required CLR profiler env vars at container start
# (CORECLR_ENABLE_PROFILING, CORECLR_PROFILER, CORECLR_PROFILER_PATH,
# DOTNET_ADDITIONAL_DEPS, DOTNET_SHARED_STORE, DOTNET_STARTUP_HOOKS) with the
# arch-appropriate profiler path, so the image works on amd64 and arm64.
ENTRYPOINT ["/bin/sh", "-c", ". \"$OTEL_DOTNET_AUTO_HOME/instrument.sh\" && exec dotnet /app/Relay.dll"]

# ---- datadog: dd-trace-dotnet (datadog-dotnet-apm) ----
# https://docs.datadoghq.com/tracing/trace_collection/dd_libraries/dotnet-core/

FROM base AS datadog
ARG TARGETARCH
ARG DD_DOTNET_TRACER_VERSION
RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends curl \
 && mkdir -p /agent /opt/datadog \
 && DD_VERSION="${DD_DOTNET_TRACER_VERSION}" \
 && case "${TARGETARCH:-amd64}" in \
      arm64) DD_ASSET="datadog-dotnet-apm-${DD_VERSION}.arm64.tar.gz" ;; \
      *)     DD_ASSET="datadog-dotnet-apm-${DD_VERSION}.tar.gz" ;; \
    esac \
 && curl -fsSL -o /tmp/datadog.tar.gz \
      "https://github.com/DataDog/dd-trace-dotnet/releases/download/v${DD_VERSION}/${DD_ASSET}" \
 && tar -C /opt/datadog -xzf /tmp/datadog.tar.gz \
 && bash /opt/datadog/createLogPath.sh \
 && echo "${DD_VERSION}" > /agent/version.txt \
 && cat /agent/version.txt \
 && rm -f /tmp/datadog.tar.gz \
 && apt-get purge -y -qq curl && apt-get autoremove -y -qq && rm -rf /var/lib/apt/lists/*
# Both arch tarballs ship the loader at the /opt/datadog root, so this path is
# arch-safe (it dispatches to linux-x64/ or linux-arm64/ underneath).
ENV CORECLR_ENABLE_PROFILING=1 \
    CORECLR_PROFILER="{846F5F1C-F9AE-4B07-969E-05C26BC060D8}" \
    CORECLR_PROFILER_PATH=/opt/datadog/Datadog.Trace.ClrProfiler.Native.so \
    DD_DOTNET_TRACER_HOME=/opt/datadog

# ---- newrelic: New Relic .NET agent ----
# https://docs.newrelic.com/install/dotnet/ (Linux tarball install)

FROM base AS newrelic
ARG TARGETARCH
ARG NEWRELIC_DOTNET_AGENT_VERSION
RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends curl \
 && mkdir -p /agent \
 && case "${TARGETARCH:-amd64}" in \
      arm64) NR_ARCH=arm64 ;; \
      *)     NR_ARCH=amd64 ;; \
    esac \
 && NR_VERSION="${NEWRELIC_DOTNET_AGENT_VERSION}" \
 && curl -fsSL -o /tmp/newrelic.tar.gz \
      "https://download.newrelic.com/dot_net_agent/previous_releases/${NR_VERSION}/newrelic-dotnet-agent_${NR_VERSION}_${NR_ARCH}.tar.gz" \
 && tar -xzf /tmp/newrelic.tar.gz -C /usr/local \
 && echo "${NR_VERSION}" > /agent/version.txt \
 && cat /agent/version.txt \
 && rm -f /tmp/newrelic.tar.gz \
 && apt-get purge -y -qq curl && apt-get autoremove -y -qq && rm -rf /var/lib/apt/lists/*
# libNewRelicProfiler.so at the agent home root is the correct arch build in
# each per-arch tarball.
ENV CORECLR_ENABLE_PROFILING=1 \
    CORECLR_PROFILER="{36032161-FFC0-4B61-B559-F6C5D41BAE5A}" \
    CORECLR_NEWRELIC_HOME=/usr/local/newrelic-dotnet-agent \
    CORECLR_PROFILER_PATH=/usr/local/newrelic-dotnet-agent/libNewRelicProfiler.so

# ---- elastic: Elastic APM .NET agent (zero-code startup hook) ----
# https://www.elastic.co/docs/reference/apm/agents/dotnet/setup-dotnet-net-core
# "Zero code change setup on .NET 8+": the ElasticApmAgent zip is injected via
# DOTNET_STARTUP_HOOKS. Managed-only, so it works on amd64 and arm64 alike
# (Elastic's alternative CLR-profiler zip is linux-x64 only). Note: Elastic's
# Kafka and RabbitMQ integrations exist only in the CLR-profiler mechanism
# (IL rewriting), so with the startup hook those legs measure as uninstrumented
# — see supported technologies:
# https://www.elastic.co/docs/reference/apm/agents/dotnet/setup-auto-instrumentation
# No ELASTIC_APM_* config baked in: env only. With no reachable
# ELASTIC_APM_SERVER_URL the agent logs send failures but the app runs fine.

FROM base AS elastic
ARG ELASTIC_APM_DOTNET_VERSION
RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends curl unzip \
 && mkdir -p /agent /opt/elastic \
 && ELASTIC_VERSION="${ELASTIC_APM_DOTNET_VERSION}" \
 && curl -fsSL -o /tmp/elastic-apm-agent.zip \
      "https://github.com/elastic/apm-agent-dotnet/releases/download/v${ELASTIC_VERSION}/ElasticApmAgent_${ELASTIC_VERSION}.zip" \
 # The startup-hook DLL sits at the zip root next to per-TFM agent dirs.
 && unzip -q /tmp/elastic-apm-agent.zip -d /opt/elastic/apm-agent \
 && echo "${ELASTIC_VERSION}" > /agent/version.txt \
 && cat /agent/version.txt \
 && rm -f /tmp/elastic-apm-agent.zip \
 && apt-get purge -y -qq unzip curl && apt-get autoremove -y -qq && rm -rf /var/lib/apt/lists/*
ENV DOTNET_STARTUP_HOOKS=/opt/elastic/apm-agent/ElasticApmAgentStartupHook.dll
