// Command relay-go-otelc is the otel-go-compiletime relay: a deliberately
// naive Go application compiled with the OpenTelemetry Go Compile-Time
// Instrumentation tool ("otelc", go.opentelemetry.io/otelc).
//
// The source below imports NO tracing API of any kind — compare it with
// relays/go/none/main.go, which it mirrors. Every propagation behaviour this
// variant exhibits is woven in at build time by `otelc go build`, which
// analyzes the module's dependency graph and injects instrumentation via
// -toolexec trampolines. That is the whole point of the variant: it measures
// what a team gets from build-time weaving on an application whose source has
// never heard of tracing.
//
// Library choices are otelc's documented happy path (its v1 supported list):
//   - HTTP:     net/http (server + client)          — woven
//   - gRPC:     google.golang.org/grpc              — woven
//   - Kafka:    github.com/segmentio/kafka-go       — woven (the ONLY Kafka
//     client otelc v1 supports; note the hand-wired otel-go relay uses
//     franz-go + kotel, so the two rows also compare client coverage)
//   - RabbitMQ: github.com/rabbitmq/amqp091-go      — NOT woven: otelc has no
//     RabbitMQ instrumentation at all. The rabbitmq legs measure that absence.
//
// This module intentionally does not share code with relays/go: the weaver
// operates on the whole module, and the point is a clean, naive dependency
// graph. The few helpers duplicated from relays/go/internal/common are
// instrumentation-free plumbing.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	kafka "github.com/segmentio/kafka-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/signadot/context-propagation-matrix/gen/cpmatrix"
)

// ---- wire types (harness contract) ----

type relayRequest struct {
	TestID     string `json:"test_id"`
	ForwardVia string `json:"forward_via"`
}

type sinkMessage struct {
	TestID string `json:"test_id"`
}

// ---- env plumbing ----

func envOr(key, dflt string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return dflt
}

func variant() string    { return envOr("VARIANT", "otel-go-compiletime") }
func relayTopic() string { return envOr("RELAY_TOPIC", "relay-in-"+variant()) }
func groupID() string    { return "relay-" + variant() }
func kafkaBrokers() []string {
	return strings.Split(envOr("KAFKA_BROKERS", "kafka:9092"), ",")
}
func sinkHTTP() string { return envOr("SINK_HTTP", "http://sink:9080") }
func sinkGRPC() string { return envOr("SINK_GRPC", "sink:9081") }

const sinkTopic = "sink-in"

func rabbitURL() string        { return envOr("RABBIT_URL", "amqp://guest:guest@rabbitmq:5672/") }
func relayRabbitQueue() string { return envOr("RELAY_RABBIT_QUEUE", "relay-rmq-"+variant()) }
func sinkRabbitQueue() string  { return envOr("SINK_RABBIT_QUEUE", "sink-rmq") }

// ---- forwarding ----

type forwarder struct {
	httpClient *http.Client
	sink       cpmatrix.SinkClient
	writer     *kafka.Writer
	rabbit     *amqp.Channel
}

func (f *forwarder) forward(ctx context.Context, testID, via string) error {
	switch via {
	case "http":
		body, err := json.Marshal(sinkMessage{TestID: testID})
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			sinkHTTP()+"/deliver", bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := f.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	case "grpc":
		_, err := f.sink.Deliver(ctx, &cpmatrix.SinkRequest{TestId: testID})
		return err
	case "kafka":
		value, err := json.Marshal(sinkMessage{TestID: testID})
		if err != nil {
			return err
		}
		// kafka-go's Writer does take a context; whether the produced record
		// carries the inbound trace context is up to the woven
		// instrumentation.
		return f.writer.WriteMessages(ctx, kafka.Message{Value: value})
	case "rabbitmq":
		body, err := json.Marshal(sinkMessage{TestID: testID})
		if err != nil {
			return err
		}
		return f.rabbit.PublishWithContext(ctx, "", sinkRabbitQueue(), false, false,
			amqp.Publishing{ContentType: "application/json", Body: body})
	default:
		return fmt.Errorf("unknown forward_via: %q", via)
	}
}

// ---- RabbitMQ plumbing (plain amqp091, no instrumentation exists to weave) ----

func dialRabbit() (*amqp.Connection, *amqp.Channel, error) {
	conn, err := amqp.Dial(rabbitURL())
	if err != nil {
		return nil, nil, err
	}
	ch, err := conn.Channel()
	if err == nil {
		for _, q := range []string{relayRabbitQueue(), sinkRabbitQueue()} {
			if _, err = ch.QueueDeclare(q, false, false, false, false, nil); err != nil {
				break
			}
		}
	}
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return conn, ch, nil
}

func rabbitConsumeLoop(handle func(d amqp.Delivery)) {
	for {
		conn, ch, err := dialRabbit()
		if err != nil {
			log.Printf("rabbitmq dial: %v (retrying in 3s)", err)
			time.Sleep(3 * time.Second)
			continue
		}
		deliveries, err := ch.Consume(relayRabbitQueue(), "", true, false, false, false, nil)
		if err != nil {
			log.Printf("rabbitmq consume: %v (retrying in 3s)", err)
			_ = conn.Close()
			time.Sleep(3 * time.Second)
			continue
		}
		log.Printf("rabbitmq consumer started (queue=%s)", relayRabbitQueue())
		for d := range deliveries {
			handle(d)
		}
		log.Printf("rabbitmq deliveries channel closed; redialing")
		_ = conn.Close()
		time.Sleep(time.Second)
	}
}

