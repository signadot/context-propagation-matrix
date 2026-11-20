module github.com/signadot/context-propagation-matrix/relays/go/newrelic

go 1.26.0

// v1.38.1 is the last release published under the Shopify path (the repo now
// redirects to IBM/sarama, whose tags declare the IBM module path); nrsarama
// is built against the Shopify module.
require github.com/Shopify/sarama v1.38.1

require (
	github.com/newrelic/go-agent/v3 v3.45.0
	github.com/newrelic/go-agent/v3/integrations/nramqp v1.0.4
	github.com/newrelic/go-agent/v3/integrations/nrgrpc v1.4.11
	github.com/newrelic/go-agent/v3/integrations/nrsarama v1.0.2
	github.com/rabbitmq/amqp091-go v1.17.0
	github.com/signadot/context-propagation-matrix/gen v0.0.0-00010101000000-000000000000
	github.com/signadot/context-propagation-matrix/relays/go/internal v0.0.0-00010101000000-000000000000
	google.golang.org/grpc v1.85.0
)

require (
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/eapache/go-resiliency v1.7.0 // indirect
	github.com/eapache/go-xerial-snappy v0.0.0-20230731223053-c322873962e3 // indirect
	github.com/eapache/queue v1.1.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/pprof v0.0.0-20260802141513-ef3492d7dac3 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-uuid v1.0.3 // indirect
	github.com/jcmturner/aescts/v2 v2.0.0 // indirect
	github.com/jcmturner/dnsutils/v2 v2.0.0 // indirect
	github.com/jcmturner/gofork v1.7.6 // indirect
	github.com/jcmturner/gokrb5/v8 v8.4.4 // indirect
	github.com/jcmturner/rpc/v2 v2.0.3 // indirect
	github.com/klauspost/compress v1.20.3 // indirect
	github.com/newrelic/csec-go-agent v1.6.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.33 // indirect
	github.com/rcrowley/go-metrics v0.0.0-20250401214520-65e299d6c5c9 // indirect
	github.com/stretchr/testify v1.12.1 // indirect
	golang.org/x/arch v0.30.0 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261110000943-09e21b237f2c // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

// Both live in this repository; see gen/go.mod for why they are separate modules.
replace (
	github.com/signadot/context-propagation-matrix/gen => ../../../gen
	github.com/signadot/context-propagation-matrix/relays/go/internal => ../internal
)
