// The generated gRPC stubs are their own module so that the Go relays under
// relays/go, relays/go-otelc and relays/go-orchestrion can depend on them
// without inheriting the root module's requirements (the driver's current
// grpc, franz-go, …).
//
// The versions below are FLOORS, not current versions, and this module is
// deliberately not in .github/dependabot.yml: back-tests `go get` an old
// tracer release in a relay module and Go must be free to downgrade grpc
// alongside it (grpc ≥ 1.70 requires go.opentelemetry.io/otel, so a current
// grpc here would cap how far back otel-go can be measured). Every consumer
// declares its own grpc/protobuf requirement and MVS takes the maximum.
module github.com/signadot/context-propagation-matrix/gen

go 1.26.0

require (
	// Newest grpc release that does not itself require go.opentelemetry.io/otel
	// (1.68.2 and 1.69.0 added that requirement); relay_grpc.pb.go needs ≥ 1.64.0.
	google.golang.org/grpc v1.68.1
	google.golang.org/protobuf v1.36.12
)

require (
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261110000943-09e21b237f2c // indirect
)
