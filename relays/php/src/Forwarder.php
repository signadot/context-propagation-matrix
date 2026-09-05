<?php

declare(strict_types=1);

namespace Relay;

use Cpmatrix\SinkClient;
use Cpmatrix\SinkRequest;
use PhpAmqpLib\Message\AMQPMessage;

/**
 * Naive relay forwarding: sends {"test_id": ...} to the sink over the
 * requested protocol. Never touches propagation headers, record headers,
 * metadata, or tracing APIs — that is exclusively the instrumentation's job.
 */
final class Forwarder
{
    private static ?\RdKafka\Producer $kafkaProducer = null;
    private static ?SinkClient $sinkClient = null;

    public static function forward(string $testId, string $via): void
    {
        match ($via) {
            'http' => self::viaHttp($testId),
            'grpc' => self::viaGrpc($testId),
            'kafka' => self::viaKafka($testId),
            'rabbitmq' => self::viaRabbit($testId),
            default => throw new \InvalidArgumentException("unknown forward_via: $via"),
        };
    }

    private static function viaHttp(string $testId): void
    {
        $ch = curl_init(Config::sinkHttp() . '/deliver');
        curl_setopt_array($ch, [
            CURLOPT_POST => true,
            CURLOPT_POSTFIELDS => json_encode(['test_id' => $testId]),
            CURLOPT_HTTPHEADER => ['Content-Type: application/json'],
            CURLOPT_RETURNTRANSFER => true,
            CURLOPT_TIMEOUT => 10,
        ]);
        $ok = curl_exec($ch);
        $err = curl_error($ch);
        curl_close($ch);
        if ($ok === false) {
            throw new \RuntimeException("http forward failed: $err");
        }
    }

    private static function viaGrpc(string $testId): void
    {
        if (self::$sinkClient === null) {
            self::$sinkClient = new SinkClient(Config::sinkGrpc(), [
                'credentials' => \Grpc\ChannelCredentials::createInsecure(),
            ]);
        }
        $req = new SinkRequest();
        $req->setTestId($testId);
        [, $status] = self::$sinkClient->Deliver($req, [], ['timeout' => 10_000_000])->wait();
        if ($status->code !== \Grpc\STATUS_OK) {
            throw new \RuntimeException('grpc forward failed: ' . $status->details);
        }
    }

    private static function viaKafka(string $testId): void
    {
        if (self::$kafkaProducer === null) {
            $conf = new \RdKafka\Conf();
            $conf->set('bootstrap.servers', Config::kafkaBrokers());
            $conf->set('log_level', (string) LOG_ERR);
            self::$kafkaProducer = new \RdKafka\Producer($conf);
        }
        $topic = self::$kafkaProducer->newTopic('sink-in');
        // producev, not produce: the headers-capable produce API, which is the
        // method the instrumentations hook to inject record headers. The
        // optional $key/$headers arguments are deliberately omitted (naive
        // call). NB: OTel PHP's ext-rdkafka hook can only inject when the
        // $headers argument is explicitly passed (even as []) — verified
        // empirically; see FINDINGS.md. Datadog injects either way.
        $topic->producev(RD_KAFKA_PARTITION_UA, 0, json_encode(['test_id' => $testId]));
        $res = self::$kafkaProducer->flush(10000);
        if ($res !== RD_KAFKA_RESP_ERR_NO_ERROR) {
            throw new \RuntimeException("kafka forward failed: flush returned $res");
        }
    }

    private static function viaRabbit(string $testId): void
    {
        [$conn, $ch] = Rabbit::connect();
        try {
            Rabbit::declareQueue($ch, Config::sinkRabbitQueue());
            $msg = new AMQPMessage(
                json_encode(['test_id' => $testId]),
                ['content_type' => 'application/json'],
            );
            // Default exchange, routing key = queue name.
            $ch->basic_publish($msg, '', Config::sinkRabbitQueue());
        } finally {
            $ch->close();
            $conn->close();
        }
    }
}
