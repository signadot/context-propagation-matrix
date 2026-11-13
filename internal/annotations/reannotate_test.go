package annotations

import (
	"testing"

	"github.com/signadot/context-propagation-matrix/internal/registry"
	"github.com/signadot/context-propagation-matrix/internal/results"
)

// A registry with the fixture's two variants and a third one the documents
// never contain, as the live registry has variants no single document holds.
func testRegistry() *registry.Registry {
	return &registry.Registry{Variants: []*registry.Variant{
		{ID: "datadog-java", Tool: "datadog", Language: "java", Flavour: "default", DisplayName: "Java"},
		{ID: "datadog-node", Tool: "datadog", Language: "node", Flavour: "default", DisplayName: "Node.js"},
		{ID: "datadog-php", Tool: "datadog", Language: "php", Flavour: "default", DisplayName: "PHP"},
	}}
}

// single extracts one variant of the fixture as a per-version document,
// carrying stale annotations as a filed document would.
func single(res *results.Results, id string) *results.Results {
	v := res.Variant(id)
	return &results.Results{Variants: []*results.Variant{v},
		Annotations: []results.Annotation{{ID: "stale-note", Cells: []string{v.Cells[0].CellID}, Variants: []string{id}}}}
}

func TestReannotate_ReplacesStaleAnnotations(t *testing.T) {
	doc := single(fixture(nil), "datadog-java")
	a := ann("dd-java-baggage", "fixed", Selector{Variant: []string{"datadog-java"}, Header: []string{"baggage"}})
	a.FixedIn = "2.0.0"
	ps, err := Reannotate(doc, testRegistry(), []*Annotation{a})
	if err != nil || fatal(ps) != 0 {
		t.Fatalf("err=%v problems=%v", err, ps)
	}
	if len(doc.Annotations) != 1 || doc.Annotations[0].ID != "dd-java-baggage" {
		t.Fatalf("stale annotation must be replaced by the YAML's: %+v", doc.Annotations)
	}
	got := doc.Annotations[0]
	if got.Resolved || len(got.Cells) != 4 || got.FixedIn != "2.0.0" || got.Status != "fixed" {
		t.Fatalf("on a version where the cells fail the note is attached to them, fixed_in kept: %+v", got)
	}
	// Only this variant's cells: the document has no others to speak for.
	for _, c := range got.Cells {
		if c[:len("datadog-java/")] != "datadog-java/" {
			t.Fatalf("cell of another variant attached: %v", got.Cells)
		}
	}
}

func TestReannotate_ResolvedOnTheVersionThatPasses(t *testing.T) {
	outcomes := map[string]results.Outcome{}
	for _, b := range []string{"http-http", "http-kafka", "kafka-http", "kafka-kafka"} {
		outcomes["datadog-java/baggage/"+b] = results.OutcomePass
	}
	// datadog-node's baggage cells still fail: on a full run this note would
	// not be resolved; on datadog-java's own document it is.
	doc := single(fixture(outcomes), "datadog-java")
	a := ann("dd-baggage", "fixed", Selector{Tool: []string{"datadog"}, Header: []string{"baggage"}})
	a.FixedIn = "2.0.0"
	ps, err := Reannotate(doc, testRegistry(), []*Annotation{a})
	if err != nil || fatal(ps) != 0 {
		t.Fatalf("err=%v problems=%v", err, ps)
	}
	if len(doc.Annotations) != 1 {
		t.Fatalf("want the note attached as resolved, got %+v", doc.Annotations)
	}
	got := doc.Annotations[0]
	if !got.Resolved || len(got.Cells) != 4 || got.FixedIn != "2.0.0" {
		t.Fatalf("want resolved on the 4 passing cells with the YAML's fixed_in: %+v", got)
	}
	if len(got.Variants) != 1 || got.Variants[0] != "datadog-java" {
		t.Fatalf("want this variant alone: %v", got.Variants)
	}
}

func TestReannotate_FixedInFallsBackToTheDocumentsVersion(t *testing.T) {
	outcomes := map[string]results.Outcome{}
	for _, b := range []string{"http-http", "http-kafka", "kafka-http", "kafka-kafka"} {
		outcomes["datadog-java/baggage/"+b] = results.OutcomePass
	}
	doc := single(fixture(outcomes), "datadog-java")
	a := ann("dd-java-baggage", "open", Selector{Variant: []string{"datadog-java"}, Header: []string{"baggage"}})
	ps, err := Reannotate(doc, testRegistry(), []*Annotation{a})
	if err != nil || fatal(ps) != 0 {
		t.Fatalf("err=%v problems=%v", err, ps)
	}
	if got := doc.Annotations[0]; !got.Resolved || got.FixedIn != "1.65.1" {
		t.Fatalf("want resolved with the version this document records: %+v", got)
	}
}

func TestReannotate_OtherVariantsNotesAreNeitherAttachedNorFatal(t *testing.T) {
	doc := single(fixture(nil), "datadog-java")
	anns := []*Annotation{
		ann("dd-node-only", "open", Selector{Variant: []string{"datadog-node"}, Header: []string{"baggage"}}),
		ann("dd-php-only", "open", Selector{Tool: []string{"datadog"}, Language: []string{"php"}}), // registered, never in this document
	}
	ps, err := Reannotate(doc, testRegistry(), anns)
	if err != nil || fatal(ps) != 0 {
		t.Fatalf("notes about registered variants absent from the document are nothing to check: err=%v problems=%v", err, ps)
	}
	if len(doc.Annotations) != 0 {
		t.Fatalf("nothing should attach: %+v", doc.Annotations)
	}
}

func TestReannotate_StaleSelectorIsStillFatal(t *testing.T) {
	doc := single(fixture(nil), "datadog-java")
	a := ann("gone", "open", Selector{Tool: []string{"datadog"}, Language: []string{"rust"}}) // no such variant anywhere
	ps, err := Reannotate(doc, testRegistry(), []*Annotation{a})
	if err != nil {
		t.Fatal(err)
	}
	if fatal(ps) != 1 {
		t.Fatalf("a selector naming no registered variant is stale: %v", ps)
	}
	if len(doc.Annotations) != 0 {
		t.Fatalf("a stale note attaches to nothing: %+v", doc.Annotations)
	}
}

func TestReannotate_RejectsWhatIsNotASingleVariantDocument(t *testing.T) {
	if _, err := Reannotate(fixture(nil), testRegistry(), nil); err == nil {
		t.Fatal("a two-variant document is not a per-version document")
	}
	doc := single(fixture(nil), "datadog-java")
	doc.Variants[0].ID = "datadog-rust"
	if _, err := Reannotate(doc, testRegistry(), nil); err == nil {
		t.Fatal("a variant the registry does not know cannot be padded")
	}
}
