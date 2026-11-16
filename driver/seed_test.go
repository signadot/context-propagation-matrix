package main

import (
	"strings"
	"testing"

	"github.com/signadot/context-propagation-matrix/internal/eval"
	"github.com/signadot/context-propagation-matrix/internal/registry"
)

// What the driver seeds, what the evaluator looks for and what the registry
// says is on the wire are written in three places. This test is what keeps
// them the same: a seed the evaluator does not look for scores "absent" on
// every hop of every variant, and nothing else would say why.
func TestSeedIsWhatTheEvaluatorAndTheRegistryExpect(t *testing.T) {
	seed := newSeed("otel-node--http-kafka--1")

	// A relay that copied every seeded header through unchanged.
	echoed := map[string][]string{}
	for name, value := range seed.Headers {
		echoed[name] = []string{value}
	}
	got := eval.Evaluate(echoed, seed)
	for field, want := range map[string]string{
		"traceparent": "same_trace",
		"tracestate":  "preserved",
		"baggage":     "propagated",
		"ot_baggage":  "propagated",
	} {
		if have, _ := got.Field(field); have != want {
			t.Errorf("%s: the seeded headers echoed back evaluate to %q, want %q", field, have, want)
		}
	}
	if !got.CanaryLeak {
		t.Errorf("the seeded control header %s, echoed back, was not seen as a leak", eval.CanaryHeader)
	}

	// The evaluator follows whatever the seed records, so the names
	// themselves are checked here: the ones internal/eval declares, which
	// the documents and the questions below are written with.
	for name, want := range map[string]string{
		"tracestate":                          eval.TracestateEntry,
		"baggage":                             eval.RoutingKeyMember + "=" + seed.RoutingKey + ",team=alpha",
		"ot-baggage-" + eval.RoutingKeyMember: seed.RoutingKey,
		eval.CanaryHeader:                     "otel-node--http-kafka--1",
	} {
		if got, ok := seed.Headers[name]; !ok || got != want {
			t.Errorf("seeded %s: got %q, want %q", name, got, want)
		}
	}

	reg, err := registry.Load("../variants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for id, h := range reg.Headers {
		if _, ok := seed.Headers[h.WireName]; !ok {
			t.Errorf("variants.yaml scores header %s on the wire name %s, which the driver does not seed", id, h.WireName)
		}
	}
	// The question each header's results are published under names what was
	// seeded.
	for id, name := range map[string]string{"baggage": eval.RoutingKeyMember, "tracestate": eval.TracestateEntry} {
		h := reg.Headers[id]
		if h == nil {
			t.Errorf("variants.yaml has no header %s", id)
		} else if !strings.Contains(h.Question, "`"+name+"`") {
			t.Errorf("variants.yaml asks of %s: %q. It does not name `%s`, which is what the driver seeds", id, h.Question, name)
		}
	}
	if len(seed.Headers) != len(reg.Headers)+1 {
		t.Errorf("the driver seeds %d headers; variants.yaml scores %d and one more is the control header", len(seed.Headers), len(reg.Headers))
	}
}
