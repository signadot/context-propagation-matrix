// Command driver runs one test per protocol boundary against one relay
// variant, seeding context propagation headers on the inbound side and
// evaluating what the sink observed on the outbound side.
//
// The driver is deliberately uninstrumented: headers are seeded by hand.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	pb "github.com/signadot/context-propagation-matrix/gen/cpmatrix"
	"github.com/signadot/context-propagation-matrix/internal/eval"
	"github.com/signadot/context-propagation-matrix/internal/raw"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

var protocols = []string{"http", "grpc", "kafka", "rabbitmq"}

type Env struct {
	Variant          string
	RelayHTTP        string // http://relay-{variant}:8080
	RelayGRPC        string // relay-{variant}:8081
	RelayTopic       string // relay-in-{variant}
	RelayRabbitQueue string // relay-rmq-{variant}
	SinkURL          string // http://sink:9080
	Brokers          []string
	Kafka            *kgo.Client
	Rabbit           *amqp.Channel
	HTTP             *http.Client
	PollTimeout      time.Duration
}

func randHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func newSeed(testID string) raw.Seeded {
	traceID := randHex(16)
	spanID := randHex(8)
	rk := "rk-" + randHex(4)
	return raw.Seeded{
		TraceID:    traceID,
		SpanID:     spanID,
		RoutingKey: rk,
		Headers: map[string]string{
			"traceparent":                         fmt.Sprintf("00-%s-%s-01", traceID, spanID),
			"tracestate":                          eval.TracestateEntry,
			"baggage":                             fmt.Sprintf("%s=%s,team=alpha", eval.RoutingKeyMember, rk),
			"ot-baggage-" + eval.RoutingKeyMember: rk,
			eval.CanaryHeader:                     testID,
		},
	}
}

// ---- inbound senders ----

func (e *Env) sendHTTP(seed raw.Seeded, testID, forwardVia string) error {
	body, _ := json.Marshal(map[string]string{"test_id": testID, "forward_via": forwardVia})
	req, err := http.NewRequest("POST", e.RelayHTTP+"/relay", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range seed.Headers {
		req.Header.Set(k, v)
	}
	resp, err := e.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("relay http status %d", resp.StatusCode)
	}
	return nil
}

func (e *Env) sendGRPC(seed raw.Seeded, testID, forwardVia string) error {
	conn, err := grpc.NewClient(e.RelayGRPC, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
	pairs := []string{}
	for k, v := range seed.Headers {
		pairs = append(pairs, k, v)
	}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(pairs...))
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err = pb.NewRelayClient(conn).Handle(ctx, &pb.RelayRequest{TestId: testID, ForwardVia: forwardVia})
	return err
}

