// Package results defines results schema v2 — the single published artifact
// consumed by contextpropagation.io — and builds it from the registry plus the
// driver's raw per-variant JSON.
package results

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

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
}

type Dimensions struct {
	Tools      []*registry.Tool      `json:"tools"`
	Languages  []*registry.Language  `json:"languages"`
	Protocols  []*registry.Protocol  `json:"protocols"`
	Boundaries []string              `json:"boundaries"`
	Headers    []*registry.Header    `json:"headers"`
	SetupTiers []*registry.SetupTier `json:"setup_tiers"`
}

// Package is a registry.Package plus the version the relay actually reported.
type Package struct {
	registry.Package
	Version string `json:"version"`
}

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
	// VendorNative records the tool's own header continuity where the driver
	// evaluates one (currently Datadog's x-datadog-* set).
	VendorNative string `json:"vendor_native,omitempty"`
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
	Hops           []Hop             `json:"hops"`
	Cells          []Cell            `json:"cells"`
	Counts         map[string]*Count `json:"counts"`            // by header id
	Missing        bool              `json:"missing,omitempty"` // registered but no raw result in this run
}

// Annotation is an explanation attached to a set of cells (see
// internal/annotations); it is embedded verbatim so the site has one input.
type Annotation struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Cause       string   `json:"cause"`
	Explanation string   `json:"explanation,omitempty"`
	Status      string   `json:"status"`
	FixedIn     string   `json:"fixed_in,omitempty"`
	Links       []Link   `json:"links,omitempty"`
	Cells       []string `json:"cells"`
	Variants    []string `json:"variants"`
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
			v.Missing = true
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
				if t.Status != nil {
					hop.CanaryLeak = t.Status.CanaryLeak
					hop.VendorNative = t.Status.DatadogNative
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
	return res, nil
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
