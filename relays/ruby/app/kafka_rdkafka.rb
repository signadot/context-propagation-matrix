# frozen_string_literal: true

require 'json'
require 'rdkafka'

# Kafka adapter for the otel and newrelic targets, using the rdkafka gem.
#
# Both tools patch Rdkafka::Producer#produce (header injection) and
# Rdkafka::Consumer#each (header extraction / consume transaction), so the
# code below stays header- and tracing-API-free. Record headers on inbound
# messages are simply ignored (we only parse the JSON payload).
module KafkaAdapter
  BROKERS = ENV.fetch('KAFKA_BROKERS', 'kafka:9092')
  VARIANT = ENV.fetch('VARIANT') { "#{ENV.fetch('TOOL', 'unknown')}-ruby" }
  TOPIC = ENV.fetch('RELAY_TOPIC') { "relay-in-#{VARIANT}" }
  GROUP = "relay-#{VARIANT}"

  def self.produce(topic, payload)
    producer.produce(topic: topic, payload: payload).wait
  end

  def self.producer
    @producer ||= Rdkafka::Config.new({ 'bootstrap.servers': BROKERS }).producer
  end

  def self.start!
    Thread.new do
      loop do
        consume_loop
      rescue StandardError => e
        warn "kafka consumer error (restarting): #{e.class}: #{e.message}"
        sleep 2
      end
    end
  end

  def self.consume_loop
    consumer = Rdkafka::Config.new({
      'bootstrap.servers': BROKERS,
      'group.id': GROUP,
      'auto.offset.reset': 'earliest',
      'allow.auto.create.topics': true
    }).consumer
    consumer.subscribe(TOPIC)
    consumer.each do |message|
      handle(message)
    end
  end

  def self.handle(message)
    req = JSON.parse(message.payload)
    Forwarder.forward(req['test_id'], req['forward_via'])
  rescue StandardError => e
    warn "kafka message error: #{e.class}: #{e.message}"
  end
end
