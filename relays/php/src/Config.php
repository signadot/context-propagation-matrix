<?php

declare(strict_types=1);

namespace Relay;

/** Environment-driven configuration, mirroring the other relays. */
final class Config
{
    public static function variant(): string
    {
        return getenv('VARIANT') ?: 'unknown';
    }

    public static function tool(): string
    {
        return getenv('TOOL') ?: 'unknown';
    }

    public static function relayTopic(): string
    {
        return getenv('RELAY_TOPIC') ?: 'relay-in-' . self::variant();
    }

    public static function kafkaBrokers(): string
    {
        return getenv('KAFKA_BROKERS') ?: 'kafka:9092';
    }

    public static function sinkHttp(): string
    {
        return getenv('SINK_HTTP') ?: 'http://sink:9080';
    }

    public static function sinkGrpc(): string
    {
        return getenv('SINK_GRPC') ?: 'sink:9081';
    }

    public static function rabbitUrl(): string
    {
        return getenv('RABBIT_URL') ?: 'amqp://guest:guest@rabbitmq:5672/';
    }

    public static function relayRabbitQueue(): string
    {
        return getenv('RELAY_RABBIT_QUEUE') ?: 'relay-rmq-' . self::variant();
    }

    public static function sinkRabbitQueue(): string
    {
        return getenv('SINK_RABBIT_QUEUE') ?: 'sink-rmq';
    }
}
