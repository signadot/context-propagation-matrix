"""Naive relay: receives {test_id, forward_via} over HTTP/gRPC/Kafka/RabbitMQ
and forwards to the sink over the requested protocol. Never touches headers or
tracing APIs — context propagation is the instrumentation's job. (The only
exception is boot-time agent setup where a tool documents it, e.g. Elastic's
Flask middleware below.)"""

import json
import os
import threading
import time
from concurrent import futures
from importlib.metadata import version as pkg_version

import grpc
import pika
import requests
from confluent_kafka import Consumer, Producer
from flask import Flask, jsonify, request

import relay_pb2
import relay_pb2_grpc

VARIANT = os.getenv("VARIANT", "unknown")
TOOL = os.getenv("TOOL", "unknown")
RELAY_TOPIC = os.getenv("RELAY_TOPIC", f"relay-in-{VARIANT}")
KAFKA_BROKERS = os.getenv("KAFKA_BROKERS", "kafka:9092")
SINK_HTTP = os.getenv("SINK_HTTP", "http://sink:9080")
SINK_GRPC = os.getenv("SINK_GRPC", "sink:9081")
RABBIT_URL = os.getenv("RABBIT_URL", "amqp://guest:guest@rabbitmq:5672/")
RELAY_RABBIT_QUEUE = os.getenv("RELAY_RABBIT_QUEUE", f"relay-rmq-{VARIANT}")
SINK_RABBIT_QUEUE = os.getenv("SINK_RABBIT_QUEUE", "sink-rmq")

producer = Producer({"bootstrap.servers": KAFKA_BROKERS})
_sink_stub = None
_stub_lock = threading.Lock()


def sink_stub():
    global _sink_stub
    with _stub_lock:
        if _sink_stub is None:
            channel = grpc.insecure_channel(SINK_GRPC)
            _sink_stub = relay_pb2_grpc.SinkStub(channel)
    return _sink_stub


def forward(test_id, via):
    if via == "http":
        requests.post(f"{SINK_HTTP}/deliver", json={"test_id": test_id}, timeout=10)
    elif via == "grpc":
        sink_stub().Deliver(relay_pb2.SinkRequest(test_id=test_id), timeout=10)
    elif via == "kafka":
        producer.produce("sink-in", json.dumps({"test_id": test_id}).encode())
        producer.flush(10)
    elif via == "rabbitmq":
        # pika connections are not thread-safe and forward() is called from
        # several threads, so use a short-lived connection per publish.
        conn = pika.BlockingConnection(pika.URLParameters(RABBIT_URL))
        try:
            ch = conn.channel()
            ch.queue_declare(queue=SINK_RABBIT_QUEUE, durable=False)
            ch.basic_publish(
                exchange="",
                routing_key=SINK_RABBIT_QUEUE,
                body=json.dumps({"test_id": test_id}).encode(),
            )
        finally:
            conn.close()
    else:
        raise ValueError(f"unknown forward_via: {via}")


def safe_version(name):
    try:
        return pkg_version(name)
    except Exception:
        return "unknown"


app = Flask(__name__)


@app.post("/relay")
def relay():
    body = request.get_json(force=True)
    forward(body["test_id"], body["forward_via"])
    return jsonify(status="forwarded")


@app.get("/info")
def info():
    tracer_pkg = {
        "otel": "opentelemetry-distro",
        "datadog": "ddtrace",
        "newrelic": "newrelic",
        "elastic": "elastic-apm",
    }.get(TOOL)
    config = {}
    for key in (
        "OTEL_PROPAGATORS",
        "OTEL_TRACES_EXPORTER",
        "DD_TRACE_PROPAGATION_STYLE",
        "DD_KAFKA_PROPAGATION_ENABLED",
        "NEW_RELIC_DISTRIBUTED_TRACING_ENABLED",
        "ELASTIC_APM_SERVER_URL",
    ):
        if os.getenv(key):
            config[key] = os.environ[key]
    return jsonify(
        variant=VARIANT,
        tool=TOOL,
        language="python",
        tracer_version=safe_version(tracer_pkg) if tracer_pkg else "unknown",
        runtime_version=f"python {os.sys.version.split()[0]}",
        config=config,
        libs={
            "flask": safe_version("flask"),
            "requests": safe_version("requests"),
            "grpcio": safe_version("grpcio"),
            "confluent-kafka": safe_version("confluent-kafka"),
            "rabbitmq": f"pika {safe_version('pika')}",
        },
    )


class RelayServicer(relay_pb2_grpc.RelayServicer):
    def Handle(self, req, context):
        forward(req.test_id, req.forward_via)
        return relay_pb2.RelayResponse(status="forwarded")


def serve_grpc():
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
    relay_pb2_grpc.add_RelayServicer_to_server(RelayServicer(), server)
    server.add_insecure_port("[::]:8081")
    server.start()
    server.wait_for_termination()


def consume():
    consumer = Consumer(
        {
            "bootstrap.servers": KAFKA_BROKERS,
            "group.id": f"relay-{VARIANT}",
            "auto.offset.reset": "earliest",
        }
    )
    consumer.subscribe([RELAY_TOPIC])
    while True:
        msg = consumer.poll(1.0)
        if msg is None or msg.error():
            continue
        try:
            req = json.loads(msg.value())
            forward(req["test_id"], req["forward_via"])
        except Exception as e:  # noqa: BLE001
            print("kafka relay error:", e, flush=True)


def consume_rabbit():
    while True:
        try:
            conn = pika.BlockingConnection(pika.URLParameters(RABBIT_URL))
            ch = conn.channel()
            # Contract queue properties: durable=False, auto_delete=False,
            # exclusive=False (must match the driver's declare).
            ch.queue_declare(queue=RELAY_RABBIT_QUEUE, durable=False)

            def on_message(channel, method, properties, body):
                try:
                    req = json.loads(body)
                    forward(req["test_id"], req["forward_via"])
                except Exception as e:  # noqa: BLE001
                    print("rabbitmq relay error:", e, flush=True)
                channel.basic_ack(delivery_tag=method.delivery_tag)

            ch.basic_consume(queue=RELAY_RABBIT_QUEUE, on_message_callback=on_message)
            print(f"rabbitmq consumer on {RELAY_RABBIT_QUEUE}", flush=True)
            ch.start_consuming()
        except Exception as e:  # noqa: BLE001
            print("rabbitmq consumer error:", e, "(retrying in 3s)", flush=True)
            time.sleep(3)


# Elastic APM has no zero-code runner for Python: the documented setup is the
# Flask middleware plus elasticapm.instrument() for library patching. This is
# boot-time agent setup (equivalent to ddtrace-run), not header handling.
if TOOL == "elastic":
    import elasticapm
    from elasticapm.contrib.flask import ElasticAPM

    elasticapm.instrument()
    ElasticAPM(app)


if __name__ == "__main__":
    threading.Thread(target=serve_grpc, daemon=True).start()
    threading.Thread(target=consume, daemon=True).start()
    threading.Thread(target=consume_rabbit, daemon=True).start()
    print(f"relay {VARIANT} on :8080 (http), :8081 (grpc), topic {RELAY_TOPIC}, queue {RELAY_RABBIT_QUEUE}", flush=True)
    app.run(host="0.0.0.0", port=8080, threaded=True)
