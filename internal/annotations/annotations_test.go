package annotations

import (
	"os"
	"strings"
	"testing"

	"github.com/signadot/context-propagation-matrix/internal/registry"
	"github.com/signadot/context-propagation-matrix/internal/results"
)

// fixture builds a two-variant results document with the four boundaries
// that matter for these tests and lets the caller set outcomes per cell.
func fixture(outcomes map[string]results.Outcome) *results.Results {
	mk := func(id, tool, lang, ver string) *results.Variant {
		v := &results.Variant{ID: id, Tool: tool, Language: lang, Flavour: "default",
			Tracer: &results.Package{Package: registry.Package{Name: "tracer"}, Version: ver}}
		for _, h := range []string{"baggage", "tracestate"} {
			for _, b := range []string{"http->http", "http->kafka", "kafka->http", "kafka->kafka"} {
				in, out, _ := strings.Cut(b, "->")
				cid := id + "/" + h + "/" + in + "-" + out
				o := results.OutcomeFail
				if x, ok := outcomes[cid]; ok {
					o = x
				}
				v.Cells = append(v.Cells, results.Cell{CellID: cid, Header: h, Boundary: b, Inbound: in, Outbound: out, Outcome: o})
			}
		}
		return v
	}
	return &results.Results{Variants: []*results.Variant{
		mk("datadog-java", "datadog", "java", "1.65.1"),
		mk("datadog-node", "datadog", "node", "6.14.0"),
	}}
}

func ann(id string, status string, sel ...Selector) *Annotation {
	return &Annotation{ID: id, Title: id, Cause: "c", Status: status, Match: sel, sourceFile: "test.yaml"}
}

func fatal(ps []Problem) (n int) {
	for _, p := range ps {
		if p.Fatal {
			n++
		}
	}
	return
}

func TestApply_AttachesToFailingCells(t *testing.T) {
	res := fixture(map[string]results.Outcome{"datadog-java/baggage/http-http": results.OutcomePass})
	a := ann("dd-java-baggage", "open", Selector{Tool: []string{"datadog"}, Language: []string{"java"}, Header: []string{"baggage"}})
	ps := Apply(res, []*Annotation{a})
	// One of the four cells it covers passes: partially resolved, a warning
	// (see TestApply_PartiallyResolvedWarnsNamingVariantAndCells).
	if fatal(ps) != 0 || len(ps) != 1 || !strings.Contains(ps[0].Message, "partially resolved") {
		t.Fatalf("want one partial-resolution warning, nothing else: %v", ps)
	}
	got := res.Annotations[0]
	if got.Resolved || len(got.Cells) != 3 {
		t.Fatalf("want 3 failing cells attached, not resolved; got resolved=%v cells=%v", got.Resolved, got.Cells)
	}
	for _, c := range got.Cells {
		if c == "datadog-java/baggage/http-http" {
			t.Fatalf("passing cell must not be attached while others fail: %v", got.Cells)
		}
	}
}

func TestApply_StaleSelectorIsFatal(t *testing.T) {
	res := fixture(nil)
	a := ann("gone", "open", Selector{Tool: []string{"datadog"}, Language: []string{"php"}}) // no php variant
	ps := Apply(res, []*Annotation{a})
	if fatal(ps) != 1 {
		t.Fatalf("selector naming a non-existent variant must be fatal; got %v", ps)
	}
	b := ann("gone-boundary", "open", Selector{Tool: []string{"datadog"}, Boundary: []string{"*->sqs"}})
	ps = Apply(res, []*Annotation{b})
	if fatal(ps) != 1 {
		t.Fatalf("selector naming a non-existent boundary must be fatal; got %v", ps)
	}
}

