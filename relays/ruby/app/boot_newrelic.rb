# frozen_string_literal: true

# Boot file for the newrelic target: the agent starts on require and patches
# whatever supported libraries are already loaded, so load them first and
# newrelic_rpm last.
#
# The app must boot without NEW_RELIC_LICENSE_KEY: the agent fails its
# connect handshake and no-ops without crashing the process (the driver
# records the variant as not_run in that case).
ENV['NEW_RELIC_APP_NAME'] ||= ENV.fetch('VARIANT', 'relay-newrelic-ruby')
ENV['NEW_RELIC_DISTRIBUTED_TRACING_ENABLED'] ||= 'true'
ENV['NEW_RELIC_LOG'] ||= 'stdout'

require 'sinatra/base'
require 'net/http'
require 'grpc'
require 'rdkafka'
# bunny is auto-instrumented by newrelic_rpm (since agent 4.3.0): DT headers
# are inserted on Bunny::Exchange#publish and push-style consumers are
# wrapped in a message transaction via Bunny::Consumer#call.
require 'bunny'

require 'newrelic_rpm'

require_relative 'main'
