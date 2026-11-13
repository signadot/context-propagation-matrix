// Package registry loads and validates variants.yaml, the single source of
// truth for the matrix's dimensions (tools, languages, protocols, boundaries,
// headers, setup tiers, and the variants themselves).
package registry

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Tool struct {
	ID                 string   `yaml:"-" json:"id"`
	DisplayName        string   `yaml:"display_name" json:"display_name"`
	ShortName          string   `yaml:"short_name" json:"short_name"`
	Vendor             *string  `yaml:"vendor" json:"vendor"`
	URL                *string  `yaml:"url" json:"url"`
	NativeHeaders      []string `yaml:"native_headers" json:"native_headers"`
	DefaultPropagators []string `yaml:"default_propagators" json:"default_propagators"`
	RequiresSecret     string   `yaml:"requires_secret,omitempty" json:"requires_secret,omitempty"`
	IsBaseline         bool     `yaml:"is_baseline,omitempty" json:"is_baseline,omitempty"`
}

type Language struct {
	ID          string `yaml:"-" json:"id"`
	DisplayName string `yaml:"display_name" json:"display_name"`
	Order       int    `yaml:"order" json:"order"`
}

type Protocol struct {
	ID          string `yaml:"-" json:"id"`
	DisplayName string `yaml:"display_name" json:"display_name"`
	// ShortName is the label for tight grid axes (e.g. "rmq"); the id is used
	// when it is empty.
	ShortName string `yaml:"short_name,omitempty" json:"short_name,omitempty"`
	Carrier   string `yaml:"carrier" json:"carrier"`
	Async     bool   `yaml:"async,omitempty" json:"async,omitempty"`
}

type Header struct {
	ID          string   `yaml:"-" json:"id"`
	DisplayName string   `yaml:"display_name" json:"display_name"`
	WireName    string   `yaml:"wire_name" json:"wire_name"`
	RawField    string   `yaml:"raw_field" json:"raw_field"`
	Question    string   `yaml:"question" json:"question"`
	Success     []string `yaml:"success" json:"success"`
	Partial     []string `yaml:"partial" json:"partial"`
	NewTrace    []string `yaml:"new_trace,omitempty" json:"new_trace,omitempty"`
	Spec        string   `yaml:"spec" json:"spec"`
}

type SetupTier struct {
	ID          string `yaml:"-" json:"id"`
	DisplayName string `yaml:"display_name" json:"display_name"`
	Description string `yaml:"description" json:"description"`
}

// Wiring is one way an in-code variant's relay can be wired for a leg: whose
// code moves the context, an instrumentation library's or the relay's own.
type Wiring struct {
	ID          string `yaml:"-" json:"id"`
	DisplayName string `yaml:"display_name" json:"display_name"`
	Description string `yaml:"description" json:"description"`
}

// Leg is how the relay is wired for one protocol: Inbound when it receives
// (and extracts), Outbound when it sends (and injects). Both are Wiring ids.
type Leg struct {
	Inbound  string `yaml:"inbound" json:"inbound"`
	Outbound string `yaml:"outbound" json:"outbound"`
}

// inCodeTier is the setup tier whose variants say how each leg is wired.
const inCodeTier = "in-code"

// Package identifies a tracer (or weaver) in a package registry so versions
// can be discovered automatically.
type Package struct {
	Name     string `yaml:"name" json:"name"`
	Registry string `yaml:"registry" json:"registry"`
	Package  string `yaml:"package" json:"package"`
	GitHub   string `yaml:"github,omitempty" json:"github,omitempty"`
	// TagPrefix is stripped from GitHub release tags to obtain the version
	// (most projects tag "v1.2.3"; dd-trace-php tags "1.2.3").
	TagPrefix string `yaml:"tag_prefix,omitempty" json:"tag_prefix,omitempty"`
}

type Variant struct {
	ID          string   `yaml:"id" json:"id"`
	Tool        string   `yaml:"tool" json:"tool"`
	Language    string   `yaml:"language" json:"language"`
	Flavour     string   `yaml:"flavour" json:"flavour"`
	DisplayName string   `yaml:"display_name" json:"display_name"`
	SetupTier   *string  `yaml:"setup_tier" json:"setup_tier"`
	Attach      string   `yaml:"attach,omitempty" json:"attach,omitempty"`
	Docs        string   `yaml:"docs,omitempty" json:"docs,omitempty"`
	Tracer      *Package `yaml:"tracer,omitempty" json:"tracer,omitempty"`
	Weaver      *Package `yaml:"weaver,omitempty" json:"weaver,omitempty"`
	Source      string   `yaml:"source" json:"source"`
	// Legs says, per protocol, how an in-code variant's relay is wired for
	// each direction. Required for the in-code tier, absent everywhere else.
	Legs map[string]Leg `yaml:"legs,omitempty" json:"legs,omitempty"`
}

