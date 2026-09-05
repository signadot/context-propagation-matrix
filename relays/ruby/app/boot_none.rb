# frozen_string_literal: true

# Baseline boot file: no instrumentation gem is loaded at all. The app runs
# with plain sinatra/net-http/grpc/rdkafka/bunny, proving that nothing
# propagates unless a tool is attached.
require_relative 'main'
