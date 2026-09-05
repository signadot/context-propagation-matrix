# frozen_string_literal: true

# Relay entrypoint (loaded by boot_{tool}.rb AFTER instrumentation is set up).
#
# Starts, in one process:
#   - gRPC server on :8081 (background thread)
#   - Kafka consumer (background thread; rdkafka or embedded karafka per tool)
#   - Sinatra on :8080 via puma (main thread, blocks)

require 'json'
require 'sinatra/base'
require 'net/http'
require 'grpc'

# Generated gRPC stubs live in /app/gen (see Dockerfile). The generated
# *_services_pb.rb requires 'relay_pb' from the load path.
$LOAD_PATH.unshift(File.expand_path('../gen', __dir__))

require_relative 'info'
require_relative 'forwarder'
require_relative 'grpc_server'

# Kafka client differs per tool (see README): datadog has no rdkafka/ruby-kafka
# propagation support, so it uses the karafka framework; otel and newrelic use
# plain rdkafka.
if ENV['TOOL'] == 'datadog'
  require_relative 'kafka_karafka'
else
  require_relative 'kafka_rdkafka'
end

require_relative 'http_app'

GrpcServer.start!
KafkaAdapter.start!

HttpApp.set :environment, :production
HttpApp.set :bind, '0.0.0.0'
HttpApp.set :port, 8080
HttpApp.set :server, :puma
HttpApp.run!
