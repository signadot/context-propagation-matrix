// Command sink is the uninstrumented recorder at the end of every test hop.
// It listens on HTTP, gRPC and Kafka and records, verbatim, the headers /
// metadata / record headers that arrive with each delivery.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	pb "github.com/signadot/context-propagation-matrix/gen/cpmatrix"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type Observation struct {
	TestID     string              `json:"test_id"`
	Transport  string              `json:"transport"` // http | grpc | kafka
	Headers    map[string][]string `json:"headers"`
	ReceivedAt time.Time           `json:"received_at"`
}

type Store struct {
	mu   sync.Mutex
	byID map[string][]Observation
	f    *os.File
}

func NewStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &Store{byID: map[string][]Observation{}, f: f}, nil
}

func (s *Store) Add(o Observation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[o.TestID] = append(s.byID[o.TestID], o)
	b, _ := json.Marshal(o)
	fmt.Fprintf(s.f, "%s\n", b)
	log.Printf("observation test_id=%q transport=%s headers=%d", o.TestID, o.Transport, len(o.Headers))
}

func (s *Store) Get(testID string) []Observation {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Observation, len(s.byID[testID]))
	copy(out, s.byID[testID])
	return out
}

// ---- HTTP ----

type deliverBody struct {
	TestID string `json:"test_id"`
}

func httpServer(store *Store) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /deliver", func(w http.ResponseWriter, r *http.Request) {
		var body deliverBody
		_ = json.NewDecoder(r.Body).Decode(&body)
		headers := map[string][]string{}
		for k, vs := range r.Header {
			headers[strings.ToLower(k)] = vs
		}
		store.Add(Observation{
			TestID:     body.TestID,
			Transport:  "http",
			Headers:    headers,
			ReceivedAt: time.Now().UTC(),
		})
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"recorded"}`)
	})
	mux.HandleFunc("GET /observations", func(w http.ResponseWriter, r *http.Request) {
		obs := store.Get(r.URL.Query().Get("test_id"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"observations": obs})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	return &http.Server{Addr: ":9080", Handler: mux}
}

// ---- gRPC ----

type sinkServer struct {
	pb.UnimplementedSinkServer
	store *Store
}

func (s *sinkServer) Deliver(ctx context.Context, req *pb.SinkRequest) (*pb.SinkResponse, error) {
	headers := map[string][]string{}
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		for k, vs := range md {
			k = strings.ToLower(k)
			if strings.HasSuffix(k, "-bin") {
				enc := make([]string, len(vs))
				for i, v := range vs {
					enc[i] = "base64:" + base64.StdEncoding.EncodeToString([]byte(v))
				}
				headers[k] = enc
			} else {
				headers[k] = vs
			}
		}
	}
	s.store.Add(Observation{
		TestID:     req.GetTestId(),
		Transport:  "grpc",
		Headers:    headers,
		ReceivedAt: time.Now().UTC(),
	})
	return &pb.SinkResponse{Status: "recorded"}, nil
}

// ---- Kafka ----

func kafkaConsume(store *Store, brokers string) {
	for {
		cl, err := kgo.NewClient(
			kgo.SeedBrokers(strings.Split(brokers, ",")...),
			kgo.ConsumerGroup("sink"),
			kgo.ConsumeTopics("sink-in"),
			kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
			kgo.AllowAutoTopicCreation(),
		)
		if err != nil {
			log.Printf("kafka client error: %v (retrying in 3s)", err)
			time.Sleep(3 * time.Second)
			continue
		}
		log.Printf("kafka consumer started (brokers=%s topic=sink-in)", brokers)
		for {
			fetches := cl.PollFetches(context.Background())
			if fetches.IsClientClosed() {
				break
			}
			for _, ferr := range fetches.Errors() {
				log.Printf("kafka fetch error: topic=%s err=%v", ferr.Topic, ferr.Err)
			}
			fetches.EachRecord(func(rec *kgo.Record) {
				var body deliverBody
				_ = json.Unmarshal(rec.Value, &body)
				headers := map[string][]string{}
				for _, h := range rec.Headers {
					k := strings.ToLower(h.Key)
					v := string(h.Value)
					if !utf8.ValidString(v) {
						v = "base64:" + base64.StdEncoding.EncodeToString(h.Value)
					}
					headers[k] = append(headers[k], v)
				}
				store.Add(Observation{
					TestID:     body.TestID,
					Transport:  "kafka",
					Headers:    headers,
					ReceivedAt: time.Now().UTC(),
				})
			})
		}
		cl.Close()
		time.Sleep(3 * time.Second)
	}
}

func main() {
	dataFile := os.Getenv("SINK_DATA_FILE")
	if dataFile == "" {
		dataFile = "/data/observations.jsonl"
	}
	brokers := os.Getenv("KAFKA_BROKERS")
	if brokers == "" {
		brokers = "kafka:9092"
	}

	store, err := NewStore(dataFile)
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	// gRPC
	lis, err := net.Listen("tcp", ":9081")
	if err != nil {
		log.Fatalf("grpc listen: %v", err)
	}
	gs := grpc.NewServer()
	pb.RegisterSinkServer(gs, &sinkServer{store: store})
	go func() {
		log.Printf("grpc listening on :9081")
		if err := gs.Serve(lis); err != nil {
			log.Fatalf("grpc serve: %v", err)
		}
	}()

	// Kafka
	go kafkaConsume(store, brokers)

	// HTTP (foreground)
	log.Printf("http listening on :9080")
	srv := httpServer(store)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("http serve: %v", err)
	}
}
