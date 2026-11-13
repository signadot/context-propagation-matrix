package results

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/signadot/context-propagation-matrix/internal/registry"
)

// The fixtures below are shaped like published documents: the header names
// are what the sink recorded for these relays in run-2026-11-10.22.

// plain is what an uninstrumented client sends on each transport.
func plain(out string) []string {
	switch out {
	case "http":
		return []string{"accept", "content-type", "user-agent"}
	case "grpc":
		return []string{":authority", "content-type", "grpc-accept-encoding", "user-agent"}
	}
	return nil // Kafka and RabbitMQ messages carry no headers of their own
}

func with(base []string, more ...string) []string {
	return append(append([]string{}, base...), more...)
}

// noAgent is a relay nothing is attached to, on a runtime that sends no
// tracing header itself (Java, PHP, …).
func noAgent(in, out string) []string { return with(plain(out)) }

// dotnetRuntime is none-dotnet: HttpClient and the gRPC client continue the
// server's Activity with no agent installed, so the four boundaries between
// HTTP and gRPC carry W3C headers.
func dotnetRuntime(in, out string) []string {
	if (in == "http" || in == "grpc") && (out == "http" || out == "grpc") {
		return with(plain(out), "traceparent", "tracestate", "baggage")
	}
	return with(plain(out))
}

// otelDotnet is a working otel-dotnet: the same names as the bare runtime
// between HTTP and gRPC, and headers of its own everywhere else.
func otelDotnet(in, out string) []string {
	switch in {
	case "http", "grpc":
		return with(plain(out), "traceparent", "tracestate", "baggage")
	case "kafka":
		return with(plain(out), "traceparent")
	}
	return with(plain(out), "traceparent", "tracestate")
}

// noGRPCServer wraps a relay that cannot be reached over gRPC (PHP): its
// four gRPC-inbound hops are never delivered.
func noGRPCServer(sent func(in, out string) []string) func(in, out string) []string {
	return func(in, out string) []string {
		if in == "grpc" {
			return nil
		}
		return sent(in, out)
	}
}

