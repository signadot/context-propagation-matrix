// Package results defines results schema v2 — the single published artifact
// consumed by contextpropagation.io — and builds it from the registry plus the
// driver's raw per-variant JSON.
package results

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/signadot/context-propagation-matrix/internal/eval"
	"github.com/signadot/context-propagation-matrix/internal/raw"
	"github.com/signadot/context-propagation-matrix/internal/registry"
)

const SchemaVersion = 2

// Outcome is the normalised per-cell result. Every header uses the same
// vocabulary so the site can colour cells uniformly.
type Outcome string

const (
	OutcomePass        Outcome = "pass"        // seeded value arrived intact
	OutcomePartial     Outcome = "partial"     // header present but seeded value lost
	OutcomeNewTrace    Outcome = "new_trace"   // traceparent present but a different trace-id
	OutcomeFail        Outcome = "fail"        // header absent
	OutcomeUndelivered Outcome = "undelivered" // relay never forwarded anything
	OutcomeNotRun      Outcome = "not_run"     // boundary missing from the raw result
)

// Glyph is the markdown/plain-text rendering of an outcome.
func (o Outcome) Glyph() string {
	switch o {
	case OutcomePass:
		return "✅"
	case OutcomePartial:
		return "🟡"
	case OutcomeNewTrace:
		return "🔀"
	case OutcomeFail:
		return "❌"
	case OutcomeUndelivered:
		return "🚫"
	}
	return "—"
}

type Run struct {
	RunID          string            `json:"run_id"`
	Kind           string            `json:"kind"` // "matrix" | "backtest" | "local"
	HarnessRepo    string            `json:"harness_repo"`
	HarnessCommit  string            `json:"harness_commit,omitempty"`
	StartedAt      time.Time         `json:"started_at"`
	FinishedAt     time.Time         `json:"finished_at"`
	BrokerVersions map[string]string `json:"broker_versions,omitempty"`
	Generator      string            `json:"generator"`
	Notes          string            `json:"notes,omitempty"`
	// MatrixVersion is the measurement contract this run scored against
	// (variants.yaml matrix_version). Carried-over variants may carry an
	// older one; see Variant.MatrixVersion.
	MatrixVersion int `json:"matrix_version"`
	// Partial is set when only some variants were measured in this run and
	// the rest were carried over from the previous release (a push to main
	// re-runs just the variants it affects). Reran lists the measured ids;
	// each variant's ran_at / measured_in say when and where its own cells
	// were last measured.
	Partial bool     `json:"partial,omitempty"`
	Reran   []string `json:"reran,omitempty"`
}

type Dimensions struct {
	Tools      []*registry.Tool      `json:"tools"`
	Languages  []*registry.Language  `json:"languages"`
	Protocols  []*registry.Protocol  `json:"protocols"`
	Boundaries []string              `json:"boundaries"`
	Headers    []*registry.Header    `json:"headers"`
	SetupTiers []*registry.SetupTier `json:"setup_tiers"`
}

// Package is a registry.Package plus the version the relay actually reported
// (Version) and, for the tracer, the version the harness pinned when it built
// the relay (Pinned: what `cpm pins` reads, with no "v" prefix — the value a
// bump PR names, a back-test tag carries and the site puts in a URL). The two
// normally agree; they differ where a relay reports a composite string (PHP:
// "open-telemetry/sdk 1.15.0 (ext-opentelemetry 1.2.1)") or when an install
// silently fell back, which Pinned makes visible.
type Package struct {
	registry.Package
	Version string `json:"version"`
	Pinned  string `json:"pinned,omitempty"`
}

// CanonicalVersion is the version that identifies this measurement of the
// variant: the pinned tracer version, or, for results written before pins
// were recorded, the one version number in what the relay reported ("2.31.1",
// "v2.10.1", "ddtrace 1.25.0"). Empty when there is none or more than one
// (baseline variants; "open-telemetry/sdk 1.15.0 (ext-opentelemetry 1.2.1)"
// names two and only the pin knows which one it is filed under).
func (v *Variant) CanonicalVersion() string {
	if v.Tracer == nil {
		return ""
	}
	if v.Tracer.Pinned != "" {
		return v.Tracer.Pinned
	}
	var found []string
	for _, tok := range strings.FieldsFunc(v.Tracer.Version, func(r rune) bool { return r == ' ' || r == '(' || r == ')' }) {
		if plainVersion.MatchString(tok) {
			found = append(found, strings.TrimPrefix(tok, "v"))
		}
	}
	if len(found) == 1 {
		return found[0]
	}
	return ""
}

