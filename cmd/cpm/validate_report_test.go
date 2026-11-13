package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/signadot/context-propagation-matrix/internal/annotations"
)

func TestReportProblems_StrictFailsWarningsButNeverNotesOrAdvisories(t *testing.T) {
	problems := []annotations.Problem{
		{Annotation: "a", File: "annotations/x.yaml", Message: "every cell passes; set status: fixed"},
		{Annotation: "b", File: "annotations/x.yaml", Info: true, Message: "nothing to check"},
		{Annotation: "c", File: "annotations/x.yaml", Fatal: true, Message: "stale"},
		{Annotation: "d", File: "annotations/x.yaml", Advisory: true, Message: "partially resolved: drift"},
	}
	var out bytes.Buffer
	if got := reportProblems(&out, false, false, problems); len(got) != 1 || !strings.Contains(got[0], "stale") {
		t.Fatalf("non-strict fails only fatal problems; got %v", got)
	}
	got := reportProblems(&out, false, true, problems)
	if len(got) != 2 || !strings.Contains(got[0], "set status: fixed") || !strings.Contains(got[1], "stale") {
		t.Fatalf("strict fails warnings and fatal problems but not notes or advisory warnings; got %v", got)
	}
	if strings.Contains(out.String(), "::") {
		t.Fatalf("no workflow commands outside GitHub Actions:\n%s", out.String())
	}
}

func TestReportProblems_GitHubLevels(t *testing.T) {
	problems := []annotations.Problem{
		{Annotation: "a", File: "annotations/x.yaml", Message: "drift"},
		{Annotation: "b", File: "annotations/x.yaml", Info: true, Message: "note"},
	}
	var out bytes.Buffer
	reportProblems(&out, true, false, problems)
	for _, want := range []string{
		"::warning file=annotations/x.yaml,title=annotation a::drift",
		"::notice file=annotations/x.yaml,title=annotation b::note",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in:\n%s", want, out.String())
		}
	}
	out.Reset()
	reportProblems(&out, true, true, problems[:1])
	if !strings.Contains(out.String(), "::error file=annotations/x.yaml,title=annotation a::drift") {
		t.Fatalf("a warning that fails under -strict must be an error annotation:\n%s", out.String())
	}
	out.Reset()
	reportProblems(&out, true, true, []annotations.Problem{{Annotation: "d", File: "annotations/x.yaml", Advisory: true, Message: "partially resolved"}})
	if !strings.Contains(out.String(), "::warning file=annotations/x.yaml,title=annotation d::partially resolved") {
		t.Fatalf("an advisory warning stays a warning annotation under -strict:\n%s", out.String())
	}
}

// Unexplained cells are a warning in both modes: reportUnexplained returns
// nothing to fail on, so a new upstream regression can never block publishing.
func TestReportUnexplained(t *testing.T) {
	cells := []string{"otel-php/baggage/kafka-http", "otel-php/traceparent/kafka-http", "otel-php/tracestate/kafka-http"}
	var out bytes.Buffer
	reportUnexplained(&out, false, cells)
	want := "warn: annotations: 3 non-pass cell(s) have no annotation:\n  otel-php: baggage/kafka-http, traceparent/kafka-http, tracestate/kafka-http\n"
	if out.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out.String(), want)
	}

	out.Reset()
	reportUnexplained(&out, true, cells)
	if !strings.Contains(out.String(), "::warning title=failing cells without an annotation::annotations: 3 non-pass cell(s) have no annotation:%0Aotel-php: baggage/kafka-http, ") {
		t.Fatalf("want a single-line warning command with escaped newlines:\n%s", out.String())
	}

	out.Reset()
	reportUnexplained(&out, true, nil)
	if out.String() != "annotations: every non-pass cell is explained\n" {
		t.Fatalf("nothing unexplained must print no warning:\n%s", out.String())
	}
}

func TestGitHubEscape(t *testing.T) {
	if got := githubEscape("100%\r\nx"); got != "100%25%0D%0Ax" {
		t.Fatalf("got %q", got)
	}
	if got := githubProperty("a:b,c"); got != "a%3Ab%2Cc" {
		t.Fatalf("got %q", got)
	}
}
