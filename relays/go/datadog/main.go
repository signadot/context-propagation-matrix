// Command datadog is the Datadog (dd-trace-go v2) Go relay.
//
// Instrumentation (all official contrib packages, no manual header handling):
//   - HTTP server via contrib/net/http/v2 NewServeMux, outbound HTTP via
//     WrapClient.
//   - gRPC server/client via contrib/google.golang.org/grpc/v2 interceptors.
//   - Kafka via IBM/sarama with contrib/IBM/sarama/v2: WrapSyncProducer and
//     WrapConsumerGroupHandler. sarama's SendMessage has no context
//     parameter, so the produce span is parented by injecting the active span
//     context into the message with the contrib's ProducerMessageCarrier
//     (exactly where the wrapped producer looks for its parent). On consume,
//     the documented pattern extracts the message's span context with
//     tracer.Extract(NewConsumerMessageCarrier(msg)) and starts a child span
//     whose context is used for the outbound call.
//   - RabbitMQ via amqp091-go. dd-trace-go has NO contrib for amqp091, so
//     this leg uses the vendor's documented propagator API directly:
//     tracer.Inject into (and tracer.Extract from, with tracer.ChildOf) a
//     TextMapWriter/TextMapReader carrier over the AMQP header table — the
//     same carrier pattern the sarama contrib documents for its message
//     carriers.
//
// tracer.Start() reads all configuration (DD_TRACE_PROPAGATION_STYLE, ...)
// from the environment.
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

	saramatrace "github.com/DataDog/dd-trace-go/contrib/IBM/sarama/v2"
	grpctrace "github.com/DataDog/dd-trace-go/contrib/google.golang.org/grpc/v2"
	httptrace "github.com/DataDog/dd-trace-go/contrib/net/http/v2"
	"github.com/DataDog/dd-trace-go/v2/ddtrace/tracer"
	"github.com/IBM/sarama"
	amqp "github.com/rabbitmq/amqp091-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/signadot/context-propagation-matrix/gen/cpmatrix"
	"github.com/signadot/context-propagation-matrix/relays/go/internal/common"
)

// amqpHeaderCarrier implements tracer.TextMapWriter and tracer.TextMapReader
// over an AMQP header table, mirroring the sarama contrib's message carriers
// (dd-trace-go has no amqp091 contrib). Only string-typed table values
// participate in extraction.
type amqpHeaderCarrier amqp.Table

func (c amqpHeaderCarrier) Set(key, val string) { amqp.Table(c)[key] = val }

func (c amqpHeaderCarrier) ForeachKey(handler func(key, val string) error) error {
	for k, v := range c {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if err := handler(k, s); err != nil {
			return err
		}
	}
	return nil
}

type forwarder struct {
	httpClient *http.Client
	sink       cpmatrix.SinkClient
	producer   sarama.SyncProducer
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
		msg := &sarama.ProducerMessage{
			Topic: common.SinkTopic,
			Value: sarama.ByteEncoder(value),
		}
		// sarama.SyncProducer.SendMessage takes no context, so hand the
		// active span context to the traced producer via the contrib
		// carrier; the wrapped producer extracts it, parents the produce
		// span on it and re-injects the produce span's context.
		if span, ok := tracer.SpanFromContext(ctx); ok {
			if err := tracer.Inject(span.Context(), saramatrace.NewProducerMessageCarrier(msg)); err != nil {
				log.Printf("kafka: inject: %v", err)
			}
		}
		_, _, err = f.producer.SendMessage(msg)
		return err
	case "rabbitmq":
		value, err := json.Marshal(common.SinkMessage{TestID: testID})
		if err != nil {
			return err
		}
		// No amqp091 contrib: start a produce span from ctx and inject its
		// span context into the AMQP header table with the vendor's
		// propagator API (what the sarama contrib's wrapped producer does
		// internally for Kafka).
		span, ctx := tracer.StartSpanFromContext(ctx, "rabbitmq.produce",
			tracer.ResourceName(common.SinkRabbitQueue()))
		defer span.Finish()
		headers := amqp.Table{}
		if err := tracer.Inject(span.Context(), amqpHeaderCarrier(headers)); err != nil {
			log.Printf("rabbitmq: inject: %v", err)
		}
		return common.RabbitPublish(ctx, f.rabbit, headers, value)
	default:
		return fmt.Errorf("unknown forward_via: %q", via)
	}
}

// kafkaHandler is the application-side sarama.ConsumerGroupHandler; it is
// wrapped with saramatrace.WrapConsumerGroupHandler in main.
type kafkaHandler struct {
	fwd *forwarder
}

