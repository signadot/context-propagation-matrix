# frozen_string_literal: true

require 'grpc'
require 'relay_services_pb'

# cpmatrix.Relay/Handle implementation.
class RelayGrpcService < Cpmatrix::Relay::Service
  def handle(request, _call)
    Forwarder.forward(request.test_id, request.forward_via)
    Cpmatrix::RelayResponse.new(status: 'forwarded')
  end
end

# Plain grpc-gem server on :8081, run in a background thread.
# No interceptors are added by application code; if a tool instruments the
# gRPC server, it does so by patching the grpc gem.
module GrpcServer
  def self.start!
    server = GRPC::RpcServer.new
    server.add_http2_port('0.0.0.0:8081', :this_port_is_insecure)
    server.handle(RelayGrpcService)
    Thread.new do
      server.run
    rescue StandardError => e
      warn "grpc server crashed: #{e.class}: #{e.message}"
    end
  end
end