// ---- gRPC service ----

type grpcRelay struct {
	cpmatrix.UnimplementedRelayServer
	fwd *forwarder
}

func (s *grpcRelay) Handle(ctx context.Context, req *cpmatrix.RelayRequest) (*cpmatrix.RelayResponse, error) {
	if err := s.fwd.forward(ctx, req.GetTestId(), req.GetForwardVia()); err != nil {
		return nil, err
	}
	return &cpmatrix.RelayResponse{Status: "forwarded"}, nil
}

// ---- /info ----

// tracerVersion is stamped by the Dockerfile via
// -ldflags "-X main.tracerVersion=...": the version of go.opentelemetry.io/otelc,
// the package this variant pins and is versioned by (pins.yaml `otelc`). otelc
// is a `tool` dependency, so it is not among the binary's build-info deps and
// moduleVersion() cannot see it. The OTel SDK it weaves in is visible there
// and is reported under libs.
var tracerVersion = "unknown"

func moduleVersion(path string) string {
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

var configKeys = []string{
	"OTEL_PROPAGATORS",
	"OTEL_TRACES_EXPORTER",
	"DD_TRACE_PROPAGATION_STYLE",
	"NEW_RELIC_DISTRIBUTED_TRACING_ENABLED",
	"ELASTIC_APM_SERVER_URL",
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func infoHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		config := map[string]string{}
		for _, k := range configKeys {
			if v := os.Getenv(k); v != "" {
				config[k] = v
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"variant":         variant(),
			"tool":            envOr("TOOL", "otel"),
			"language":        "go",
			"tracer_version":  tracerVersion,
			"runtime_version": runtime.Version(),
			"config":          config,
			"libs": map[string]string{
				"weaver":   "otelc (compile-time, no source changes)",
				"sdk":      "go.opentelemetry.io/otel " + moduleVersion("go.opentelemetry.io/otel") + " (woven in by otelc)",
				"http":     "net/http (plain source, woven)",
				"grpc":     "google.golang.org/grpc (plain source, woven)",
				"kafka":    "segmentio/kafka-go " + moduleVersion("github.com/segmentio/kafka-go") + " (plain source, woven)",
				"rabbitmq": "amqp091-go " + moduleVersion("github.com/rabbitmq/amqp091-go") + " (plain source, NO instrumentation exists)",
			},
		})
	}
}

// ---- main ----

func relayHandler(fwd *forwarder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req relayRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := fwd.forward(r.Context(), req.TestID, req.ForwardVia); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "forwarded"})
	}
}

func main() {
	writer := &kafka.Writer{
		Addr:                   kafka.TCP(kafkaBrokers()...),
		Topic:                  sinkTopic,
		Balancer:               &kafka.LeastBytes{},
		AllowAutoTopicCreation: true,
	}

	conn, err := grpc.NewClient(sinkGRPC(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("grpc client: %v", err)
	}

	_, rabbitCh, err := dialRabbit()
	if err != nil {
		log.Fatalf("rabbitmq: %v", err)
	}

	fwd := &forwarder{
		httpClient: http.DefaultClient,
		sink:       cpmatrix.NewSinkClient(conn),
		writer:     writer,
		rabbit:     rabbitCh,
	}

	// gRPC server on :8081.
	grpcServer := grpc.NewServer()
	cpmatrix.RegisterRelayServer(grpcServer, &grpcRelay{fwd: fwd})
	lis, err := net.Listen("tcp", ":8081")
	if err != nil {
		log.Fatalf("grpc listen: %v", err)
	}
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("grpc serve: %v", err)
		}
	}()

	// Kafka consumer loop (kafka-go Reader).
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     kafkaBrokers(),
		GroupID:     groupID(),
		Topic:       relayTopic(),
		StartOffset: kafka.FirstOffset,
	})
	go func() {
		ctx := context.Background()
		for {
			m, err := reader.ReadMessage(ctx)
			if err != nil {
				log.Printf("kafka read: %v (retrying in 3s)", err)
				time.Sleep(3 * time.Second)
				continue
			}
			var req relayRequest
			if err := json.Unmarshal(m.Value, &req); err != nil {
				log.Printf("kafka: bad message: %v", err)
				continue
			}
			// The naive app has only the loop context in hand; continuing the
			// trace from the record headers is the weaver's job, if it
			// happens.
			if err := fwd.forward(ctx, req.TestID, req.ForwardVia); err != nil {
				log.Printf("kafka: forward: %v", err)
			}
		}
	}()

	// RabbitMQ consumer loop.
	go rabbitConsumeLoop(func(d amqp.Delivery) {
		var req relayRequest
		if err := json.Unmarshal(d.Body, &req); err != nil {
			log.Printf("rabbitmq: bad message: %v", err)
			return
		}
		if err := fwd.forward(context.Background(), req.TestID, req.ForwardVia); err != nil {
			log.Printf("rabbitmq: forward: %v", err)
		}
	})

	// HTTP server on :8080.
	mux := http.NewServeMux()
	mux.HandleFunc("/relay", relayHandler(fwd))
	mux.HandleFunc("/info", infoHandler())
	log.Printf("relay %s listening on :8080 (http), :8081 (grpc); consuming %s (kafka), %s (rabbitmq)",
		variant(), relayTopic(), relayRabbitQueue())
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("http serve: %v", err)
	}
}
