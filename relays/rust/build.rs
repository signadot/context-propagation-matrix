//! Build script shared by the per-tool crates in ../<tool>/Cargo.toml:
//! generates the tonic gRPC stubs from the shared proto/relay.proto (repo
//! root), and stamps dependency versions from the crate's own Cargo.lock
//! plus the rustc version into env vars consumed by `/info` (via `env!` —
//! no runtime lookups, per the harness contract). A crate the lock does not
//! hold (opentelemetry_sdk outside the otel relay, datadog-opentelemetry
//! outside the datadog one) is stamped "unknown" and left out of `/info`.

use std::env;
use std::error::Error;
use std::fs;
use std::path::Path;
use std::process::Command;

fn main() -> Result<(), Box<dyn Error>> {
    // gRPC stubs. The proto lives at the repo root; paths are relative to
    // the crate directory (relays/rust/<tool>) and work both locally and in
    // the Dockerfile (build context = repo root, crate at
    // /src/relays/rust/<tool>). Requires protoc on PATH (tonic-prost-build
    // shells out to it).
    println!("cargo:rerun-if-changed=../../../proto/relay.proto");
    tonic_prost_build::configure()
        .build_server(true)
        .build_client(true)
        .compile_protos(&["../../../proto/relay.proto"], &["../../../proto"])?;

    // Version stamping from Cargo.lock (present before build scripts run;
    // the Docker build uses --locked so these match the committed lockfile).
    let manifest_dir = env::var("CARGO_MANIFEST_DIR")?;
    let lock = fs::read_to_string(Path::new(&manifest_dir).join("Cargo.lock"))?;
    for (crate_name, env_name) in [
        ("opentelemetry", "VER_OPENTELEMETRY"),
        ("opentelemetry_sdk", "VER_OPENTELEMETRY_SDK"),
        ("opentelemetry-http", "VER_OPENTELEMETRY_HTTP"),
        ("datadog-opentelemetry", "VER_DATADOG_OPENTELEMETRY"),
        ("axum", "VER_AXUM"),
        ("reqwest", "VER_REQWEST"),
        ("tonic", "VER_TONIC"),
        ("rdkafka", "VER_RDKAFKA"),
        ("lapin", "VER_LAPIN"),
    ] {
        let version = lock_version(&lock, crate_name).unwrap_or_else(|| "unknown".into());
        println!("cargo:rustc-env={env_name}={version}");
    }

    // Runtime version = the rustc that compiles this binary.
    let rustc = env::var("RUSTC").unwrap_or_else(|_| "rustc".into());
    let out = Command::new(rustc).arg("--version").output()?;
    println!(
        "cargo:rustc-env=RUSTC_VERSION={}",
        String::from_utf8_lossy(&out.stdout).trim()
    );
    Ok(())
}

/// Extracts `version` from the `[[package]]` block in Cargo.lock whose `name`
/// matches. The lockfile format is stable enough for this line scan (name is
/// always immediately followed by version within a block).
fn lock_version(lock: &str, name: &str) -> Option<String> {
    let mut in_block = false;
    for line in lock.lines() {
        let line = line.trim();
        if let Some(n) = line.strip_prefix("name = ") {
            in_block = n.trim_matches('"') == name;
        } else if in_block {
            if let Some(v) = line.strip_prefix("version = ") {
                return Some(v.trim_matches('"').to_string());
            }
        }
    }
    None
}
