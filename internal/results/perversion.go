package results

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/signadot/context-propagation-matrix/internal/versions"
)

// Per-version results: one rolling GitHub release per variant
// (`variant-{id}`) whose assets are `{version}.json`, a single-variant schema
// v2 document for each tracer version ever measured, and `index.json`, the
// list below. A run or back-test that measures a variant upserts its
// version; the site reads the index to know which versions exist and fetches
// one document per page instead of every run release.

// VersionIndexSchema is the schema_version of index.json.
const VersionIndexSchema = 1

// VersionIndex is index.json: every measured tracer version of one variant,
// newest first, with enough of each measurement to render a version list
// (counts, provenance) without opening the per-version document.
type VersionIndex struct {
	SchemaVersion int            `json:"schema_version"`
	Variant       string         `json:"variant"`
	Tool          string         `json:"tool"`
	Language      string         `json:"language"`
	DisplayName   string         `json:"display_name"`
	UpdatedAt     time.Time      `json:"updated_at"`
	Versions      []VersionEntry `json:"versions"`
}

// VersionEntry is one measured version. Asset is the document's file name
// in the release; Version is the canonical (pinned) tracer version, Reported
// what the relay said at run time.
type VersionEntry struct {
	Version        string            `json:"version"`
	Reported       string            `json:"reported,omitempty"`
	Asset          string            `json:"asset"`
	Kind           string            `json:"kind"` // run kind that measured it: matrix | backtest | local
	MeasuredIn     string            `json:"measured_in"`
	MeasuredAt     time.Time         `json:"measured_at"`
	MeasuredCommit string            `json:"measured_commit,omitempty"`
	MatrixVersion  int               `json:"matrix_version"`
	Counts         map[string]*Count `json:"counts"`
	CanaryLeaks    int               `json:"canary_leaks"`
	// Invalid is the document's Variant.Invalid: with it a reader of the
	// index can tell an agent that never attached from one that propagates
	// nothing, which have the same counts.
	Invalid []Problem `json:"invalid,omitempty"`
}

