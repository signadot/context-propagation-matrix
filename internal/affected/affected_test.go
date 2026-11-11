package affected

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/signadot/context-propagation-matrix/internal/registry"
)

func str(s string) *string { return &s }

// fixture is a small registry shaped like the real one: one Dockerfile per
// language with a target per tool (java, rust), Node with a source dir per
// tool under relays/node/tools, and a baseline variant with no tracer.
func fixture() *registry.Registry {
	tier := str("zero_code")
	pkg := func(name string) *registry.Package {
		return &registry.Package{Name: name, Registry: "r", Package: name}
	}
	return &registry.Registry{
		Tools: map[string]*registry.Tool{
			"otel": {ID: "otel"}, "datadog": {ID: "datadog"}, "none": {ID: "none", IsBaseline: true},
		},
		ToolOrder: []string{"otel", "datadog", "none"},
		Languages: map[string]*registry.Language{
			"java": {ID: "java", Order: 1}, "node": {ID: "node", Order: 2}, "go": {ID: "go", Order: 3}, "rust": {ID: "rust", Order: 4}, "ruby": {ID: "ruby", Order: 5},
		},
		Variants: []*registry.Variant{
			{ID: "otel-java", Tool: "otel", Language: "java", Flavour: "default", SetupTier: tier, Attach: "-javaagent", Tracer: pkg("otel-java"), Source: "relays/java"},
			{ID: "datadog-java", Tool: "datadog", Language: "java", Flavour: "default", SetupTier: tier, Tracer: pkg("dd-java"), Source: "relays/java"},
			{ID: "none-java", Tool: "none", Language: "java", Flavour: "default", Source: "relays/java"},
			{ID: "otel-node", Tool: "otel", Language: "node", Flavour: "default", SetupTier: tier, Tracer: pkg("otel-node"), Source: "relays/node/tools/otel"},
			{ID: "datadog-node", Tool: "datadog", Language: "node", Flavour: "default", SetupTier: tier, Tracer: pkg("dd-node"), Source: "relays/node/tools/datadog"},
			{ID: "datadog-node-defaults", Tool: "datadog", Language: "node", Flavour: "defaults", SetupTier: tier, Tracer: pkg("dd-node"), Source: "relays/node/tools/datadog"},
			// As in the real registry, the Node baseline's source is the
			// directory the tool sources live under.
			{ID: "none-node", Tool: "none", Language: "node", Flavour: "default", Source: "relays/node"},
			// Go keeps one module per hand-wired tool under relays/go over the
			// shared relays/go/internal; the build-time weavers are separate
			// directories whose names share the "relays/go" prefix.
			{ID: "otel-go", Tool: "otel", Language: "go", Flavour: "default", SetupTier: tier, Tracer: pkg("otel-go"), Source: "relays/go/otel"},
			{ID: "otel-go-compiletime", Tool: "otel", Language: "go", Flavour: "compiletime", SetupTier: tier, Tracer: pkg("otelc"), Source: "relays/go-otelc"},
			{ID: "none-go", Tool: "none", Language: "go", Flavour: "default", Source: "relays/go/none"},
			// Rust keeps one crate per tool over shared source, like Ruby.
			{ID: "otel-rust", Tool: "otel", Language: "rust", Flavour: "default", SetupTier: tier, Tracer: pkg("otel-rust"), Source: "relays/rust/otel"},
			{ID: "datadog-rust", Tool: "datadog", Language: "rust", Flavour: "default", SetupTier: tier, Tracer: pkg("dd-rust"), Source: "relays/rust/datadog"},
			{ID: "none-rust", Tool: "none", Language: "rust", Flavour: "default", Source: "relays/rust/none"},
			// Ruby keeps one Gemfile directory per tool and the app code beside them.
			{ID: "otel-ruby", Tool: "otel", Language: "ruby", Flavour: "default", SetupTier: tier, Tracer: pkg("otel-ruby"), Source: "relays/ruby/gemfiles/otel"},
			{ID: "none-ruby", Tool: "none", Language: "ruby", Flavour: "default", Source: "relays/ruby/gemfiles/none"},
		},
	}
}

func allIDs(reg *registry.Registry) []string {
	var ids []string
	for _, v := range reg.SortedVariants() {
		ids = append(ids, v.ID)
	}
	return ids
}