type fixture struct {
	t   *testing.T
	reg *registry.Registry
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	reg, err := registry.Load("../../variants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t, reg}
}

// variant builds a measured variant with one hop per registry boundary.
// sent names the headers the sink saw on a boundary; nil means the hop was
// not delivered.
func (f *fixture) variant(id string, sent func(in, out string) []string) *Variant {
	f.t.Helper()
	rv := f.reg.Variant(id)
	if rv == nil {
		f.t.Fatalf("%s is not in variants.yaml", id)
	}
	v := &Variant{ID: rv.ID, Tool: rv.Tool, Language: rv.Language, Flavour: rv.Flavour}
	for _, b := range f.reg.Boundaries {
		in, out, _ := strings.Cut(b, "->")
		h := Hop{Boundary: b, Inbound: in, Outbound: out}
		if names := sent(in, out); names != nil {
			h.Delivered, h.Transport = true, out
			h.Observed = map[string][]string{}
			for _, n := range names {
				h.Observed[n] = []string{"x"}
			}
		} else {
			h.Error = "send: connection refused"
		}
		v.Hops = append(v.Hops, h)
	}
	return v
}

func (f *fixture) doc(kind string, variants ...*Variant) *Results {
	return &Results{SchemaVersion: SchemaVersion, Run: Run{RunID: "run-test", Kind: kind},
		Dimensions: dimensions(f.reg), Variants: variants}
}

func hop(v *Variant, boundary string) *Hop {
	for i := range v.Hops {
		if v.Hops[i].Boundary == boundary {
			return &v.Hops[i]
		}
	}
	panic("no hop " + boundary)
}

func kinds(ps []Problem) []string {
	out := []string{}
	for _, p := range ps {
		out = append(out, p.Kind)
	}
	return out
}

func wantKinds(t *testing.T, what string, got []Problem, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if !reflect.DeepEqual(kinds(got), want) {
		t.Errorf("%s: problems = %+v, want kinds %v", what, got, want)
	}
}

// A relay that proxies inbound headers passes every cell and delivers the
// canary. The driver's flag and the header itself both count, and the leak
// is not mistaken for anything else.
func TestJudge_CanaryLeak(t *testing.T) {
	f := newFixture(t)
	base := f.variant("none-java", noAgent)
	v := f.variant("otel-java", func(in, out string) []string {
		return with(plain(out), "traceparent", "tracestate", "baggage", "ot-baggage-sd-routing-key")
	})
	hop(v, "http->kafka").CanaryLeak = true
	hop(v, "kafka->http").Observed["X-SD-Canary"] = []string{"otel-java--kafka-http--1"}

	got := f.doc("matrix", v, base).Judge(v, base)
	wantKinds(t, "proxying relay", got, ProblemCanaryLeak)
	if want := []string{"http->kafka", "kafka->http"}; !reflect.DeepEqual(got[0].Boundaries, want) {
		t.Errorf("boundaries = %v, want %v", got[0].Boundaries, want)
	}
	if !strings.Contains(got[0].Detail, "2 of 16") {
		t.Errorf("detail = %q", got[0].Detail)
	}
}

// A hop observed on another protocol than the boundary's outbound one.
func TestJudge_TransportMismatch(t *testing.T) {
	f := newFixture(t)
	base := f.variant("none-java", noAgent)
	v := f.variant("otel-java", func(in, out string) []string { return with(plain(out), "traceparent") })
	hop(v, "http->kafka").Transport = "http"
	hop(v, "grpc->rabbitmq").Transport = ""

	got := f.doc("matrix", v, base).Judge(v, base)
	wantKinds(t, "wrong transport", got, ProblemTransportMismatch)
	if want := []string{"http->kafka", "grpc->rabbitmq"}; !reflect.DeepEqual(got[0].Boundaries, want) {
		t.Errorf("boundaries = %v, want %v", got[0].Boundaries, want)
	}
	if !strings.Contains(got[0].Detail, "http->kafka over http") {
		t.Errorf("detail = %q", got[0].Detail)
	}

	// An undelivered hop observed nothing, so it has no transport to compare.
	php := f.variant("otel-php", noGRPCServer(func(in, out string) []string { return with(plain(out), "traceparent") }))
	wantKinds(t, "undelivered hops", f.doc("matrix", php).Judge(php, nil))
}

// An agent that did not attach sends what the uninstrumented relay sends.
// One tracing header on one hop is enough to show it did; which names are
// tracing headers comes from the registry.
func TestJudge_AgentInactive(t *testing.T) {
	f := newFixture(t)
	base := f.variant("none-java", noAgent)
	base.MeasuredIn = "run-2026-11-10.22"

	inert := f.variant("elastic-java", noAgent)
	got := f.doc("backtest", inert).Judge(inert, base)
	wantKinds(t, "inert agent", got, ProblemAgentInactive)
	if got[0].Boundaries != nil || !strings.Contains(got[0].Detail, "none-java (measured in run-2026-11-10.22)") ||
		!strings.Contains(got[0].Detail, "16 delivered") {
		t.Errorf("problem = %+v", got[0])
	}

	for _, c := range []struct {
		header string
		active bool
	}{
		{"traceparent", true},
		{"Traceparent", true},             // names are compared without case
		{"baggage", true},                 // headers[].wire_name
		{"ot-baggage-team", true},         // the ot-baggage- prefix, not only the seeded item
		{"elastic-apm-traceparent", true}, // tools[].native_headers, this tool's
		{"newrelic", true},                // … and any other tool's
		{"x-datadog-trace-id", true},
		{"elasticapmtraceparent", false}, // sent by real agents, but not in the registry
		{"dd-pathway-ctx-base64", false},
		{"correlation-context", false},
		{"x-request-id", false},
		{"x-datadog-origin", false}, // native_headers are names, not prefixes
	} {
		v := f.variant("elastic-java", noAgent)
		hop(v, "rabbitmq->kafka").Observed[c.header] = []string{"x"}
		got := f.doc("backtest", v).Judge(v, base)
		if active := len(got) == 0; active != c.active {
			t.Errorf("only %s on one hop: problems = %+v, want active=%v", c.header, got, c.active)
		}
	}
}

// The .NET runtime sends W3C headers by itself, so "no tracing header" never
// holds for a .NET relay and http->http alone cannot tell a working agent
// from none. Against none-dotnet, boundary by boundary: an agent that adds
// nothing is inactive, one that adds a header anywhere is not.
func TestJudge_DotNet(t *testing.T) {
	f := newFixture(t)
	base := f.variant("none-dotnet", dotnetRuntime)

	inert := f.variant("otel-dotnet", dotnetRuntime)
	wantKinds(t, "agent identical to none-dotnet", f.doc("matrix", inert, base).Judge(inert, base), ProblemAgentInactive)

	working := f.variant("otel-dotnet", otelDotnet)
	if got, want := hop(working, "http->http").Observed, hop(base, "http->http").Observed; !reflect.DeepEqual(got, want) {
		t.Fatalf("fixture: a working otel-dotnet must match none-dotnet on http->http, got %v want %v", got, want)
	}
	wantKinds(t, "working otel-dotnet", f.doc("matrix", working, base).Judge(working, base))

	// A subset of the baseline is still nothing of the agent's own.
	less := f.variant("otel-dotnet", func(in, out string) []string {
		if in == "http" && out == "http" {
			return with(plain(out), "traceparent")
		}
		return noAgent(in, out)
	})
	wantKinds(t, "fewer headers than none-dotnet", f.doc("matrix", less, base).Judge(less, base), ProblemAgentInactive)
}

// PHP serves no gRPC: four hops are never delivered and are not evidence
// for or against the agent.
func TestJudge_UndeliveredHops(t *testing.T) {
	f := newFixture(t)
	base := f.variant("none-php", noGRPCServer(noAgent))

	// otel-php as published: traceparent on HTTP-outbound hops only.
	working := f.variant("otel-php", noGRPCServer(func(in, out string) []string {
		if out == "http" {
			return with(plain(out), "traceparent")
		}
		return noAgent(in, out)
	}))
	wantKinds(t, "working otel-php", f.doc("matrix", working, base).Judge(working, base))

	inert := f.variant("otel-php", noGRPCServer(noAgent))
	got := f.doc("matrix", inert, base).Judge(inert, base)
	wantKinds(t, "inert otel-php", got, ProblemAgentInactive)
	if !strings.Contains(got[0].Detail, "12 delivered") {
		t.Errorf("detail = %q, want the 12 delivered hops counted", got[0].Detail)
	}

	// Nothing delivered at all is a broken relay, not an inactive agent.
	dead := f.variant("otel-php", func(in, out string) []string { return nil })
	wantKinds(t, "no hop delivered", f.doc("matrix", dead, base).Judge(dead, base))

	// Where the baseline did not deliver there is nothing to be a subset
	// of: a header the variant shows there is its own.
	v := f.variant("otel-dotnet", dotnetRuntime)
	partial := f.variant("none-dotnet", noGRPCServer(dotnetRuntime))
	wantKinds(t, "baseline hop undelivered", f.doc("matrix", v, partial).Judge(v, partial))
}

// Without a baseline the rule is "no tracing header on any delivered hop",
// and the verdict says so. That still catches an inert Java agent; it
// cannot catch an inert .NET one.
func TestJudge_NoBaseline(t *testing.T) {
	f := newFixture(t)

	inert := f.variant("newrelic-java", noAgent)
	got := f.doc("backtest", inert).Judge(inert, nil)
	wantKinds(t, "inert agent, no baseline", got, ProblemAgentInactive)
	if !strings.Contains(got[0].Detail, "no uninstrumented java baseline") {
		t.Errorf("detail = %q, want it to say there was no baseline", got[0].Detail)
	}

	working := f.variant("newrelic-java", func(in, out string) []string { return with(plain(out), "newrelic") })
	wantKinds(t, "working agent, no baseline", f.doc("backtest", working).Judge(working, nil))

	dotnet := f.variant("newrelic-dotnet", dotnetRuntime)
	wantKinds(t, "inert .NET agent, no baseline", f.doc("backtest", dotnet).Judge(dotnet, nil))
}

// A baseline has no agent to be inactive, whatever it is compared with; the
// other two problems apply to it like to any relay.
func TestJudge_BaselineVariant(t *testing.T) {
	f := newFixture(t)
	base := f.variant("none-java", noAgent)
	doc := f.doc("matrix", base)
	wantKinds(t, "baseline, no baseline given", doc.Judge(base, nil))
	wantKinds(t, "baseline against itself", doc.Judge(base, base))

	hop(base, "http->http").CanaryLeak = true
	wantKinds(t, "leaking baseline", doc.Judge(base, nil), ProblemCanaryLeak)
}

// A variant that is registered but was not run has nothing to judge.
func TestJudge_MissingVariant(t *testing.T) {
	f := newFixture(t)
	res, err := Build(f.reg, nil, Run{RunID: "run-test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range res.Variants {
		if got := res.Judge(v, res.BaselineFor(v, nil)); got != nil {
			t.Errorf("%s (missing): problems = %+v", v.ID, got)
		}
	}
	res.SetValidity(nil)
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"invalid"`) {
		t.Error("a document with nothing measured must not carry invalid")
	}
}

// The baseline is the document's own none-<language> when it measured one,
// otherwise the second document's.
func TestBaselineFor(t *testing.T) {
	f := newFixture(t)
	own, latest := f.variant("none-java", noAgent), f.variant("none-java", noAgent)
	own.MeasuredIn, latest.MeasuredIn = "run-own", "run-latest"
	v := f.variant("elastic-java", noAgent)
	other := f.doc("matrix", latest, f.variant("none-dotnet", dotnetRuntime))

	if got := f.doc("matrix", v, own).BaselineFor(v, other); got != own {
		t.Errorf("own baseline present: got %+v", got)
	}
	if got := f.doc("backtest", v).BaselineFor(v, other); got != latest {
		t.Errorf("single-variant document: got %+v, want the other document's none-java", got)
	}
	absent := &Variant{ID: "none-java", Tool: "none", Language: "java", Missing: true}
	if got := f.doc("matrix", v, absent).BaselineFor(v, other); got != latest {
		t.Errorf("own baseline not run: got %+v, want the other document's", got)
	}
	if got := f.doc("backtest", v).BaselineFor(v, nil); got != nil {
		t.Errorf("no baseline anywhere: got %+v", got)
	}
	ruby := f.variant("elastic-ruby", noAgent)
	if got := f.doc("backtest", ruby).BaselineFor(ruby, other); got != nil {
		t.Errorf("another language's baseline must not be used: got %+v", got)
	}
	if got := f.doc("matrix", own).BaselineFor(own, other); got != nil {
		t.Errorf("a baseline has no baseline: got %+v", got)
	}
}

// The verdict is stored only on variants that are not valid, travels into
// the per-version document and its index entry, and survives a round trip.
func TestSetValidityIsRecordedAndIndexed(t *testing.T) {
	f := newFixture(t)
	latest := f.doc("matrix", f.variant("none-java", noAgent))
	inert := f.variant("elastic-java", noAgent)
	working := f.variant("otel-java", func(in, out string) []string { return with(plain(out), "traceparent") })
	res := f.doc("backtest", inert, working)
	res.Summary.CanaryLeaks = []string{}
	res.SetValidity(latest)

	wantKinds(t, "elastic-java", inert.Invalid, ProblemAgentInactive)
	if working.Invalid != nil {
		t.Errorf("otel-java: invalid = %+v", working.Invalid)
	}

	path := t.TempDir() + "/results.json"
	if err := res.Write(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), `"invalid"`); n != 1 {
		t.Errorf("%d variants serialise invalid, want only elastic-java", n)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Variant("elastic-java").Invalid, inert.Invalid) {
		t.Errorf("round trip: %+v", back.Variant("elastic-java").Invalid)
	}

	for id, want := range map[string][]string{"elastic-java": {ProblemAgentInactive}, "otel-java": {}} {
		doc, err := back.Extract(id)
		if err != nil {
			t.Fatal(err)
		}
		e, err := doc.Entry("1.53.0")
		if err != nil {
			t.Fatal(err)
		}
		wantKinds(t, id+" index entry", e.Invalid, want...)
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		if has := strings.Contains(string(b), `"invalid"`); has != (len(want) > 0) {
			t.Errorf("%s index entry: %s", id, b)
		}
	}
}