func TestApply_NotRunVariantsAreInfoNotStale(t *testing.T) {
	res := fixture(nil)
	// Registered but absent from this release (secret-gated tool, no key):
	// results.Load marks these Missing with no cells.
	res.Variants = append(res.Variants,
		&results.Variant{ID: "newrelic-java", Tool: "newrelic", Language: "java", Missing: true},
		&results.Variant{ID: "newrelic-node", Tool: "newrelic", Language: "node", Missing: true})
	a := ann("newrelic-no-baggage", "open", Selector{Tool: []string{"newrelic"}, Header: []string{"baggage"}, Outcome: []string{"fail"}})
	ps := Apply(res, []*Annotation{a})
	if fatal(ps) != 0 {
		t.Fatalf("variants that were not run must not make the selector stale: %v", ps)
	}
	if len(ps) != 1 || !ps[0].Info || !strings.Contains(ps[0].Message, "newrelic-java, newrelic-node") {
		t.Fatalf("want one Info problem naming the missing variants; got %v", ps)
	}
	got := res.Annotations[0]
	if got.Resolved || len(got.Cells) != 0 {
		t.Fatalf("nothing to attach and nothing resolved; got resolved=%v cells=%v", got.Resolved, got.Cells)
	}
	// A selector that is stale for a *present* variant stays fatal even when
	// another variant it names is missing: the boundary really does not exist.
	b := ann("gone-boundary", "open", Selector{Tool: []string{"datadog", "newrelic"}, Boundary: []string{"*->sqs"}})
	if ps := Apply(res, []*Annotation{b}); fatal(ps) != 1 {
		t.Fatalf("non-existent boundary on a present variant must be fatal; got %v", ps)
	}
}

func TestApply_AllPassBecomesResolvedNotFatal(t *testing.T) {
	out := map[string]results.Outcome{}
	for _, b := range []string{"http-http", "http-kafka", "kafka-http", "kafka-kafka"} {
		out["datadog-java/baggage/"+b] = results.OutcomePass
	}
	res := fixture(out)
	a := ann("dd-java-baggage", "open", Selector{Tool: []string{"datadog"}, Language: []string{"java"}, Header: []string{"baggage"}})
	ps := Apply(res, []*Annotation{a})
	if fatal(ps) != 0 {
		t.Fatalf("an upstream fix must never be fatal: %v", ps)
	}
	if len(ps) != 1 || !strings.Contains(ps[0].Message, "set status: fixed") {
		t.Fatalf("want one warning asking to mark fixed; got %v", ps)
	}
	got := res.Annotations[0]
	if !got.Resolved {
		t.Fatalf("want resolved=true")
	}
	if len(got.Cells) != 4 {
		t.Fatalf("resolved annotation must stay attached to the now-passing cells; got %v", got.Cells)
	}
	if got.FixedIn != "1.65.1" {
		t.Fatalf("fixed_in must be filled from the tracer version; got %q", got.FixedIn)
	}
	if got.Variants[0] != "datadog-java" {
		t.Fatalf("variants: %v", got.Variants)
	}
}

func TestApply_ResolvedKeepsExplicitFixedInAndIsQuietWhenStatusFixed(t *testing.T) {
	out := map[string]results.Outcome{}
	for _, b := range []string{"http-http", "http-kafka", "kafka-http", "kafka-kafka"} {
		out["datadog-java/baggage/"+b] = results.OutcomePass
	}
	res := fixture(out)
	a := ann("dd-java-baggage", "fixed", Selector{Tool: []string{"datadog"}, Language: []string{"java"}, Header: []string{"baggage"}})
	a.FixedIn = "1.64.0"
	ps := Apply(res, []*Annotation{a})
	if len(ps) != 0 {
		t.Fatalf("a correctly marked fixed annotation should produce no problems: %v", ps)
	}
	if got := res.Annotations[0]; !got.Resolved || got.FixedIn != "1.64.0" {
		t.Fatalf("want resolved with the YAML fixed_in kept; got %+v", got)
	}
}

func TestApply_FixedButFailingWarnsRegression(t *testing.T) {
	res := fixture(nil) // everything fails
	a := ann("dd-java-baggage", "fixed", Selector{Tool: []string{"datadog"}, Language: []string{"java"}, Header: []string{"baggage"}})
	ps := Apply(res, []*Annotation{a})
	if fatal(ps) != 0 || len(ps) != 1 || !strings.Contains(ps[0].Message, "regression") {
		t.Fatalf("want a non-fatal regression warning; got %v", ps)
	}
	if res.Annotations[0].Resolved {
		t.Fatalf("must not be resolved while cells fail")
	}
}

