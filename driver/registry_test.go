package main

import (
	"sort"
	"testing"

	"github.com/signadot/context-propagation-matrix/internal/registry"
)

// The driver runs every inbound × outbound pair of its own protocol list;
// variants.yaml lists the boundaries a variant is scored on. The driver does
// not read the registry, so this test is what keeps the two the same. Left
// to drift, a boundary only the registry has is a hop no run records, and a
// hop only the driver runs is scored nowhere.
func TestDriverRunsTheRegistrysBoundaries(t *testing.T) {
	reg, err := registry.Load("../variants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, b := range reg.Boundaries {
		listed[b] = true
	}
	// The boundary names main's loop builds.
	runs := map[string]bool{}
	for _, in := range protocols {
		for _, out := range protocols {
			runs[in+"->"+out] = true
		}
	}

	for _, b := range reg.Boundaries {
		if !runs[b] {
			t.Errorf("variants.yaml lists %s, which the driver does not run: add the protocol to `protocols` (and a sender to Env.send)", b)
		}
	}
	var unlisted []string
	for b := range runs {
		if !listed[b] {
			unlisted = append(unlisted, b)
		}
	}
	sort.Strings(unlisted)
	for _, b := range unlisted {
		t.Errorf("the driver runs %s, which variants.yaml does not list under boundaries", b)
	}
}