func TestPaths(t *testing.T) {
	reg := fixture()
	cases := []struct {
		name  string
		paths []string
		want  []string
		all   bool
	}{
		{"one rust tool crate", []string{"relays/rust/otel/Cargo.toml", "relays/rust/otel/Cargo.lock"}, []string{"otel-rust"}, false},
		{"rust shared source and build script", []string{"relays/rust/src/lib.rs", "relays/rust/build.rs"}, []string{"otel-rust", "datadog-rust", "none-rust"}, false},
		{"rust shared dockerfile", []string{"relays/rust/Dockerfile"}, []string{"otel-rust", "datadog-rust", "none-rust"}, false},
		{"one go tool module", []string{"relays/go/otel/go.mod", "relays/go/otel/go.sum"}, []string{"otel-go"}, false},
		{"go shared package and dockerfile reach the hand-wired tools, not the weavers", []string{"relays/go/internal/common/common.go", "relays/go/Dockerfile"}, []string{"otel-go", "none-go"}, false},
		{"go weaver directory stands alone", []string{"relays/go-otelc/go.mod"}, []string{"otel-go-compiletime"}, false},
		{"java app code", []string{"relays/java/src/main/java/Relay.java"}, []string{"otel-java", "datadog-java", "none-java"}, false},
		{"one node tool", []string{"relays/node/tools/datadog/package.json"}, []string{"datadog-node", "datadog-node-defaults"}, false},
		{"node shared dockerfile is an ancestor of the tool sources", []string{"relays/node/Dockerfile"}, []string{"otel-node", "datadog-node", "datadog-node-defaults", "none-node"}, false},
		{"node shared app manifest and code", []string{"relays/node/package.json", "relays/node/app.js"}, []string{"otel-node", "datadog-node", "datadog-node-defaults", "none-node"}, false},
		{"node shared code in a subdirectory", []string{"relays/node/lib/hop.js"}, []string{"otel-node", "datadog-node", "datadog-node-defaults", "none-node"}, false},
		{"app code beside the per-tool sources", []string{"relays/ruby/app/relay.rb"}, []string{"otel-ruby", "none-ruby"}, false},
		{"driver", []string{"driver/main.go"}, allIDs(reg), true},
		{"proto", []string{"proto/hop.proto", "gen/cpmatrix/hop.pb.go"}, allIDs(reg), true},
		{"shared go image", []string{"docker/go.Dockerfile"}, allIDs(reg), true},
		{"go.mod", []string{"go.mod", "go.sum"}, allIDs(reg), true},
		{"the workflow itself", []string{".github/workflows/matrix.yml"}, allIDs(reg), true},
		{"annotations only", []string{"annotations/datadog.yaml", "annotations/otel.yaml"}, nil, false},
		{"docs and aggregator", []string{"README.md", "relays/rust/README.md", "cmd/cpm/main.go", "internal/results/results.go", ".github/workflows/verify.yml", "scripts/fetch-latest.sh", "run.sh", "examples/stacks/otel-go-older-sdk.yaml", "cmd/cpm/try.go"}, nil, false},
		{"mixed: docs plus one relay", []string{"CONTRIBUTING.md", "relays/rust/datadog/Cargo.lock"}, []string{"datadog-rust"}, false},
		{"unknown top-level path is conservative", []string{"tools/new-thing.sh"}, allIDs(reg), true},
		{"relay path outside every source is conservative", []string{"relays/common/shared.txt"}, allIDs(reg), true},
		{"nothing changed", nil, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Compute(Inputs{Paths: c.paths, Head: reg})
			if got.All != c.all {
				t.Errorf("All = %v, want %v (%v)", got.All, c.all, got.Reasons)
			}
			if !reflect.DeepEqual(got.Variants, c.want) {
				t.Errorf("Variants = %v, want %v\nreasons: %s", got.Variants, c.want, strings.Join(got.Reasons, "; "))
			}
		})
	}
}

