// The shared relay plumbing (package common) is its own module, like gen, so
// that the per-tool modules in relays/go/<tool> depend on nothing but the
// stubs, this package and their own tracer. Versions here are FLOORS (see
// gen/go.mod): each tool module declares its own grpc and amqp091
// requirement and MVS takes the maximum. Not in .github/dependabot.yml.
module github.com/signadot/context-propagation-matrix/relays/go/internal

go 1.26.0

require (
	github.com/rabbitmq/amqp091-go v1.16.0
	github.com/signadot/context-propagation-matrix/gen v0.0.0-00010101000000-000000000000
)

require (
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261110000943-09e21b237f2c // indirect
	google.golang.org/grpc v1.68.1 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

replace github.com/signadot/context-propagation-matrix/gen => ../../../gen
