<?php

// Long-running Kafka consumer: reads {test_id, forward_via} JSON from
// RELAY_TOPIC and forwards to the sink. Retries forever — an unreachable
// broker must never crash the container. No headers, no tracing APIs.

declare(strict_types=1);

require __DIR__ . '/../vendor/autoload.php';

use Relay\Config;
use Relay\Forwarder;

$conf = new RdKafka\Conf();
$conf->set('bootstrap.servers', Config::kafkaBrokers());
$conf->set('group.id', 'relay-' . Config::variant());
$conf->set('auto.offset.reset', 'earliest');
// Explicit per-message commits: idiomatic at-least-once consumption (and the
// point where consume-span-aware instrumentation closes its spans).
$conf->set('enable.auto.commit', 'false');
$conf->set('log_level', (string) LOG_ERR);

$consumer = new RdKafka\KafkaConsumer($conf);
$consumer->subscribe([Config::relayTopic()]);
fwrite(STDOUT, sprintf(
    "kafka consumer: topic %s group relay-%s brokers %s\n",
    Config::relayTopic(),
    Config::variant(),
    Config::kafkaBrokers(),
));

while (true) {
    try {
        $message = $consumer->consume(1000);
        if ($message === null || $message->err !== RD_KAFKA_RESP_ERR_NO_ERROR) {
            continue; // timeout / broker not up yet / partition EOF — keep polling
        }
        $req = json_decode((string) $message->payload, true);
        Forwarder::forward((string) $req['test_id'], (string) $req['forward_via']);
        $consumer->commit($message);
    } catch (Throwable $e) {
        fwrite(STDERR, 'kafka relay error: ' . $e->getMessage() . "\n");
        sleep(1);
    }
}