func TestApply_MultiVariantFixedInListsVersions(t *testing.T) {
	out := map[string]results.Outcome{}
	for _, v := range []string{"datadog-java", "datadog-node"} {
		for _, b := range []string{"http-http", "http-kafka", "kafka-http", "kafka-kafka"} {
			out[v+"/tracestate/"+b] = results.OutcomePass
		}
	}
	res := fixture(out)
	a := ann("dd-tracestate", "open", Selector{Tool: []string{"datadog"}, Header: []string{"tracestate"}})
	Apply(res, []*Annotation{a})
	got := res.Annotations[0]
	if !got.Resolved || got.FixedIn != "datadog-java@1.65.1, datadog-node@6.14.0" {
		t.Fatalf("want per-variant versions when they differ; got resolved=%v fixed_in=%q", got.Resolved, got.FixedIn)
	}
}

func TestApply_ExplicitOutcomeNoLongerPresentWarns(t *testing.T) {
	res := fixture(nil) // all fail, none new_trace
	a := ann("dd-java-newtrace", "open", Selector{Tool: []string{"datadog"}, Language: []string{"java"}, Header: []string{"baggage"}, Outcome: []string{"new_trace"}})
	ps := Apply(res, []*Annotation{a})
	if fatal(ps) != 0 || len(ps) != 1 || !strings.Contains(ps[0].Message, "none currently has outcome new_trace") {
		t.Fatalf("want a non-fatal outcome-drift warning; got %v", ps)
	}
	got := res.Annotations[0]
	if got.Resolved || len(got.Cells) != 0 {
		t.Fatalf("cells with a different failure must not be attached or count as resolved: %+v", got)
	}
}

// warnings returns the non-fatal, non-info problems.
func warnings(ps []Problem) (out []Problem) {
	for _, p := range ps {
		if !p.Fatal && !p.Info {
			out = append(out, p)
		}
	}
	return
}

func TestApply_PartiallyResolvedWarnsNamingVariantAndCells(t *testing.T) {
	res := fixture(map[string]results.Outcome{
		"datadog-java/baggage/http-kafka":  results.OutcomePass,
		"datadog-java/baggage/kafka-kafka": results.OutcomePass,
	})
	a := ann("dd-baggage", "open", Selector{Tool: []string{"datadog"}, Header: []string{"baggage"}})
	ps := Apply(res, []*Annotation{a})
	if fatal(ps) != 0 || len(ps) != 1 {
		t.Fatalf("want exactly one non-fatal warning (only datadog-java drifted); got %v", ps)
	}
	p := ps[0]
	if p.Annotation != "dd-baggage" || p.Info || p.Fatal || !p.Advisory {
		t.Fatalf("want an advisory warning attributed to the annotation; got %+v", p)
	}
	for _, want := range []string{"partially resolved", "datadog-java", "baggage/http-kafka", "baggage/kafka-kafka", "2 other cell(s)"} {
		if !strings.Contains(p.Message, want) {
			t.Fatalf("message lacks %q: %s", want, p.Message)
		}
	}
	if strings.Contains(p.Message, "datadog-node") || strings.Contains(p.Message, "baggage/http-http") {
		t.Fatalf("message must name only the drifted variant and the passing cells: %s", p.Message)
	}
	if !strings.Contains(p.String(), "dd-baggage") {
		t.Fatalf("rendered problem must carry the annotation id: %s", p)
	}
	got := res.Annotations[0]
	if got.Resolved || len(got.Cells) != 6 {
		t.Fatalf("still attached to the six failing cells and not resolved; got resolved=%v cells=%v", got.Resolved, got.Cells)
	}
}

