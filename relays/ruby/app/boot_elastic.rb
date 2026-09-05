# frozen_string_literal: true

# Boot file for the elastic target: start Elastic APM BEFORE app code runs.
#
# Configuration comes from ELASTIC_APM_* env vars. An unreachable
# ELASTIC_APM_SERVER_URL must not crash the app - that is default agent
# behavior (the transport logs errors and keeps running; header injection
# does not depend on a successful export), and ElasticAPM::Sinatra.start
# additionally rescues any startup error and logs it instead of raising.
ENV['ELASTIC_APM_SERVICE_NAME'] ||= ENV.fetch('VARIANT', 'relay-elastic-ruby')

# Libraries first, so elastic-apm's spies (net/http, ...) find them loaded.
# Requiring sinatra/base before elastic-apm also makes the gem load its
# Sinatra integration (ElasticAPM::Sinatra).
require 'sinatra/base'
require 'net/http'
require 'grpc'
require 'rdkafka'
require 'bunny'

require 'elastic-apm'

# Documented Sinatra integration: rack middleware on the app class plus
# ElasticAPM::Sinatra.start. This is instrumentation setup living in the
# boot file - HttpApp itself stays untouched.
require_relative 'http_app'
HttpApp.use ElasticAPM::Middleware
ElasticAPM::Sinatra.start(HttpApp)
at_exit { ElasticAPM.stop }

# Deliberately nothing for grpc, rdkafka or bunny: elastic-apm has no
# automatic instrumentation for any of them. Its gRPC support requires
# interceptors added by hand at stub/server construction (app code touching
# tracing APIs, which the harness contract forbids), its only Kafka support
# targets the racecar framework, and RabbitMQ is not supported at all.
# Those gaps are findings the harness measures (see README).
require_relative 'main'
