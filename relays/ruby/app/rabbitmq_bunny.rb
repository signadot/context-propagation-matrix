# frozen_string_literal: true

require 'json'
require 'bunny'

# RabbitMQ adapter, using the bunny gem (same file for every tool target).
#
# Whether this leg is instrumented depends on the tool (see README): otel
# patches bunny via opentelemetry-instrumentation-bunny; datadog and elastic
# have no bunny instrumentation (plain uninstrumented bunny - a measured
# finding, not a bug). Nothing here touches
# message properties, headers or tracing APIs; inbound AMQP properties are
# simply ignored (we only parse the JSON body).
module RabbitAdapter
  # NB: no trailing slash — Bunny parses a "/" path as an *empty* vhost name
  # (NOT_ALLOWED - vhost '' not found), unlike every other AMQP client where
  # it means the default vhost. Strip one defensively.
  URL = ENV.fetch('RABBIT_URL', 'amqp://guest:guest@rabbitmq:5672').chomp('/')
  VARIANT = ENV.fetch('VARIANT') { "#{ENV.fetch('TOOL', 'unknown')}-ruby" }
  QUEUE = ENV.fetch('RELAY_RABBIT_QUEUE') { "relay-rmq-#{VARIANT}" }
  SINK_QUEUE = ENV.fetch('SINK_RABBIT_QUEUE', 'sink-rmq')

  # Queue properties must match the driver's / sink's declarations
  # (declares are idempotent when properties agree).
  QUEUE_OPTS = { durable: false, auto_delete: false, exclusive: false }.freeze

  @publish_mutex = Mutex.new

  # Publish via the default exchange (routing key == queue name). Bunny
  # channels are not thread-safe and publishes arrive from puma, gRPC and
  # consumer threads, so one lazily-created publisher channel is guarded by
  # a mutex. The target queue is declared defensively on first use.
  def self.produce(queue, payload)
    @publish_mutex.synchronize do
      declared[queue] ||= publish_channel.queue(queue, **QUEUE_OPTS)
      publish_channel.default_exchange.publish(payload, routing_key: queue)
    end
  end

  def self.start!
    Thread.new do
      loop do
        consume_loop
      rescue StandardError => e
        warn "rabbitmq consumer error (restarting): #{e.class}: #{e.message}"
        sleep 2
      end
    end
  end

  def self.consume_loop
    conn = Bunny.new(URL)
    conn.start
    queue = conn.create_channel.queue(QUEUE, **QUEUE_OPTS)
    # block: true parks this thread; deliveries run on the channel's work
    # pool, where instrumented tools wrap Bunny::Consumer#call - so the
    # forward below happens inside whatever consume context the tool set up.
    queue.subscribe(block: true) do |_delivery_info, _properties, body|
      handle(body)
    end
  end

  def self.handle(body)
    req = JSON.parse(body)
    Forwarder.forward(req['test_id'], req['forward_via'])
  rescue StandardError => e
    warn "rabbitmq message error: #{e.class}: #{e.message}"
  end

  def self.publish_channel
    @publish_channel ||= begin
      conn = Bunny.new(URL)
      conn.start
      conn.create_channel
    end
  end

  def self.declared
    @declared ||= {}
  end
end
