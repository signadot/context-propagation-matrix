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
      tracer_version: gem_version(TRACER_GEMS[tool]),
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

  def self.libs(tool)
    kafka =
      if tool == 'datadog'
        "karafka #{gem_version('karafka')} + waterdrop #{gem_version('waterdrop')}"
      else
        "rdkafka #{gem_version('rdkafka')}"
      end
    {
      'http-server' => "sinatra #{gem_version('sinatra')} (puma #{gem_version('puma')})",
      'http-client' => 'net/http (stdlib)',
      'grpc' => "grpc #{gem_version('grpc')}",
      'kafka' => kafka,
      'rabbitmq' => "bunny #{gem_version('bunny')}"
    }
  end

  def self.gem_version(name)
    spec = name && Gem.loaded_specs[name]
    spec ? spec.version.to_s : 'unknown'
  end
end
