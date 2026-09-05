// Command newrelic is the New Relic (go-agent v3) Go relay.
//
// Instrumentation (New Relic's documented approaches only):
//   - HTTP server via newrelic.WrapHandleFunc, outbound HTTP via
//     newrelic.NewRoundTripper (transaction taken from the request context).
//   - gRPC server/client via the nrgrpc integration interceptors.
//   - Kafka via Shopify/sarama with the nrsarama integration (nrsarama is
//     built against github.com/Shopify/sarama): the producer wrapper inserts
//     the transaction's distributed trace headers into the record headers;
//     the consumer handler starts a transaction per message. nrsarama does
//     not itself accept inbound trace headers, so the message handler applies
//     New Relic's documented manual message-queue pattern:
//     txn.AcceptDistributedTraceHeaders(newrelic.TransportKafka, headers).
//
// The app is created from the environment (NEW_RELIC_LICENSE_KEY etc.); if
// that fails (missing/invalid key) it falls back to a disabled no-op agent so
// the relay still serves /info.
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
	"time"

	"github.com/Shopify/sarama"
	"github.com/newrelic/go-agent/v3/integrations/nrgrpc"
	"github.com/newrelic/go-agent/v3/integrations/nrsarama"
	"github.com/newrelic/go-agent/v3/newrelic"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/signadot/context-propagation-matrix/gen/cpmatrix"
	"github.com/signadot/context-propagation-matrix/relays/go/internal/common"
)

type forwarder struct {
	httpClient *http.Client
	sink       cpmatrix.SinkClient
	producer   sarama.SyncProducer
}

func (f *forwarder) forward(ctx context.Context, testID, via string) error {
	switch via {
	case "http":
		body, err := json.Marshal(common.SinkMessage{TestID: testID})
		if err != nil {
			return err
		}
		// newrelic.NewRoundTripper reads the transaction from the request
		// context and inserts the distributed trace headers.
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
		// nrgrpc's client interceptor reads the transaction from ctx and
		// inserts the distributed trace headers into the metadata.
		_, err := f.sink.Deliver(ctx, &cpmatrix.SinkRequest{TestId: testID})
		return err
	case "kafka":
		value, err := json.Marshal(common.SinkMessage{TestID: testID})
		if err != nil {
			return err
		}
		// nrsarama's producer wrapper calls txn.InsertDistributedTraceHeaders
		// and copies the headers onto the Kafka record before sending.
		txn := newrelic.FromContext(ctx)
		return nrsarama.NewProducerWrapper(f.producer, txn).SendMessage(common.SinkTopic, nil, value)
	default:
		return fmt.Errorf("unknown forward_via: %q", via)
	}
}

func main() {
	appName := common.Env("NEW_RELIC_APP_NAME", "relay-"+common.Variant())
	app, err := newrelic.NewApplication(
		newrelic.ConfigAppName(appName),
		newrelic.ConfigDistributedTracerEnabled(true),
		newrelic.ConfigFromEnvironment(),
	)
	if err != nil {
		// Missing/invalid license: run with a disabled no-op agent so the
		// relay still starts and serves /info.
		log.Printf("newrelic agent disabled: %v", err)
		app, err = newrelic.NewApplication(
			newrelic.ConfigAppName(appName),
			newrelic.ConfigEnabled(false),
		)
		if err != nil {
			log.Fatalf("newrelic no-op application: %v", err)
		}
	} else if err := app.WaitForConnection(15 * time.Second); err != nil {
		// The agent needs a successful connect handshake before it creates
		// traces; without it the relay still works, just untraced.
		log.Printf("newrelic connect: %v", err)
	}

	cfg := sarama.NewConfig()
	cfg.Version = sarama.V2_1_0_0 // record headers require >= 0.11
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest
	cfg.Producer.Return.Successes = true

	producer, err := sarama.NewSyncProducer(common.KafkaBrokers(), cfg)
	if err != nil {
		log.Fatalf("kafka producer: %v", err)
	}

	conn, err := grpc.NewClient(common.SinkGRPC(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(nrgrpc.UnaryClientInterceptor),
	)
	if err != nil {
		log.Fatalf("grpc client: %v", err)
	}

	fwd := &forwarder{
		httpClient: &http.Client{Transport: newrelic.NewRoundTripper(nil)},
		sink:       cpmatrix.NewSinkClient(conn),
		producer:   producer,
	}

	// gRPC server on :8081.
	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(nrgrpc.UnaryServerInterceptor(app)))
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

	// Kafka consumer group with the nrsarama handler (one transaction per
	// message, transaction available from ctx in the message handler).
	group, err := sarama.NewConsumerGroup(common.KafkaBrokers(), common.GroupID(), cfg)
	if err != nil {
		log.Fatalf("kafka consumer group: %v", err)
	}
	handler := nrsarama.NewConsumerHandlerFromApp(app, common.RelayTopic(), common.GroupID(), cfg,
		func(ctx context.Context, msg *sarama.ConsumerMessage) {
			txn := newrelic.FromContext(ctx)
			// New Relic's documented manual pattern for message queues:
			// accept the distributed trace headers that arrived on the
			// record (nrsarama does not do this itself).
			hdrs := http.Header{}
			for _, h := range msg.Headers {
				if h != nil {
					hdrs.Add(string(h.Key), string(h.Value))
				}
			}
			txn.AcceptDistributedTraceHeaders(newrelic.TransportKafka, hdrs)

			var req common.RelayRequest
			if err := json.Unmarshal(msg.Value, &req); err != nil {
				log.Printf("kafka: bad message: %v", err)
				return
			}
			if err := fwd.forward(ctx, req.TestID, req.ForwardVia); err != nil {
				log.Printf("kafka: forward: %v", err)
			}
		})
	go func() {
		ctx := context.Background()
		for {
			if err := group.Consume(ctx, []string{common.RelayTopic()}, handler); err != nil {
				log.Printf("kafka consume: %v", err)
				time.Sleep(time.Second)
			}
		}
	}()

	agentVersion := common.ModuleVersion("github.com/newrelic/go-agent/v3")
	libs := map[string]string{
		"http": "newrelic.WrapHandleFunc + newrelic.NewRoundTripper (go-agent " + agentVersion + ")",
		"grpc": "nrgrpc " + common.ModuleVersion("github.com/newrelic/go-agent/v3/integrations/nrgrpc"),
		"kafka": "nrsarama " + common.ModuleVersion("github.com/newrelic/go-agent/v3/integrations/nrsarama") +
			" + manual AcceptDistributedTraceHeaders (Shopify/sarama " +
			common.ModuleVersion("github.com/Shopify/sarama") + ")",
	}

	// HTTP server on :8080.
	mux := http.NewServeMux()
	mux.HandleFunc(newrelic.WrapHandleFunc(app, "/relay", common.RelayHandler(fwd.forward)))
	mux.HandleFunc(newrelic.WrapHandleFunc(app, "/info", common.InfoHandler("github.com/newrelic/go-agent/v3", libs)))
	log.Printf("relay %s listening on :8080 (http), :8081 (grpc); consuming %s",
		common.Variant(), common.RelayTopic())
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("http serve: %v", err)
	}
}
