# frozen_string_literal: true

require 'json'
require 'karafka'

# Kafka adapter for the datadog target, using the karafka framework
# (consumer) and its bundled WaterDrop producer (Karafka.producer).
#
# Rationale (see README): dd-trace-rb has no rdkafka integration, and its
# ruby-kafka (:kafka) integration only creates spans - it cannot propagate
# context. Datadog's documented Kafka propagation path for Ruby is the
# karafka/waterdrop integrations with `distributed_tracing: true`, which
# extract trace context from record headers on consume and inject it on
# produce. Those options are enabled in boot_datadog.rb; nothing in this
# file touches headers or tracing APIs.
module KafkaAdapter
  BROKERS = ENV.fetch('KAFKA_BROKERS', 'kafka:9092')
  VARIANT = ENV.fetch('VARIANT') { "#{ENV.fetch('TOOL', 'unknown')}-ruby" }
  TOPIC = ENV.fetch('RELAY_TOPIC') { "relay-in-#{VARIANT}" }
  GROUP = "relay-#{VARIANT}"

  def self.produce(topic, payload)
    Karafka.producer.produce_sync(topic: topic, payload: payload)
  end

  def self.start!
    # Runs the karafka server in background threads of this process.
    Karafka::Embedded.start
  end
end

class KarafkaApp < Karafka::App
  setup do |config|
    config.kafka = {
      'bootstrap.servers': KafkaAdapter::BROKERS,
      'allow.auto.create.topics': true
    }
    config.client_id = KafkaAdapter::VARIANT
    config.initial_offset = 'earliest'
  end
end

# Naive consumer: parse the JSON value, forward. Record headers on the
# message are ignored by application code.
class RelayKafkaConsumer < Karafka::BaseConsumer
  def consume
    messages.each do |message|
      req = JSON.parse(message.raw_payload)
      Forwarder.forward(req['test_id'], req['forward_via'])
    rescue StandardError => e
      warn "kafka message error: #{e.class}: #{e.message}"
    end
  end
end

KarafkaApp.routes.draw do
  # Consumer group names are used verbatim as the Kafka group.id in
  # karafka >= 2.4 (the consumer mapper concept was removed).
  consumer_group KafkaAdapter::GROUP do
    topic KafkaAdapter::TOPIC do
      consumer RelayKafkaConsumer
    end
  end
end
