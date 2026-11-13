package registry

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// The registry's boundaries have to be every inbound × outbound pair of its
// protocols. Counting them is not enough: a list that repeats one pair and
// leaves another out has the right length.
func TestBoundariesAreEveryProtocolPair(t *testing.T) {
	src, err := os.ReadFile("../../variants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	reg, err := Parse(src, "variants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, b := range reg.Boundaries {
		listed[b] = true
	}
	for _, in := range reg.ProtocolOrder {
		for _, out := range reg.ProtocolOrder {
			if !listed[in+"->"+out] {
				t.Errorf("variants.yaml does not list %s->%s", in, out)
			}
		}
	}

	// The lines of the first and the last boundary, as the file writes them.
	first, last := reg.Boundaries[0], reg.Boundaries[len(reg.Boundaries)-1]
	firstLine, lastLine := "  - "+first+"\n", "  - "+last+"\n"
	if first == last || strings.Count(string(src), firstLine) != 1 || strings.Count(string(src), lastLine) != 1 {
		t.Fatalf("fixture: variants.yaml does not write %q and %q as one list line each", first, last)
	}
	// Same length, one pair twice, one pair missing.
	_, err = Parse([]byte(strings.Replace(string(src), lastLine, firstLine, 1)), "variants.yaml")
	if want := fmt.Sprintf("boundary %q is listed twice", first); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("a list with %s twice and no %s: got %v, want %q", first, last, err, want)
	}
	// One pair short.
	n := len(reg.Boundaries)
	_, err = Parse([]byte(strings.Replace(string(src), lastLine, "", 1)), "variants.yaml")
	if want := fmt.Sprintf("expected %d boundaries (protocols²), got %d", n, n-1); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("a list without %s: got %v, want %q", last, err, want)
	}
}