func (e *Env) sendKafka(seed raw.Seeded, testID, forwardVia string) error {
	value, _ := json.Marshal(map[string]string{"test_id": testID, "forward_via": forwardVia})
	rec := &kgo.Record{Topic: e.RelayTopic, Value: value}
	for k, v := range seed.Headers {
		rec.Headers = append(rec.Headers, kgo.RecordHeader{Key: k, Value: []byte(v)})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return e.Kafka.ProduceSync(ctx, rec).FirstErr()
}

func (e *Env) sendRabbit(seed raw.Seeded, testID, forwardVia string) error {
	body, _ := json.Marshal(map[string]string{"test_id": testID, "forward_via": forwardVia})
	headers := amqp.Table{}
	for k, v := range seed.Headers {
		headers[k] = v
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return e.Rabbit.PublishWithContext(ctx, "", e.RelayRabbitQueue, false, false, amqp.Publishing{
		ContentType: "application/json",
		Headers:     headers,
		Body:        body,
	})
}

func (e *Env) send(inbound string, seed raw.Seeded, testID, forwardVia string) error {
	switch inbound {
	case "http":
		return e.sendHTTP(seed, testID, forwardVia)
	case "grpc":
		return e.sendGRPC(seed, testID, forwardVia)
	case "kafka":
		return e.sendKafka(seed, testID, forwardVia)
	case "rabbitmq":
		return e.sendRabbit(seed, testID, forwardVia)
	}
	return fmt.Errorf("unknown inbound %q", inbound)
}

// ---- sink polling ----

func (e *Env) pollSink(testID string, timeout time.Duration) (*raw.Observed, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := e.HTTP.Get(e.SinkURL + "/observations?test_id=" + testID)
		if err == nil {
			var out struct {
				Observations []struct {
					Transport string              `json:"transport"`
					Headers   map[string][]string `json:"headers"`
				} `json:"observations"`
			}
			err = json.NewDecoder(resp.Body).Decode(&out)
			resp.Body.Close()
			if err == nil && len(out.Observations) > 0 {
				o := out.Observations[0]
				return &raw.Observed{Transport: o.Transport, Headers: o.Headers}, nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil, fmt.Errorf("no observation within %s", timeout)
}

// ---- orchestration ----

func (e *Env) waitRelayReady(timeout time.Duration) (map[string]any, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := e.HTTP.Get(e.RelayHTTP + "/info")
		if err == nil {
			var info map[string]any
			err = json.NewDecoder(resp.Body).Decode(&info)
			resp.Body.Close()
			if err == nil && resp.StatusCode == 200 {
				return info, nil
			}
		}
		lastErr = err
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("relay not ready within %s: %v", timeout, lastErr)
}

// warmupKafkaIn makes sure the relay's Kafka consumer group has joined and is
// forwarding before the timed kafka-inbound tests run.
func (e *Env) warmupKafkaIn() error {
	testID := fmt.Sprintf("warmup--%s--%s", e.Variant, randHex(4))
	seed := newSeed(testID)
	if err := e.sendKafka(seed, testID, "http"); err != nil {
		return err
	}
	_, err := e.pollSink(testID, 120*time.Second)
	return err
}

func main() {
	variant := flag.String("variant", "", "variant to test, e.g. otel-java")
	outDir := flag.String("out", "/results", "output directory")
	flag.Parse()
	if *variant == "" {
		log.Fatal("-variant is required")
	}

	brokers := os.Getenv("KAFKA_BROKERS")
	if brokers == "" {
		brokers = "kafka:9092"
	}
	sinkURL := os.Getenv("SINK_URL")
	if sinkURL == "" {
		sinkURL = "http://sink:9080"
	}
	relayHost := os.Getenv("RELAY_HOST")
	if relayHost == "" {
		relayHost = "relay-" + *variant
	}

	kcl, err := kgo.NewClient(
		kgo.SeedBrokers(strings.Split(brokers, ",")...),
		kgo.AllowAutoTopicCreation(),
		// No compression: some relay clients (e.g. kafkajs) cannot decode
		// snappy, which franz-go would otherwise prefer by default.
		kgo.ProducerBatchCompression(kgo.NoCompression()),
	)
	if err != nil {
		log.Fatalf("kafka client: %v", err)
	}
	defer kcl.Close()

	rabbitURL := os.Getenv("RABBIT_URL")
	if rabbitURL == "" {
		rabbitURL = "amqp://guest:guest@rabbitmq:5672/"
	}
	rconn, err := amqp.Dial(rabbitURL)
	if err != nil {
		log.Fatalf("rabbitmq dial: %v", err)
	}
	defer rconn.Close()
	rch, err := rconn.Channel()
	if err != nil {
		log.Fatalf("rabbitmq channel: %v", err)
	}

	env := &Env{
		Variant:          *variant,
		RelayHTTP:        "http://" + relayHost + ":8080",
		RelayGRPC:        relayHost + ":8081",
		RelayTopic:       "relay-in-" + *variant,
		RelayRabbitQueue: "relay-rmq-" + *variant,
		SinkURL:          sinkURL,
		Brokers:          strings.Split(brokers, ","),
		Kafka:            kcl,
		Rabbit:           rch,
		HTTP:             &http.Client{Timeout: 30 * time.Second},
		PollTimeout:      45 * time.Second,
	}

	// Declare the relay's inbound queue (idempotent; contract: non-durable,
	// non-exclusive, no auto-delete). Messages published before the relay
	// subscribes are held by the broker, so no warmup is needed for RabbitMQ.
	if _, err := rch.QueueDeclare(env.RelayRabbitQueue, false, false, false, false, nil); err != nil {
		log.Fatalf("rabbitmq queue declare: %v", err)
	}

	// Pre-create topics so consumer groups can join before first produce.
	adm := kadm.NewClient(kcl)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	_, err = adm.CreateTopics(ctx, 1, 1, nil, env.RelayTopic, "sink-in")
	cancel()
	if err != nil {
		log.Printf("topic create (non-fatal): %v", err)
	}

	log.Printf("waiting for relay %s ...", env.RelayHTTP)
	info, err := env.waitRelayReady(180 * time.Second)
	if err != nil {
		log.Fatalf("relay readiness: %v", err)
	}
	log.Printf("relay info: %v", info)

	parts := strings.SplitN(*variant, "-", 3)
	result := &raw.VariantResult{
		Variant: *variant,
		Tool:    parts[0],
		RanAt:   time.Now().UTC(),
		Info:    info,
	}
	if len(parts) > 1 {
		result.Language = parts[1]
	}
	if cfg, ok := info["config"].(map[string]any); ok {
		result.Config = cfg
	}

	warmKafka := false
	for _, in := range protocols {
		for _, out := range protocols {
			if in == "kafka" && !warmKafka {
				log.Printf("warming up kafka consumer ...")
				if err := env.warmupKafkaIn(); err != nil {
					log.Printf("kafka warmup failed: %v", err)
				}
				warmKafka = true
			}
			boundary := in + "->" + out
			testID := fmt.Sprintf("%s--%s-%s--%s", *variant, in, out, randHex(4))
			seed := newSeed(testID)
			tr := raw.TestResult{Boundary: boundary, TestID: testID, Seeded: &seed}
			log.Printf("test %-14s id=%s", boundary, testID)
			if err := env.send(in, seed, testID, out); err != nil {
				tr.Error = "send: " + err.Error()
			} else if obs, err := env.pollSink(testID, env.PollTimeout); err != nil {
				tr.Error = "observe: " + err.Error()
			} else {
				tr.Delivered = true
				tr.Observed = obs
				tr.Status = eval.Evaluate(obs.Headers, seed)
				log.Printf("  -> traceparent=%s tracestate=%s baggage=%s ot_baggage=%s dd=%s canary=%v",
					tr.Status.Traceparent, tr.Status.Tracestate, tr.Status.Baggage,
					tr.Status.OTBaggage, tr.Status.DatadogNative, tr.Status.CanaryLeak)
			}
			result.Tests = append(result.Tests, tr)
		}
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatalf("outdir: %v", err)
	}
	outPath := filepath.Join(*outDir, *variant+".json")
	b, _ := json.MarshalIndent(result, "", "  ")
	if err := os.WriteFile(outPath, b, 0o644); err != nil {
		log.Fatalf("write results: %v", err)
	}
	log.Printf("wrote %s", outPath)

	// Non-zero exit if the relay never worked at all (all sends failed).
	allFailed := true
	for _, t := range result.Tests {
		if t.Delivered {
			allFailed = false
			break
		}
	}
	if allFailed {
		log.Fatalf("all tests failed to deliver for %s", *variant)
	}
}