// safeVersion is what may appear in an asset file name: registries hand out
// "1.2.3", "0.65b0", "12.10.0.39", "1.0.0-rc.1"; nothing with a slash, a
// space or a leading dot.
var safeVersion = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+_-]*$`)

// Release assets that are not per-version documents.
const (
	IndexAsset  = "index.json"
	BundleAsset = "versions.json"
)

// AssetName is the per-version document's file name for a version.
func AssetName(version string) (string, error) {
	if !safeVersion.MatchString(version) {
		return "", fmt.Errorf("version %q cannot name a release asset", version)
	}
	if n := version + ".json"; n == IndexAsset || n == BundleAsset {
		return "", fmt.Errorf("version %q would collide with the release's %s", version, n)
	}
	return version + ".json", nil
}

// VersionBundleSchema is the schema_version of versions.json.
const VersionBundleSchema = 1

// VersionBundle is versions.json: the index and every per-version document
// of one variant in a single asset, so that a consumer rebuilding the whole
// history (the site, on every build) needs one download per variant rather
// than one per version. Documents are kept as written, keyed by version,
// and always agree with Index: Sync enforces it.
type VersionBundle struct {
	SchemaVersion int                        `json:"schema_version"`
	Index         *VersionIndex              `json:"index"`
	Documents     map[string]json.RawMessage `json:"documents"`
}

// LoadVersionBundle reads versions.json for the variant described by v; a
// missing file yields an empty bundle, which Sync then reports as lacking
// every document the index lists.
func LoadVersionBundle(path string, v *Variant) (*VersionBundle, error) {
	b := &VersionBundle{SchemaVersion: VersionBundleSchema, Documents: map[string]json.RawMessage{}}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return b, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, b); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if b.SchemaVersion != VersionBundleSchema {
		return nil, fmt.Errorf("%s: schema_version %d, want %d", path, b.SchemaVersion, VersionBundleSchema)
	}
	if b.Index != nil && b.Index.Variant != v.ID {
		return nil, fmt.Errorf("%s: is for %s, not %s", path, b.Index.Variant, v.ID)
	}
	if b.Documents == nil {
		b.Documents = map[string]json.RawMessage{}
	}
	return b, nil
}

// Put stores the document for a version, as written to its asset.
func (b *VersionBundle) Put(version string, doc []byte) {
	b.Documents[version] = json.RawMessage(doc)
}

// Each decodes every document in the bundle, oldest version first, hands it
// to fn and stores it back as fn left it. It is how the bundle is
// re-annotated: the cells of a document never change after it is filed, but
// the annotations follow annotations/*.yaml and are re-derived on every
// publish, so the bundle — what the site renders — always carries the
// current explanation of each version, while the {version}.json asset keeps
// the one it was filed with.
func (b *VersionBundle) Each(fn func(version string, doc *Results) error) error {
	vs := make([]string, 0, len(b.Documents))
	for v := range b.Documents {
		vs = append(vs, v)
	}
	sort.Slice(vs, func(i, j int) bool { return versions.Compare(vs[i], vs[j]) < 0 })
	for _, v := range vs {
		var doc Results
		if err := json.Unmarshal(b.Documents[v], &doc); err != nil {
			return fmt.Errorf("%s.json in bundle: %w", v, err)
		}
		if err := fn(v, &doc); err != nil {
			return fmt.Errorf("%s.json: %w", v, err)
		}
		raw, err := doc.Encode()
		if err != nil {
			return err
		}
		b.Documents[v] = json.RawMessage(raw)
	}
	return nil
}

// Sync makes the bundle follow idx: documents for versions the index no
// longer lists are dropped, and the versions the index lists without a
// document in the bundle are returned (the caller must supply them before
// writing, or the bundle would be incomplete).
func (b *VersionBundle) Sync(idx *VersionIndex) (missing []string) {
	listed := map[string]bool{}
	for _, e := range idx.Versions {
		listed[e.Version] = true
		if _, ok := b.Documents[e.Version]; !ok {
			missing = append(missing, e.Version)
		}
	}
	for v := range b.Documents {
		if !listed[v] {
			delete(b.Documents, v)
		}
	}
	b.Index = idx
	return missing
}

// Write serialises the bundle compactly (it is machine-read and large).
func (b *VersionBundle) Write(path string) error {
	if b.Index == nil {
		return fmt.Errorf("bundle has no index")
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

// MeasuredHere reports whether v's hops ran in r's own run, as opposed to
// having been carried over from an earlier release. Documents written before
// measured_in existed are judged by the run's reran list.
func (r *Results) MeasuredHere(v *Variant) bool {
	if v.Missing {
		return false
	}
	if v.MeasuredIn != "" {
		return v.MeasuredIn == r.Run.RunID
	}
	if len(r.Run.Reran) == 0 {
		return true
	}
	for _, id := range r.Run.Reran {
		if id == v.ID {
			return true
		}
	}
	return false
}

// Extract returns a document holding only variant id: the same run manifest
// and dimensions, the annotations that touch the variant, and a summary
// recomputed for it alone. The variant is shared with r, not copied.
func (r *Results) Extract(id string) (*Results, error) {
	v := r.Variant(id)
	if v == nil {
		return nil, fmt.Errorf("%s: not in document", id)
	}
	if v.Missing {
		return nil, fmt.Errorf("%s: not measured in %s", id, r.Run.RunID)
	}
	out := &Results{SchemaVersion: r.SchemaVersion, Run: r.Run, Dimensions: r.Dimensions,
		Variants: []*Variant{v}, Annotations: []Annotation{}}
	for _, a := range r.Annotations {
		if touches(a, id) {
			out.Annotations = append(out.Annotations, a)
		}
	}
	s := &out.Summary
	s.VariantCount = 1
	if v.Tracer != nil {
		s.InstrumentedN = 1
	}
	s.CellCount = len(v.Cells)
	s.HopCount = len(v.Hops)
	s.CanaryLeaks = []string{}
	for _, l := range r.Summary.CanaryLeaks {
		if strings.HasPrefix(l, id+" ") {
			s.CanaryLeaks = append(s.CanaryLeaks, l)
		}
	}
	for _, p := range r.Summary.TraceparentPassthroughs {
		if strings.HasPrefix(p, id+" ") {
			s.TraceparentPassthroughs = append(s.TraceparentPassthroughs, p)
		}
	}
	s.Scorecard = []Score{}
	for _, sc := range r.Summary.Scorecard {
		if sc.Variant == id {
			s.Scorecard = append(s.Scorecard, sc)
		}
	}
	s.TotalsByHeader = map[string]*Count{}
	for h, c := range v.Counts {
		cc := *c
		s.TotalsByHeader[h] = &cc
	}
	return out, nil
}

func touches(a Annotation, id string) bool {
	for _, v := range a.Variants {
		if v == id {
			return true
		}
	}
	for _, c := range a.Cells {
		if strings.HasPrefix(c, id+"/") {
			return true
		}
	}
	return false
}

// Entry builds the index entry for a single-variant document under the
// given canonical version.
func (r *Results) Entry(version string) (VersionEntry, error) {
	if len(r.Variants) != 1 {
		return VersionEntry{}, fmt.Errorf("want a single-variant document, got %d variants", len(r.Variants))
	}
	v := r.Variants[0]
	asset, err := AssetName(version)
	if err != nil {
		return VersionEntry{}, err
	}
	e := VersionEntry{Version: version, Asset: asset, Kind: r.Run.Kind,
		MeasuredIn: v.MeasuredIn, MeasuredAt: v.RanAt, MeasuredCommit: v.MeasuredCommit,
		MatrixVersion: v.MatrixVersion, Counts: v.Counts, CanaryLeaks: len(r.Summary.CanaryLeaks),
		Invalid: v.Invalid}
	if v.Tracer != nil {
		e.Reported = v.Tracer.Version
	}
	if e.MeasuredIn == "" {
		e.MeasuredIn = r.Run.RunID
	}
	if e.MeasuredCommit == "" {
		e.MeasuredCommit = r.Run.HarnessCommit
	}
	if e.MatrixVersion == 0 {
		e.MatrixVersion = r.Run.MatrixVersion
	}
	if e.MatrixVersion == 0 {
		e.MatrixVersion = 1
	}
	return e, nil
}

// LoadVersionIndex reads index.json; a missing file yields an empty index
// for the variant described by v.
func LoadVersionIndex(path string, v *Variant) (*VersionIndex, error) {
	idx := &VersionIndex{SchemaVersion: VersionIndexSchema, Variant: v.ID, Tool: v.Tool,
		Language: v.Language, DisplayName: v.DisplayName, Versions: []VersionEntry{}}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return idx, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, idx); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if idx.SchemaVersion != VersionIndexSchema {
		return nil, fmt.Errorf("%s: schema_version %d, want %d", path, idx.SchemaVersion, VersionIndexSchema)
	}
	if idx.Variant != v.ID {
		return nil, fmt.Errorf("%s: is for %s, not %s", path, idx.Variant, v.ID)
	}
	// Display metadata follows the registry, which may have been edited.
	idx.Tool, idx.Language, idx.DisplayName = v.Tool, v.Language, v.DisplayName
	if idx.Versions == nil {
		idx.Versions = []VersionEntry{}
	}
	return idx, nil
}

// Upsert replaces the entry for e.Version or adds it: the latest measurement
// of a version is the one that stands.
func (idx *VersionIndex) Upsert(e VersionEntry) {
	for i := range idx.Versions {
		if idx.Versions[i].Version == e.Version {
			idx.Versions[i] = e
			return
		}
	}
	idx.Versions = append(idx.Versions, e)
}

// Prune drops entries whose asset is not among the names present on the
// release (a document that was deleted, or an index that got ahead of a
// failed upload), so the index never points at a 404.
func (idx *VersionIndex) Prune(present map[string]bool) (dropped []string) {
	kept := idx.Versions[:0]
	for _, e := range idx.Versions {
		if present[e.Asset] {
			kept = append(kept, e)
		} else {
			dropped = append(dropped, e.Asset)
		}
	}
	idx.Versions = kept
	return dropped
}

// Write sorts newest version first and writes index.json.
func (idx *VersionIndex) Write(path string) error {
	sort.SliceStable(idx.Versions, func(i, j int) bool {
		return versions.Compare(idx.Versions[i].Version, idx.Versions[j].Version) > 0
	})
	idx.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	b, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
