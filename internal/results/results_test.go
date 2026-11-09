package results

import (
	"encoding/json"
	"testing"

	"github.com/signadot/context-propagation-matrix/internal/registry"
)

// A variant that is registered but did not run must serialise with empty
// arrays, not null: the site types hops/cells as arrays.
func TestBuild_MissingVariantSerialisesEmptyArrays(t *testing.T) {
	reg, err := registry.Load("../../variants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Build(reg, nil, Run{RunID: "run-test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Variants) == 0 {
		t.Fatal("no variants")
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Variants []struct {
			ID      string            `json:"id"`
			Missing bool              `json:"missing"`
			Hops    *json.RawMessage  `json:"hops"`
			Cells   *json.RawMessage  `json:"cells"`
			Counts  map[string]*Count `json:"counts"`
		} `json:"variants"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	for _, v := range doc.Variants {
		if !v.Missing {
			t.Errorf("%s: missing=false with no raw results", v.ID)
		}
		if v.Hops == nil || string(*v.Hops) != "[]" {
			t.Errorf("%s: hops = %s, want []", v.ID, rawString(v.Hops))
		}
		if v.Cells == nil || string(*v.Cells) != "[]" {
			t.Errorf("%s: cells = %s, want []", v.ID, rawString(v.Cells))
		}
		if len(v.Counts) == 0 {
			t.Errorf("%s: counts empty; want one zeroed Count per header", v.ID)
		}
	}
}

// Measured variants take this run's identity; carried-over ones keep the
// provenance of the release they came from, falling back to that release's
// run id for results written before the field existed; a carried-over
// variant with nothing to inherit from is an error.
func TestSetProvenance(t *testing.T) {
	v := func(id string, missing bool) *Variant { return &Variant{ID: id, Missing: missing} }
	prev := &Results{
		Run: Run{RunID: "run-2026-11-02", HarnessCommit: "aaa", MatrixVersion: 1},
		Variants: []*Variant{
			// Measured in run-2026-11-02 itself (field absent: an old release).
			v("otel-java", false),
			// Carried into run-2026-11-02 from an even older run.
			{ID: "otel-node", MeasuredIn: "run-2026-10-26", MeasuredCommit: "999", MatrixVersion: 1},
			v("otel-go", true),
		},
	}
	cur := &Results{
		Run:      Run{RunID: "run-2026-11-09", HarnessCommit: "bbb", MatrixVersion: 2},
		Variants: []*Variant{v("otel-java", false), v("otel-node", false), v("otel-php", false), v("otel-go", true)},
	}
	if err := cur.SetProvenance(prev, map[string]bool{"otel-php": true}, map[string]string{"otel-php": "1.2.1"}); err != nil {
		t.Fatal(err)
	}
	want := map[string][3]any{
		"otel-java": {"run-2026-11-02", "aaa", 1},
		"otel-node": {"run-2026-10-26", "999", 1},
		"otel-php":  {"run-2026-11-09", "bbb", 2},
		"otel-go":   {"", "", 0},
	}
	for _, got := range cur.Variants {
		w := want[got.ID]
		if got.MeasuredIn != w[0] || got.MeasuredCommit != w[1] || got.MatrixVersion != w[2] {
			t.Errorf("%s: got (%q, %q, %d), want %v", got.ID, got.MeasuredIn, got.MeasuredCommit, got.MatrixVersion, w)
		}
	}

	// A full run (measured == nil) stamps everything with the run itself.
	full := &Results{Run: Run{RunID: "run-x", HarnessCommit: "ccc", MatrixVersion: 1},
		Variants: []*Variant{v("otel-java", false), v("otel-go", true)}}
	if err := full.SetProvenance(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if full.Variants[0].MeasuredIn != "run-x" || full.Variants[1].MeasuredIn != "" {
		t.Errorf("full run: got %q / %q", full.Variants[0].MeasuredIn, full.Variants[1].MeasuredIn)
	}

	// Carried over with no prev, or with prev lacking the variant → error.
	orphan := &Results{Run: Run{RunID: "run-y"}, Variants: []*Variant{v("otel-java", false), v("otel-php", false)}}
	if err := orphan.SetProvenance(nil, map[string]bool{"otel-php": true}, nil); err == nil {
		t.Error("no prev: want error for carried-over otel-java")
	}
	if err := orphan.SetProvenance(&Results{Variants: []*Variant{v("otel-java", true)}}, map[string]bool{"otel-php": true}, nil); err == nil {
		t.Error("prev has otel-java missing: want error")
	}
}

func TestCanonicalVersion(t *testing.T) {
	cases := []struct{ pinned, reported, want string }{
		{"1.2.1", "open-telemetry/sdk 1.15.0 (ext-opentelemetry 1.2.1)", "1.2.1"}, // the pin wins
		{"", "2.31.1", "2.31.1"},
		{"", "v2.10.1", "2.10.1"},
		{"", "0.65b0", "0.65b0"},
		{"", "ddtrace 1.25.0", "1.25.0"},
		{"", "newrelic 12.10.0.39", "12.10.0.39"},
		{"", "open-telemetry/sdk 1.15.0 (ext-opentelemetry 1.2.1)", ""}, // two numbers: ambiguous
		{"", "unknown", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		v := &Variant{Tracer: &Package{Version: c.reported, Pinned: c.pinned}}
		if got := v.CanonicalVersion(); got != c.want {
			t.Errorf("pinned %q reported %q: got %q, want %q", c.pinned, c.reported, got, c.want)
		}
	}
	if got := (&Variant{}).CanonicalVersion(); got != "" {
		t.Errorf("baseline: got %q", got)
	}
}

// Extract keeps one variant with the annotations that touch it and a summary
// for it alone; the index entry and asset name follow from the document.
func TestExtractAndVersionIndex(t *testing.T) {
	reg, err := registry.Load("../../variants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Build(reg, nil, Run{RunID: "run-x", Kind: "matrix", HarnessCommit: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	// Pretend otel-java and otel-node were measured.
	for _, id := range []string{"otel-java", "otel-node"} {
		v := res.Variant(id)
		v.Missing = false
		v.Tracer = &Package{Package: *reg.Variant(id).Tracer, Version: "2.31.1", Pinned: "2.31.1"}
		v.Cells = []Cell{{CellID: CellID(id, "traceparent", "http", "http"), Header: "traceparent", Outcome: OutcomePass}}
		v.Counts["traceparent"].add(OutcomePass)
	}
	res.Annotations = []Annotation{
		{ID: "a-java", Variants: []string{"otel-java"}},
		{ID: "a-cell", Cells: []string{"otel-node/traceparent/http-http"}},
	}
	res.Summary.Scorecard = scorecard(reg, res.Variants)
	res.Summary.CanaryLeaks = []string{"otel-node http->kafka"}
	if err := res.SetProvenance(nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	doc, err := res.Extract("otel-node")
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Variants) != 1 || doc.Variants[0].ID != "otel-node" {
		t.Fatalf("variants = %v", doc.Variants)
	}
	if len(doc.Annotations) != 1 || doc.Annotations[0].ID != "a-cell" {
		t.Errorf("annotations = %+v, want just a-cell", doc.Annotations)
	}
	if s := doc.Summary; s.VariantCount != 1 || s.InstrumentedN != 1 || s.CellCount != 1 ||
		len(s.CanaryLeaks) != 1 || len(s.Scorecard) != 1 || s.Scorecard[0].Variant != "otel-node" ||
		s.TotalsByHeader["traceparent"].Pass != 1 {
		t.Errorf("summary = %+v", s)
	}
	if _, err := res.Extract("otel-go"); err == nil {
		t.Error("extracting a missing variant should fail")
	}

	e, err := doc.Entry("2.31.1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Asset != "2.31.1.json" || e.MeasuredIn != "run-x" || e.MeasuredCommit != "abc" || e.Kind != "matrix" ||
		e.MatrixVersion != reg.MatrixVersion || e.Counts["traceparent"].Pass != 1 || e.CanaryLeaks != 1 || e.Reported != "2.31.1" {
		t.Errorf("entry = %+v", e)
	}
	if _, err := AssetName("open-telemetry/sdk 1.15.0"); err == nil {
		t.Error("slash and space must not name an asset")
	}

	dir := t.TempDir()
	idx, err := LoadVersionIndex(dir+"/index.json", doc.Variants[0])
	if err != nil {
		t.Fatal(err)
	}
	idx.Upsert(VersionEntry{Version: "2.30.0", Asset: "2.30.0.json"})
	idx.Upsert(e)
	idx.Upsert(VersionEntry{Version: "2.31.1", Asset: "2.31.1.json", Kind: "backtest"}) // same version: replaces
	if len(idx.Versions) != 2 {
		t.Fatalf("upsert: %d entries, want 2", len(idx.Versions))
	}
	idx.Upsert(VersionEntry{Version: "2.33.1", Asset: "2.33.1.json"})
	if dropped := idx.Prune(map[string]bool{"2.31.1.json": true, "2.33.1.json": true}); len(dropped) != 1 || dropped[0] != "2.30.0.json" {
		t.Errorf("prune dropped %v", dropped)
	}
	if err := idx.Write(dir + "/index.json"); err != nil {
		t.Fatal(err)
	}
	again, err := LoadVersionIndex(dir+"/index.json", doc.Variants[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Versions) != 2 || again.Versions[0].Version != "2.33.1" || again.Versions[1].Kind != "backtest" {
		t.Errorf("round trip: %+v", again.Versions)
	}
	if _, err := LoadVersionIndex(dir+"/index.json", res.Variant("otel-java")); err == nil {
		t.Error("an index for another variant must be rejected")
	}
}

func rawString(r *json.RawMessage) string {
	if r == nil {
		return "<absent>"
	}
	return string(*r)
}
