// This file was created by `orchestrion pin`, and is used to ensure the
// `go.mod` file contains the necessary entries to ensure repeatable builds when
// using `orchestrion`. It is also used to set up which integrations are enabled.
//
// This is the pin default: importing dd-trace-go's catch-all
// `orchestrion/all/v2` module enables every integration, and the weaver only
// injects code for libraries the application actually imports (net/http,
// grpc, IBM/sarama here — there is no amqp091/RabbitMQ integration to enable,
// which the rabbitmq legs of the matrix measure). Note that `orchestrion pin`
// itself prints a spurious "removing unnecessary import ... replacement
// directory ../../contrib/99designs/gqlgen does not exist" warning when
// re-validating this file (the published all/v2 module carries dangling local
// replace directives), but the warning is cosmetic: `orchestrion go build`
// resolves and weaves the integrations regardless, verified by symbol
// inspection of the produced binary.

//go:build tools

//go:generate go run github.com/DataDog/orchestrion pin -generate

package tools

// Imports in this file determine which tracer integrations are enabled in
// orchestrion. New integrations can be automatically discovered by running
// `orchestrion pin` again. You can also manually add new imports here to
// enable additional integrations. When doing so, you can run `orchestrion pin`
// to make sure manually added integrations are valid (i.e, the imported package
// includes a valid `orchestrion.yml` file).
import (
	// Ensures `orchestrion` is present in `go.mod` so that builds are repeatable.
	// Do not remove.
	_ "github.com/DataDog/orchestrion" // integration

	_ "github.com/DataDog/dd-trace-go/orchestrion/all/v2" // integration
)
