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

func rawString(r *json.RawMessage) string {
	if r == nil {
		return "<absent>"
	}
	return string(*r)
}
