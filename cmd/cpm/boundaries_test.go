package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/signadot/context-propagation-matrix/internal/registry"
)

// verify.yml hands `cpm boundaries -json` to its smoke jobs as a workflow
// output, which is one line, and they compare it with the boundaries the
// driver recorded: the list has to be the registry's, in the registry's
// order.
func TestBoundariesPrintsTheRegistrysList(t *testing.T) {
	const path = "../../variants.yaml"
	reg, err := registry.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Boundaries) == 0 {
		t.Fatal("the registry lists no boundaries")
	}

	status, out := cpm(t, false, "boundaries", "-json", "-registry", path)
	if status != 0 {
		t.Fatalf("cpm boundaries -json: exit %d\n%s", status, out)
	}
	if n := strings.Count(out, "\n"); n != 1 || !strings.HasSuffix(out, "\n") {
		t.Errorf("-json wrote %d line breaks, want one line (a workflow output): %q", n, out)
	}
	if !strings.Contains(out, `"`+reg.Boundaries[0]+`"`) {
		t.Errorf("-json does not write %q as it is spelled in the registry: %s", reg.Boundaries[0], out)
	}
	var got []string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("-json output is not a JSON array of strings: %v\n%s", err, out)
	}
	if !reflect.DeepEqual(got, reg.Boundaries) {
		t.Errorf("-json = %v, want the registry's %v", got, reg.Boundaries)
	}

	status, out = cpm(t, false, "boundaries", "-registry", path)
	if status != 0 {
		t.Fatalf("cpm boundaries: exit %d\n%s", status, out)
	}
	if got := strings.Split(strings.TrimSuffix(out, "\n"), "\n"); !reflect.DeepEqual(got, reg.Boundaries) {
		t.Errorf("one per line = %v, want the registry's %v", got, reg.Boundaries)
	}
}