func TestApply_PartialIsQuietWithExplicitOutcomeThatStillMatches(t *testing.T) {
	// The selector claims only `fail` cells. A passing cell inside its shape
	// was never claimed, and a cell with another failure outcome is not its
	// business either: nothing it says has become false.
	res := fixture(map[string]results.Outcome{
		"datadog-java/baggage/http-kafka":  results.OutcomePass,
		"datadog-java/baggage/kafka-kafka": results.OutcomeNewTrace,
	})
	a := ann("dd-java-baggage-fail", "open", Selector{Tool: []string{"datadog"}, Language: []string{"java"}, Header: []string{"baggage"}, Outcome: []string{"fail"}})
	ps := Apply(res, []*Annotation{a})
	if len(ps) != 0 {
		t.Fatalf("an outcome filter that still matches must not warn about passing cells: %v", ps)
	}
	if got := res.Annotations[0]; got.Resolved || len(got.Cells) != 2 {
		t.Fatalf("want the two fail cells attached; got resolved=%v cells=%v", got.Resolved, got.Cells)
	}
}

func TestApply_PartialIgnoresExplicitOutcomeSelectorButNotItsDefaultSibling(t *testing.T) {
	// One annotation, two selectors: the explicit-outcome one (datadog-node)
	// stays quiet over its passing cell, the default one (datadog-java) warns.
	res := fixture(map[string]results.Outcome{
		"datadog-node/baggage/http-http":    results.OutcomePass,
		"datadog-java/tracestate/http-http": results.OutcomePass,
	})
	a := ann("mixed", "open",
		Selector{Tool: []string{"datadog"}, Language: []string{"node"}, Header: []string{"baggage"}, Outcome: []string{"fail"}},
		Selector{Tool: []string{"datadog"}, Language: []string{"java"}, Header: []string{"tracestate"}})
	ws := warnings(Apply(res, []*Annotation{a}))
	if len(ws) != 1 || !strings.Contains(ws[0].Message, "datadog-java") || strings.Contains(ws[0].Message, "datadog-node") {
		t.Fatalf("want one warning, for datadog-java only; got %v", ws)
	}
}

func TestApply_PartialMultiVariantOneFixedOneNot(t *testing.T) {
	out := map[string]results.Outcome{}
	for _, b := range []string{"http-http", "http-kafka", "kafka-http", "kafka-kafka"} {
		out["datadog-java/tracestate/"+b] = results.OutcomePass // java fixed, node still fails
	}
	res := fixture(out)
	a := ann("dd-tracestate", "open", Selector{Tool: []string{"datadog"}, Header: []string{"tracestate"}})
	ps := Apply(res, []*Annotation{a})
	if fatal(ps) != 0 || len(ps) != 1 {
		t.Fatalf("want one warning for the fixed variant; got %v", ps)
	}
	m := ps[0].Message
	if !strings.Contains(m, "datadog-java") || strings.Contains(m, "datadog-node") || !strings.Contains(m, "tracestate/kafka-http") {
		t.Fatalf("warning must name the fixed variant and its four cells only: %s", m)
	}
	if !strings.Contains(m, "other variants") {
		t.Fatalf("a fully passing variant has no failing cells of its own; message should say the annotation still applies elsewhere: %s", m)
	}
	got := res.Annotations[0]
	if got.Resolved || len(got.Variants) != 1 || got.Variants[0] != "datadog-node" || len(got.Cells) != 4 {
		t.Fatalf("the annotation stays attached to datadog-node's failing cells only; got %+v", got)
	}
	// Once the other variant passes too, the all-pass warning takes over and
	// the per-variant one is not repeated.
	for _, b := range []string{"http-http", "http-kafka", "kafka-http", "kafka-kafka"} {
		out["datadog-node/tracestate/"+b] = results.OutcomePass
	}
	res = fixture(out)
	ps = Apply(res, []*Annotation{ann("dd-tracestate", "open", Selector{Tool: []string{"datadog"}, Header: []string{"tracestate"}})})
	if len(ps) != 1 || !strings.Contains(ps[0].Message, "set status: fixed") {
		t.Fatalf("want only the all-pass warning; got %v", ps)
	}
}

