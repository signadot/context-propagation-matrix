// Command relay-go-orchestrion is the datadog-go-orchestrion relay: a
// deliberately naive Go application compiled with Datadog Orchestrion
// (compile-time auto-instrumentation).
//
// The source below imports NO tracing API of any kind — compare it with
// relays/go/none/main.go, which it mirrors. Every propagation behaviour this
// variant exhibits is woven in at build time by `orchestrion go build`,
// driven by the orchestrion.tool.go file next to this one (which enables all
// dd-trace-go v2 integrations). That is the whole point of the variant: it
// measures what a team gets from build-time weaving on an application whose
// source has never heard of tracing.
//
// Library choices are Orchestrion's documented happy path, matching the
// hand-wired datadog-go relay where possible:
//   - HTTP:     net/http (server + client)         — woven
//   - gRPC:     google.golang.org/grpc             — woven
//   - Kafka:    github.com/IBM/sarama              — woven (same client and
//     version as the hand-wired datadog-go relay, so the two rows compare
//     manual contrib wiring against compile-time weaving directly)
//   - RabbitMQ: github.com/rabbitmq/amqp091-go     — NOT woven: dd-trace-go
//     has no amqp091 integration, so Orchestrion has nothing to weave. The
//     rabbitmq legs measure that absence.
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

	"github.com/IBM/sarama"
	amqp "github.com/rabbitmq/amqp091-go"
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

func variant() string       { return envOr("VARIANT", "datadog-go-orchestrion") }
func relayTopic() string    { return envOr("RELAY_TOPIC", "relay-in-"+variant()) }
func groupID() string       { return "relay-" + variant() }
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
	producer   sarama.SyncProducer
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
		// sarama's SyncProducer API takes no context at all: whether the
		// produced record carries the inbound trace context is entirely up to
		// the woven instrumentation (dd-trace-go's goroutine-local context
		// stack under Orchestrion).
		_, _, err = f.producer.SendMessage(&sarama.ProducerMessage{
			Topic: sinkTopic,
			Value: sarama.ByteEncoder(value),
		})
		return err
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

// ---- Kafka consumer (plain sarama consumer group) ----

type kafkaHandler struct{ fwd *forwarder }

func (h *kafkaHandler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (h *kafkaHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (h *kafkaHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		var req relayRequest
		if err := json.Unmarshal(msg.Value, &req); err != nil {
			log.Printf("kafka: bad message: %v", err)
			sess.MarkMessage(msg, "")
			continue
		}
		// The naive app has only the session context in hand; continuing the
		// trace from the record headers is the weaver's job, if it happens.
		if err := h.fwd.forward(sess.Context(), req.TestID, req.ForwardVia); err != nil {
			log.Printf("kafka: forward: %v", err)
		}
		sess.MarkMessage(msg, "")
	}
	return nil
}

// ---- RabbitMQ plumbing (plain amqp091, no integration exists to weave) ----

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
// -ldflags "-X main.tracerVersion=...": Orchestrion links dd-trace-go into the
// binary behind the go tool's back, so the woven tracer never appears in the
// binary's build info and moduleVersion() cannot see it.
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
			"tool":            envOr("TOOL", "datadog"),
			"language":        "go",
			"tracer_version":  tracerVersion,
			"runtime_version": runtime.Version(),
			"config":          config,
			"libs": map[string]string{
				"weaver":   "orchestrion (compile-time, no source changes)",
				"http":     "net/http (plain source, woven)",
				"grpc":     "google.golang.org/grpc (plain source, woven)",
				"kafka":    "IBM/sarama " + moduleVersion("github.com/IBM/sarama") + " (plain source, woven)",
				"rabbitmq": "amqp091-go " + moduleVersion("github.com/rabbitmq/amqp091-go") + " (plain source, NO integration exists)",
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
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V3_6_0_0
	cfg.Producer.Return.Successes = true
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest

	producer, err := sarama.NewSyncProducer(kafkaBrokers(), cfg)
	if err != nil {
		log.Fatalf("kafka producer: %v", err)
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
		producer:   producer,
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

	// Kafka consumer group loop.
	group, err := sarama.NewConsumerGroup(kafkaBrokers(), groupID(), cfg)
	if err != nil {
		log.Fatalf("kafka consumer group: %v", err)
	}
	go func() {
		ctx := context.Background()
		for {
			if err := group.Consume(ctx, []string{relayTopic()}, &kafkaHandler{fwd: fwd}); err != nil {
				log.Printf("kafka consume: %v (retrying in 3s)", err)
				time.Sleep(3 * time.Second)
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
