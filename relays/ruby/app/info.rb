# frozen_string_literal: true

# Builds the GET /info payload the driver stamps into results.
module Info
  TRACER_GEMS = {
    'otel' => 'opentelemetry-sdk',
    'datadog' => 'datadog',
    'newrelic' => 'newrelic_rpm',
    'elastic' => 'elastic-apm'
  }.freeze

  CONFIG_KEYS = %w[
    OTEL_PROPAGATORS
    OTEL_TRACES_EXPORTER
    DD_TRACE_PROPAGATION_STYLE
    NEW_RELIC_DISTRIBUTED_TRACING_ENABLED
  ].freeze

  def self.payload
    tool = ENV.fetch('TOOL', 'unknown')
    {
      variant: ENV.fetch('VARIANT', 'unknown'),
      tool: tool,
      language: 'ruby',
      tracer_version: tool == 'none' ? 'none' : gem_version(TRACER_GEMS[tool]),
      runtime_version: "ruby #{RUBY_VERSION}",
      config: config_echo,
      libs: libs(tool)
    }
  end

  def self.config_echo
    CONFIG_KEYS.each_with_object({}) do |key, out|
      value = ENV[key]
      out[key] = value if value && !value.empty?
    end
  end

  # A tool's instrumentation gem is named in the lib string of the protocol it
  # instruments, as the Go relays name their contribs ("franz-go v1.x + kotel
  # v1.y"). The opentelemetry-instrumentation-* gems carry their own version
  # lines in gemfiles/otel/Gemfile and are bumped separately from the SDK gem
  # that is the tracer version, so one of them can change a cell on its own.
  # Only the otel image loads any; the other images' strings are the bare
  # client library.
  def self.libs(tool)
    kafka =
      if tool == 'datadog'
        "karafka #{gem_version('karafka')} + waterdrop #{gem_version('waterdrop')}"
      else
        "rdkafka #{gem_version('rdkafka')}"
      end
    {
      'http-server' => with_instrumentation(
        "sinatra #{gem_version('sinatra')} (puma #{gem_version('puma')})",
        'opentelemetry-instrumentation-sinatra',
        'opentelemetry-instrumentation-rack'
      ),
      'http-client' => with_instrumentation(
        'net/http (stdlib)',
        'opentelemetry-instrumentation-net_http'
      ),
      'grpc' => with_instrumentation(
        "grpc #{gem_version('grpc')}",
        'opentelemetry-instrumentation-grpc'
      ),
      'kafka' => with_instrumentation(kafka, 'opentelemetry-instrumentation-rdkafka'),
      'rabbitmq' => with_instrumentation(
        "bunny #{gem_version('bunny')}",
        'opentelemetry-instrumentation-bunny'
      )
    }
  end

  # lib, followed by each of the named instrumentation gems this process has
  # loaded and its version. Read from Gem.loaded_specs, like the tracer
  # version, so it is what the relay runs with.
  def self.with_instrumentation(lib, *gems)
    loaded = gems.select { |name| Gem.loaded_specs[name] }
    ([lib] + loaded.map { |name| "#{name} #{gem_version(name)}" }).join(' + ')
  end

  def self.gem_version(name)
    spec = name && Gem.loaded_specs[name]
    spec ? spec.version.to_s : 'unknown'
  end
end
