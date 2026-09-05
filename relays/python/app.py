"""Naive relay: receives {test_id, forward_via} over HTTP/gRPC/Kafka and
forwards to the sink over the requested protocol. Never touches headers or
tracing APIs — context propagation is the instrumentation's job."""

import json
import os
import threading
from concurrent import futures
from importlib.metadata import version as pkg_version

import grpc
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
    }.get(TOOL)
    config = {}
    for key in (
        "OTEL_PROPAGATORS",
        "OTEL_TRACES_EXPORTER",
        "DD_TRACE_PROPAGATION_STYLE",
        "NEW_RELIC_DISTRIBUTED_TRACING_ENABLED",
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


if __name__ == "__main__":
    threading.Thread(target=serve_grpc, daemon=True).start()
    threading.Thread(target=consume, daemon=True).start()
    print(f"relay {VARIANT} on :8080 (http), :8081 (grpc), topic {RELAY_TOPIC}", flush=True)
    app.run(host="0.0.0.0", port=8080, threaded=True)
