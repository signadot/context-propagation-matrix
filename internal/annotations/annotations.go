// Package annotations loads annotations/*.yaml — human explanations for sets
// of cells (root cause, upstream links, fix status) — and attaches them to a
// results document by evaluating their selectors against real cells.
package annotations

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/signadot/context-propagation-matrix/internal/results"
)

// Selector picks cells. Every populated field must match; list fields match
// any element; `variant` and `boundary` accept "*" globs on either side of
// "->" (e.g. "*->kafka", "kafka->*", "datadog-python-*").
type Selector struct {
	Tool     StringList `yaml:"tool"`
	Language StringList `yaml:"language"`
	Variant  StringList `yaml:"variant"`
	Flavour  StringList `yaml:"flavour"`
	Header   StringList `yaml:"header"`
	Boundary StringList `yaml:"boundary"`
	// Outcome restricts the match to cells with these outcomes; defaults to
	// every non-pass outcome so annotations describe problems, not successes.
	Outcome StringList `yaml:"outcome"`
}

// StringList accepts either a scalar or a sequence in YAML.
type StringList []string

func (s *StringList) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		*s = []string{n.Value}
		return nil
	case yaml.SequenceNode:
		var out []string
		for _, c := range n.Content {
			out = append(out, c.Value)
		}
		*s = out
		return nil
	}
	return fmt.Errorf("line %d: expected string or list", n.Line)
}

type Annotation struct {
	ID          string          `yaml:"id"`
	Title       string          `yaml:"title"`
	Match       []Selector      `yaml:"match"`
	Cause       string          `yaml:"cause"`
	Explanation string          `yaml:"explanation"`
	Status      string          `yaml:"status"` // open | by-design | fixed | wontfix | config
	FixedIn     string          `yaml:"fixed_in"`
	Links       []results.Link  `yaml:"links"`
	sourceFile  string
}

var validStatus = map[string]bool{"open": true, "by-design": true, "fixed": true, "wontfix": true, "config": true, "app-limitation": true}

// LoadDir reads every *.yaml / *.yml under dir. Each file holds a list of
// annotations.
func LoadDir(dir string) ([]*Annotation, error) {
	var files []string
	for _, pat := range []string{"*.yaml", "*.yml"} {
		m, err := filepath.Glob(filepath.Join(dir, pat))
		if err != nil {
			return nil, err
		}
		files = append(files, m...)
	}
	sort.Strings(files)
	var all []*Annotation
	seen := map[string]string{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var list []*Annotation
		if err := yaml.Unmarshal(b, &list); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		for _, a := range list {
			a.sourceFile = f
			if a.ID == "" {
				return nil, fmt.Errorf("%s: annotation without id", f)
			}
			if prev, dup := seen[a.ID]; dup {
				return nil, fmt.Errorf("%s: duplicate annotation id %q (also in %s)", f, a.ID, prev)
			}
			seen[a.ID] = f
			if a.Title == "" || a.Cause == "" {
				return nil, fmt.Errorf("%s: %s: title and cause are required", f, a.ID)
			}
			if a.Status == "" {
				a.Status = "open"
			}
			if !validStatus[a.Status] {
				return nil, fmt.Errorf("%s: %s: invalid status %q", f, a.ID, a.Status)
			}
			if len(a.Match) == 0 {
				return nil, fmt.Errorf("%s: %s: at least one match selector is required", f, a.ID)
			}
			for _, l := range a.Links {
				if !strings.HasPrefix(l.URL, "http") {
					return nil, fmt.Errorf("%s: %s: link %q is not a URL", f, a.ID, l.URL)
				}
			}
		}
		all = append(all, list...)
	}
	return all, nil
}

// Problem is a validation finding.
type Problem struct {
	Annotation string
	File       string
	Fatal      bool
	Message    string
}

func (p Problem) String() string {
	lvl := "warn"
	if p.Fatal {
		lvl = "error"
	}
	return fmt.Sprintf("%s: %s (%s): %s", lvl, p.Annotation, p.File, p.Message)
}

