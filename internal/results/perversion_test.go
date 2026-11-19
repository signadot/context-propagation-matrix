package results

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A re-measured version keeps its release date: the entry Upsert is given is
// built from a document, which never carries one.
func TestUpsertKeepsReleasedAt(t *testing.T) {
	released := time.Date(2025, 9, 14, 21, 5, 52, 0, time.UTC)
	idx := &VersionIndex{Versions: []VersionEntry{
		{Version: "2.20.0", Asset: "2.20.0.json", MeasuredIn: "run-1", ReleasedAt: &released},
		{Version: "2.19.0", Asset: "2.19.0.json", MeasuredIn: "run-1"},
	}}

	idx.Upsert(VersionEntry{Version: "2.20.0", Asset: "2.20.0.json", MeasuredIn: "run-2"})
	if e := idx.Versions[0]; e.MeasuredIn != "run-2" || e.ReleasedAt == nil || !e.ReleasedAt.Equal(released) {
		t.Errorf("re-measured entry = %+v, want the new measurement with the release date it had", e)
	}

	// An entry that brings a date replaces the one there.
	corrected := released.Add(24 * time.Hour)
	idx.Upsert(VersionEntry{Version: "2.20.0", Asset: "2.20.0.json", MeasuredIn: "run-3", ReleasedAt: &corrected})
	if e := idx.Versions[0]; e.ReleasedAt == nil || !e.ReleasedAt.Equal(corrected) {
		t.Errorf("entry = %+v, want the date it was upserted with", e)
	}

	// Nothing to keep: a version without a date stays without, and so does
	// a new one.
	idx.Upsert(VersionEntry{Version: "2.19.0", Asset: "2.19.0.json", MeasuredIn: "run-2"})
	idx.Upsert(VersionEntry{Version: "2.21.0", Asset: "2.21.0.json", MeasuredIn: "run-2"})
	if len(idx.Versions) != 3 || idx.Versions[1].ReleasedAt != nil || idx.Versions[2].ReleasedAt != nil {
		t.Errorf("versions = %+v, want three, the last two undated", idx.Versions)
	}
}

// released_at is optional: left out of index.json when unknown, and read
// back as written when set.
func TestVersionIndexReleasedAtJSON(t *testing.T) {
	released := time.Date(2025, 9, 14, 21, 5, 52, 0, time.UTC)
	v := &Variant{ID: "otel-java", Tool: "otel", Language: "java", DisplayName: "OTel Java"}
	path := filepath.Join(t.TempDir(), IndexAsset)
	idx, err := LoadVersionIndex(path, v)
	if err != nil {
		t.Fatal(err)
	}
	idx.Upsert(VersionEntry{Version: "2.20.0", Asset: "2.20.0.json", ReleasedAt: &released})
	idx.Upsert(VersionEntry{Version: "2.19.0", Asset: "2.19.0.json"})
	if err := idx.Write(path); err != nil {
		t.Fatal(err)
	}

	back, err := LoadVersionIndex(path, v)
	if err != nil {
		t.Fatal(err)
	}
	if back.SchemaVersion != 1 {
		t.Errorf("schema_version = %d: the field is additive and must not move it", back.SchemaVersion)
	}
	if len(back.Versions) != 2 {
		t.Fatalf("versions = %+v", back.Versions)
	}
	if e := back.Versions[0]; e.Version != "2.20.0" || e.ReleasedAt == nil || !e.ReleasedAt.Equal(released) {
		t.Errorf("dated entry read back as %+v", e)
	}
	if e := back.Versions[1]; e.Version != "2.19.0" || e.ReleasedAt != nil {
		t.Errorf("undated entry read back as %+v", e)
	}

	dated, err := json.Marshal(back.Versions[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dated), `"released_at":"2025-09-14T21:05:52Z"`) {
		t.Errorf("dated entry = %s, want released_at", dated)
	}
	undated, err := json.Marshal(back.Versions[1])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(undated), "released_at") {
		t.Errorf("undated entry = %s, want no released_at key", undated)
	}

	// The bundle carries the index it was synced with, dates included.
	b := &VersionBundle{SchemaVersion: VersionBundleSchema, Documents: map[string]json.RawMessage{
		"2.20.0": json.RawMessage(`{}`), "2.19.0": json.RawMessage(`{}`)}}
	if missing := b.Sync(back); len(missing) != 0 {
		t.Fatalf("missing = %v", missing)
	}
	bundlePath := filepath.Join(t.TempDir(), BundleAsset)
	if err := b.Write(bundlePath); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadVersionBundle(bundlePath, v)
	if err != nil {
		t.Fatal(err)
	}
	if e := loaded.Index.Versions[0]; e.ReleasedAt == nil || !e.ReleasedAt.Equal(released) {
		t.Errorf("bundle index entry = %+v, want the release date", e)
	}
	if e := loaded.Index.Versions[1]; e.ReleasedAt != nil {
		t.Errorf("bundle index entry = %+v, want no release date", e)
	}
}

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