// ComposeService is the docker-compose service name for the variant.
func (v Variant) ComposeService() string { return "relay-" + v.ID }

type Registry struct {
	SchemaVersion int `yaml:"schema_version"`
	// MatrixVersion is the version of the measurement contract: the cells a
	// variant is scored on and what each outcome means. It is bumped by hand
	// when a dimension (headers, boundaries, protocols) or an outcome's
	// semantics change, so results measured under different values are not
	// comparable cell for cell. It is recorded on every result and stays with
	// a measurement when a later run carries it over. It is independent of
	// schema_version (the shape of this file) and of the results.json schema.
	MatrixVersion int                   `yaml:"matrix_version"`
	Tools         map[string]*Tool      `yaml:"tools"`
	Languages     map[string]*Language  `yaml:"languages"`
	Protocols     map[string]*Protocol  `yaml:"protocols"`
	Boundaries    []string              `yaml:"boundaries"`
	Headers       map[string]*Header    `yaml:"headers"`
	SetupTiers    map[string]*SetupTier `yaml:"setup_tiers"`
	Wirings       map[string]*Wiring    `yaml:"wirings"`
	Variants      []*Variant            `yaml:"variants"`

	// HeaderOrder preserves the document order of the headers map.
	HeaderOrder []string `yaml:"-"`
	// ToolOrder preserves the document order of the tools map.
	ToolOrder []string `yaml:"-"`
	// ProtocolOrder preserves the document order of the protocols map.
	ProtocolOrder []string `yaml:"-"`
	// TierOrder preserves the document order of the setup_tiers map.
	TierOrder []string `yaml:"-"`
	// WiringOrder preserves the document order of the wirings map.
	WiringOrder []string `yaml:"-"`
}

// Load reads and validates a registry file.
func Load(path string) (*Registry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b, path)
}

// Parse decodes and validates registry YAML; name is used in error messages
// (a path, or e.g. "abc123:variants.yaml" for a historical revision).
func Parse(b []byte, name string) (*Registry, error) {
	var r Registry
	if err := yaml.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	// Recover map key order from the raw document (yaml.v3 maps are unordered).
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	r.ToolOrder = mapKeyOrder(&doc, "tools")
	r.HeaderOrder = mapKeyOrder(&doc, "headers")
	r.ProtocolOrder = mapKeyOrder(&doc, "protocols")
	r.TierOrder = mapKeyOrder(&doc, "setup_tiers")
	r.WiringOrder = mapKeyOrder(&doc, "wirings")

	for id, t := range r.Tools {
		t.ID = id
	}
	for id, l := range r.Languages {
		l.ID = id
	}
	for id, p := range r.Protocols {
		p.ID = id
	}
	for id, h := range r.Headers {
		h.ID = id
	}
	for id, s := range r.SetupTiers {
		s.ID = id
	}
	for id, w := range r.Wirings {
		w.ID = id
	}
	if r.MatrixVersion == 0 {
		// Registries written before the field existed (historical revisions
		// read by `cpm affected`) measured under the first contract.
		r.MatrixVersion = 1
	}
	if err := r.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return &r, nil
}

func mapKeyOrder(doc *yaml.Node, key string) []string {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != key {
			continue
		}
		m := root.Content[i+1]
		var keys []string
		for j := 0; j+1 < len(m.Content); j += 2 {
			keys = append(keys, m.Content[j].Value)
		}
		return keys
	}
	return nil
}

