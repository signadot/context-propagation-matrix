// Command none is the baseline (no instrumentation) Go relay.
//
// It is the otel relay with every instrumentation touchpoint removed: plain
// net/http server and client, plain gRPC server and client, plain franz-go,
// plain amqp091-go. No tracing API is imported, no propagator exists, no
// header is read or written. Its purpose is to prove the harness's control
// expectation: with no agent or wrapper attached, nothing propagates, and
// therefore everything the other variants propagate is attributable to the
// tool under test rather than to the app, the frameworks, or the harness.
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

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/signadot/context-propagation-matrix/gen/cpmatrix"
	"github.com/signadot/context-propagation-matrix/relays/go/internal/common"
)

type forwarder struct {
	httpClient *http.Client
	sink       cpmatrix.SinkClient
	kafka      *kgo.Client
	rabbit     *amqp.Channel
}

func (f *forwarder) forward(ctx context.Context, testID, via string) error {
	switch via {
	case "http":
		body, err := json.Marshal(common.SinkMessage{TestID: testID})
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			common.SinkHTTP()+"/deliver", bytes.NewReader(body))
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
		value, err := json.Marshal(common.SinkMessage{TestID: testID})
		if err != nil {
			return err
		}
		return f.kafka.ProduceSync(ctx, &kgo.Record{
			Topic: common.SinkTopic,
			Value: value,
		}).FirstErr()
	case "rabbitmq":
		body, err := json.Marshal(common.SinkMessage{TestID: testID})
		if err != nil {
			return err
		}
		return common.RabbitPublish(ctx, f.rabbit, nil, body)
	default:
		return fmt.Errorf("unknown forward_via: %q", via)
	}
}

func main() {
	kafkaClient, err := kgo.NewClient(
		kgo.SeedBrokers(common.KafkaBrokers()...),
		kgo.ConsumeTopics(common.RelayTopic()),
		kgo.ConsumerGroup(common.GroupID()),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		log.Fatalf("kafka client: %v", err)
	}

	conn, err := grpc.NewClient(common.SinkGRPC(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("grpc client: %v", err)
	}

	_, rabbitCh, err := common.DialRabbit()
	if err != nil {
		log.Fatalf("rabbitmq: %v", err)
	}

	fwd := &forwarder{
		httpClient: http.DefaultClient,
		sink:       cpmatrix.NewSinkClient(conn),
		kafka:      kafkaClient,
		rabbit:     rabbitCh,
	}

	// gRPC server on :8081.
	grpcServer := grpc.NewServer()
	cpmatrix.RegisterRelayServer(grpcServer, &common.GRPCRelay{Forward: fwd.forward})
	lis, err := net.Listen("tcp", ":8081")
	if err != nil {
		log.Fatalf("grpc listen: %v", err)
	}
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("grpc serve: %v", err)
		}
	}()

	// Kafka consumer loop.
	go func() {
		ctx := context.Background()
		for {
			fetches := kafkaClient.PollFetches(ctx)
			if fetches.IsClientClosed() {
				return
			}
			fetches.EachError(func(topic string, partition int32, err error) {
				log.Printf("kafka fetch %s/%d: %v", topic, partition, err)
			})
			fetches.EachRecord(func(rec *kgo.Record) {
				var req common.RelayRequest
				if err := json.Unmarshal(rec.Value, &req); err != nil {
					log.Printf("kafka: bad message: %v", err)
					return
				}
				if err := fwd.forward(ctx, req.TestID, req.ForwardVia); err != nil {
					log.Printf("kafka: forward: %v", err)
				}
			})
		}
	}()

	// RabbitMQ consumer loop.
	go common.RabbitConsumeLoop(common.RabbitConsume, func(d amqp.Delivery) {
		var req common.RelayRequest
		if err := json.Unmarshal(d.Body, &req); err != nil {
			log.Printf("rabbitmq: bad message: %v", err)
			return
		}
		if err := fwd.forward(context.Background(), req.TestID, req.ForwardVia); err != nil {
			log.Printf("rabbitmq: forward: %v", err)
		}
	})

	libs := map[string]string{
		"http":     "net/http (plain)",
		"grpc":     "google.golang.org/grpc (plain)",
		"kafka":    "franz-go " + common.ModuleVersion("github.com/twmb/franz-go") + " (plain)",
		"rabbitmq": "amqp091-go " + common.ModuleVersion("github.com/rabbitmq/amqp091-go") + " (plain)",
	}

	// HTTP server on :8080.
	mux := http.NewServeMux()
	mux.HandleFunc("/relay", common.RelayHandler(fwd.forward))
	mux.HandleFunc("/info", common.InfoHandler("none", "", libs))
	log.Printf("relay %s listening on :8080 (http), :8081 (grpc); consuming %s (kafka), %s (rabbitmq)",
		common.Variant(), common.RelayTopic(), common.RelayRabbitQueue())
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("http serve: %v", err)
	}
}