var plainVersion = regexp.MustCompile(`^v?[0-9]+(\.[0-9]+)+[0-9A-Za-z.+-]*$`)

// Hop is one boundary through one relay: the evidence shared by the four
// header cells that were evaluated on it.
type Hop struct {
	Boundary   string              `json:"boundary"`
	Inbound    string              `json:"inbound"`
	Outbound   string              `json:"outbound"`
	TestID     string              `json:"test_id,omitempty"`
	Delivered  bool                `json:"delivered"`
	Error      string              `json:"error,omitempty"`
	CanaryLeak bool                `json:"canary_leak"`
	Seeded     map[string]string   `json:"seeded_headers,omitempty"`
	Observed   map[string][]string `json:"observed_headers,omitempty"`
	Transport  string              `json:"observed_transport,omitempty"`
	// The three fields below are evaluated by the aggregator from Observed
	// (internal/eval), not read from the driver's status, so re-aggregating
	// raw results that predate them fills them in. They are informational:
	// no cell outcome and no count depends on them.
	//
	// VendorNative says whether the tool's own (non-W3C) trace header
	// carried the seeded trace: present_same_trace | present_new_trace |
	// absent, judged on the tool's native_headers in variants.yaml (see
	// eval.Native). Omitted for tools that have none.
	VendorNative string `json:"vendor_native,omitempty"`
	// TraceparentSpan and TraceparentFlags describe a traceparent that
	// arrived on the seeded trace, and are omitted otherwise. Span is
	// new_span when the relay put a span id of its own into it and same_span
	// when the seeded one came back out, which is a relay passing the header
	// through verbatim rather than a tracer propagating it. Flags is the
	// observed trace-flags field; the driver seeds "01" (sampled).
	TraceparentSpan  string `json:"traceparent_span,omitempty"`
	TraceparentFlags string `json:"traceparent_flags,omitempty"`
}

type Cell struct {
	CellID    string  `json:"cell_id"` // {variant}/{header}/{inbound}-{outbound}
	Header    string  `json:"header"`
	Boundary  string  `json:"boundary"`
	Inbound   string  `json:"inbound"`
	Outbound  string  `json:"outbound"`
	Outcome   Outcome `json:"outcome"`
	RawStatus string  `json:"raw_status,omitempty"`
}

type Count struct {
	Pass        int `json:"pass"`
	Partial     int `json:"partial"`
	NewTrace    int `json:"new_trace"`
	Fail        int `json:"fail"`
	Undelivered int `json:"undelivered"`
	NotRun      int `json:"not_run"`
	Total       int `json:"total"`
}

func (c *Count) add(o Outcome) {
	c.Total++
	switch o {
	case OutcomePass:
		c.Pass++
	case OutcomePartial:
		c.Partial++
	case OutcomeNewTrace:
		c.NewTrace++
	case OutcomeFail:
		c.Fail++
	case OutcomeUndelivered:
		c.Undelivered++
	default:
		c.NotRun++
	}
}

type Variant struct {
	ID             string            `json:"id"`
	Tool           string            `json:"tool"`
	Language       string            `json:"language"`
	Flavour        string            `json:"flavour"`
	DisplayName    string            `json:"display_name"`
	SetupTier      *string           `json:"setup_tier"`
	Attach         string            `json:"attach,omitempty"`
	Tracer         *Package          `json:"tracer,omitempty"`
	Weaver         *Package          `json:"weaver,omitempty"`
	Runtime        string            `json:"runtime,omitempty"`
	Libs           map[string]string `json:"libs,omitempty"`
	Config         map[string]string `json:"config,omitempty"`
	RequiresSecret string            `json:"requires_secret,omitempty"`
	Source         string            `json:"source"`
	RanAt          time.Time         `json:"ran_at"`
	// Provenance of the measurement itself, which may predate this run when
	// the variant was carried over from an earlier release: MeasuredIn is the
	// run id (release tag) whose job ran the hops, MeasuredCommit the harness
	// commit that job ran, MatrixVersion the contract it scored against.
	// Together with Tracer.Version they identify a result independently of
	// the run that happens to contain it. Empty for Missing variants.
	MeasuredIn     string            `json:"measured_in,omitempty"`
	MeasuredCommit string            `json:"measured_commit,omitempty"`
	MatrixVersion  int               `json:"matrix_version,omitempty"`
	Hops           []Hop             `json:"hops"`
	Cells          []Cell            `json:"cells"`
	Counts         map[string]*Count `json:"counts"`            // by header id
	Missing        bool              `json:"missing,omitempty"` // registered but no raw result in this run
}

