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
	ID          string         `yaml:"id"`
	Title       string         `yaml:"title"`
	Match       []Selector     `yaml:"match"`
	Cause       string         `yaml:"cause"`
	Explanation string         `yaml:"explanation"`
	Status      string         `yaml:"status"` // open | by-design | fixed | wontfix | config
	FixedIn     string         `yaml:"fixed_in"`
	Links       []results.Link `yaml:"links"`
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

// Problem is a validation finding. Fatal problems stop aggregation; warnings
// are reported and, under `cpm validate -strict`, fail a pull request; Info
// problems are reported only — they describe this release, not the YAML.
// An Advisory warning is reported as a warning and never fails, strict or
// not: it asks for an edit that a published run made necessary, so it must
// not stop unrelated pull requests (the tracer bumps merge themselves).
type Problem struct {
	Annotation string
	File       string
	Fatal      bool
	Message    string
	Info       bool
	Advisory   bool
}

func (p Problem) String() string {
	lvl := "warn"
	switch {
	case p.Fatal:
		lvl = "error"
	case p.Info:
		lvl = "note"
	}
	return fmt.Sprintf("%s: %s (%s): %s", lvl, p.Annotation, p.File, p.Message)
}

// Apply evaluates selectors against res, appends matched annotations to
// res.Annotations, and returns validation problems.
//
// Each selector is evaluated in two passes so that an upstream fix can never
// break a run:
//
//   - structural: tool/language/variant/flavour/header/boundary, ignoring
//     outcome. Zero structural matches is fatal — the selector names a cell
//     that does not exist (stale after a variant/protocol removal, or a typo)
//     — unless every variant it names is registered but Missing from this
//     release (a secret-gated tool without its key, or a partial run with no
//     earlier release to carry cells from). That is reported as an Info
//     problem and the annotation is attached to nothing: there is nothing to
//     check yet, and nothing in the YAML to fix.
//   - outcome: the structural cells further filtered by the selector's
//     outcome list (default: every non-pass, non-not_run outcome). These are
//     the cells the annotation is attached to.
//
// When an annotation has structural matches but no outcome matches anywhere,
// every cell it describes now passes. It is then marked Resolved, attached to
// those passing cells (so the page can still say what used to fail), FixedIn
// is filled from the variants' reported tracer version if the YAML does not
// state one, and a warning asks the maintainer to record `status: fixed` /
// `fixed_in`. Warnings never fail aggregation; `cpm validate --strict` turns
// them into errors on pull requests so the YAML is kept honest.
//
// When only some of the cells an annotation covers now pass, it is partially
// resolved: the explanation is still right for the failing cells and wrong for
// the passing ones. An Advisory warning is raised per variant, naming the
// cells that now pass, so a maintainer can narrow the selector. The rule is
// deliberately narrow so that it fires on drift only:
//
//   - only cells covered by a selector without an explicit `outcome:` list
//     count. Such a selector claims every non-pass outcome, so a passing cell
//     inside its shape is a claim that no longer holds. A selector with an
//     explicit list claims only the cells with those outcomes; a passing cell
//     it does not list was never claimed, so it says nothing about drift
//     (a selector whose listed outcome has vanished has its own warning).
//   - the annotation must still describe at least one failing cell somewhere;
//     with none left it is resolved outright and gets the warning above.
//   - an annotation with `status: fixed` is quiet: passing cells are what
//     fixed means, and failing ones already warn as a regression.
//   - one variant may be fixed while another is not; that is exactly the case
//     to flag, since the text still claims the fixed variant is broken.
//   - not_run and undelivered cells are not passes and never count.
func Apply(res *results.Results, anns []*Annotation) []Problem {
	var problems []Problem
	res.Annotations = res.Annotations[:0]
	for _, a := range anns {
		active := map[string]bool{}     // cells matched including outcome
		structural := map[string]bool{} // cells matched ignoring outcome
		activeVars := map[string]bool{}
		structVars := map[string]bool{}
		// Cells that a selector without an explicit outcome list covers and
		// that pass, per variant: what the explanation claims is broken but
		// is not. See the partial-resolution comment on Apply.
		passingClaimed := map[string]map[string]bool{}
		var passingOrder []string // variants in results order
		anyNonPassStructural := false
		for _, sel := range a.Match {
			nStruct, nActive, nPresent := 0, 0, 0
			var notRun []string // registered variants the selector names that have no results in this release
			for _, v := range res.Variants {
				if !sel.matchesVariant(v) {
					continue
				}
				if v.Missing {
					notRun = append(notRun, v.ID)
					continue
				}
				nPresent++
				for i := range v.Cells {
					c := &v.Cells[i]
					if !sel.matchesShape(c) {
						continue
					}
					nStruct++
					if c.Outcome == results.OutcomePass || c.Outcome == results.OutcomeNotRun {
						// Only remember passing cells for the resolved case.
						structural[c.CellID] = c.Outcome == results.OutcomePass
					} else {
						structural[c.CellID] = false
						anyNonPassStructural = true
					}
					structVars[v.ID] = true
					if sel.matchesOutcome(c) {
						nActive++
						active[c.CellID] = true
						activeVars[v.ID] = true
					} else if c.Outcome == results.OutcomePass && len(sel.Outcome) == 0 {
						if passingClaimed[v.ID] == nil {
							passingClaimed[v.ID] = map[string]bool{}
							passingOrder = append(passingOrder, v.ID)
						}
						passingClaimed[v.ID][c.CellID] = true
					}
				}
			}
			switch {
			case nStruct == 0 && nPresent == 0 && len(notRun) > 0:
				problems = append(problems, Problem{Annotation: a.ID, File: a.sourceFile, Info: true,
					Message: fmt.Sprintf("selector %s matches only variants with no results in this release (%s); nothing to check", sel, strings.Join(notRun, ", "))})
			case nStruct == 0:
				problems = append(problems, Problem{Annotation: a.ID, File: a.sourceFile, Fatal: true,
					Message: fmt.Sprintf("selector %s matches no cell (variant, header or boundary no longer exists)", sel)})
			case nActive == 0 && len(sel.Outcome) > 0:
				problems = append(problems, Problem{Annotation: a.ID, File: a.sourceFile,
					Message: fmt.Sprintf("selector %s matches cells but none currently has outcome %s", sel, strings.Join(sel.Outcome, "|"))})
			}
		}

		ann := results.Annotation{
			ID: a.ID, Title: a.Title, Cause: a.Cause, Explanation: strings.TrimSpace(a.Explanation),
			Status: a.Status, FixedIn: a.FixedIn, Links: a.Links,
		}
		if ann.Links == nil {
			ann.Links = []results.Link{}
		}

		switch {
		case len(active) > 0:
			// Still describes at least one failing cell.
			ann.Cells = keys(active)
			ann.Variants = keys(activeVars)
			if a.Status == "fixed" {
				problems = append(problems, Problem{Annotation: a.ID, File: a.sourceFile, Message: fmt.Sprintf("status is fixed but %d matched cell(s) do not pass — regression?", len(active))})
			} else {
				problems = append(problems, partiallyResolved(a, passingClaimed, passingOrder, active)...)
			}
		case len(structural) > 0 && !anyNonPassStructural:
			// Everything it describes passes: keep it, attached to the green cells.
			passing := map[string]bool{}
			for id, pass := range structural {
				if pass {
					passing[id] = true
				}
			}
			ann.Resolved = true
			ann.Cells = keys(passing)
			ann.Variants = keys(structVars)
			if ann.FixedIn == "" {
				ann.FixedIn = tracerVersions(res, ann.Variants)
			}
			if a.Status != "fixed" {
				problems = append(problems, Problem{Annotation: a.ID, File: a.sourceFile, Message: fmt.Sprintf("every matched cell now passes; set status: fixed and fixed_in: %q", ann.FixedIn)})
			}
		default:
			// Structural matches exist but only with outcomes the selector
			// excludes (explicit outcome lists). Nothing to attach.
			ann.Cells = []string{}
			ann.Variants = keys(structVars)
		}
		res.Annotations = append(res.Annotations, ann)
	}
	return problems
}

