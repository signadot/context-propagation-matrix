#!/bin/sh
# Starts the two queue consumers (long-running PHP CLI) and Apache (HTTP
# :8080). There is no gRPC server: PHP cannot serve gRPC natively — see
# README.md. The shell loops are a backstop; the workers also retry
# internally and are expected never to exit.
set -u

# Datadog's documented configuration for long-running CLI processes: trace
# each consumed message as its own trace and flush per message. Set per
# worker process only (a container-wide setting would disable the root span
# for Apache web requests too). Ignored by the otel/elastic images.
worker_env="DD_TRACE_CLI_ENABLED=1 DD_TRACE_AUTO_FLUSH_ENABLED=1 DD_TRACE_GENERATE_ROOT_SPAN=0"

(while true; do
    env $worker_env php /app/workers/kafka_consumer.php
    echo "kafka consumer exited ($?); restarting in 3s" >&2
    sleep 3
done) &

(while true; do
    env $worker_env php /app/workers/rabbit_consumer.php
    echo "rabbitmq consumer exited ($?); restarting in 3s" >&2
    sleep 3
done) &

exec apache2-foreground
