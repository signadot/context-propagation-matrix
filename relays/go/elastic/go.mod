module github.com/signadot/context-propagation-matrix/relays/go/elastic

go 1.26.0

require (
	github.com/rabbitmq/amqp091-go v1.17.0
	github.com/signadot/context-propagation-matrix/gen v0.0.0-00010101000000-000000000000
	github.com/signadot/context-propagation-matrix/relays/go/internal v0.0.0-00010101000000-000000000000
	github.com/twmb/franz-go v1.22.3
	go.elastic.co/apm/module/apmgrpc/v2 v2.7.12
	go.elastic.co/apm/module/apmhttp/v2 v2.7.12
	go.elastic.co/apm/v2 v2.7.12
	google.golang.org/grpc v1.85.0
)

require (
	github.com/armon/go-radix v1.0.0 // indirect
	github.com/elastic/go-sysinfo v1.7.1 // indirect
	github.com/elastic/go-windows v1.0.0 // indirect
	github.com/joeshaw/multierror v0.0.0-20140124173710-69b34d4ec901 // indirect
	github.com/klauspost/compress v1.20.3 // indirect
	github.com/pierrec/lz4/v4 v4.1.33 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/procfs v0.21.1 // indirect
	github.com/stretchr/testify v1.12.1 // indirect
	github.com/twmb/franz-go/pkg/kmsg v1.15.1 // indirect
	go.elastic.co/fastjson v1.5.1 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261110000943-09e21b237f2c // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	howett.net/plist v0.0.0-20181124034731-591f970eefbb // indirect
)

// Both live in this repository; see gen/go.mod for why they are separate modules.
replace (
	github.com/signadot/context-propagation-matrix/gen => ../../../gen
	github.com/signadot/context-propagation-matrix/relays/go/internal => ../internal
)
