package results

import (
	"encoding/json"
	"testing"
)

func TestVersionBundle_EachRewritesEveryDocumentOldestFirst(t *testing.T) {
	b := &VersionBundle{SchemaVersion: VersionBundleSchema, Documents: map[string]json.RawMessage{}}
	for _, v := range []string{"1.10.0", "1.2.0", "1.9.0"} {
		doc := &Results{SchemaVersion: SchemaVersion, Variants: []*Variant{{ID: "otel-java", Hops: []Hop{}, Cells: []Cell{}}},
			Annotations: []Annotation{{ID: "stale", Cells: []string{}, Variants: []string{"otel-java"}}}}
		raw, err := doc.Encode()
		if err != nil {
			t.Fatal(err)
		}
		b.Put(v, raw)
	}
	var seen []string
	err := b.Each(func(v string, doc *Results) error {
		seen = append(seen, v)
		doc.Annotations = []Annotation{{ID: "current-" + v, Cells: []string{}, Variants: []string{"otel-java"}}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"1.2.0", "1.9.0", "1.10.0"}; len(seen) != 3 || seen[0] != want[0] || seen[1] != want[1] || seen[2] != want[2] {
		t.Fatalf("want versions in ascending version order %v, got %v", want, seen)
	}
	for v, raw := range b.Documents {
		var doc Results
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		if len(doc.Annotations) != 1 || doc.Annotations[0].ID != "current-"+v {
			t.Fatalf("%s: document not stored back as fn left it: %+v", v, doc.Annotations)
		}
		if doc.Variants[0].ID != "otel-java" || doc.Variants[0].Cells == nil {
			t.Fatalf("%s: the rest of the document must survive the round trip: %+v", v, doc.Variants[0])
		}
	}
}