// Annotation is an explanation attached to a set of cells (see
// internal/annotations); it is embedded verbatim so the site has one input.
type Annotation struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Cause       string `json:"cause"`
	Explanation string `json:"explanation,omitempty"`
	Status      string `json:"status"`
	FixedIn     string `json:"fixed_in,omitempty"`
	// Resolved is set by the aggregator when every cell the annotation's
	// selectors describe now passes: the explanation is kept, attached to the
	// passing cells, so readers can see what used to fail and since when.
	// FixedIn is filled from the variant's reported tracer version when the
	// annotation itself does not state one.
	Resolved bool     `json:"resolved,omitempty"`
	Links    []Link   `json:"links"`
	Cells    []string `json:"cells"`
	Variants []string `json:"variants"`
}

type Link struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// ScoreKey identifies one scorecard square: a tool × language pair, using the
// default-flavour variant for that pair (or the only variant if none is
// default).
type Score struct {
	Tool      string            `json:"tool"`
	Language  string            `json:"language"`
	Variant   string            `json:"variant"`
	SetupTier *string           `json:"setup_tier"`
	Version   string            `json:"tracer_version,omitempty"`
	Counts    map[string]*Count `json:"counts"`
}

type Summary struct {
	VariantCount   int               `json:"variant_count"`
	InstrumentedN  int               `json:"instrumented_variant_count"`
	CellCount      int               `json:"cell_count"`
	HopCount       int               `json:"hop_count"`
	CanaryLeaks    []string          `json:"canary_leaks"`
	Scorecard      []Score           `json:"scorecard"`
	TotalsByHeader map[string]*Count `json:"totals_by_header"`
	// TraceparentPassthroughs lists the hops whose traceparent came out with
	// the seeded span id (Hop.TraceparentSpan == same_span), as "variant
	// boundary" like CanaryLeaks, so a check can read them without walking
	// every hop. Their traceparent cells are still scored as passes. Unlike
	// CanaryLeaks the key is absent when the list is empty: a field added to
	// schema v2 after the fact has to be optional.
	TraceparentPassthroughs []string `json:"traceparent_passthroughs,omitempty"`
}

type Results struct {
	SchemaVersion int          `json:"schema_version"`
	Run           Run          `json:"run"`
	Dimensions    Dimensions   `json:"dimensions"`
	Variants      []*Variant   `json:"variants"`
	Annotations   []Annotation `json:"annotations"`
	Summary       Summary      `json:"summary"`
}

