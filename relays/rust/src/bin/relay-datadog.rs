//! Command relay-datadog is the Datadog Rust relay, built on Datadog's
//! official Rust SDK (the datadog-opentelemetry crate, "Tracing Rust
//! Applications" / dd-trace-rs). The SDK is an OpenTelemetry SDK
//! distribution: the same OTel API calls and carriers as the otel relay,
//! with Datadog's tracer provider and propagator installed globally.
//!
//! `datadog_opentelemetry::tracing().init()` is the documented entry point:
//! it reads DD_* env configuration (including DD_TRACE_PROPAGATION_STYLE)
//! and installs the tracer provider *and* the Datadog text-map propagator
//! globally. With no Datadog agent running, export attempts fail and traces
//! are dropped — header injection is unaffected (see DESIGN.md).

#[tokio::main]
async fn main() -> Result<(), relay::BoxError> {
    let _provider = datadog_opentelemetry::tracing().init();

    relay::run(relay::Tool {
        name: "datadog",
        tracer_version: env!("VER_DATADOG_OPENTELEMETRY"),
    })
    .await
}
