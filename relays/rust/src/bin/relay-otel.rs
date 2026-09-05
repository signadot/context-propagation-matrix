//! Command relay-otel is the OpenTelemetry Rust relay.
//!
//! Rust has no OTel auto-instrumentation, so (like relays/go) propagation
//! goes through the official propagator API with the documented carriers —
//! see src/propagation.rs and the README.
//!
//! The global propagator is TraceContext+Baggage, set explicitly: the Rust
//! SDK does not read OTEL_PROPAGATORS (env-based configuration is not
//! implemented in opentelemetry_sdk 0.32). The SdkTracerProvider has **no
//! exporter registered**: spans are still created and sampled (so downstream
//! injection carries fresh span ids instead of pass-through), but nothing
//! leaves the process — same rationale as relays/go/otel.

use opentelemetry::global;
use opentelemetry::propagation::{TextMapCompositePropagator, TextMapPropagator};
use opentelemetry_sdk::propagation::{BaggagePropagator, TraceContextPropagator};
use opentelemetry_sdk::trace::SdkTracerProvider;

#[tokio::main]
async fn main() -> Result<(), relay::BoxError> {
    let provider = SdkTracerProvider::builder().build(); // no exporter
    global::set_tracer_provider(provider);
    let propagators: Vec<Box<dyn TextMapPropagator + Send + Sync>> = vec![
        Box::new(TraceContextPropagator::new()),
        Box::new(BaggagePropagator::new()),
    ];
    global::set_text_map_propagator(TextMapCompositePropagator::new(propagators));

    relay::run(relay::Tool {
        name: "otel",
        tracer_version: env!("VER_OPENTELEMETRY"),
    })
    .await
}