// partiallyResolved reports, per variant, the cells an annotation still covers
// that now pass while it still describes failing cells elsewhere. passing is
// keyed by variant, in the order given by order; active is every cell the
// annotation still describes as a problem.
func partiallyResolved(a *Annotation, passing map[string]map[string]bool, order []string, active map[string]bool) []Problem {
	stillFailing := map[string]int{}
	for id := range active {
		v, _, _ := strings.Cut(id, "/")
		stillFailing[v]++
	}
	var out []Problem
	for _, v := range order {
		var short []string
		for _, id := range keys(passing[v]) {
			if !active[id] {
				short = append(short, strings.TrimPrefix(id, v+"/"))
			}
		}
		if len(short) == 0 {
			continue
		}
		where := fmt.Sprintf("%d other cell(s) of it still fail", stillFailing[v])
		if stillFailing[v] == 0 {
			where = "it still describes failing cells in other variants"
		}
		out = append(out, Problem{Annotation: a.ID, File: a.sourceFile, Advisory: true,
			Message: fmt.Sprintf("partially resolved: %d cell(s) it covers now pass in %s (%s) while %s; narrow the selector or record the fix for that variant",
				len(short), v, strings.Join(short, ", "), where)})
	}
	return out
}

// tracerVersions returns the tracer (or weaver) version reported by the given
// variants: a single version when they agree, otherwise "variant@version"
// pairs joined with ", ".
func tracerVersions(res *results.Results, variantIDs []string) string {
	byVariant := map[string]string{}
	distinct := map[string]bool{}
	for _, id := range variantIDs {
		for _, v := range res.Variants {
			if v.ID != id {
				continue
			}
			ver := ""
			if v.Tracer != nil && v.Tracer.Version != "" {
				ver = v.Tracer.Version
			} else if v.Weaver != nil && v.Weaver.Version != "" {
				ver = v.Weaver.Version
			}
			if ver != "" {
				byVariant[id] = ver
				distinct[ver] = true
			}
		}
	}
	if len(distinct) == 1 {
		return keys(distinct)[0]
	}
	var parts []string
	for _, id := range variantIDs {
		if ver, ok := byVariant[id]; ok {
			parts = append(parts, id+"@"+ver)
		}
	}
	return strings.Join(parts, ", ")
}

