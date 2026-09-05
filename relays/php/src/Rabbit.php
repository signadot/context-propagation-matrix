<?php

declare(strict_types=1);

namespace Relay;

use PhpAmqpLib\Channel\AMQPChannel;
use PhpAmqpLib\Connection\AMQPStreamConnection;

/** Plain php-amqplib connection helpers. No headers, no tracing APIs. */
final class Rabbit
{
    /** @return array{AMQPStreamConnection, AMQPChannel} */
    public static function connect(): array
    {
        $u = parse_url(Config::rabbitUrl());
        if ($u === false) {
            throw new \RuntimeException('invalid RABBIT_URL');
        }
        $path = $u['path'] ?? '';
        $vhost = ($path !== '' && $path !== '/') ? rawurldecode(ltrim($path, '/')) : '/';
        $conn = new AMQPStreamConnection(
            $u['host'] ?? 'rabbitmq',
            $u['port'] ?? 5672,
            rawurldecode($u['user'] ?? 'guest'),
            rawurldecode($u['pass'] ?? 'guest'),
            $vhost,
        );
        return [$conn, $conn->channel()];
    }

    /** Queue properties fixed by the harness contract: durable=false, exclusive=false, auto_delete=false. */
    public static function declareQueue(AMQPChannel $ch, string $queue): void
    {
        $ch->queue_declare($queue, false, false, false, false);
    }
}
