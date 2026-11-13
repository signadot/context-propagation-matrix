package registry

import (
	"strings"
	"testing"
)

// minimal is the smallest registry that passes validation: two protocols, one
// in-code variant with all four legs, one zero-code variant and a baseline.
// Tests edit it with strings.Replace so each case names only what it changes.
const minimal = `
matrix_version: 1
tools:
  otel: { display_name: OpenTelemetry, short_name: OTel, vendor: CNCF, url: x, native_headers: [], default_propagators: [tracecontext] }
  none: { display_name: Baseline, short_name: Baseline, vendor: null, url: null, native_headers: [], default_propagators: [], is_baseline: true }
languages:
  go:   { display_name: Go,   order: 0 }
  java: { display_name: Java, order: 1 }
protocols:
  http:  { display_name: HTTP,  carrier: "HTTP request headers" }
  kafka: { display_name: Kafka, carrier: "Kafka record headers", async: true }
boundaries: [http->http, http->kafka, kafka->http, kafka->kafka]
headers:
  traceparent: { display_name: traceparent, wire_name: traceparent, raw_field: traceparent, question: q, success: [same_trace], partial: [], spec: x }
setup_tiers:
  zero-code: { display_name: zero-code, description: d }
  in-code:   { display_name: in-code,   description: d }
wirings:
  library: { display_name: library,          description: d }
  app:     { display_name: application code, description: d }
variants:
  - id: otel-go
    tool: otel
    language: go
    flavour: default
    display_name: Go
    setup_tier: in-code
    tracer: { name: otel, registry: goproxy, package: p }
    docs: https://example.com/otel/go
    source: relays/go/otel
    legs:
      http:  { inbound: library, outbound: library }
      kafka: { inbound: app,     outbound: app }
  - id: otel-java
    tool: otel
    language: java
    flavour: default
    display_name: Java
    setup_tier: zero-code
    tracer: { name: otel, registry: maven, package: p }
    docs: https://example.com/otel/java
    source: relays/java
  - { id: none-go, tool: none, language: go, flavour: default, display_name: Go, setup_tier: null, source: relays/go/none }
`

func TestParse_LegsAndWirings(t *testing.T) {
	reg, err := Parse([]byte(minimal), "test")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(reg.WiringOrder, ","); got != "library,app" {
		t.Errorf("WiringOrder = %s, want library,app", got)
	}
	if w := reg.Wirings["app"]; w == nil || w.ID != "app" || w.DisplayName != "application code" {
		t.Errorf("Wirings[app] = %+v", w)
	}
	legs := reg.Variant("otel-go").Legs
	if legs["http"] != (Leg{Inbound: "library", Outbound: "library"}) || legs["kafka"] != (Leg{Inbound: "app", Outbound: "app"}) {
		t.Errorf("otel-go legs = %+v", legs)
	}
	for _, id := range []string{"otel-java", "none-go"} {
		if n := len(reg.Variant(id).Legs); n != 0 {
			t.Errorf("%s: %d legs, want none", id, n)
		}
	}
}

func TestValidate_Legs(t *testing.T) {
	legs := "      http:  { inbound: library, outbound: library }\n      kafka: { inbound: app,     outbound: app }\n"
	cases := []struct {
		name string
		edit func(string) string
		want string // substring of the error; "" means valid
	}{
		{"valid", func(s string) string { return s }, ""},
		{"protocol missing", func(s string) string {
			return strings.Replace(s, "      kafka: { inbound: app,     outbound: app }\n", "", 1)
		}, `otel-go: legs: no entry for protocol "kafka"`},
		{"all legs missing", func(s string) string { return strings.Replace(s, "    legs:\n"+legs, "", 1) },
			`otel-go: legs: no entry for protocol "http"`},
		{"direction missing", func(s string) string {
			return strings.Replace(s, "http:  { inbound: library, outbound: library }", "http:  { inbound: library }", 1)
		}, "otel-go: legs.http: outbound is required"},
		{"unknown wiring", func(s string) string {
			return strings.Replace(s, "inbound: library", "inbound: libary", 1)
		}, `otel-go: legs.http: unknown wiring "libary" for inbound`},
		{"unknown protocol", func(s string) string {
			return strings.Replace(s, "      kafka:", "      amqp:  { inbound: app, outbound: app }\n      kafka:", 1)
		}, `otel-go: legs: unknown protocol "amqp"`},
		{"legs on a zero-code variant", func(s string) string {
			return strings.Replace(s, "    source: relays/java\n", "    source: relays/java\n    legs:\n"+legs, 1)
		}, "otel-java: legs are only for setup_tier in-code, not zero-code"},
		{"legs on a baseline", func(s string) string {
			return strings.Replace(s, "source: relays/go/none }", "source: relays/go/none, legs: { http: { inbound: app, outbound: app } } }", 1)
		}, "none-go: baseline variants must not have legs"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.edit(minimal)), "test")
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case c.want != "" && err == nil:
				t.Fatalf("want error containing %q, got none", c.want)
			case c.want != "" && !strings.Contains(err.Error(), c.want):
				t.Fatalf("error %q does not contain %q", err, c.want)
			}
		})
	}
}

// A revision written before the field existed has neither a wirings block nor
// legs. `cpm affected` reads it to see which variants changed, and a parse
// error there would re-run every variant, so it must still parse. Legs become
// required as soon as the registry defines wirings.
func TestParse_RevisionOlderThanLegs(t *testing.T) {
	const (
		block = "wirings:\n  library: { display_name: library,          description: d }\n  app:     { display_name: application code, description: d }\n"
		legs  = "    legs:\n      http:  { inbound: library, outbound: library }\n      kafka: { inbound: app,     outbound: app }\n"
	)
	noLegs := strings.Replace(minimal, legs, "", 1)
	old := strings.Replace(noLegs, block, "", 1)
	if noLegs == minimal || old == noLegs {
		t.Fatal("edit did not apply")
	}
	if _, err := Parse([]byte(old), "abc123:variants.yaml"); err != nil {
		t.Errorf("no wirings, no legs: %v", err)
	}
	if _, err := Parse([]byte(noLegs), "variants.yaml"); err == nil {
		t.Error("wirings defined, in-code variant without legs: accepted")
	}
	// Legs with nothing to define their values are an error, not a pass.
	orphan := strings.Replace(minimal, block, "", 1)
	if _, err := Parse([]byte(orphan), "variants.yaml"); err == nil || !strings.Contains(err.Error(), "unknown wiring") {
		t.Errorf("legs without a wirings block: got %v, want unknown wiring", err)
	}
}

// The checked-in registry must satisfy its own rules; Load is what every
// command runs first, so this also pins that the in-code tier is not empty.
func TestLoad_CheckedInRegistryLegs(t *testing.T) {
	reg, err := Load("../../variants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	inCode := 0
	for _, v := range reg.Variants {
		if v.SetupTier != nil && *v.SetupTier == "in-code" {
			inCode++
			if len(v.Legs) != len(reg.Protocols) {
				t.Errorf("%s: %d legs, want %d", v.ID, len(v.Legs), len(reg.Protocols))
			}
		}
	}
	if inCode == 0 {
		t.Error("no in-code variants in variants.yaml")
	}
}