// MarkFixed rewrites the annotation with the given id in place, under dir, to
// `status: fixed` with `fixed_in: version`. It edits lines rather than
// re-marshalling so comments, quoting and ordering in the file survive; the
// result is re-parsed to make sure the file is still valid. It returns the
// file that was changed.
func MarkFixed(dir, id, version string) (string, error) {
	if strings.TrimSpace(version) == "" {
		return "", fmt.Errorf("a version is required")
	}
	anns, err := LoadDir(dir)
	if err != nil {
		return "", err
	}
	var target *Annotation
	for _, a := range anns {
		if a.ID == id {
			target = a
		}
	}
	if target == nil {
		return "", fmt.Errorf("no annotation with id %q under %s", id, dir)
	}
	b, err := os.ReadFile(target.sourceFile)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(b), "\n")

	// Locate the list item: the line whose stripped form is "- id: <id>", then
	// its extent up to the next list item at the same indent.
	start := -1
	for i, l := range lines {
		if k, v, ok := strings.Cut(strings.TrimLeft(l, " "), ":"); ok && strings.TrimSpace(k) == "- id" && unquote(strings.TrimSpace(v)) == id {
			start = i
			break
		}
	}
	if start < 0 {
		return "", fmt.Errorf("%s: cannot find `- id: %s` (is the id on its own line?)", target.sourceFile, id)
	}
	indent := len(lines[start]) - len(strings.TrimLeft(lines[start], " "))
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" || strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		if len(l)-len(strings.TrimLeft(l, " ")) <= indent && strings.HasPrefix(strings.TrimLeft(l, " "), "- ") {
			end = i
			break
		}
	}
	fieldIndent := strings.Repeat(" ", indent+2)
	statusLine, fixedInLine := -1, -1
	for i := start + 1; i < end; i++ {
		t := strings.TrimLeft(lines[i], " ")
		if len(lines[i])-len(t) != indent+2 {
			continue // nested (match:, links:) content
		}
		switch {
		case strings.HasPrefix(t, "status:"):
			statusLine = i
		case strings.HasPrefix(t, "fixed_in:"):
			fixedInLine = i
		}
	}
	newFixedIn := fieldIndent + "fixed_in: " + quote(version)
	switch {
	case statusLine >= 0:
		// Keep any trailing comment on the status line.
		rest := strings.TrimPrefix(strings.TrimLeft(lines[statusLine], " "), "status:")
		comment := ""
		if i := strings.Index(rest, "#"); i >= 0 {
			comment = "  " + strings.TrimSpace(rest[i:])
		}
		lines[statusLine] = fieldIndent + "status: fixed" + comment
		if fixedInLine >= 0 {
			lines[fixedInLine] = newFixedIn
		} else {
			lines = append(lines[:statusLine+1], append([]string{newFixedIn}, lines[statusLine+1:]...)...)
		}
	default:
		// No explicit status (defaulted to open): append both after `cause:`
		// or, failing that, at the end of the item.
		at := end
		for i := start + 1; i < end; i++ {
			t := strings.TrimLeft(lines[i], " ")
			if len(lines[i])-len(t) == indent+2 && strings.HasPrefix(t, "cause:") {
				at = i + 1
				break
			}
		}
		ins := []string{fieldIndent + "status: fixed", newFixedIn}
		if fixedInLine >= 0 {
			lines[fixedInLine] = newFixedIn
			ins = ins[:1]
		}
		lines = append(lines[:at], append(ins, lines[at:]...)...)
	}
	out := strings.Join(lines, "\n")
	var check []*Annotation
	if err := yaml.Unmarshal([]byte(out), &check); err != nil {
		return "", fmt.Errorf("%s: rewrite produced invalid YAML: %w", target.sourceFile, err)
	}
	for _, a := range check {
		if a.ID == id && (a.Status != "fixed" || a.FixedIn != version) {
			return "", fmt.Errorf("%s: rewrite did not take effect (status=%q fixed_in=%q)", target.sourceFile, a.Status, a.FixedIn)
		}
	}
	if err := os.WriteFile(target.sourceFile, []byte(out), 0o644); err != nil {
		return "", err
	}
	return target.sourceFile, nil
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

