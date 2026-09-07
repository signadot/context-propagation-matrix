module github.com/signadot/context-propagation-matrix/relays/go-otelc

go 1.25.11

// Naive app dependencies only — no tracing library is imported by main.go.
// The otelc weaver (pinned by the `tool` directive below, per the documented
// Go 1.24+ tool-dependency flow) adds its instrumentation and the OTel SDK at
// build time via `go tool otelc go build`. Versions pinned exactly
// (Dependabot bumps them); kafka-go because it is the only Kafka client otelc
// v1 instruments.
require (
	github.com/rabbitmq/amqp091-go v1.14.0
	github.com/segmentio/kafka-go v0.4.51
	github.com/signadot/context-propagation-matrix v0.0.0-00010101000000-000000000000
	google.golang.org/grpc v1.83.2
)

require (
	github.com/bmatcuk/doublestar/v4 v4.10.0 // indirect
	github.com/dave/dst v0.27.4 // indirect
	github.com/gofrs/flock v0.13.0 // indirect
	github.com/klauspost/compress v1.18.7 // indirect
	github.com/pierrec/lz4/v4 v4.1.26 // indirect
	github.com/urfave/cli/v3 v3.10.1 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	go.opentelemetry.io/otelc v1.1.0 // indirect
	golang.org/x/mod v0.40.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/signadot/context-propagation-matrix => ../../

tool go.opentelemetry.io/otelc/tool/cmd/otelc
