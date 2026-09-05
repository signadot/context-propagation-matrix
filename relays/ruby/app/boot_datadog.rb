# frozen_string_literal: true

# Boot file for the datadog target: set up dd-trace BEFORE app code loads.
#
# Propagation style comes from DD_TRACE_PROPAGATION_STYLE automatically
# (library defaults apply when unset - the datadog-ruby-defaults variant).

# Libraries must be loaded before Datadog.configure so the integrations can
# patch them.
require 'sinatra/base'
require 'net/http'
require 'grpc'
require 'karafka'
require 'waterdrop'

require 'datadog'

Datadog.configure do |c|
  c.tracing.instrument :sinatra
  c.tracing.instrument :http # Net::HTTP
  c.tracing.instrument :grpc # client + server interceptors, patched in
  # Datadog's documented Kafka propagation path for Ruby (see README):
  # karafka resumes the trace from record headers on consume, waterdrop
  # injects trace context on produce. Both options default to false and are
  # documented as DD_TRACE_KARAFKA_DISTRIBUTED_TRACING /
  # DD_TRACE_WATERDROP_DISTRIBUTED_TRACING.
  c.tracing.instrument :karafka, distributed_tracing: true
  c.tracing.instrument :waterdrop, distributed_tracing: true
end

require_relative 'main'
