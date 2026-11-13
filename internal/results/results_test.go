package results

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/signadot/context-propagation-matrix/internal/eval"
	"github.com/signadot/context-propagation-matrix/internal/raw"
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

// The aggregator evaluates vendor_native, traceparent_span and
// traceparent_flags from each hop's observed headers, per tool, whatever the
// driver recorded. They are optional keys, and a hop that passed the seeded
// traceparent through verbatim is listed in the summary but still scored as
// a pass.
func TestBuild_HopObservations(t *testing.T) {
	reg, err := registry.Load("../../variants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	const (
		traceID   = "239b89fcef379d4793e9097d24404872"
		seededTP  = "00-" + traceID + "-a9afda86883fab17-01"
		newSpanTP = "00-" + traceID + "-2138c1a30d31ad21-01"
		otherTP   = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
		ddLow64   = "10658060426232744050" // low 64 bits of traceID, decimal
	)
	seed := raw.Seeded{TraceID: traceID, SpanID: "a9afda86883fab17", RoutingKey: "rk-3e26ef84",
		Headers: map[string]string{"traceparent": seededTP}}
	// A delivered hop as the driver writes it: the status is the evaluation
	// of the observed headers.
	hop := func(boundary string, h map[string][]string) raw.TestResult {
		s := seed
		return raw.TestResult{Boundary: boundary, TestID: "t", Delivered: true, Seeded: &s,
			Observed: &raw.Observed{Transport: "http", Headers: h}, Status: eval.Evaluate(h, s)}
	}
	type hdrs = map[string][]string
	nr := base64.StdEncoding.EncodeToString([]byte(`{"v":[0,1],"d":{"ty":"App","tr":"` + traceID + `","sa":false}}`))

	raws := []*raw.VariantResult{
		{Variant: "otel-node", Tests: []raw.TestResult{
			hop("http->http", hdrs{"traceparent": {seededTP}}),
			hop("http->grpc", hdrs{"traceparent": {newSpanTP}}),
		}},
		{Variant: "datadog-java", Tests: []raw.TestResult{
			hop("http->http", hdrs{"traceparent": {newSpanTP}, "x-datadog-trace-id": {ddLow64}}),
			hop("http->grpc", hdrs{"traceparent": {otherTP}, "x-datadog-trace-id": {"42"}}),
			hop("http->kafka", hdrs{}),
			{Boundary: "http->rabbitmq", TestID: "t", Seeded: &seed, Error: "observe: no observation within 45s"},
		}},
		{Variant: "elastic-java", Tests: []raw.TestResult{
			hop("http->http", hdrs{"traceparent": {newSpanTP}, "elastic-apm-traceparent": {newSpanTP}}),
			hop("http->grpc", hdrs{"traceparent": {newSpanTP}}),
		}},
		{Variant: "newrelic-java", Tests: []raw.TestResult{
			hop("http->http", hdrs{"traceparent": {"00-" + traceID + "-2138c1a30d31ad21-00"}, "newrelic": {nr}}),
			hop("http->grpc", hdrs{"traceparent": {newSpanTP}, "newrelic": {"%%%"}}),
		}},
		// The baseline is recorded like any other variant.
		{Variant: "none-node", Tests: []raw.TestResult{
			hop("http->http", hdrs{}),
			hop("http->grpc", hdrs{"traceparent": {seededTP}}),
		}},
	}
	res, err := Build(reg, raws, Run{RunID: "run-test"})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string][3]string{ // vendor_native, traceparent_span, traceparent_flags
		"otel-node http->http":         {"", "same_span", "01"},
		"otel-node http->grpc":         {"", "new_span", "01"},
		"datadog-java http->http":      {"present_same_trace", "new_span", "01"},
		"datadog-java http->grpc":      {"present_new_trace", "", ""},
		"datadog-java http->kafka":     {"absent", "", ""},
		"datadog-java http->rabbitmq":  {"", "", ""}, // undelivered: nothing to evaluate
		"elastic-java http->http":      {"present_same_trace", "new_span", "01"},
		"elastic-java http->grpc":      {"absent", "new_span", "01"},
		"newrelic-java http->http":     {"present_same_trace", "new_span", "00"},
		"newrelic-java http->grpc":     {"present_new_trace", "new_span", "01"},
		"none-node http->http":         {"", "", ""},
		"none-node http->grpc":         {"", "same_span", "01"},
		"none-node kafka->kafka":       {"", "", ""}, // not run
		"datadog-java kafka->rabbitmq": {"", "", ""}, // not run
	}
	seen := 0
	for _, v := range res.Variants {
		for _, h := range v.Hops {
			w, ok := want[v.ID+" "+h.Boundary]
			if !ok {
				continue
			}
			seen++
			if got := [3]string{h.VendorNative, h.TraceparentSpan, h.TraceparentFlags}; got != w {
				t.Errorf("%s %s: got %q, want %q", v.ID, h.Boundary, got, w)
			}
		}
	}
	if seen != len(want) {
		t.Errorf("checked %d hops, want %d", seen, len(want))
	}

	// Registry order, like canary_leaks.
	if got, w := res.Summary.TraceparentPassthroughs, []string{"otel-node http->http", "none-node http->grpc"}; !reflect.DeepEqual(got, w) {
		t.Errorf("summary pass-throughs = %q, want %q", got, w)
	}
	// Informational: a passed-through hop is a traceparent pass like the
	// propagated one next to it, in the cell and in every count.
	outcomes := map[string]Outcome{}
	for _, v := range res.Variants {
		for _, c := range v.Cells {
			outcomes[c.CellID] = c.Outcome
		}
	}
	for _, id := range []string{"otel-node/traceparent/http-http", "otel-node/traceparent/http-grpc", "none-node/traceparent/http-grpc"} {
		if outcomes[id] != OutcomePass {
			t.Errorf("%s: outcome %q, want pass", id, outcomes[id])
		}
	}
	if c := res.Variant("otel-node").Counts["traceparent"]; c.Pass != 2 || c.NotRun != 14 || c.Total != 16 {
		t.Errorf("otel-node traceparent counts = %+v, want 2 passes and 14 not run", *c)
	}
	// otel-node 2, datadog-java 1, elastic-java 2, newrelic-java 2, none-node 1.
	if got := res.Summary.TotalsByHeader["traceparent"].Pass; got != 8 {
		t.Errorf("traceparent passes = %d, want 8", got)
	}

	// A single-variant document keeps its own pass-throughs.
	doc, err := res.Extract("otel-node")
	if err != nil {
		t.Fatal(err)
	}
	if got, w := doc.Summary.TraceparentPassthroughs, []string{"otel-node http->http"}; !reflect.DeepEqual(got, w) {
		t.Errorf("extracted pass-throughs = %q, want %q", got, w)
	}
	if doc, err = res.Extract("datadog-java"); err != nil {
		t.Fatal(err)
	}

	// The new keys are optional: absent where there is nothing to say, so a
	// document without them and one with them read alike to the site.
	keys := func(v any) map[string]json.RawMessage {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	present := func(m map[string]json.RawMessage, names ...string) (out []string) {
		for _, n := range names {
			if _, ok := m[n]; ok {
				out = append(out, n)
			}
		}
		return out
	}
	fields := []string{"vendor_native", "traceparent_span", "traceparent_flags"}
	for id, w := range map[string][]string{
		"otel-node":    {"traceparent_span", "traceparent_flags"}, // http->http
		"datadog-java": fields,
		"none-node":    nil,
	} {
		if got := present(keys(res.Variant(id).Hops[0]), fields...); !reflect.DeepEqual(got, w) {
			t.Errorf("%s http->http: keys %q, want %q", id, got, w)
		}
	}
	if got := present(keys(res.Summary), "canary_leaks", "traceparent_passthroughs"); len(got) != 2 {
		t.Errorf("summary keys %q, want both lists", got)
	}
	if got := present(keys(doc.Summary), "canary_leaks", "traceparent_passthroughs"); !reflect.DeepEqual(got, []string{"canary_leaks"}) {
		t.Errorf("summary without pass-throughs: keys %q, want canary_leaks alone", got)
	}
}