// Build assembles a schema v2 document from the registry and raw results.
// Variants registered but absent from raws are included with Missing=true and
// no cells, so the site can show "not run" rather than silently dropping rows.
func Build(reg *registry.Registry, raws []*raw.VariantResult, run Run) (*Results, error) {
	byID := map[string]*raw.VariantResult{}
	for _, r := range raws {
		if reg.Variant(r.Variant) == nil {
			return nil, fmt.Errorf("raw result %q is not in variants.yaml", r.Variant)
		}
		byID[r.Variant] = r
	}

	res := &Results{SchemaVersion: SchemaVersion, Run: run}
	res.Dimensions = dimensions(reg)
	res.Summary.TotalsByHeader = map[string]*Count{}
	for _, h := range reg.HeaderOrder {
		res.Summary.TotalsByHeader[h] = &Count{}
	}

	var earliest, latest time.Time
	for _, rv := range reg.SortedVariants() {
		v := &Variant{
			ID: rv.ID, Tool: rv.Tool, Language: rv.Language, Flavour: rv.Flavour,
			DisplayName: rv.DisplayName, SetupTier: rv.SetupTier, Attach: rv.Attach,
			Source: rv.Source, RequiresSecret: reg.RequiresSecret(rv),
			Counts: map[string]*Count{},
		}
		for _, h := range reg.HeaderOrder {
			v.Counts[h] = &Count{}
		}
		r, ok := byID[rv.ID]
		if !ok {
			// Empty, not nil: the site types hops/cells as arrays and reads
			// them unguarded in places, so a missing variant must serialise
			// as [] rather than null.
			v.Missing = true
			v.Hops = []Hop{}
			v.Cells = []Cell{}
			res.Variants = append(res.Variants, v)
			continue
		}
		v.RanAt = r.RanAt
		if earliest.IsZero() || r.RanAt.Before(earliest) {
			earliest = r.RanAt
		}
		if r.RanAt.After(latest) {
			latest = r.RanAt
		}
		v.Runtime = r.InfoString("runtime_version")
		v.Libs = r.Libs()
		v.Config = r.ConfigStrings()
		if rv.Tracer != nil {
			v.Tracer = &Package{Package: *rv.Tracer, Version: r.InfoString("tracer_version")}
		}
		if rv.Weaver != nil {
			v.Weaver = &Package{Package: *rv.Weaver, Version: r.InfoString("weaver_version")}
		}

		var native []string
		if t := reg.Tools[rv.Tool]; t != nil {
			native = t.NativeHeaders
		}
		tests := map[string]raw.TestResult{}
		for _, t := range r.Tests {
			tests[t.Boundary] = t
		}
		for _, b := range reg.Boundaries {
			in, out, _ := strings.Cut(b, "->")
			t, ran := tests[b]
			hop := Hop{Boundary: b, Inbound: in, Outbound: out}
			if ran {
				hop.TestID = t.TestID
				hop.Delivered = t.Delivered
				hop.Error = t.Error
				if t.Seeded != nil {
					hop.Seeded = t.Seeded.Headers
				}
				if t.Observed != nil {
					hop.Observed = t.Observed.Headers
					hop.Transport = t.Observed.Transport
				}
				if t.Observed != nil && t.Seeded != nil {
					hop.VendorNative = eval.Native(hop.Observed, *t.Seeded, native)
					hop.TraceparentSpan = eval.Span(hop.Observed, *t.Seeded)
					hop.TraceparentFlags = eval.Flags(hop.Observed, *t.Seeded)
					if hop.TraceparentSpan == eval.SameSpan {
						res.Summary.TraceparentPassthroughs = append(res.Summary.TraceparentPassthroughs, rv.ID+" "+b)
					}
				}
				if t.Status != nil {
					hop.CanaryLeak = t.Status.CanaryLeak
					if t.Status.CanaryLeak {
						res.Summary.CanaryLeaks = append(res.Summary.CanaryLeaks, rv.ID+" "+b)
					}
				}
				res.Summary.HopCount++
			}
			v.Hops = append(v.Hops, hop)

			for _, hid := range reg.HeaderOrder {
				h := reg.Headers[hid]
				cell := Cell{
					CellID: CellID(rv.ID, hid, in, out), Header: hid,
					Boundary: b, Inbound: in, Outbound: out,
				}
				switch {
				case !ran:
					cell.Outcome = OutcomeNotRun
				case !t.Delivered || t.Status == nil:
					cell.Outcome = OutcomeUndelivered
				default:
					rs, _ := t.Status.Field(h.RawField)
					cell.RawStatus = rs
					cell.Outcome = classify(h, rs)
				}
				v.Cells = append(v.Cells, cell)
				v.Counts[hid].add(cell.Outcome)
				res.Summary.TotalsByHeader[hid].add(cell.Outcome)
				res.Summary.CellCount++
			}
		}
		res.Variants = append(res.Variants, v)
	}
	if res.Summary.CanaryLeaks == nil {
		res.Summary.CanaryLeaks = []string{}
	}
	if res.Run.StartedAt.IsZero() {
		res.Run.StartedAt = earliest
	}
	if res.Run.FinishedAt.IsZero() {
		res.Run.FinishedAt = latest
	}
	res.Summary.VariantCount = len(res.Variants)
	for _, v := range res.Variants {
		if t := reg.Tools[v.Tool]; t != nil && !t.IsBaseline && !v.Missing {
			res.Summary.InstrumentedN++
		}
	}
	res.Summary.Scorecard = scorecard(reg, res.Variants)
	res.Annotations = []Annotation{}
	if res.Run.MatrixVersion == 0 {
		res.Run.MatrixVersion = reg.MatrixVersion
	}
	return res, nil
}

