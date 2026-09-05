// Command elastic is the Elastic APM (go.elastic.co/apm/v2) Go relay.
//
// Instrumentation:
//   - HTTP server wrapped with apmhttp.Wrap, outbound HTTP through
//     apmhttp.WrapClient (injects traceparent + tracestate, plus the legacy
//     elastic-apm-traceparent header unless
//     ELASTIC_APM_USE_ELASTIC_TRACEPARENT_HEADER=false).
//   - gRPC server/client via the apmgrpc unary interceptors.
//   - Kafka (franz-go) and RabbitMQ (amqp091-go): Elastic APM Go has NO
//     official messaging instrumentation modules. Both legs apply the
//     agent's documented manual distributed-tracing pattern (APM guide
//     "Manual distributed tracing", Go tab): outbound formats the current
//     transaction's TraceContext with apmhttp.FormatTraceparentHeader (and
//     TraceContext.State.String() for tracestate) onto the documented
//     header-name constants exported by apmhttp; inbound parses with
//     apmhttp.ParseTraceparentHeader / ParseTracestateHeader and continues
//     the trace via apm.TransactionOptions{TraceContext: ...}. No header
//     value is ever hand-built.
//
// The default tracer configures itself from the environment
// (ELASTIC_APM_SERVER_URL, ...) with continue-on-error semantics; when the
// APM server is unreachable the background sender just buffers and drops
// events — transactions, sampling and therefore header injection are
// unaffected and the process never crashes.
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
	"strings"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.elastic.co/apm/module/apmgrpc/v2"
	"go.elastic.co/apm/module/apmhttp/v2"
	"go.elastic.co/apm/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/signadot/context-propagation-matrix/gen/cpmatrix"
	"github.com/signadot/context-propagation-matrix/relays/go/internal/common"
)

// traceHeaders renders the current transaction's TraceContext into the
// documented trace headers (w3c traceparent, legacy elastic-apm-traceparent,
// tracestate) using only apmhttp's documented helpers and header-name
// constants. Returns nil when there is no transaction in ctx.
func traceHeaders(ctx context.Context) map[string]string {
	tx := apm.TransactionFromContext(ctx)
	if tx == nil {
		return nil
	}
	tc := tx.TraceContext()
	traceparent := apmhttp.FormatTraceparentHeader(tc)
	h := map[string]string{
		apmhttp.W3CTraceparentHeader:     traceparent,
		apmhttp.ElasticTraceparentHeader: traceparent,
	}
	if ts := tc.State.String(); ts != "" {
		h[apmhttp.TracestateHeader] = ts
	}
	return h
}

// inboundTxOptions parses the documented trace headers from an inbound
// message into TransactionOptions per the documented manual pattern. get must
// return the header value for a name (matched case-insensitively — message
// header keys are raw bytes, unlike canonicalized HTTP headers). Zero options
// (parse failure or no header) start a new trace, as the agent does for an
// uninstrumented inbound request.
func inboundTxOptions(get func(name string) string) apm.TransactionOptions {
	var opts apm.TransactionOptions
	traceparent := get(apmhttp.W3CTraceparentHeader)
	if traceparent == "" {
		traceparent = get(apmhttp.ElasticTraceparentHeader)
	}
	tc, err := apmhttp.ParseTraceparentHeader(traceparent)
	if err != nil {
		return opts
	}
	if ts := get(apmhttp.TracestateHeader); ts != "" {
		tc.State, _ = apmhttp.ParseTracestateHeader(ts)
	}
	opts.TraceContext = tc
	return opts
}

