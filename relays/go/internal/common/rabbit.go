package common

// RabbitMQ plumbing shared by the Go relays. Everything in this file is
// instrumentation-free: dialing, idempotent queue declaration, raw publishing
// and the consume loop. Injecting/extracting propagation headers on the AMQP
// header table is exclusively the per-tool main's job (via the vendor's
// documented propagation API).

import (
	"context"
	"log"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// RabbitURL returns the broker URL.
func RabbitURL() string { return Env("RABBIT_URL", "amqp://guest:guest@rabbitmq:5672/") }

// RelayRabbitQueue returns the queue this relay consumes.
func RelayRabbitQueue() string { return Env("RELAY_RABBIT_QUEUE", "relay-rmq-"+Variant()) }

// SinkRabbitQueue returns the queue the sink consumes; the relay publishes to
// it via the default exchange (routing key = queue name).
func SinkRabbitQueue() string { return Env("SINK_RABBIT_QUEUE", "sink-rmq") }

// DialRabbit connects to RABBIT_URL, opens a channel and idempotently
// declares both queues with the contract's properties: durable=false,
// auto-delete=false, exclusive=false. (The driver declares the relay queue
// too; declaring defensively on startup is part of the contract.)
func DialRabbit() (*amqp.Connection, *amqp.Channel, error) {
	conn, err := amqp.Dial(RabbitURL())
	if err != nil {
		return nil, nil, err
	}
	ch, err := conn.Channel()
	if err == nil {
		for _, q := range []string{RelayRabbitQueue(), SinkRabbitQueue()} {
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

// RabbitPublish publishes body to the sink queue via the default exchange.
// headers is the AMQP header table already populated by the caller's
// instrumentation (nil when there is nothing to propagate).
func RabbitPublish(ctx context.Context, ch *amqp.Channel, headers amqp.Table, body []byte) error {
	return ch.PublishWithContext(ctx, "", SinkRabbitQueue(), false, false, amqp.Publishing{
		ContentType: "application/json",
		Headers:     headers,
		Body:        body,
	})
}

// RabbitConsume starts a plain auto-ack consumer on the relay queue. It is
// the default `consume` argument for RabbitConsumeLoop; a tool main can
// substitute a traced Consume implementation instead.
func RabbitConsume(ch *amqp.Channel) (<-chan amqp.Delivery, error) {
	return ch.Consume(RelayRabbitQueue(), "", true, false, false, false, nil)
}

// RabbitConsumeLoop consumes the relay queue forever, calling handle for
// every delivery and redialing on connection loss. consume starts the
// consumer on a freshly dialed channel. Run in a goroutine.
func RabbitConsumeLoop(consume func(*amqp.Channel) (<-chan amqp.Delivery, error), handle func(d amqp.Delivery)) {
	for {
		conn, ch, err := DialRabbit()
		if err != nil {
			log.Printf("rabbitmq dial: %v (retrying in 3s)", err)
			time.Sleep(3 * time.Second)
			continue
		}
		deliveries, err := consume(ch)
		if err != nil {
			log.Printf("rabbitmq consume: %v (retrying in 3s)", err)
			_ = conn.Close()
			time.Sleep(3 * time.Second)
			continue
		}
		log.Printf("rabbitmq consumer started (queue=%s)", RelayRabbitQueue())
		for d := range deliveries {
			handle(d)
		}
		log.Printf("rabbitmq deliveries channel closed; redialing")
		_ = conn.Close()
		time.Sleep(time.Second)
	}
}