// SetProvenance stamps every measured variant with this run's identity and
// gives every carried-over variant the provenance it had in prev, the
// release it was carried over from. measured is the set of variant ids whose
// hops ran in this run; nil means all of them. pinned gives the tracer
// version the harness built each measured variant with (by variant id;
// absent entries leave Tracer.Pinned empty). Run.RunID, HarnessCommit and
// MatrixVersion must already be set. A carried-over variant with no prev to
// read from is an error: it would otherwise look like it was measured here.
func (r *Results) SetProvenance(prev *Results, measured map[string]bool, pinned map[string]string) error {
	var orphans []string
	for _, v := range r.Variants {
		if v.Missing {
			continue
		}
		if measured == nil || measured[v.ID] {
			v.MeasuredIn = r.Run.RunID
			v.MeasuredCommit = r.Run.HarnessCommit
			v.MatrixVersion = r.Run.MatrixVersion
			if v.Tracer != nil {
				v.Tracer.Pinned = pinned[v.ID]
			}
			continue
		}
		var pv *Variant
		if prev != nil {
			pv = prev.Variant(v.ID)
		}
		if pv == nil || pv.Missing {
			orphans = append(orphans, v.ID)
			continue
		}
		// Releases older than this field fall back to the run that
		// contained them, which is as precise as their data allows.
		v.MeasuredIn = pv.MeasuredIn
		if v.MeasuredIn == "" {
			v.MeasuredIn = prev.Run.RunID
		}
		v.MeasuredCommit = pv.MeasuredCommit
		if v.MeasuredCommit == "" {
			v.MeasuredCommit = prev.Run.HarnessCommit
		}
		v.MatrixVersion = pv.MatrixVersion
		if v.MatrixVersion == 0 {
			v.MatrixVersion = prev.Run.MatrixVersion
		}
		if v.MatrixVersion == 0 {
			v.MatrixVersion = 1
		}
		if v.Tracer != nil && pv.Tracer != nil {
			v.Tracer.Pinned = pv.Tracer.Pinned
		}
	}
	if len(orphans) > 0 {
		return fmt.Errorf("carried-over variant(s) with no previous results to take provenance from: %s", strings.Join(orphans, ", "))
	}
	return nil
}

// CellID builds the stable, URL-safe id for a cell.
func CellID(variant, header, in, out string) string {
	return variant + "/" + header + "/" + in + "-" + out
}

func classify(h *registry.Header, rawStatus string) Outcome {
	for _, s := range h.Success {
		if s == rawStatus {
			return OutcomePass
		}
	}
	for _, s := range h.Partial {
		if s == rawStatus {
			return OutcomePartial
		}
	}
	for _, s := range h.NewTrace {
		if s == rawStatus {
			return OutcomeNewTrace
		}
	}
	return OutcomeFail
}

func dimensions(reg *registry.Registry) Dimensions {
	d := Dimensions{Boundaries: reg.Boundaries}
	for _, id := range reg.ToolOrder {
		d.Tools = append(d.Tools, reg.Tools[id])
	}
	d.Languages = reg.SortedLanguages()
	for _, id := range reg.ProtocolOrder {
		d.Protocols = append(d.Protocols, reg.Protocols[id])
	}
	for _, id := range reg.HeaderOrder {
		d.Headers = append(d.Headers, reg.Headers[id])
	}
	for _, id := range reg.TierOrder {
		d.SetupTiers = append(d.SetupTiers, reg.SetupTiers[id])
	}
	return d
}

// scorecard picks one representative variant per tool × language: the
// default flavour when it exists, otherwise the first in canonical order.
func scorecard(reg *registry.Registry, variants []*Variant) []Score {
	pick := map[string]*Variant{}
	for _, v := range variants {
		if v.Missing {
			continue
		}
		k := v.Tool + "/" + v.Language
		cur, ok := pick[k]
		if !ok || (v.Flavour == "default" && cur.Flavour != "default") {
			pick[k] = v
		}
	}
	var out []Score
	for _, tool := range reg.ToolOrder {
		for _, lang := range reg.SortedLanguages() {
			v, ok := pick[tool+"/"+lang.ID]
			if !ok {
				continue
			}
			s := Score{Tool: tool, Language: lang.ID, Variant: v.ID, SetupTier: v.SetupTier, Counts: v.Counts}
			if v.Tracer != nil {
				s.Version = v.Tracer.Version
			}
			out = append(out, s)
		}
	}
	return out
}