// Apply evaluates selectors against res, appends matched annotations to
// res.Annotations, and returns validation problems. A selector that matches
// no cell at all is fatal (it is stale or mistyped); one that matches only
// passing cells is a warning (the problem may have been fixed upstream).
func Apply(res *results.Results, anns []*Annotation) []Problem {
	var problems []Problem
	res.Annotations = res.Annotations[:0]
	for _, a := range anns {
		cellSet := map[string]bool{}
		varSet := map[string]bool{}
		anyNonPass := false
		for _, sel := range a.Match {
			matched := 0
			for _, v := range res.Variants {
				if v.Missing || !sel.matchesVariant(v) {
					continue
				}
				for _, c := range v.Cells {
					if !sel.matchesCell(&c) {
						continue
					}
					matched++
					cellSet[c.CellID] = true
					varSet[v.ID] = true
					if c.Outcome != results.OutcomePass {
						anyNonPass = true
					}
				}
			}
			if matched == 0 {
				problems = append(problems, Problem{a.ID, a.sourceFile, true, fmt.Sprintf("selector %s matches no cell", sel)})
			}
		}
		if len(cellSet) > 0 && !anyNonPass && a.Status != "fixed" {
			problems = append(problems, Problem{a.ID, a.sourceFile, false, "every matched cell now passes; set status: fixed and fixed_in"})
		}
		cells := keys(cellSet)
		vars := keys(varSet)
		res.Annotations = append(res.Annotations, results.Annotation{
			ID: a.ID, Title: a.Title, Cause: a.Cause, Explanation: strings.TrimSpace(a.Explanation),
			Status: a.Status, FixedIn: a.FixedIn, Links: a.Links, Cells: cells, Variants: vars,
		})
	}
	return problems
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s Selector) String() string {
	var parts []string
	add := func(k string, v StringList) {
		if len(v) > 0 {
			parts = append(parts, k+"="+strings.Join(v, "|"))
		}
	}
	add("tool", s.Tool)
	add("language", s.Language)
	add("variant", s.Variant)
	add("flavour", s.Flavour)
	add("header", s.Header)
	add("boundary", s.Boundary)
	add("outcome", s.Outcome)
	return "{" + strings.Join(parts, " ") + "}"
}

func (s Selector) matchesVariant(v *results.Variant) bool {
	if !inList(s.Tool, v.Tool) || !inList(s.Language, v.Language) || !inList(s.Flavour, v.Flavour) {
		return false
	}
	if len(s.Variant) > 0 {
		ok := false
		for _, pat := range s.Variant {
			if glob(pat, v.ID) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func (s Selector) matchesCell(c *results.Cell) bool {
	if !inList(s.Header, c.Header) {
		return false
	}
	if len(s.Boundary) > 0 {
		ok := false
		for _, pat := range s.Boundary {
			if glob(pat, c.Boundary) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(s.Outcome) == 0 {
		return c.Outcome != results.OutcomePass && c.Outcome != results.OutcomeNotRun
	}
	return inList(s.Outcome, string(c.Outcome))
}

func inList(l StringList, v string) bool {
	if len(l) == 0 {
		return true
	}
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

// glob supports a single "*" anywhere, plus "*" on both sides of "->" for
// boundary patterns like "*->kafka" and "kafka->*".
func glob(pat, s string) bool {
	if pat == "*" || pat == s {
		return true
	}
	if pin, pout, ok := strings.Cut(pat, "->"); ok {
		sin, sout, ok2 := strings.Cut(s, "->")
		if !ok2 {
			return false
		}
		return (pin == "*" || pin == sin) && (pout == "*" || pout == sout)
	}
	if i := strings.IndexByte(pat, '*'); i >= 0 {
		return strings.HasPrefix(s, pat[:i]) && strings.HasSuffix(s, pat[i+1:])
	}
	return false
}
