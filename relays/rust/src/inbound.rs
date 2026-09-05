//! Inbound transports: axum HTTP server, tonic gRPC service, rdkafka
//! consumer, lapin consumer. Each extracts the propagation context from the
//! transport's native headers via the global propagator and hands it to
//! `handle_inbound`, which forwards to the sink from inside the
//! handler/consumer callback (honest "current context" semantics).

use std::sync::Arc;
use std::time::Duration;

use axum::extract::State;
use axum::http::StatusCode;
use axum::routing::{get, post};
use axum::{Json, Router};
use futures_util::StreamExt;
use lapin::options::{BasicAckOptions, BasicConsumeOptions, QueueDeclareOptions};
use lapin::types::FieldTable;
use opentelemetry::global;
use opentelemetry::Context;
use opentelemetry_http::HeaderExtractor;
use rdkafka::config::ClientConfig;
use rdkafka::consumer::{Consumer, StreamConsumer};
use rdkafka::Message;
use serde_json::json;

use crate::forward::App;
use crate::propagation::{AmqpHeaderExtractor, KafkaHeaderExtractor, MetadataExtractor};
use crate::{
    cpmatrix, group_id, handle_inbound, info_json, kafka_brokers, rabbit_url, relay_rabbit_queue,
    relay_topic, sink_rabbit_queue, RelayRequest,
};

// --- HTTP (axum) --------------------------------------------------------------

pub fn router(app: Arc<App>) -> Router {
    Router::new()
        .route("/relay", post(relay_handler))
        .route("/info", get(info_handler))
        .with_state(app)
}

async fn relay_handler(
    State(app): State<Arc<App>>,
    headers: axum::http::HeaderMap,
    Json(req): Json<RelayRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    // opentelemetry-http's documented carrier over http::HeaderMap.
    let parent_cx =
        global::get_text_map_propagator(|prop| prop.extract(&HeaderExtractor(&headers)));
    match handle_inbound(&app, parent_cx, "http", &req).await {
        Ok(()) => (StatusCode::OK, Json(json!({"status": "forwarded"}))),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(json!({"error": e.to_string()})),
        ),
    }
}

async fn info_handler(State(app): State<Arc<App>>) -> Json<serde_json::Value> {
    Json(info_json(&app.tool))
}

// --- gRPC (tonic) -------------------------------------------------------------

pub struct GrpcRelay {
    pub app: Arc<App>,
}

#[tonic::async_trait]
impl cpmatrix::relay_server::Relay for GrpcRelay {
    async fn handle(
        &self,
        request: tonic::Request<cpmatrix::RelayRequest>,
    ) -> Result<tonic::Response<cpmatrix::RelayResponse>, tonic::Status> {
        let parent_cx = global::get_text_map_propagator(|prop| {
            prop.extract(&MetadataExtractor(request.metadata()))
        });
        let msg = request.get_ref();
        let req = RelayRequest {
            test_id: msg.test_id.clone(),
            forward_via: msg.forward_via.clone(),
        };
        handle_inbound(&self.app, parent_cx, "grpc", &req)
            .await
            .map_err(|e| tonic::Status::internal(e.to_string()))?;
        Ok(tonic::Response::new(cpmatrix::RelayResponse {
            status: "forwarded".into(),
        }))
    }
}

// --- Kafka (rdkafka) ----------------------------------------------------------

/// Consumes the relay topic forever. Client creation only fails on bad
/// config, but is still retried; broker unavailability is handled inside
/// librdkafka (recv errors are logged and consumption continues).
pub async fn run_kafka_consumer(app: Arc<App>) {
    loop {
        let consumer: StreamConsumer = match ClientConfig::new()
            .set("bootstrap.servers", kafka_brokers())
            .set("group.id", group_id())
            .set("auto.offset.reset", "earliest")
            .create()
        {
            Ok(c) => c,
            Err(e) => {
                eprintln!("kafka: create consumer: {e}; retrying in 5s");
                tokio::time::sleep(Duration::from_secs(5)).await;
                continue;
            }
        };
        let topic = relay_topic();
        if let Err(e) = consumer.subscribe(&[&topic]) {
            eprintln!("kafka: subscribe {topic}: {e}; retrying in 5s");
            tokio::time::sleep(Duration::from_secs(5)).await;
            continue;
        }
        loop {
            match consumer.recv().await {
                Err(e) => {
                    eprintln!("kafka: recv: {e}");
                    tokio::time::sleep(Duration::from_secs(1)).await;
                }
                Ok(msg) => {
                    let parent_cx = msg
                        .headers()
                        .map(|h| {
                            global::get_text_map_propagator(|prop| {
                                prop.extract(&KafkaHeaderExtractor(h))
                            })
                        })
                        .unwrap_or_else(Context::new);
                    match serde_json::from_slice::<RelayRequest>(msg.payload().unwrap_or_default())
                    {
                        Ok(req) => {
                            if let Err(e) = handle_inbound(&app, parent_cx, "kafka", &req).await {
                                eprintln!("kafka: forward: {e}");
                            }
                        }
                        Err(e) => eprintln!("kafka: bad message: {e}"),
                    }
                }
            }
        }
    }
}

// --- RabbitMQ (lapin) ---------------------------------------------------------

/// Consumes the relay queue forever, reconnecting on any failure (broker not
/// yet up, connection dropped, ...) without crashing the process.
pub async fn run_rabbit_consumer(app: Arc<App>) {
    loop {
        if let Err(e) = rabbit_consume(&app).await {
            eprintln!("rabbitmq: consumer: {e}; retrying in 2s");
        }
        tokio::time::sleep(Duration::from_secs(2)).await;
    }
}

async fn rabbit_consume(app: &App) -> Result<(), crate::BoxError> {
    let conn =
        lapin::Connection::connect(&rabbit_url(), lapin::ConnectionProperties::default()).await?;
    let channel = conn.create_channel().await?;
    // Contract queue properties: durable=false, auto_delete=false,
    // exclusive=false (the defaults). Declared defensively, like the other
    // relays; the sink queue too so rabbit-outbound tests can't drop messages.
    for queue in [relay_rabbit_queue(), sink_rabbit_queue()] {
        channel
            .queue_declare(
                queue.into(),
                QueueDeclareOptions::default(),
                FieldTable::default(),
            )
            .await?;
    }
    let mut consumer = channel
        .basic_consume(
            relay_rabbit_queue().into(),
            "relay".into(),
            BasicConsumeOptions::default(),
            FieldTable::default(),
        )
        .await?;
    while let Some(delivery) = consumer.next().await {
        let delivery = delivery?;
        let parent_cx = delivery
            .properties
            .headers()
            .as_ref()
            .map(|table| {
                global::get_text_map_propagator(|prop| prop.extract(&AmqpHeaderExtractor(table)))
            })
            .unwrap_or_else(Context::new);
        match serde_json::from_slice::<RelayRequest>(&delivery.data) {
            Ok(req) => {
                if let Err(e) = handle_inbound(app, parent_cx, "rabbitmq", &req).await {
                    eprintln!("rabbitmq: forward: {e}");
                }
            }
            Err(e) => eprintln!("rabbitmq: bad message: {e}"),
        }
        // Ack even on error — never redeliver-loop in the harness (matches
        // the other relays).
        delivery.ack(BasicAckOptions::default()).await?;
    }
    Ok(())
}
