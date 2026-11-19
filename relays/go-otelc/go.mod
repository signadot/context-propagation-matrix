module github.com/signadot/context-propagation-matrix/relays/go-otelc

go 1.26.0

// Naive app dependencies only — no tracing library is imported by main.go.
// The otelc weaver (pinned by the `tool` directive below, per the documented
// Go 1.24+ tool-dependency flow) adds its instrumentation and the OTel SDK at
// build time via `go tool otelc go build`. Versions pinned exactly
// (Dependabot bumps them); kafka-go because it is the only Kafka client otelc
// v1 instruments.
require (
	github.com/rabbitmq/amqp091-go v1.16.0
	github.com/segmentio/kafka-go v0.4.51
	github.com/signadot/context-propagation-matrix/gen v0.0.0-00010101000000-000000000000
	google.golang.org/grpc v1.85.0
)

require (
	github.com/bmatcuk/doublestar/v4 v4.10.0 // indirect
	github.com/dave/dst v0.28.0 // indirect
	github.com/gofrs/flock v0.13.1 // indirect
	github.com/klauspost/compress v1.20.3 // indirect
	github.com/pierrec/lz4/v4 v4.1.33 // indirect
	github.com/urfave/cli/v3 v3.12.0 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	go.opentelemetry.io/otelc v1.2.2 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261110000943-09e21b237f2c // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

// The generated stubs live in this repository (see gen/go.mod).
replace github.com/signadot/context-propagation-matrix/gen => ../../gen

tool go.opentelemetry.io/otelc/tool/cmd/otelc
