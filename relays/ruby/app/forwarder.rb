# frozen_string_literal: true

require 'json'
require 'net/http'
require 'uri'

# Forwards a message to the sink over the requested protocol.
#
# Deliberately naive: never touches headers, metadata or tracing APIs.
# Whatever context propagation happens is the instrumentation's doing.
module Forwarder
  SINK_HTTP = ENV.fetch('SINK_HTTP', 'http://sink:9080')
  SINK_GRPC = ENV.fetch('SINK_GRPC', 'sink:9081')

  def self.forward(test_id, via)
    case via
    when 'http'
      Net::HTTP.post(
        URI("#{SINK_HTTP}/deliver"),
        JSON.generate({ test_id: test_id }),
        'Content-Type' => 'application/json'
      )
    when 'grpc'
      sink_stub.deliver(Cpmatrix::SinkRequest.new(test_id: test_id))
    when 'kafka'
      KafkaAdapter.produce('sink-in', JSON.generate({ test_id: test_id }))
    when 'rabbitmq'
      RabbitAdapter.produce(RabbitAdapter::SINK_QUEUE, JSON.generate({ test_id: test_id }))
    else
      raise ArgumentError, "unknown forward_via: #{via.inspect}"
    end
  end

  def self.sink_stub
    @sink_stub ||= Cpmatrix::Sink::Stub.new(SINK_GRPC, :this_channel_is_insecure)
  end
end
