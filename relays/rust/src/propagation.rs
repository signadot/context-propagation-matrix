//! Carrier implementations over each transport library's native header
//! container.
//!
//! Rust has no auto-instrumentation for any tool, so — exactly like
//! `relays/go` — propagation goes through the vendor's documented propagator
//! API. Where no ready-made carrier crate exists, implementing
//! `opentelemetry::propagation::{Injector, Extractor}` over the library's own
//! header type *is* the documented API usage (the propagator decides every
//! header name and value; this module never composes a propagation header):
//!
//! - HTTP uses `opentelemetry_http::{HeaderExtractor, HeaderInjector}`
//!   directly (the official carrier crate over `http::HeaderMap`, shared by
//!   axum and reqwest); no custom impl needed, see `inbound.rs`/`forward.rs`.
//!   This is also the pattern Datadog's own Rust docs prescribe
//!   (<https://docs.datadoghq.com/tracing/trace_collection/trace_context_propagation/?tab=rust>).
//! - gRPC mirrors the `MetadataMap` carrier from opentelemetry-rust's
//!   tracing-grpc example
//!   (<https://github.com/open-telemetry/opentelemetry-rust/tree/main/examples/tracing-grpc>).
//! - Kafka implements the traits over rdkafka's `OwnedHeaders` /
//!   `BorrowedHeaders` record-header types.
//! - RabbitMQ implements the traits over lapin's `FieldTable`
//!   (AMQP `basic.properties` headers).

use opentelemetry::propagation::{Extractor, Injector};

// --- gRPC (tonic metadata) -------------------------------------------------

/// Extractor over inbound `tonic::metadata::MetadataMap`.
pub struct MetadataExtractor<'a>(pub &'a tonic::metadata::MetadataMap);

impl Extractor for MetadataExtractor<'_> {
    fn get(&self, key: &str) -> Option<&str> {
        self.0.get(key).and_then(|v| v.to_str().ok())
    }

    fn keys(&self) -> Vec<&str> {
        self.0
            .keys()
            .map(|k| match k {
                tonic::metadata::KeyRef::Ascii(v) => v.as_str(),
                tonic::metadata::KeyRef::Binary(v) => v.as_str(),
            })
            .collect()
    }
}

/// Injector over outbound `tonic::metadata::MetadataMap`.
pub struct MetadataInjector<'a>(pub &'a mut tonic::metadata::MetadataMap);

impl Injector for MetadataInjector<'_> {
    fn set(&mut self, key: &str, value: String) {
        if let Ok(key) = tonic::metadata::MetadataKey::from_bytes(key.as_bytes()) {
            if let Ok(value) = value.parse() {
                self.0.insert(key, value);
            }
        }
    }
}

// --- Kafka (rdkafka record headers) ----------------------------------------

use rdkafka::message::{BorrowedHeaders, Header, Headers, OwnedHeaders};

/// Injector over rdkafka's `OwnedHeaders` (attached to the produced record).
/// `OwnedHeaders::insert` is consuming, hence the swap.
pub struct KafkaHeaderInjector<'a>(pub &'a mut OwnedHeaders);

impl Injector for KafkaHeaderInjector<'_> {
    fn set(&mut self, key: &str, value: String) {
        let headers = std::mem::replace(self.0, OwnedHeaders::new());
        *self.0 = headers.insert(Header {
            key,
            value: Some(value.as_str()),
        });
    }
}

/// Extractor over the consumed record's `BorrowedHeaders`.
pub struct KafkaHeaderExtractor<'a>(pub &'a BorrowedHeaders);

impl Extractor for KafkaHeaderExtractor<'_> {
    fn get(&self, key: &str) -> Option<&str> {
        self.0
            .iter()
            .find(|h| h.key == key)
            .and_then(|h| h.value)
            .and_then(|v| std::str::from_utf8(v).ok())
    }

    fn keys(&self) -> Vec<&str> {
        self.0.iter().map(|h| h.key).collect()
    }
}

// --- RabbitMQ (lapin AMQP header table) -------------------------------------

use lapin::types::{AMQPValue, FieldTable};

/// Injector over lapin's `FieldTable` (set as `basic.properties` headers on
/// publish). Values go in as AMQP long-strings, the interoperable type every
/// other client reads back as a string.
pub struct AmqpHeaderInjector<'a>(pub &'a mut FieldTable);

impl Injector for AmqpHeaderInjector<'_> {
    fn set(&mut self, key: &str, value: String) {
        self.0.insert(key.into(), AMQPValue::LongString(value.into()));
    }
}

/// Extractor over the delivered message's `FieldTable` headers.
pub struct AmqpHeaderExtractor<'a>(pub &'a FieldTable);

impl Extractor for AmqpHeaderExtractor<'_> {
    fn get(&self, key: &str) -> Option<&str> {
        self.0.inner().iter().find(|(k, _)| k.as_str() == key).and_then(|(_, v)| match v {
            AMQPValue::LongString(s) => std::str::from_utf8(s.as_bytes()).ok(),
            _ => None,
        })
    }

    fn keys(&self) -> Vec<&str> {
        self.0.inner().keys().map(|k| k.as_str()).collect()
    }
}