func quote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// Unexplained lists cells whose outcome is a failure the site should be able
// to explain but that no annotation is attached to. undelivered and not_run
// are excluded: they are harness topology limits, not propagation results.
func Unexplained(res *results.Results) []string {
	covered := map[string]bool{}
	for _, a := range res.Annotations {
		for _, c := range a.Cells {
			covered[c] = true
		}
	}
	var out []string
	for _, v := range res.Variants {
		if v.Missing {
			continue
		}
		for _, c := range v.Cells {
			switch c.Outcome {
			case results.OutcomePass, results.OutcomeNotRun, results.OutcomeUndelivered:
				continue
			}
			if !covered[c.CellID] {
				out = append(out, c.CellID)
			}
		}
	}
	sort.Strings(out)
	return out
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

// matchesShape is the structural half of a cell match: header and boundary,
// ignoring outcome.
func (s Selector) matchesShape(c *results.Cell) bool {
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
	return true
}

// matchesOutcome is the outcome half: the selector's explicit outcome list,
// or by default every outcome that is a real, delivered non-pass.
func (s Selector) matchesOutcome(c *results.Cell) bool {
	if len(s.Outcome) == 0 {
		return c.Outcome != results.OutcomePass && c.Outcome != results.OutcomeNotRun
	}
	return inList(s.Outcome, string(c.Outcome))
}

// matchesCell is the full match (shape and outcome).
func (s Selector) matchesCell(c *results.Cell) bool {
	return s.matchesShape(c) && s.matchesOutcome(c)
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