// Load reads a results.json.
func Load(path string) (*Results, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Results
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if r.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("%s: schema_version %d, want %d", path, r.SchemaVersion, SchemaVersion)
	}
	return &r, nil
}

// Write serialises results to path with stable formatting.
func (r *Results) Write(path string) error {
	b, err := json.MarshalIndent(r, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Variant returns the variant with the given id.
func (r *Results) Variant(id string) *Variant {
	for _, v := range r.Variants {
		if v.ID == id {
			return v
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Changes between two runs
// ---------------------------------------------------------------------------

type CellChange struct {
	CellID      string  `json:"cell_id"`
	Variant     string  `json:"variant"`
	Header      string  `json:"header"`
	Boundary    string  `json:"boundary"`
	From        Outcome `json:"from"`
	To          Outcome `json:"to"`
	Improvement bool    `json:"improvement"`
}

type VersionChange struct {
	Variant string `json:"variant"`
	Package string `json:"package"`
	From    string `json:"from"`
	To      string `json:"to"`
}

type Changes struct {
	FromRun         string          `json:"from_run"`
	ToRun           string          `json:"to_run"`
	ChangedCells    []CellChange    `json:"changed_cells"`
	AddedVariants   []string        `json:"added_variants"`
	RemovedVariants []string        `json:"removed_variants"`
	VersionBumps    []VersionChange `json:"version_bumps"`
	Improved        int             `json:"improved"`
	Regressed       int             `json:"regressed"`
}

// Empty reports whether nothing observable changed (used to decide whether to
// post a Slack summary or write a changelog entry).
func (c *Changes) Empty() bool {
	return len(c.ChangedCells) == 0 && len(c.AddedVariants) == 0 &&
		len(c.RemovedVariants) == 0 && len(c.VersionBumps) == 0
}

var outcomeRank = map[Outcome]int{
	OutcomeNotRun: 0, OutcomeUndelivered: 1, OutcomeFail: 2, OutcomeNewTrace: 3, OutcomePartial: 4, OutcomePass: 5,
}

// Diff computes cell-level changes between two results documents.
func Diff(from, to *Results) *Changes {
	ch := &Changes{FromRun: from.Run.RunID, ToRun: to.Run.RunID,
		ChangedCells: []CellChange{}, AddedVariants: []string{}, RemovedVariants: []string{}, VersionBumps: []VersionChange{}}
	oldCells := map[string]Outcome{}
	oldVariants := map[string]*Variant{}
	for _, v := range from.Variants {
		if v.Missing {
			continue
		}
		oldVariants[v.ID] = v
		for _, c := range v.Cells {
			oldCells[c.CellID] = c.Outcome
		}
	}
	newVariants := map[string]bool{}
	for _, v := range to.Variants {
		if v.Missing {
			continue
		}
		newVariants[v.ID] = true
		ov, existed := oldVariants[v.ID]
		if !existed {
			ch.AddedVariants = append(ch.AddedVariants, v.ID)
			continue
		}
		if v.Tracer != nil && ov.Tracer != nil && v.Tracer.Version != ov.Tracer.Version {
			ch.VersionBumps = append(ch.VersionBumps, VersionChange{Variant: v.ID, Package: v.Tracer.Package.Package, From: ov.Tracer.Version, To: v.Tracer.Version})
		}
		for _, c := range v.Cells {
			prev, ok := oldCells[c.CellID]
			if !ok || prev == c.Outcome {
				continue
			}
			cc := CellChange{CellID: c.CellID, Variant: v.ID, Header: c.Header, Boundary: c.Boundary, From: prev, To: c.Outcome,
				Improvement: outcomeRank[c.Outcome] > outcomeRank[prev]}
			if cc.Improvement {
				ch.Improved++
			} else {
				ch.Regressed++
			}
			ch.ChangedCells = append(ch.ChangedCells, cc)
		}
	}
	for id := range oldVariants {
		if !newVariants[id] {
			ch.RemovedVariants = append(ch.RemovedVariants, id)
		}
	}
	sort.Strings(ch.AddedVariants)
	sort.Strings(ch.RemovedVariants)
	return ch
}

// Write serialises changes to path.
func (c *Changes) Write(path string) error {
	b, err := json.MarshalIndent(c, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
