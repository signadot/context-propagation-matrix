//! Outbound legs. Each leg starts a client/producer span as a child of the
//! inbound context and injects *from that context* into the transport's
//! native header container via the global propagator — never composing a
//! header by hand.

use std::time::Duration;

use lapin::options::{BasicPublishOptions, QueueDeclareOptions};
use lapin::types::FieldTable;
use lapin::BasicProperties;
use opentelemetry::global;
use opentelemetry::trace::{SpanKind, TraceContextExt, Tracer};
use opentelemetry::Context;
use opentelemetry_http::HeaderInjector;
use rdkafka::config::ClientConfig;
use rdkafka::message::OwnedHeaders;
use rdkafka::producer::{FutureProducer, FutureRecord};

use crate::cpmatrix;
use crate::propagation::{AmqpHeaderInjector, KafkaHeaderInjector, MetadataInjector};
use crate::{
    kafka_brokers, sink_grpc, sink_http, sink_rabbit_queue, BoxError, SinkMessage, Tool,
    SINK_TOPIC,
};

/// Shared clients for the four outbound legs. All are cheap to create at
/// startup and none connects eagerly (the gRPC channel is lazy, the Kafka
/// producer and RabbitMQ connection handle broker downtime themselves), so
/// startup never crashes on unreachable peers.
pub struct App {
    pub tool: Tool,
    http: reqwest::Client,
    sink_grpc: cpmatrix::sink_client::SinkClient<tonic::transport::Channel>,
    kafka: FutureProducer,
    /// Lazily-connected publish channel (the consumer task has its own
    /// connection). Reset to None on publish failure so the next test
    /// reconnects.
    rabbit: tokio::sync::Mutex<Option<(lapin::Connection, lapin::Channel)>>,
}

impl App {
    pub fn new(tool: Tool) -> Result<Self, BoxError> {
        let target = sink_grpc();
        let uri = if target.contains("://") {
            target
        } else {
            // Plaintext, per the contract.
            format!("http://{target}")
        };
        let channel = tonic::transport::Endpoint::from_shared(uri)?.connect_lazy();
        let kafka = ClientConfig::new()
            .set("bootstrap.servers", kafka_brokers())
            .create()?;
        Ok(Self {
            tool,
            http: reqwest::Client::new(),
            sink_grpc: cpmatrix::sink_client::SinkClient::new(channel),
            kafka,
            rabbit: tokio::sync::Mutex::new(None),
        })
    }
}

/// Forwards {"test_id": ...} to the sink over `via`, injecting the
/// propagation context from a fresh client/producer span parented on `cx`.
pub async fn forward(app: &App, cx: &Context, test_id: &str, via: &str) -> Result<(), BoxError> {
    let tracer = global::tracer("cpmatrix-relay");
    let kind = match via {
        "kafka" | "rabbitmq" => SpanKind::Producer,
        _ => SpanKind::Client,
    };
    let span = tracer
        .span_builder(format!("forward {via}"))
        .with_kind(kind)
        .start_with_context(&tracer, cx);
    let out_cx = cx.with_span(span);
    let res = match via {
        "http" => forward_http(app, &out_cx, test_id).await,
        "grpc" => forward_grpc(app, &out_cx, test_id).await,
        "kafka" => forward_kafka(app, &out_cx, test_id).await,
        "rabbitmq" => forward_rabbitmq(app, &out_cx, test_id).await,
        other => Err(format!("unknown forward_via: {other:?}").into()),
    };
    out_cx.span().end();
    res
}

async fn forward_http(app: &App, cx: &Context, test_id: &str) -> Result<(), BoxError> {
    let mut req = app
        .http
        .post(format!("{}/deliver", sink_http()))
        .json(&SinkMessage {
            test_id: test_id.to_string(),
        })
        .build()?;
    // opentelemetry-http's documented carrier over http::HeaderMap.
    global::get_text_map_propagator(|prop| {
        prop.inject_context(cx, &mut HeaderInjector(req.headers_mut()))
    });
    app.http.execute(req).await?.error_for_status()?;
    Ok(())
}

async fn forward_grpc(app: &App, cx: &Context, test_id: &str) -> Result<(), BoxError> {
    let mut request = tonic::Request::new(cpmatrix::SinkRequest {
        test_id: test_id.to_string(),
    });
    global::get_text_map_propagator(|prop| {
        prop.inject_context(cx, &mut MetadataInjector(request.metadata_mut()))
    });
    // Clones share the underlying lazy channel.
    let mut client = app.sink_grpc.clone();
    client.deliver(request).await?;
    Ok(())
}

async fn forward_kafka(app: &App, cx: &Context, test_id: &str) -> Result<(), BoxError> {
    let payload = serde_json::to_vec(&SinkMessage {
        test_id: test_id.to_string(),
    })?;
    let mut headers = OwnedHeaders::new();
    global::get_text_map_propagator(|prop| {
        prop.inject_context(cx, &mut KafkaHeaderInjector(&mut headers))
    });
    let record: FutureRecord<'_, (), Vec<u8>> = FutureRecord::to(SINK_TOPIC)
        .payload(&payload)
        .headers(headers);
    app.kafka
        .send(record, Duration::from_secs(15))
        .await
        .map_err(|(err, _msg)| err)?;
    Ok(())
}

async fn forward_rabbitmq(app: &App, cx: &Context, test_id: &str) -> Result<(), BoxError> {
    let payload = serde_json::to_vec(&SinkMessage {
        test_id: test_id.to_string(),
    })?;
    let mut table = FieldTable::default();
    global::get_text_map_propagator(|prop| {
        prop.inject_context(cx, &mut AmqpHeaderInjector(&mut table))
    });
    let props = BasicProperties::default()
        .with_headers(table)
        .with_content_type("application/json".into());
    let channel = rabbit_channel(app).await?;
    let publish = channel
        .basic_publish(
            "".into(), // default exchange, routing key = queue name
            sink_rabbit_queue().into(),
            BasicPublishOptions::default(),
            &payload,
            props,
        )
        .await;
    match publish {
        Ok(confirm) => {
            confirm.await?;
            Ok(())
        }
        Err(e) => {
            // Drop the cached channel so the next attempt reconnects.
            *app.rabbit.lock().await = None;
            Err(e.into())
        }
    }
}

/// Returns the cached publish channel, connecting (and defensively declaring
/// the sink queue with the contract's properties — idempotent) if needed.
async fn rabbit_channel(app: &App) -> Result<lapin::Channel, BoxError> {
    let mut guard = app.rabbit.lock().await;
    if let Some((_, ch)) = guard.as_ref() {
        if ch.status().connected() {
            return Ok(ch.clone());
        }
        *guard = None;
    }
    let conn =
        lapin::Connection::connect(&crate::rabbit_url(), lapin::ConnectionProperties::default())
            .await?;
    let ch = conn.create_channel().await?;
    // durable=false, auto_delete=false, exclusive=false (the defaults).
    ch.queue_declare(
        sink_rabbit_queue().into(),
        QueueDeclareOptions::default(),
        FieldTable::default(),
    )
    .await?;
    *guard = Some((conn, ch.clone()));
    Ok(ch)
}
