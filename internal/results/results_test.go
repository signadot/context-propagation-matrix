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
	if err := cur.SetProvenance(prev, map[string]bool{"otel-php": true}); err != nil {
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
	if err := full.SetProvenance(nil, nil); err != nil {
		t.Fatal(err)
	}
	if full.Variants[0].MeasuredIn != "run-x" || full.Variants[1].MeasuredIn != "" {
		t.Errorf("full run: got %q / %q", full.Variants[0].MeasuredIn, full.Variants[1].MeasuredIn)
	}

	// Carried over with no prev, or with prev lacking the variant → error.
	orphan := &Results{Run: Run{RunID: "run-y"}, Variants: []*Variant{v("otel-java", false), v("otel-php", false)}}
	if err := orphan.SetProvenance(nil, map[string]bool{"otel-php": true}); err == nil {
		t.Error("no prev: want error for carried-over otel-java")
	}
	if err := orphan.SetProvenance(&Results{Variants: []*Variant{v("otel-java", true)}}, map[string]bool{"otel-php": true}); err == nil {
		t.Error("prev has otel-java missing: want error")
	}
}

func rawString(r *json.RawMessage) string {
	if r == nil {
		return "<absent>"
	}
	return string(*r)
}
