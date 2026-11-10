//! Shared, tool-agnostic plumbing for the Rust relays (mirrors
//! relays/go/internal/common): env handling, JSON wire types, /info assembly,
//! transport servers/consumers and the forwarder.
//!
//! Rust has no auto-instrumentation, so — like the Go relays — context flows
//! through the vendor's documented propagator API with explicit
//! `opentelemetry::Context` plumbing: extract on inbound, start a span with
//! the extracted parent, pass that context to the outbound leg and inject
//! from it there. No propagation header is ever composed by hand; see
//! `propagation.rs` for the carrier implementations.

pub mod forward;
pub mod inbound;
pub mod propagation;

use opentelemetry::global;
use opentelemetry::trace::{SpanKind, TraceContextExt, Tracer};
use opentelemetry::Context;
use serde::{Deserialize, Serialize};
use serde_json::json;

/// Generated tonic stubs for the shared cpmatrix proto.
pub mod cpmatrix {
    tonic::include_proto!("cpmatrix");
}

pub type BoxError = Box<dyn std::error::Error + Send + Sync>;

/// Payload received on any of the four inbound protocols.
#[derive(Debug, Clone, Deserialize)]
pub struct RelayRequest {
    pub test_id: String,
    #[serde(default)]
    pub forward_via: String,
}

/// Payload forwarded to the sink.
#[derive(Debug, Serialize)]
pub struct SinkMessage {
    pub test_id: String,
}

/// Which tool this binary was built for (set by the respective main).
#[derive(Debug, Clone, Copy)]
pub struct Tool {
    /// "otel" or "datadog"; default for /info's `tool` when TOOL env unset.
    pub name: &'static str,
    /// The tracer crate's version, stamped from Cargo.lock at build time.
    pub tracer_version: &'static str,
}

// --- env / contract values (defaults per DESIGN.md) --------------------------

pub fn env_or(key: &str, dflt: &str) -> String {
    match std::env::var(key) {
        Ok(v) if !v.is_empty() => v,
        _ => dflt.to_string(),
    }
}

pub fn variant() -> String {
    env_or("VARIANT", "unknown")
}

pub fn relay_topic() -> String {
    env_or("RELAY_TOPIC", &format!("relay-in-{}", variant()))
}

pub fn group_id() -> String {
    format!("relay-{}", variant())
}

pub fn kafka_brokers() -> String {
    env_or("KAFKA_BROKERS", "kafka:9092")
}

pub fn sink_http() -> String {
    env_or("SINK_HTTP", "http://sink:9080")
}

pub fn sink_grpc() -> String {
    env_or("SINK_GRPC", "sink:9081")
}

pub const SINK_TOPIC: &str = "sink-in";

pub fn rabbit_url() -> String {
    env_or("RABBIT_URL", "amqp://guest:guest@rabbitmq:5672/")
}

pub fn relay_rabbit_queue() -> String {
    env_or("RELAY_RABBIT_QUEUE", &format!("relay-rmq-{}", variant()))
}

pub fn sink_rabbit_queue() -> String {
    env_or("SINK_RABBIT_QUEUE", "sink-rmq")
}

// --- /info -------------------------------------------------------------------

/// Assembles the /info document. Versions come from Cargo.lock (stamped by
/// build.rs); runtime_version is the rustc that built the binary.
pub fn info_json(tool: &Tool) -> serde_json::Value {
    let mut config = serde_json::Map::new();
    for key in [
        "OTEL_PROPAGATORS",
        "OTEL_TRACES_EXPORTER",
        "DD_TRACE_PROPAGATION_STYLE",
    ] {
        if let Ok(v) = std::env::var(key) {
            if !v.is_empty() {
                config.insert(key.to_string(), v.into());
            }
        }
    }
    json!({
        "variant": variant(),
        "tool": env_or("TOOL", tool.name),
        "language": "rust",
        "tracer_version": tool.tracer_version,
        "runtime_version": env!("RUSTC_VERSION"),
        "config": config,
        "libs": {
            "http_server": concat!("axum ", env!("VER_AXUM")),
            "http_client": concat!("reqwest ", env!("VER_REQWEST")),
            "grpc": concat!("tonic ", env!("VER_TONIC")),
            "kafka": concat!("rust-rdkafka ", env!("VER_RDKAFKA")),
            "rabbitmq": concat!("lapin ", env!("VER_LAPIN")),
            "propagation": propagation_libs(),
        },
    })
}

/// The OpenTelemetry crates this binary links, from its own Cargo.lock:
/// `opentelemetry 0.33.0 + opentelemetry_sdk 0.33.0 + opentelemetry-http
/// 0.33.0` for the otel relay; the datadog and baseline relays have no
/// opentelemetry_sdk of their own and list the two they do link.
fn propagation_libs() -> String {
    [
        ("opentelemetry", env!("VER_OPENTELEMETRY")),
        ("opentelemetry_sdk", env!("VER_OPENTELEMETRY_SDK")),
        ("opentelemetry-http", env!("VER_OPENTELEMETRY_HTTP")),
    ]
    .iter()
    .filter(|(_, v)| *v != "unknown")
    .map(|(name, v)| format!("{name} {v}"))
    .collect::<Vec<_>>()
    .join(" + ")
}

// --- inbound -> outbound glue --------------------------------------------------

/// Handles one inbound message: starts a span parented on the extracted
/// context (span ids must change across the hop — the provider is real, just
/// exporter-less) and forwards to the sink from within that context. This is
/// the documented OTel API pattern for uninstrumented transports (see
/// module docs and README).
pub async fn handle_inbound(
    app: &forward::App,
    parent_cx: Context,
    transport: &str,
    req: &RelayRequest,
) -> Result<(), BoxError> {
    let tracer = global::tracer("cpmatrix-relay");
    let kind = match transport {
        "http" | "grpc" => SpanKind::Server,
        _ => SpanKind::Consumer,
    };
    let span = tracer
        .span_builder(format!("relay {transport}"))
        .with_kind(kind)
        .start_with_context(&tracer, &parent_cx);
    let cx = parent_cx.with_span(span);
    let res = forward::forward(app, &cx, &req.test_id, &req.forward_via).await;
    cx.span().end();
    res
}

/// Starts all four inbound transports; runs forever (HTTP serves in the
/// foreground, the rest on tasks). Called by both mains after they have
/// installed their tool's global tracer provider and propagator.
pub async fn run(tool: Tool) -> Result<(), BoxError> {
    let app = std::sync::Arc::new(forward::App::new(tool)?);

    // gRPC server on :8081.
    let grpc_app = app.clone();
    tokio::spawn(async move {
        let svc = cpmatrix::relay_server::RelayServer::new(inbound::GrpcRelay { app: grpc_app });
        let addr = "0.0.0.0:8081".parse().expect("grpc addr");
        if let Err(e) = tonic::transport::Server::builder()
            .add_service(svc)
            .serve(addr)
            .await
        {
            eprintln!("grpc serve: {e}");
            std::process::exit(1);
        }
    });

    // Kafka + RabbitMQ consumers (retry forever, never crash the process).
    tokio::spawn(inbound::run_kafka_consumer(app.clone()));
    tokio::spawn(inbound::run_rabbit_consumer(app.clone()));

    // HTTP server on :8080 (foreground).
    let listener = tokio::net::TcpListener::bind("0.0.0.0:8080").await?;
    println!(
        "relay {} listening on :8080 (http), :8081 (grpc); topic {}, queue {}",
        variant(),
        relay_topic(),
        relay_rabbit_queue()
    );
    axum::serve(listener, inbound::router(app)).await?;
    Ok(())
}
