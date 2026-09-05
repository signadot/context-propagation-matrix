<?php

// Long-running RabbitMQ consumer: reads {test_id, forward_via} JSON from
// RELAY_RABBIT_QUEUE and forwards to the sink. Reconnects forever — an
// unreachable broker must never crash the container. No headers, no tracing
// APIs.

declare(strict_types=1);

require __DIR__ . '/../vendor/autoload.php';

use PhpAmqpLib\Message\AMQPMessage;
use Relay\Config;
use Relay\Forwarder;
use Relay\Rabbit;

while (true) {
    try {
        [$conn, $ch] = Rabbit::connect();
        // Declare defensively on startup (see DESIGN.md); properties must
        // match the driver's declare: durable=false, exclusive=false,
        // auto_delete=false.
        Rabbit::declareQueue($ch, Config::relayRabbitQueue());
        fwrite(STDOUT, sprintf("rabbitmq consumer: queue %s\n", Config::relayRabbitQueue()));

        $ch->basic_consume(
            Config::relayRabbitQueue(),
            '',
            false, // no_local
            false, // no_ack: ack manually after forwarding
            false, // exclusive
            false, // nowait
            function (AMQPMessage $message): void {
                try {
                    $req = json_decode($message->getBody(), true);
                    Forwarder::forward((string) $req['test_id'], (string) $req['forward_via']);
                    $message->ack();
                } catch (Throwable $e) {
                    fwrite(STDERR, 'rabbitmq relay error: ' . $e->getMessage() . "\n");
                    $message->nack(); // no requeue: a failed forward is a finding
                }
            },
        );

        while ($ch->is_consuming()) {
            $ch->wait();
        }
    } catch (Throwable $e) {
        fwrite(STDERR, 'rabbitmq consumer error (will retry): ' . $e->getMessage() . "\n");
        sleep(3);
    }
}
