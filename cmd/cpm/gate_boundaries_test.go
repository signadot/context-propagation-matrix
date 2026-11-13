package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/signadot/context-propagation-matrix/internal/raw"
	"github.com/signadot/context-propagation-matrix/internal/registry"
	"github.com/signadot/context-propagation-matrix/internal/results"
)

// editRaw rewrites dir/<variant>.json after change has had its way with it.
func editRaw(t *testing.T, dir, variant string, change func(r *raw.VariantResult)) {
	t.Helper()
	path := filepath.Join(dir, variant+".json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var r raw.VariantResult
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatal(err)
	}
	change(&r)
	if body, err = json.Marshal(r); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, variant+".json", string(body))
}

// The driver runs its own protocol list and variants.yaml lists the
// boundaries; nothing at run time makes them the same. A boundary of the
// registry the driver did not run comes out of `cpm aggregate` as not_run
// cells, and `cpm gate` must not let a run that measured it be published.
func TestGateRefusesABoundaryWithNoHop(t *testing.T) {
	reg, err := registry.Load("../../variants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	n := len(reg.Boundaries)
	last := reg.Boundaries[n-1]
	withoutLast := func(r *raw.VariantResult) {
		if r.Tests[len(r.Tests)-1].Boundary != last {
			t.Fatalf("fixture: the last test is %s, not %s", r.Tests[len(r.Tests)-1].Boundary, last)
		}
		r.Tests = r.Tests[:len(r.Tests)-1]
	}
	said := fmt.Sprintf("otel-java: boundary_not_run: no hop on 1 of %d boundaries (%s)", n, last)

	rawDir := t.TempDir()
	writeRaw(t, reg, rawDir, "none-java", sendsPlain)
	writeRaw(t, reg, rawDir, "otel-java", sendsTraced)
	run1 := aggregate(t, rawDir, "matrix", "run-1")

	// run-2 measures otel-java with a driver that ran one boundary fewer.
	editRaw(t, rawDir, "otel-java", withoutLast)
	run2 := aggregate(t, rawDir, "matrix", "run-2", "-prev", run1, "-reran", "otel-java")
	res, err := results.Load(run2)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Variant("otel-java").Counts["traceparent"].NotRun; got != 1 {
		t.Fatalf("fixture: aggregate wrote %d not_run traceparent cells for otel-java, want 1", got)
	}
	if got := invalidKinds(t, run2, "otel-java"); got != "" {
		t.Errorf("aggregate recorded invalid = %q: a boundary not run is the gate's to refuse, not a verdict on the tool", got)
	}
	code, out := cpm(t, true, "gate", "-reran", "otel-java", run2)
	if code != 1 || !strings.Contains(out, "::error::gate: "+said) {
		t.Errorf("a boundary not run on a variant measured in this run: exit %d, want 1 with an ::error:: saying %q\n%s", code, said, out)
	}
	// Without -reran the document's own record of what it measured decides.
	if code, _ := cpm(t, false, "gate", run2); code != 1 {
		t.Errorf("same, measured set from the document: exit %d, want 1", code)
	}

	// run-3 carries that otel-java over: the run that measured it was the
	// one to refuse it.
	run3 := aggregate(t, rawDir, "matrix", "run-3", "-prev", run2, "-reran", "none-java")
	code, out = cpm(t, true, "gate", "-reran", "none-java", run3)
	if code != 0 || !strings.Contains(out, "::warning::gate: "+said) || !strings.Contains(out, "carried over from run-2") || strings.Contains(out, "::error::") {
		t.Errorf("the same variant carried over: exit %d, want 0 with a ::warning::\n%s", code, out)
	}

	// A back-test is refused too: unlike an agent that did not attach,
	// this says nothing about the version.
	backRaw := t.TempDir()
	writeRaw(t, reg, backRaw, "otel-java", sendsTraced)
	editRaw(t, backRaw, "otel-java", withoutLast)
	back := aggregate(t, backRaw, "backtest", "backtest-otel-java-2.0.0", "-baseline", run1)
	code, out = cpm(t, true, "gate", "-baseline", run1, back)
	if code != 1 || !strings.Contains(out, "::error::gate: "+said) {
		t.Errorf("a boundary not run in a back-test: exit %d, want 1 with an ::error::\n%s", code, out)
	}

	// A hop that ran and was not delivered is a result (PHP serves no
	// gRPC), not a boundary without a hop.
	writeRaw(t, reg, backRaw, "otel-java", sendsTraced)
	editRaw(t, backRaw, "otel-java", func(r *raw.VariantResult) {
		r.Tests[len(r.Tests)-1] = raw.TestResult{Boundary: last, TestID: "otel-java--" + last, Error: "send: connection refused"}
	})
	undelivered := aggregate(t, backRaw, "backtest", "backtest-otel-java-2.1.0", "-baseline", run1)
	if code, out := cpm(t, true, "gate", "-baseline", run1, undelivered); code != 0 || strings.Contains(out, "boundary_not_run") {
		t.Errorf("an undelivered hop: exit %d, want 0 and no boundary_not_run\n%s", code, out)
	}
}

// The other direction: a hop on a boundary variants.yaml does not list has
// no cells to be filed under, so `cpm aggregate` used to leave it out of
// results.json without saying so.
func TestAggregateRefusesAHopOnAnUnlistedBoundary(t *testing.T) {
	reg, err := registry.Load("../../variants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rawDir := t.TempDir()
	writeRaw(t, reg, rawDir, "otel-java", sendsTraced)
	editRaw(t, rawDir, "otel-java", func(r *raw.VariantResult) {
		extra := r.Tests[0]
		extra.Boundary, extra.TestID = "http->carrier-pigeon", "otel-java--http-carrier-pigeon"
		r.Tests = append(r.Tests, extra)
	})
	out := t.TempDir()
	code, text := cpm(t, false, "aggregate", "-registry", "../../variants.yaml", "-compose", "../../docker-compose.yml",
		"-annotations", t.TempDir(), "-pins", "", "-raw", rawDir, "-out", out, "-kind", "matrix", "-run-id", "run-1")
	if code == 0 || !strings.Contains(text, `raw result "otel-java" has a hop on "http->carrier-pigeon", which is not a boundary in variants.yaml`) {
		t.Errorf("a hop on an unlisted boundary: exit %d, want a refusal that names it\n%s", code, text)
	}
	if _, err := os.Stat(filepath.Join(out, "results.json")); err == nil {
		t.Error("aggregate wrote a results.json all the same")
	}
}
