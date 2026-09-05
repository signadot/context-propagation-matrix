// Command driver runs the 9 protocol-boundary tests against one relay
// variant, seeding context propagation headers on the inbound side and
// evaluating what the sink observed on the outbound side.
//
// The driver is deliberately uninstrumented: headers are seeded by hand.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	pb "github.com/signadot/context-propagation-matrix/gen/cpmatrix"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

var protocols = []string{"http", "grpc", "kafka"}

type Seeded struct {
	TraceID    string            `json:"trace_id"`
	SpanID     string            `json:"span_id"`
	RoutingKey string            `json:"routing_key"`
	Headers    map[string]string `json:"headers"`
}

type Observed struct {
	Transport string              `json:"transport"`
	Headers   map[string][]string `json:"headers"`
}

type Status struct {
	Traceparent   string `json:"traceparent"`
	Tracestate    string `json:"tracestate"`
	Baggage       string `json:"baggage"`
	OTBaggage     string `json:"ot_baggage"`
	DatadogNative string `json:"datadog_native"`
	CanaryLeak    bool   `json:"canary_leak"`
}

type TestResult struct {
	Boundary  string    `json:"boundary"`
	TestID    string    `json:"test_id"`
	Delivered bool      `json:"delivered"`
	Seeded    Seeded    `json:"seeded"`
	Observed  *Observed `json:"observed,omitempty"`
	Status    *Status   `json:"status,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type VariantResult struct {
	Variant  string         `json:"variant"`
	Tool     string         `json:"tool"`
	Language string         `json:"language"`
	Config   map[string]any `json:"config,omitempty"`
	Info     map[string]any `json:"info,omitempty"`
	RanAt    time.Time      `json:"ran_at"`
	Tests    []TestResult   `json:"tests"`
}

type Env struct {
	Variant     string
	RelayHTTP   string // http://relay-{variant}:8080
	RelayGRPC   string // relay-{variant}:8081
	RelayTopic  string // relay-in-{variant}
	SinkURL     string // http://sink:9080
	Brokers     []string
	Kafka       *kgo.Client
	HTTP        *http.Client
	PollTimeout time.Duration
}

func randHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func newSeed(testID string) Seeded {
	traceID := randHex(16)
	spanID := randHex(8)
	rk := "rk-" + randHex(4)
	return Seeded{
		TraceID:    traceID,
		SpanID:     spanID,
		RoutingKey: rk,
		Headers: map[string]string{
			"traceparent":               fmt.Sprintf("00-%s-%s-01", traceID, spanID),
			"tracestate":                "sdtest=v1",
			"baggage":                   fmt.Sprintf("sd-routing-key=%s,team=alpha", rk),
			"ot-baggage-sd-routing-key": rk,
			"x-sd-canary":               testID,
		},
	}
}

// ---- inbound senders ----

func (e *Env) sendHTTP(seed Seeded, testID, forwardVia string) error {
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

func (e *Env) sendGRPC(seed Seeded, testID, forwardVia string) error {
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

func (e *Env) sendKafka(seed Seeded, testID, forwardVia string) error {
	value, _ := json.Marshal(map[string]string{"test_id": testID, "forward_via": forwardVia})
	rec := &kgo.Record{Topic: e.RelayTopic, Value: value}
	for k, v := range seed.Headers {
		rec.Headers = append(rec.Headers, kgo.RecordHeader{Key: k, Value: []byte(v)})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return e.Kafka.ProduceSync(ctx, rec).FirstErr()
}

func (e *Env) send(inbound string, seed Seeded, testID, forwardVia string) error {
	switch inbound {
	case "http":
		return e.sendHTTP(seed, testID, forwardVia)
	case "grpc":
		return e.sendGRPC(seed, testID, forwardVia)
	case "kafka":
		return e.sendKafka(seed, testID, forwardVia)
	}
	return fmt.Errorf("unknown inbound %q", inbound)
}

// ---- sink polling ----

func (e *Env) pollSink(testID string, timeout time.Duration) (*Observed, error) {
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
				return &Observed{Transport: o.Transport, Headers: o.Headers}, nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil, fmt.Errorf("no observation within %s", timeout)
}

// ---- evaluation ----

func first(h map[string][]string, key string) (string, bool) {
	vs, ok := h[key]
	if !ok || len(vs) == 0 {
		return "", false
	}
	return vs[0], true
}

func evalTraceparent(h map[string][]string, seed Seeded) string {
	v, ok := first(h, "traceparent")
	if !ok {
		return "absent"
	}
	parts := strings.Split(strings.TrimSpace(v), "-")
	if len(parts) >= 3 && strings.EqualFold(parts[1], seed.TraceID) {
		return "same_trace"
	}
	return "new_trace"
}

func evalTracestate(h map[string][]string, seed Seeded) string {
	v, ok := first(h, "tracestate")
	if !ok {
		return "absent"
	}
	for _, entry := range strings.Split(v, ",") {
		if strings.TrimSpace(entry) == "sdtest=v1" {
			return "preserved"
		}
	}
	return "present_entry_lost"
}

func parseBaggage(v string) map[string]string {
	out := map[string]string{}
	for _, member := range strings.Split(v, ",") {
		member = strings.TrimSpace(member)
		// strip properties (";" suffix)
		if i := strings.Index(member, ";"); i >= 0 {
			member = member[:i]
		}
		kv := strings.SplitN(member, "=", 2)
		if len(kv) == 2 {
			out[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return out
}

func evalBaggage(h map[string][]string, seed Seeded) string {
	v, ok := first(h, "baggage")
	if !ok {
		return "absent"
	}
	items := parseBaggage(v)
	if items["sd-routing-key"] == seed.RoutingKey {
		return "propagated"
	}
	return "partial"
}

func evalOTBaggage(h map[string][]string, seed Seeded) string {
	v, ok := first(h, "ot-baggage-sd-routing-key")
	if ok && v == seed.RoutingKey {
		return "propagated"
	}
	if ok {
		return "partial"
	}
	return "absent"
}

func evalDatadogNative(h map[string][]string, seed Seeded) string {
	v, ok := first(h, "x-datadog-trace-id")
	if !ok {
		return "absent"
	}
	// x-datadog-trace-id is the decimal low 64 bits of the (128-bit) trace id
	low := seed.TraceID[16:]
	b, err := hex.DecodeString(low)
	if err != nil || len(b) != 8 {
		return "present_new_trace"
	}
	want := strconv.FormatUint(binary.BigEndian.Uint64(b), 10)
	if strings.TrimSpace(v) == want {
		return "present_same_trace"
	}
	return "present_new_trace"
}

func evaluate(obs *Observed, seed Seeded) *Status {
	_, canary := first(obs.Headers, "x-sd-canary")
	return &Status{
		Traceparent:   evalTraceparent(obs.Headers, seed),
		Tracestate:    evalTracestate(obs.Headers, seed),
		Baggage:       evalBaggage(obs.Headers, seed),
		OTBaggage:     evalOTBaggage(obs.Headers, seed),
		DatadogNative: evalDatadogNative(obs.Headers, seed),
		CanaryLeak:    canary,
	}
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

	env := &Env{
		Variant:     *variant,
		RelayHTTP:   "http://" + relayHost + ":8080",
		RelayGRPC:   relayHost + ":8081",
		RelayTopic:  "relay-in-" + *variant,
		SinkURL:     sinkURL,
		Brokers:     strings.Split(brokers, ","),
		Kafka:       kcl,
		HTTP:        &http.Client{Timeout: 30 * time.Second},
		PollTimeout: 45 * time.Second,
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
	result := &VariantResult{
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
			tr := TestResult{Boundary: boundary, TestID: testID, Seeded: seed}
			log.Printf("test %-14s id=%s", boundary, testID)
			if err := env.send(in, seed, testID, out); err != nil {
				tr.Error = "send: " + err.Error()
			} else if obs, err := env.pollSink(testID, env.PollTimeout); err != nil {
				tr.Error = "observe: " + err.Error()
			} else {
				tr.Delivered = true
				tr.Observed = obs
				tr.Status = evaluate(obs, seed)
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