func TestRegistryDiff(t *testing.T) {
	base, head := fixture(), fixture()
	head.Variants[0].Attach = "-javaagent -Dotel.propagators=b3" // otel-java
	for _, v := range head.Variants {
		if v.ID == "otel-rust" {
			v.Tracer = &registry.Package{Name: "otel-rust", Registry: "r", Package: "otel-rust", TagPrefix: "v"}
		}
	}
	head.Variants = append(head.Variants, &registry.Variant{
		ID: "otel-php", Tool: "otel", Language: "php", Flavour: "default", SetupTier: str("zero_code"), Source: "relays/php",
	})
	head.Languages["php"] = &registry.Language{ID: "php", Order: 9}

	got := Compute(Inputs{Paths: []string{"variants.yaml"}, Head: head, Base: base})
	want := []string{"otel-java", "otel-rust", "otel-php"}
	if got.All || !reflect.DeepEqual(got.Variants, want) {
		t.Fatalf("got all=%v %v, want %v\n%s", got.All, got.Variants, want, strings.Join(got.Reasons, "\n"))
	}

	// Dimension-only edits (a header's success pattern, a tool's display
	// name) change aggregation, not measurement: nothing re-runs.
	head2 := fixture()
	head2.Tools["otel"].DisplayName = "OpenTelemetry"
	got = Compute(Inputs{Paths: []string{"variants.yaml"}, Head: head2, Base: fixture()})
	if got.All || len(got.Variants) != 0 {
		t.Fatalf("dimension edit: got all=%v %v, want none", got.All, got.Variants)
	}

	// Removed variants are not run; an unreadable base re-runs everything.
	got = Compute(Inputs{Paths: []string{"variants.yaml"}, Head: head2, Base: nil})
	if !got.All {
		t.Fatalf("nil base should re-run everything: %v", got.Reasons)
	}
}

const composeBase = `
x-relay-common-env: &relay-common-env
  KAFKA_BROKERS: kafka:9092
  SINK_HTTP: http://sink:9080
services:
  kafka:
    image: apache/kafka:3.9.0
  sink:
    build: { context: ., dockerfile: docker/go.Dockerfile, target: sink }
  relay-otel-java:
    build: { context: ., dockerfile: relays/java/Dockerfile, target: otel }
    profiles: [otel-java]
    environment:
      <<: *relay-common-env
      VARIANT: otel-java
      OTEL_PROPAGATORS: tracecontext,baggage
  relay-datadog-java:
    build: { context: ., dockerfile: relays/java/Dockerfile, target: datadog }
    profiles: [datadog-java]
    environment:
      <<: *relay-common-env
      VARIANT: datadog-java
  relay-otel-rust:
    build: { context: ., dockerfile: relays/rust/Dockerfile, args: { TOOL: otel } }
    profiles: [otel-rust]
    environment:
      <<: *relay-common-env
      VARIANT: otel-rust
`

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestComposeDiff(t *testing.T) {
	reg := fixture()
	base := decode(t, composeBase)

	t.Run("one relay env changed", func(t *testing.T) {
		head := decode(t, strings.Replace(composeBase, "OTEL_PROPAGATORS: tracecontext,baggage", "OTEL_PROPAGATORS: tracecontext,baggage,b3", 1))
		got := Compute(Inputs{Paths: []string{"docker-compose.yml"}, Head: reg, HeadCompose: head, BaseCompose: base})
		if got.All || !reflect.DeepEqual(got.Variants, []string{"otel-java"}) {
			t.Fatalf("got all=%v %v: %v", got.All, got.Variants, got.Reasons)
		}
	})
	t.Run("shared anchor reaches every relay through the merge key", func(t *testing.T) {
		head := decode(t, strings.Replace(composeBase, "SINK_HTTP: http://sink:9080", "SINK_HTTP: http://sink:9090", 1))
		got := Compute(Inputs{Paths: []string{"docker-compose.yml"}, Head: reg, HeadCompose: head, BaseCompose: base})
		// Every relay service in this compose changed, and nothing shared did.
		if got.All || !reflect.DeepEqual(got.Variants, []string{"otel-java", "otel-rust", "datadog-java"}) {
			t.Fatalf("got all=%v %v: %v", got.All, got.Variants, got.Reasons)
		}
	})
	t.Run("broker image bump is shared", func(t *testing.T) {
		head := decode(t, strings.Replace(composeBase, "apache/kafka:3.9.0", "apache/kafka:4.0.0", 1))
		got := Compute(Inputs{Paths: []string{"docker-compose.yml"}, Head: reg, HeadCompose: head, BaseCompose: base})
		if !got.All {
			t.Fatalf("kafka change should re-run everything: %v", got.Reasons)
		}
	})
	t.Run("unchanged compose selects nothing", func(t *testing.T) {
		got := Compute(Inputs{Paths: []string{"docker-compose.yml"}, Head: reg, HeadCompose: decode(t, composeBase), BaseCompose: base})
		if got.All || len(got.Variants) != 0 {
			t.Fatalf("got all=%v %v", got.All, got.Variants)
		}
	})
	t.Run("unreadable base compose is conservative", func(t *testing.T) {
		got := Compute(Inputs{Paths: []string{"docker-compose.yml"}, Head: reg, HeadCompose: base})
		if !got.All {
			t.Fatal("nil base compose should re-run everything")
		}
	})
}
