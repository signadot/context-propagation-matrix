module github.com/signadot/context-propagation-matrix

go 1.26.0

require (
	github.com/rabbitmq/amqp091-go v1.16.0
	github.com/signadot/context-propagation-matrix/gen v0.0.0-00010101000000-000000000000
	github.com/twmb/franz-go v1.22.3
	github.com/twmb/franz-go/pkg/kadm v1.20.0
	google.golang.org/grpc v1.85.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/klauspost/compress v1.20.3 // indirect
	github.com/pierrec/lz4/v4 v4.1.33 // indirect
	github.com/twmb/franz-go/pkg/kmsg v1.15.1 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261110000943-09e21b237f2c // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

// The generated stubs are a separate module (see gen/go.mod for why).
replace github.com/signadot/context-propagation-matrix/gen => ./gen