func TestApply_PartialIsQuietWhenStatusFixedOrCellsAreNotPasses(t *testing.T) {
	// status: fixed expects passing cells; failing ones already warn as a
	// regression, so the partial warning would only repeat it.
	res := fixture(map[string]results.Outcome{"datadog-java/baggage/http-kafka": results.OutcomePass})
	a := ann("dd-java-baggage", "fixed", Selector{Tool: []string{"datadog"}, Language: []string{"java"}, Header: []string{"baggage"}})
	ps := Apply(res, []*Annotation{a})
	if len(ps) != 1 || !strings.Contains(ps[0].Message, "regression") {
		t.Fatalf("want only the regression warning; got %v", ps)
	}
	// undelivered and not_run are not passes: a harness limit is not drift.
	res = fixture(map[string]results.Outcome{
		"datadog-java/baggage/http-kafka":  results.OutcomeUndelivered,
		"datadog-java/baggage/kafka-kafka": results.OutcomeNotRun,
	})
	a = ann("dd-java-baggage", "open", Selector{Tool: []string{"datadog"}, Language: []string{"java"}, Header: []string{"baggage"}})
	if ps := Apply(res, []*Annotation{a}); len(ps) != 0 {
		t.Fatalf("undelivered and not_run cells must not read as fixed: %v", ps)
	}
}

func TestMarkFixed(t *testing.T) {
	dir := t.TempDir()
	src := `# vendor notes
- id: first
  title: "first"
  match:
    - tool: datadog
      boundary: ["*->kafka", "*->grpc"]
  cause: "c"
  status: open          # open | fixed
  links:
    - { title: "issue", url: "https://example.com/1" }

- id: second
  title: "second"
  match:
    - tool: datadog
  cause: "c2"
`
	f := dir + "/x.yaml"
	if err := os.WriteFile(f, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MarkFixed(dir, "first", "1.66.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := MarkFixed(dir, "second", "2.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := MarkFixed(dir, "nope", "1"); err == nil {
		t.Fatal("unknown id must error")
	}
	b, _ := os.ReadFile(f)
	got := string(b)
	for _, want := range []string{
		"# vendor notes",                  // comments kept
		"  status: fixed  # open | fixed", // trailing comment kept
		"  fixed_in: \"1.66.0\"",
		"  cause: \"c2\"\n  status: fixed\n  fixed_in: \"2.0.0\"", // inserted after cause when no status line
		`boundary: ["*->kafka", "*->grpc"]`,                       // untouched formatting
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	anns, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range anns {
		if a.Status != "fixed" || a.FixedIn == "" {
			t.Fatalf("%s not fixed after rewrite: %+v", a.ID, a)
		}
	}
	// Idempotent: marking again just updates the version.
	if _, err := MarkFixed(dir, "first", "1.67.0"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(f)
	if strings.Count(string(b), "fixed_in:") != 2 || !strings.Contains(string(b), `"1.67.0"`) {
		t.Fatalf("second mark must replace, not duplicate:\n%s", b)
	}
}

func TestUnexplained(t *testing.T) {
	res := fixture(map[string]results.Outcome{
		"datadog-java/baggage/http-http":   results.OutcomePass,
		"datadog-node/baggage/http-kafka":  results.OutcomeUndelivered,
		"datadog-node/baggage/kafka-kafka": results.OutcomeNotRun,
	})
	a := ann("dd-java-baggage", "open", Selector{Tool: []string{"datadog"}, Language: []string{"java"}, Header: []string{"baggage"}})
	Apply(res, []*Annotation{a})
	got := Unexplained(res)
	// datadog-java: tracestate x4 unexplained (baggage covered).
	// datadog-node: baggage http-http, kafka-http (http-kafka undelivered and kafka-kafka not_run excluded) + tracestate x4.
	if len(got) != 10 {
		t.Fatalf("want 10 unexplained cells, got %d: %v", len(got), got)
	}
	for _, c := range got {
		if strings.HasPrefix(c, "datadog-java/baggage/") || strings.Contains(c, "http-kafka") && strings.HasPrefix(c, "datadog-node/baggage") {
			t.Fatalf("covered or undelivered cell reported: %s", c)
		}
	}
}
