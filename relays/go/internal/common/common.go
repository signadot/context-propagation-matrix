// Package common holds the shared, instrumentation-free bits of the Go
// relays: env handling, the JSON wire types, /info assembly, and the HTTP
// /relay handler plumbing. Nothing in here touches headers or tracing APIs.
package common

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
)

// RelayRequest is the payload received on any of the four inbound protocols.
type RelayRequest struct {
	TestID     string `json:"test_id"`
	ForwardVia string `json:"forward_via"`
}

// SinkMessage is the payload forwarded to the sink.
type SinkMessage struct {
	TestID string `json:"test_id"`
}

// ForwardFunc forwards a test message to the sink over the named protocol
// ("http", "grpc", "kafka" or "rabbitmq"), with ctx carrying whatever notion
// of current trace context the tool under test uses.
type ForwardFunc func(ctx context.Context, testID, via string) error

// Env returns the value of the environment variable key, or dflt if unset or
// empty.
func Env(key, dflt string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return dflt
}

// Variant returns the VARIANT env value ({tool}-go).
func Variant() string { return Env("VARIANT", "unknown") }

// RelayTopic returns the Kafka topic this relay consumes.
func RelayTopic() string { return Env("RELAY_TOPIC", "relay-in-"+Variant()) }

// GroupID returns the consumer group id, "relay-"+VARIANT per the contract.
func GroupID() string { return "relay-" + Variant() }

// KafkaBrokers returns the bootstrap broker list.
func KafkaBrokers() []string {
	return strings.Split(Env("KAFKA_BROKERS", "kafka:9092"), ",")
}

// SinkHTTP returns the sink's HTTP base URL.
func SinkHTTP() string { return Env("SINK_HTTP", "http://sink:9080") }

// SinkGRPC returns the sink's gRPC target.
func SinkGRPC() string { return Env("SINK_GRPC", "sink:9081") }

// SinkTopic is the Kafka topic the sink consumes.
const SinkTopic = "sink-in"

// configKeys mirrors the set echoed by the Java relay's /info endpoint.
var configKeys = []string{
	"OTEL_PROPAGATORS",
	"OTEL_TRACES_EXPORTER",
	"DD_TRACE_PROPAGATION_STYLE",
	"ELASTIC_APM_SERVER_URL",
}

// ModuleVersion returns the version of the named module as recorded in the
// binary's build info, or "unknown".
func ModuleVersion(path string) string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range bi.Deps {
		if dep.Path == path {
			if dep.Replace != nil {
				return dep.Replace.Version
			}
			return dep.Version
		}
	}
	return "unknown"
}

// Info assembles the /info document. tool is the tool name reported when the
// TOOL env override is unset; tracerModule is the module whose version is
// reported as tracer_version; libs names the instrumentation libraries in
// use.
func Info(tool, tracerModule string, libs map[string]string) map[string]any {
	config := map[string]string{}
	for _, k := range configKeys {
		if v := os.Getenv(k); v != "" {
			config[k] = v
		}
	}
	return map[string]any{
		"variant":         Variant(),
		"tool":            Env("TOOL", tool),
		"language":        "go",
		"tracer_version":  ModuleVersion(tracerModule),
		"runtime_version": runtime.Version(),
		"config":          config,
		"libs":            libs,
	}
}

// InfoHandler serves GET /info.
func InfoHandler(tool, tracerModule string, libs map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, Info(tool, tracerModule, libs))
	}
}

// RelayHandler serves POST /relay, delegating to forward with the request
// context (which carries the tool's extracted trace context).
func RelayHandler(forward ForwardFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req RelayRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := forward(r.Context(), req.TestID, req.ForwardVia); err != nil {
			WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "forwarded"})
	}
}

// WriteJSON writes v as a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
