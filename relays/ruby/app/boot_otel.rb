# frozen_string_literal: true

# Boot file for the otel target: set up OpenTelemetry BEFORE app code loads.
#
# Propagators come from OTEL_PROPAGATORS (SDK default: tracecontext,baggage).
# No exporter is needed for header injection; default to none if unset.
ENV['OTEL_TRACES_EXPORTER'] ||= 'none'
ENV['OTEL_METRICS_EXPORTER'] ||= 'none'
ENV['OTEL_LOGS_EXPORTER'] ||= 'none'

# Libraries must be loaded before SDK.configure so the instrumentations'
# presence checks pass.
require 'sinatra/base'
require 'net/http'
require 'grpc'
require 'rdkafka'
require 'bunny'

require 'opentelemetry/sdk'
require 'opentelemetry-instrumentation-rack'
require 'opentelemetry-instrumentation-sinatra'
require 'opentelemetry-instrumentation-net_http'
require 'opentelemetry-instrumentation-grpc'
require 'opentelemetry-instrumentation-rdkafka'
require 'opentelemetry-instrumentation-bunny'

OpenTelemetry::SDK.configure do |c|
  c.use 'OpenTelemetry::Instrumentation::Rack'
  c.use 'OpenTelemetry::Instrumentation::Sinatra'
  c.use 'OpenTelemetry::Instrumentation::Net::HTTP'
  # Client-side only: no OTel Ruby server instrumentation exists for the
  # plain grpc gem (see README).
  c.use 'OpenTelemetry::Instrumentation::Grpc'
  c.use 'OpenTelemetry::Instrumentation::Rdkafka'
  c.use 'OpenTelemetry::Instrumentation::Bunny'
end

require_relative 'main'
