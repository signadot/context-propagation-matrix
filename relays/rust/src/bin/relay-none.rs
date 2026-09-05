//! Command relay-none is the baseline (no instrumentation) Rust relay.
//!
//! No tracer provider and no propagator are installed: the OpenTelemetry
//! global defaults are no-ops, so the shared relay code's inject calls write
//! no headers and its extract calls yield an empty context. The binary
//! exists to prove the harness's control expectation — with nothing
//! installed, nothing propagates — so the headers the otel/datadog variants
//! deliver are attributable to their installed propagators, not to the app
//! or the client libraries.

#[tokio::main]
async fn main() -> Result<(), relay::BoxError> {
    relay::run(relay::Tool {
        name: "none",
        tracer_version: "none",
    })
    .await
}
