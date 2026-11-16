package main

import (
	"strings"
	"testing"

	"github.com/signadot/context-propagation-matrix/internal/registry"
)

// A matrix run whose job for a variant failed publishes that variant's
// previous result, exactly as it does for a variant the run was never to
// measure. The release notes are where the difference is written down:
// matrix.yml passes the ids the plan chose and no job delivered.
func TestSummaryNamesVariantsTheRunCouldNotMeasure(t *testing.T) {
	reg, err := registry.Load("../../variants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rawDir := t.TempDir()
	writeRaw(t, reg, rawDir, "none-java", sendsPlain)
	writeRaw(t, reg, rawDir, "otel-java", sendsTraced)
	writeRaw(t, reg, rawDir, "elastic-java", sendsTraced)
	prev := aggregate(t, rawDir, "matrix", "run-1")
	// run-2 was to measure all three; only otel-java's job delivered.
	doc := aggregate(t, rawDir, "matrix", "run-2", "-prev", prev, "-reran", "otel-java")

	const heading = "**Could not be measured in this run:"
	code, out := cpm(t, false, "summary", "-not-measured", "elastic-java none-java", doc)
	if code != 0 {
		t.Fatalf("cpm summary -not-measured: exit %d\n%s", code, out)
	}
	want := heading + " 2 variant(s)** — elastic-java, none-java."
	if !strings.Contains(out, want) {
		t.Errorf("summary does not say %q:\n%s", want, out)
	}
	// Slack gets the first 2,800 bytes and a release is read from the top:
	// the line comes before the list of what was measured.
	if at, measured := strings.Index(out, heading), strings.Index(out, "Re-measured 1 variant(s): otel-java."); measured < 0 || at > measured {
		t.Errorf("the line is at byte %d and the measured list at %d; want it first:\n%s", at, measured, out)
	}

	// Nothing failed: the workflow passes an empty value, and the summary is
	// the one a run without the flag writes, which is the one above less its
	// one paragraph.
	start := strings.Index(out, heading)
	end := start + strings.Index(out[start:], "\n\n") + 2
	without := out[:start] + out[end:]
	for _, args := range [][]string{{"summary", doc}, {"summary", "-not-measured", "", doc}, {"summary", "-not-measured", " ", doc}} {
		code, plain := cpm(t, false, args...)
		if code != 0 {
			t.Fatalf("cpm %s: exit %d\n%s", strings.Join(args, " "), code, plain)
		}
		if strings.Contains(plain, heading) {
			t.Errorf("cpm %s names variants it could not measure:\n%s", strings.Join(args, " "), plain)
		}
		if plain != without {
			t.Errorf("cpm %s:\n%s\nwant the summary above less that paragraph:\n%s", strings.Join(args, " "), plain, without)
		}
	}
}