// Validate checks referential integrity: every variant points at a known
// tool, language and tier; ids are unique and follow the naming convention;
// in-code variants wire every protocol in both directions and no other
// variant has legs; boundaries reference known protocols.
func (r *Registry) Validate() error {
	var errs []string
	if r.MatrixVersion < 1 {
		errs = append(errs, fmt.Sprintf("matrix_version must be a positive integer, got %d", r.MatrixVersion))
	}
	seen := map[string]bool{}
	for _, v := range r.Variants {
		if v.ID == "" {
			errs = append(errs, "variant with empty id")
			continue
		}
		if seen[v.ID] {
			errs = append(errs, fmt.Sprintf("duplicate variant id %q", v.ID))
		}
		seen[v.ID] = true
		tool, ok := r.Tools[v.Tool]
		if !ok {
			errs = append(errs, fmt.Sprintf("%s: unknown tool %q", v.ID, v.Tool))
		}
		if _, ok := r.Languages[v.Language]; !ok {
			errs = append(errs, fmt.Sprintf("%s: unknown language %q", v.ID, v.Language))
		}
		if !strings.HasPrefix(v.ID, v.Tool+"-"+v.Language) {
			errs = append(errs, fmt.Sprintf("%s: id must start with %s-%s", v.ID, v.Tool, v.Language))
		}
		if v.Flavour == "" {
			errs = append(errs, fmt.Sprintf("%s: flavour is required (use \"default\")", v.ID))
		}
		if v.Flavour != "default" && !strings.HasSuffix(v.ID, "-"+v.Flavour) {
			errs = append(errs, fmt.Sprintf("%s: non-default flavour %q must be the id suffix", v.ID, v.Flavour))
		}
		if v.Flavour == "default" && v.ID != v.Tool+"-"+v.Language {
			errs = append(errs, fmt.Sprintf("%s: default flavour id must be exactly %s-%s", v.ID, v.Tool, v.Language))
		}
		if ok && tool.IsBaseline {
			if v.SetupTier != nil {
				errs = append(errs, fmt.Sprintf("%s: baseline variants must have setup_tier: null", v.ID))
			}
		} else {
			if v.SetupTier == nil {
				errs = append(errs, fmt.Sprintf("%s: setup_tier is required", v.ID))
			} else if _, ok := r.SetupTiers[*v.SetupTier]; !ok {
				errs = append(errs, fmt.Sprintf("%s: unknown setup_tier %q", v.ID, *v.SetupTier))
			}
			if v.Tracer == nil {
				errs = append(errs, fmt.Sprintf("%s: tracer coordinates are required", v.ID))
			}
		}
		if v.Source == "" {
			errs = append(errs, fmt.Sprintf("%s: source is required", v.ID))
		}
		errs = append(errs, r.legErrors(v, ok && tool.IsBaseline)...)
	}
	// docs is the vendor's page for the variant's agent in this language, the
	// one a reader of its results should open next (the site's per-language
	// "Docs" link). Every instrumented variant has one; flavours of a tracer
	// share their language's page. Baselines have no agent to document.
	for _, v := range r.Variants {
		if tool, ok := r.Tools[v.Tool]; ok && tool.IsBaseline {
			if v.Docs != "" {
				errs = append(errs, fmt.Sprintf("%s: baseline variants have no docs page", v.ID))
			}
			continue
		}
		if v.Docs == "" {
			errs = append(errs, fmt.Sprintf("%s: docs is required (the vendor's page for this agent in this language)", v.ID))
		} else if !strings.HasPrefix(v.Docs, "https://") {
			errs = append(errs, fmt.Sprintf("%s: docs must be an https URL, got %q", v.ID, v.Docs))
		}
	}
	for _, b := range r.Boundaries {
		in, out, ok := strings.Cut(b, "->")
		if !ok {
			errs = append(errs, fmt.Sprintf("boundary %q must be inbound->outbound", b))
			continue
		}
		if _, ok := r.Protocols[in]; !ok {
			errs = append(errs, fmt.Sprintf("boundary %q: unknown protocol %q", b, in))
		}
		if _, ok := r.Protocols[out]; !ok {
			errs = append(errs, fmt.Sprintf("boundary %q: unknown protocol %q", b, out))
		}
	}
	if want := len(r.Protocols) * len(r.Protocols); len(r.Boundaries) != want {
		errs = append(errs, fmt.Sprintf("expected %d boundaries (protocols²), got %d", want, len(r.Boundaries)))
	}
	for id, h := range r.Headers {
		if h.RawField == "" {
			errs = append(errs, fmt.Sprintf("header %s: raw_field is required", id))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid registry:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// legErrors checks a variant's legs: the in-code tier says how every protocol
// is wired in both directions, every other tier and the baseline says
// nothing.
//
// The requirement applies to a registry that defines wirings. One without the
// block is a revision older than the field; `cpm affected` reads those to tell
// which variants changed, and a parse error there would re-run every variant.
func (r *Registry) legErrors(v *Variant, baseline bool) []string {
	var errs []string
	inCode := v.SetupTier != nil && *v.SetupTier == inCodeTier
	switch {
	case baseline && len(v.Legs) > 0:
		return []string{fmt.Sprintf("%s: baseline variants must not have legs", v.ID)}
	case !inCode && len(v.Legs) > 0:
		tier := "null"
		if v.SetupTier != nil {
			tier = *v.SetupTier
		}
		return []string{fmt.Sprintf("%s: legs are only for setup_tier %s, not %s", v.ID, inCodeTier, tier)}
	case !inCode:
		return nil
	}

	protocols := make([]string, 0, len(r.Protocols))
	for p := range r.Protocols {
		protocols = append(protocols, p)
	}
	sort.Strings(protocols)
	if len(r.Wirings) > 0 {
		for _, p := range protocols {
			if _, ok := v.Legs[p]; !ok {
				errs = append(errs, fmt.Sprintf("%s: legs: no entry for protocol %q (an in-code variant wires every protocol)", v.ID, p))
			}
		}
	}
	legged := make([]string, 0, len(v.Legs))
	for p := range v.Legs {
		legged = append(legged, p)
	}
	sort.Strings(legged)
	for _, p := range legged {
		if _, ok := r.Protocols[p]; !ok {
			errs = append(errs, fmt.Sprintf("%s: legs: unknown protocol %q", v.ID, p))
			continue
		}
		leg := v.Legs[p]
		for _, d := range []struct{ name, wiring string }{{"inbound", leg.Inbound}, {"outbound", leg.Outbound}} {
			if d.wiring == "" {
				errs = append(errs, fmt.Sprintf("%s: legs.%s: %s is required", v.ID, p, d.name))
			} else if _, ok := r.Wirings[d.wiring]; !ok {
				errs = append(errs, fmt.Sprintf("%s: legs.%s: unknown wiring %q for %s", v.ID, p, d.wiring, d.name))
			}
		}
	}
	return errs
}

// Variant returns the variant with the given id.
func (r *Registry) Variant(id string) *Variant {
	for _, v := range r.Variants {
		if v.ID == id {
			return v
		}
	}
	return nil
}

// VariantIDs returns every variant id in registry order.
func (r *Registry) VariantIDs() []string {
	ids := make([]string, 0, len(r.Variants))
	for _, v := range r.Variants {
		ids = append(ids, v.ID)
	}
	return ids
}

// RequiresSecret returns the name of the secret the variant's tool needs, or "".
func (r *Registry) RequiresSecret(v *Variant) string {
	if t, ok := r.Tools[v.Tool]; ok {
		return t.RequiresSecret
	}
	return ""
}

// Less orders variants by tool (document order), language (order field),
// then default flavour first, then id.
func (r *Registry) Less(a, b *Variant) bool {
	if a.Tool != b.Tool {
		return r.toolIndex(a.Tool) < r.toolIndex(b.Tool)
	}
	la, lb := r.Languages[a.Language], r.Languages[b.Language]
	if la != nil && lb != nil && la.Order != lb.Order {
		return la.Order < lb.Order
	}
	if (a.Flavour == "default") != (b.Flavour == "default") {
		return a.Flavour == "default"
	}
	return a.ID < b.ID
}

func (r *Registry) toolIndex(id string) int {
	for i, t := range r.ToolOrder {
		if t == id {
			return i
		}
	}
	return len(r.ToolOrder)
}

// SortedVariants returns a copy of Variants in canonical order.
func (r *Registry) SortedVariants() []*Variant {
	vs := append([]*Variant(nil), r.Variants...)
	sort.Slice(vs, func(i, j int) bool { return r.Less(vs[i], vs[j]) })
	return vs
}

// SortedLanguages returns languages by their order field.
func (r *Registry) SortedLanguages() []*Language {
	ls := make([]*Language, 0, len(r.Languages))
	for _, l := range r.Languages {
		ls = append(ls, l)
	}
	sort.Slice(ls, func(i, j int) bool { return ls[i].Order < ls[j].Order })
	return ls
}
