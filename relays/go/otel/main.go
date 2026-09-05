// Command otel is the OpenTelemetry Go relay.
//
// Instrumentation (all official contrib / plugin libraries, no manual header
// handling):
//   - HTTP server wrapped with otelhttp.NewHandler, outbound HTTP through
//     otelhttp.NewTransport.
//   - gRPC server/client via otelgrpc stats handlers.
//   - Kafka via franz-go with the kotel plugin; on consume the documented
//     tracer.WithProcessSpan(record) pattern yields the context passed to the
//     outbound call.
//   - RabbitMQ via amqp091-go. There is no official OTel Go contrib for
//     amqp091, so this leg applies the documented propagator pattern
//     directly: otel.GetTextMapPropagator().Inject on produce and .Extract
//     on consume, over a TextMapCarrier adapter for the AMQP header table,
//     with producer/consumer spans from the tracer (mirroring what kotel
//     does for Kafka).
//
// The global propagator is TraceContext+Baggage. The SDK TracerProvider has
// no exporter: spans are created (so fresh span ids are injected downstream)
// but never leave the process.
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
	"github.com/twmb/franz-go/plugin/kotel"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/signadot/context-propagation-matrix/gen/cpmatrix"
	"github.com/signadot/context-propagation-matrix/relays/go/internal/common"
)

// amqpHeaderCarrier adapts an AMQP header table to OTel's
// propagation.TextMapCarrier so the global propagator can inject/extract on
// RabbitMQ messages. Only string-typed table values participate.
type amqpHeaderCarrier amqp.Table

func (c amqpHeaderCarrier) Get(key string) string {
	if s, ok := amqp.Table(c)[key].(string); ok {
		return s
	}
	return ""
}

func (c amqpHeaderCarrier) Set(key, value string) { amqp.Table(c)[key] = value }

func (c amqpHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

type forwarder struct {
	httpClient *http.Client
	sink       cpmatrix.SinkClient
	kafka      *kgo.Client
	rabbit     *amqp.Channel
	tracer     trace.Tracer
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
		// kotel's produce hook injects the propagation headers into the
		// record, using ctx as the parent context.
		return f.kafka.ProduceSync(ctx, &kgo.Record{
			Topic: common.SinkTopic,
			Value: value,
		}).FirstErr()
	case "rabbitmq":
		body, err := json.Marshal(common.SinkMessage{TestID: testID})
		if err != nil {
			return err
		}
		// No official amqp091 contrib exists: start a producer span and let
		// the global propagator inject into the AMQP header table (the
		// documented otel.GetTextMapPropagator() carrier pattern).
		ctx, span := f.tracer.Start(ctx, common.SinkRabbitQueue()+" publish",
			trace.WithSpanKind(trace.SpanKindProducer))
		defer span.End()
		headers := amqp.Table{}
		otel.GetTextMapPropagator().Inject(ctx, amqpHeaderCarrier(headers))
		return common.RabbitPublish(ctx, f.rabbit, headers, body)
	default:
		return fmt.Errorf("unknown forward_via: %q", via)
	}
}

func main() {
	tp := sdktrace.NewTracerProvider() // no exporter registered
	otel.SetTracerProvider(tp)
	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{})
	otel.SetTextMapPropagator(prop)

	kotelTracer := kotel.NewTracer(
		kotel.TracerProvider(tp),
		kotel.TracerPropagator(prop),
	)
	kotelService := kotel.NewKotel(kotel.WithTracer(kotelTracer))

	kafkaClient, err := kgo.NewClient(
		kgo.SeedBrokers(common.KafkaBrokers()...),
		kgo.ConsumeTopics(common.RelayTopic()),
		kgo.ConsumerGroup(common.GroupID()),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.WithHooks(kotelService.Hooks()...),
	)
	if err != nil {
		log.Fatalf("kafka client: %v", err)
	}

	conn, err := grpc.NewClient(common.SinkGRPC(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		log.Fatalf("grpc client: %v", err)
	}

	_, rabbitCh, err := common.DialRabbit()
	if err != nil {
		log.Fatalf("rabbitmq: %v", err)
	}

	fwd := &forwarder{
		httpClient: &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)},
		sink:       cpmatrix.NewSinkClient(conn),
		kafka:      kafkaClient,
		rabbit:     rabbitCh,
		tracer:     tp.Tracer("relay/rabbitmq"),
	}

	// gRPC server on :8081.
	grpcServer := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()))
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
				// kotel's documented consume pattern: start a process span
				// from the record and use its context for follow-up work.
				recCtx, span := kotelTracer.WithProcessSpan(rec)
				defer span.End()
				var req common.RelayRequest
				if err := json.Unmarshal(rec.Value, &req); err != nil {
					log.Printf("kafka: bad message: %v", err)
					return
				}
				if err := fwd.forward(recCtx, req.TestID, req.ForwardVia); err != nil {
					log.Printf("kafka: forward: %v", err)
				}
			})
		}
	}()

	// RabbitMQ consumer loop: extract with the global propagator, then start
	// a consumer span so the forward call has a current context with fresh
	// span ids (mirroring kotel's process-span pattern for Kafka).
	go common.RabbitConsumeLoop(common.RabbitConsume, func(d amqp.Delivery) {
		ctx := otel.GetTextMapPropagator().Extract(context.Background(), amqpHeaderCarrier(d.Headers))
		ctx, span := fwd.tracer.Start(ctx, common.RelayRabbitQueue()+" process",
			trace.WithSpanKind(trace.SpanKindConsumer))
		defer span.End()
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
		"http": "otelhttp " + common.ModuleVersion("go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"),
		"grpc": "otelgrpc " + common.ModuleVersion("go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"),
		"kafka": "franz-go " + common.ModuleVersion("github.com/twmb/franz-go") +
			" + kotel " + common.ModuleVersion("github.com/twmb/franz-go/plugin/kotel"),
		"rabbitmq": "amqp091-go " + common.ModuleVersion("github.com/rabbitmq/amqp091-go") +
			" (manual otel.GetTextMapPropagator inject/extract; no official contrib)",
	}

	// HTTP server on :8080.
	mux := http.NewServeMux()
	mux.HandleFunc("/relay", common.RelayHandler(fwd.forward))
	mux.HandleFunc("/info", common.InfoHandler("otel", "go.opentelemetry.io/otel", libs))
	log.Printf("relay %s listening on :8080 (http), :8081 (grpc); consuming %s (kafka), %s (rabbitmq)",
		common.Variant(), common.RelayTopic(), common.RelayRabbitQueue())
	if err := http.ListenAndServe(":8080", otelhttp.NewHandler(mux, "relay")); err != nil {
		log.Fatalf("http serve: %v", err)
	}
}