func (h *kafkaHandler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (h *kafkaHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (h *kafkaHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		h.handle(msg)
		sess.MarkMessage(msg, "")
	}
	return nil
}

func (h *kafkaHandler) handle(msg *sarama.ConsumerMessage) {
	// Documented pattern for continuing the trace from a consumed message:
	// extract the span context from the message and start a child span.
	// (WrapConsumerGroupHandler has already traced the consume and
	// re-injected its span context into the message headers.)
	opts := []tracer.StartSpanOption{tracer.ResourceName("relay.process")}
	if spanctx, err := tracer.Extract(saramatrace.NewConsumerMessageCarrier(msg)); err == nil {
		opts = append(opts, tracer.ChildOf(spanctx))
	}
	span := tracer.StartSpan("kafka.process", opts...)
	defer span.Finish()
	ctx := tracer.ContextWithSpan(context.Background(), span)

	var req common.RelayRequest
	if err := json.Unmarshal(msg.Value, &req); err != nil {
		log.Printf("kafka: bad message: %v", err)
		return
	}
	if err := h.fwd.forward(ctx, req.TestID, req.ForwardVia); err != nil {
		log.Printf("kafka: forward: %v", err)
	}
}

func main() {
	if err := tracer.Start(); err != nil {
		log.Printf("datadog tracer start: %v", err)
	}
	defer tracer.Stop()

	cfg := sarama.NewConfig()
	cfg.Version = sarama.V2_1_0_0 // record headers require >= 0.11
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest
	cfg.Producer.Return.Successes = true

	producer, err := sarama.NewSyncProducer(common.KafkaBrokers(), cfg)
	if err != nil {
		log.Fatalf("kafka producer: %v", err)
	}
	producer = saramatrace.WrapSyncProducer(cfg, producer)

	conn, err := grpc.NewClient(common.SinkGRPC(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(grpctrace.UnaryClientInterceptor()),
	)
	if err != nil {
		log.Fatalf("grpc client: %v", err)
	}

	_, rabbitCh, err := common.DialRabbit()
	if err != nil {
		log.Fatalf("rabbitmq: %v", err)
	}

	fwd := &forwarder{
		httpClient: httptrace.WrapClient(&http.Client{}),
		sink:       cpmatrix.NewSinkClient(conn),
		producer:   producer,
		rabbit:     rabbitCh,
	}

	// gRPC server on :8081.
	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(grpctrace.UnaryServerInterceptor()))
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

	// Kafka consumer group.
	group, err := sarama.NewConsumerGroup(common.KafkaBrokers(), common.GroupID(), cfg)
	if err != nil {
		log.Fatalf("kafka consumer group: %v", err)
	}
	handler := saramatrace.WrapConsumerGroupHandler(&kafkaHandler{fwd: fwd},
		saramatrace.WithGroupID(common.GroupID()))
	go func() {
		ctx := context.Background()
		for {
			if err := group.Consume(ctx, []string{common.RelayTopic()}, handler); err != nil {
				log.Printf("kafka consume: %v", err)
				time.Sleep(time.Second)
			}
		}
	}()

	// RabbitMQ consumer loop: documented consume pattern (as for the sarama
	// contrib) — extract the remote span context from the message headers and
	// start a child span whose context is used for the outbound call.
	go common.RabbitConsumeLoop(common.RabbitConsume, func(d amqp.Delivery) {
		opts := []tracer.StartSpanOption{tracer.ResourceName("relay.process")}
		if spanctx, err := tracer.Extract(amqpHeaderCarrier(d.Headers)); err == nil {
			opts = append(opts, tracer.ChildOf(spanctx))
		}
		span := tracer.StartSpan("rabbitmq.process", opts...)
		defer span.Finish()
		ctx := tracer.ContextWithSpan(context.Background(), span)
		var req common.RelayRequest
		if err := json.Unmarshal(d.Body, &req); err != nil {
			log.Printf("rabbitmq: bad message: %v", err)
			return
		}
		if err := fwd.forward(ctx, req.TestID, req.ForwardVia); err != nil {
			log.Printf("rabbitmq: forward: %v", err)
		}
	})

	libs := map[string]string{
		"http": "dd contrib net/http " + common.ModuleVersion("github.com/DataDog/dd-trace-go/contrib/net/http/v2"),
		"grpc": "dd contrib google.golang.org/grpc " + common.ModuleVersion("github.com/DataDog/dd-trace-go/contrib/google.golang.org/grpc/v2"),
		"kafka": "dd contrib IBM/sarama " + common.ModuleVersion("github.com/DataDog/dd-trace-go/contrib/IBM/sarama/v2") +
			" (sarama " + common.ModuleVersion("github.com/IBM/sarama") + ")",
		"rabbitmq": "amqp091-go " + common.ModuleVersion("github.com/rabbitmq/amqp091-go") +
			" (manual tracer.Inject/Extract carrier; no dd contrib)",
	}

	// HTTP server on :8080 via the traced ServeMux.
	mux := httptrace.NewServeMux()
	mux.HandleFunc("/relay", common.RelayHandler(fwd.forward))
	mux.HandleFunc("/info", common.InfoHandler("datadog", "github.com/DataDog/dd-trace-go/v2", libs))
	log.Printf("relay %s listening on :8080 (http), :8081 (grpc); consuming %s (kafka), %s (rabbitmq)",
		common.Variant(), common.RelayTopic(), common.RelayRabbitQueue())
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("http serve: %v", err)
	}
}