// startMessagingTransaction starts a "messaging" transaction continuing the
// inbound trace context (documented StartTransactionOptions pattern) and
// returns a context carrying it for the forward call.
func startMessagingTransaction(name string, get func(name string) string) (context.Context, *apm.Transaction) {
	tx := apm.DefaultTracer().StartTransactionOptions(name, "messaging", inboundTxOptions(get))
	return apm.ContextWithTransaction(context.Background(), tx), tx
}

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
		// apmhttp.WrapClient reads the transaction from the request context
		// and injects traceparent/tracestate (+ legacy header).
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
		// apmgrpc's client interceptor injects the trace headers into the
		// outgoing metadata from the transaction in ctx.
		_, err := f.sink.Deliver(ctx, &cpmatrix.SinkRequest{TestId: testID})
		return err
	case "kafka":
		value, err := json.Marshal(common.SinkMessage{TestID: testID})
		if err != nil {
			return err
		}
		// No elastic Kafka module: documented manual pattern — format the
		// transaction's TraceContext onto the record headers.
		rec := &kgo.Record{Topic: common.SinkTopic, Value: value}
		for k, v := range traceHeaders(ctx) {
			rec.Headers = append(rec.Headers, kgo.RecordHeader{Key: k, Value: []byte(v)})
		}
		return f.kafka.ProduceSync(ctx, rec).FirstErr()
	case "rabbitmq":
		value, err := json.Marshal(common.SinkMessage{TestID: testID})
		if err != nil {
			return err
		}
		// No elastic RabbitMQ module: same documented manual pattern over
		// the AMQP header table.
		headers := amqp.Table{}
		for k, v := range traceHeaders(ctx) {
			headers[k] = v
		}
		return common.RabbitPublish(ctx, f.rabbit, headers, value)
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
		grpc.WithUnaryInterceptor(apmgrpc.NewUnaryClientInterceptor()),
	)
	if err != nil {
		log.Fatalf("grpc client: %v", err)
	}

	_, rabbitCh, err := common.DialRabbit()
	if err != nil {
		log.Fatalf("rabbitmq: %v", err)
	}

	fwd := &forwarder{
		httpClient: apmhttp.WrapClient(&http.Client{}),
		sink:       cpmatrix.NewSinkClient(conn),
		kafka:      kafkaClient,
		rabbit:     rabbitCh,
	}

	// gRPC server on :8081.
	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(apmgrpc.NewUnaryServerInterceptor()))
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

	// Kafka consumer loop: manual documented pattern — parse the record's
	// trace headers and continue the trace in a new messaging transaction.
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
				recCtx, tx := startMessagingTransaction(common.RelayTopic()+" process",
					func(name string) string {
						for _, h := range rec.Headers {
							if strings.EqualFold(h.Key, name) {
								return string(h.Value)
							}
						}
						return ""
					})
				defer tx.End()
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

	// RabbitMQ consumer loop: same manual pattern over the AMQP header table.
	go common.RabbitConsumeLoop(common.RabbitConsume, func(d amqp.Delivery) {
		ctx, tx := startMessagingTransaction(common.RelayRabbitQueue()+" process",
			func(name string) string {
				for k, v := range d.Headers {
					if strings.EqualFold(k, name) {
						if s, ok := v.(string); ok {
							return s
						}
					}
				}
				return ""
			})
		defer tx.End()
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
		"http": "apmhttp " + common.ModuleVersion("go.elastic.co/apm/module/apmhttp/v2") +
			" (Wrap + WrapClient)",
		"grpc": "apmgrpc " + common.ModuleVersion("go.elastic.co/apm/module/apmgrpc/v2") +
			" (unary interceptors)",
		"kafka": "franz-go " + common.ModuleVersion("github.com/twmb/franz-go") +
			" + manual apmhttp trace headers (no elastic messaging module)",
		"rabbitmq": "amqp091-go " + common.ModuleVersion("github.com/rabbitmq/amqp091-go") +
			" + manual apmhttp trace headers (no elastic messaging module)",
	}

	// HTTP server on :8080.
	mux := http.NewServeMux()
	mux.HandleFunc("/relay", common.RelayHandler(fwd.forward))
	mux.HandleFunc("/info", common.InfoHandler("elastic", "go.elastic.co/apm/v2", libs))
	log.Printf("relay %s listening on :8080 (http), :8081 (grpc); consuming %s (kafka), %s (rabbitmq)",
		common.Variant(), common.RelayTopic(), common.RelayRabbitQueue())
	if err := http.ListenAndServe(":8080", apmhttp.Wrap(mux)); err != nil {
		log.Fatalf("http serve: %v", err)
	}
}
