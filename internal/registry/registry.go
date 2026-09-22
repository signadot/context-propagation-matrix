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
	Carrier     string `yaml:"carrier" json:"carrier"`
	Async       bool   `yaml:"async,omitempty" json:"async,omitempty"`
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

// Package identifies a tracer (or weaver) in a package registry so versions
// can be discovered automatically.
type Package struct {
	Name     string `yaml:"name" json:"name"`
	Registry string `yaml:"registry" json:"registry"`
	Package  string `yaml:"package" json:"package"`
	GitHub   string `yaml:"github,omitempty" json:"github,omitempty"`
}

type Variant struct {
	ID          string   `yaml:"id" json:"id"`
	Tool        string   `yaml:"tool" json:"tool"`
	Language    string   `yaml:"language" json:"language"`
	Flavour     string   `yaml:"flavour" json:"flavour"`
	DisplayName string   `yaml:"display_name" json:"display_name"`
	SetupTier   *string  `yaml:"setup_tier" json:"setup_tier"`
	Attach      string   `yaml:"attach,omitempty" json:"attach,omitempty"`
	Tracer      *Package `yaml:"tracer,omitempty" json:"tracer,omitempty"`
	Weaver      *Package `yaml:"weaver,omitempty" json:"weaver,omitempty"`
	Source      string   `yaml:"source" json:"source"`
}

// ComposeService is the docker-compose service name for the variant.
func (v Variant) ComposeService() string { return "relay-" + v.ID }

type Registry struct {
	SchemaVersion int                  `yaml:"schema_version"`
	Tools         map[string]*Tool     `yaml:"tools"`
	Languages     map[string]*Language `yaml:"languages"`
	Protocols     map[string]*Protocol `yaml:"protocols"`
	Boundaries    []string             `yaml:"boundaries"`
	Headers       map[string]*Header   `yaml:"headers"`
	SetupTiers    map[string]*SetupTier `yaml:"setup_tiers"`
	Variants      []*Variant           `yaml:"variants"`

	// HeaderOrder preserves the document order of the headers map.
	HeaderOrder []string `yaml:"-"`
	// ToolOrder preserves the document order of the tools map.
	ToolOrder []string `yaml:"-"`
	// ProtocolOrder preserves the document order of the protocols map.
	ProtocolOrder []string `yaml:"-"`
	// TierOrder preserves the document order of the setup_tiers map.
	TierOrder []string `yaml:"-"`
}

// Load reads and validates a registry file.
func Load(path string) (*Registry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Registry
	if err := yaml.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
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
	if err := r.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
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
// boundaries reference known protocols.
func (r *Registry) Validate() error {
	var errs []string
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