// Every tool that lists native headers must list one the evaluation can read
// a trace id from; otherwise its hops would carry no vendor_native and
// nothing would say why.
func TestNativeHeadersAreEvaluated(t *testing.T) {
	reg, err := registry.Load("../../variants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range reg.ToolOrder {
		tool := reg.Tools[id]
		want := "absent"
		if len(tool.NativeHeaders) == 0 {
			want = ""
		}
		if got := eval.Native(nil, raw.Seeded{}, tool.NativeHeaders); got != want {
			t.Errorf("%s (native_headers %v): nothing observed evaluates to %q, want %q", id, tool.NativeHeaders, got, want)
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

	// A republish measures nothing (measured is empty, not nil): rendering a
	// release again from its own results must not move any variant's
	// provenance to the commit doing the rendering, or the pinned version to
	// whatever that commit pins.
	again := &Results{
		Run:      Run{RunID: "run-2026-11-09", HarnessCommit: "ddd", MatrixVersion: 2},
		Variants: []*Variant{{ID: "otel-java"}, {ID: "otel-node"}, {ID: "otel-php", Tracer: &Package{}}, v("otel-go", true)},
	}
	cur.Variant("otel-php").Tracer = &Package{Pinned: "1.2.1"}
	if err := again.SetProvenance(cur, map[string]bool{}, map[string]string{"otel-php": "9.9.9"}); err != nil {
		t.Fatal(err)
	}
	for _, got := range again.Variants {
		w := want[got.ID]
		if got.MeasuredIn != w[0] || got.MeasuredCommit != w[1] || got.MatrixVersion != w[2] {
			t.Errorf("republish %s: got (%q, %q, %d), want %v", got.ID, got.MeasuredIn, got.MeasuredCommit, got.MatrixVersion, w)
		}
	}
	if p := again.Variant("otel-php").Tracer.Pinned; p != "1.2.1" {
		t.Errorf("republish: otel-php pinned = %q, want the measured 1.2.1", p)
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

	// The bundle follows the index: a missing file is an empty bundle that
	// lacks every listed document, documents are stored byte for byte, and
	// Sync drops what the index no longer lists.
	if _, err := AssetName("versions"); err == nil {
		t.Error(`a version named "versions" would collide with the bundle asset`)
	}
	b, err := LoadVersionBundle(dir+"/versions.json", doc.Variants[0])
	if err != nil {
		t.Fatal(err)
	}
	if missing := b.Sync(again); len(missing) != 2 {
		t.Errorf("empty bundle: missing %v, want both listed versions", missing)
	}
	b.Put("2.33.1", []byte(`{"schema_version":2,"n":1}`))
	b.Put("2.31.1", []byte(`{"schema_version":2,"n":2}`))
	b.Put("2.30.0", []byte(`{"schema_version":2,"n":3}`)) // not in the index any more
	if missing := b.Sync(again); len(missing) != 0 {
		t.Errorf("complete bundle: missing %v", missing)
	}
	if _, stale := b.Documents["2.30.0"]; stale || len(b.Documents) != 2 {
		t.Errorf("sync kept documents %v, want exactly the indexed ones", b.Documents)
	}
	if err := b.Write(dir + "/versions.json"); err != nil {
		t.Fatal(err)
	}
	b2, err := LoadVersionBundle(dir+"/versions.json", doc.Variants[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(b2.Documents["2.31.1"]) != `{"schema_version":2,"n":2}` || b2.Index == nil || len(b2.Index.Versions) != 2 {
		t.Errorf("bundle round trip: %+v", b2)
	}
	if _, err := LoadVersionBundle(dir+"/versions.json", res.Variant("otel-java")); err == nil {
		t.Error("a bundle for another variant must be rejected")
	}
}

func rawString(r *json.RawMessage) string {
	if r == nil {
		return "<absent>"
	}
	return string(*r)
}

// Legs and wirings are additive: an in-code variant carries its legs and the
// dimensions define the values, while every other variant, and a registry
// that declares no wirings, serialise without the keys at all.
func TestBuild_LegsAreAdditive(t *testing.T) {
	reg, err := registry.Load("../../variants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	keys := func(reg *registry.Registry) (dims map[string]json.RawMessage, variants map[string]map[string]json.RawMessage) {
		t.Helper()
		res, err := Build(reg, nil, Run{RunID: "run-test"})
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Dimensions map[string]json.RawMessage   `json:"dimensions"`
			Variants   []map[string]json.RawMessage `json:"variants"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		variants = map[string]map[string]json.RawMessage{}
		for _, v := range doc.Variants {
			var id string
			if err := json.Unmarshal(v["id"], &id); err != nil {
				t.Fatal(err)
			}
			variants[id] = v
		}
		return doc.Dimensions, variants
	}

	dims, variants := keys(reg)
	var wirings []registry.Wiring
	if err := json.Unmarshal(dims["wirings"], &wirings); err != nil || len(wirings) != len(reg.WiringOrder) || wirings[0].ID != reg.WiringOrder[0] {
		t.Errorf("dimensions.wirings = %s (%v), want %v in document order", dims["wirings"], err, reg.WiringOrder)
	}
	inCode := 0
	for _, rv := range reg.Variants {
		raw, has := variants[rv.ID]["legs"]
		if len(rv.Legs) == 0 {
			if has {
				t.Errorf("%s: serialised legs %s, want the key omitted", rv.ID, raw)
			}
			continue
		}
		inCode++
		var legs map[string]registry.Leg
		if err := json.Unmarshal(raw, &legs); err != nil {
			t.Fatalf("%s: %v", rv.ID, err)
		}
		for p, want := range rv.Legs {
			if legs[p] != want {
				t.Errorf("%s: legs.%s = %+v, want %+v", rv.ID, p, legs[p], want)
			}
		}
	}
	if inCode == 0 {
		t.Error("no variant carried legs")
	}

	// A registry without the block and without legs is what the base
	// revision looks like: no new keys anywhere.
	reg.Wirings, reg.WiringOrder = nil, nil
	for _, v := range reg.Variants {
		v.Legs = nil
	}
	dims, variants = keys(reg)
	if _, has := dims["wirings"]; has {
		t.Errorf("dimensions.wirings = %s, want the key omitted", dims["wirings"])
	}
	for id, v := range variants {
		if _, has := v["legs"]; has {
			t.Errorf("%s: legs present, want the key omitted", id)
		}
	}
}
