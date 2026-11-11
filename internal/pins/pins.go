// Package pins reads and moves the tracer/agent version pins listed in
// pins.yaml. The pins themselves live in the manifests next to each relay
// (go.mod, package.json, requirements-*.txt, Gemfile, Cargo.toml, Dockerfile
// ARGs); pins.yaml only says where they are, and this package writes them
// the way each ecosystem expects: `go get` + `go mod tidy`, `cargo update`
// behind an exact requirement (with the crates the pin `follow`s given the
// requirement the pinned release declares), a JSON key, every `pkg==` line,
// a `gem` line, an `ARG`.
package pins

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/signadot/context-propagation-matrix/internal/registry"
	"github.com/signadot/context-propagation-matrix/internal/versions"
)

// Kinds, in the order pins.yaml documents them.
const (
	KindArg     = "arg"     // Dockerfile ARG / RUN-line regex
	KindGoMod   = "gomod"   // go.mod module family
	KindNPM     = "npm"     // package.json dependencies
	KindPip     = "pip"     // requirements file `pkg==` lines
	KindBundler = "bundler" // Gemfile `gem 'x', 'v'` lines
	KindCargo   = "cargo"   // Cargo.toml requirement + Cargo.lock
)

var kinds = map[string]bool{KindArg: true, KindGoMod: true, KindNPM: true, KindPip: true, KindBundler: true, KindCargo: true}

var versionLike = regexp.MustCompile(`^v?[0-9]+(\.[0-9]+)*[0-9A-Za-z.+-]*$`)

// Pin is one entry of pins.yaml.
type Pin struct {
	ID   string `yaml:"id" json:"id"`
	Kind string `yaml:"kind" json:"kind"`
	// kind: arg
	File    string `yaml:"file,omitempty" json:"file,omitempty"`
	Arg     string `yaml:"arg,omitempty" json:"arg,omitempty"`
	Pattern string `yaml:"pattern,omitempty" json:"pattern,omitempty"`
	// kind: gomod, cargo
	Dir     string   `yaml:"dir,omitempty" json:"dir,omitempty"`
	Modules []string `yaml:"modules,omitempty" json:"modules,omitempty"`
	// kind: gomod. Companions are modules on the same version line that are
	// `go get` with the family but are not required to appear in go.mod.
	// They exist for one failure mode: a dependency's tests import a package
	// from a module the dependency's go.mod does not require (kotel 1.5.0
	// imports go.opentelemetry.io/otel/sdk/metric this way), so `go mod tidy`
	// resolves that package to the module's *latest* version, which drags the
	// pinned family back up with it. Getting the companion at the pinned
	// version first gives tidy a module to find the package in.
	Companions []string `yaml:"companions,omitempty" json:"companions,omitempty"`
	// kind: npm, pip, bundler (File) and cargo (Dir)
	Packages []string `yaml:"packages,omitempty" json:"packages,omitempty"`
	// kind: cargo. Follow maps a dependency of the pinned crate to crates in
	// Cargo.toml that take its requirement: writing datadog-opentelemetry
	// 0.6.0, which declares `opentelemetry = "=0.33.0"`, with
	// `follow: {opentelemetry: [opentelemetry, opentelemetry-http]}` sets
	// those two requirements to "=0.33.0" too, so the lock resolves one
	// copy of the API crate and the tracer's propagator is the one the
	// relay calls.
	Follow map[string][]string `yaml:"follow,omitempty" json:"follow,omitempty"`

	// Source is the registry coordinate, equal to the variant's `tracer` in
	// variants.yaml — that equality is how a variant finds its pin.
	Source   registry.Package `yaml:"source" json:"source"`
	Variants []string         `yaml:"variants" json:"variants"`

	// Backtest narrows which older versions `cpm discover` proposes for the
	// pin's variants. Absent means every version the registry lists.
	Backtest *BacktestPolicy `yaml:"backtest,omitempty" json:"backtest,omitempty"`
}

// BacktestPolicy excludes versions that cannot be measured: Min is the
// oldest version worth building (older ones fail to build or predate the
// relay's API), Skip lists individual versions to leave out (a release that
// never shipped for every crate of a family, a yanked one). Each entry
// should carry a comment in pins.yaml saying why.
type BacktestPolicy struct {
	Min  string   `yaml:"min,omitempty" json:"min,omitempty"`
	Skip []string `yaml:"skip,omitempty" json:"skip,omitempty"`
}

// Allows reports whether version may be back-tested under the policy; a
// nil policy allows everything.
func (b *BacktestPolicy) Allows(version string) bool {
	if b == nil {
		return true
	}
	if b.Min != "" && versions.Compare(version, b.Min) < 0 {
		return false
	}
	for _, s := range b.Skip {
		if versions.Strip(s) == versions.Strip(version) {
			return false
		}
	}
	return true
}

// Manifest is pins.yaml.
type Manifest struct {
	Pins []Pin `yaml:"pins"`
}

// Load reads and validates pins.yaml.
func Load(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	seen := map[string]bool{}
	for i := range m.Pins {
		p := &m.Pins[i]
		if p.ID == "" {
			return nil, fmt.Errorf("%s: pin %d has no id", path, i)
		}
		if seen[p.ID] {
			return nil, fmt.Errorf("%s: duplicate pin id %q", path, p.ID)
		}
		seen[p.ID] = true
		if !kinds[p.Kind] {
			return nil, fmt.Errorf("%s: %s: unknown kind %q", path, p.ID, p.Kind)
		}
		if p.Source.Registry == "" || p.Source.Package == "" {
			return nil, fmt.Errorf("%s: %s: needs source {registry, package}", path, p.ID)
		}
		if b := p.Backtest; b != nil {
			if b.Min == "" && len(b.Skip) == 0 {
				return nil, fmt.Errorf("%s: %s: backtest policy needs min or skip", path, p.ID)
			}
			for _, s := range append([]string{b.Min}, b.Skip...) {
				if s != "" && !versionLike.MatchString(s) {
					return nil, fmt.Errorf("%s: %s: backtest: %q is not a version", path, p.ID, s)
				}
			}
		}
		switch p.Kind {
		case KindArg:
			if p.File == "" || (p.Arg == "" && p.Pattern == "") {
				return nil, fmt.Errorf("%s: %s: kind arg needs file and arg or pattern", path, p.ID)
			}
			if p.Pattern != "" {
				re, err := regexp.Compile(p.Pattern)
				if err != nil {
					return nil, fmt.Errorf("%s: %s: pattern: %w", path, p.ID, err)
				}
				if re.SubexpIndex("version") < 0 {
					return nil, fmt.Errorf("%s: %s: pattern needs a (?<version>...) group", path, p.ID)
				}
			}
		case KindGoMod:
			if p.Dir == "" || len(p.Modules) == 0 {
				return nil, fmt.Errorf("%s: %s: kind gomod needs dir and modules", path, p.ID)
			}
			for _, c := range p.Companions {
				if contains(p.Modules, c) {
					return nil, fmt.Errorf("%s: %s: companions: %s is already a pinned module", path, p.ID, c)
				}
			}
		case KindCargo:
			if p.Dir == "" || len(p.Packages) == 0 {
				return nil, fmt.Errorf("%s: %s: kind cargo needs dir and packages", path, p.ID)
			}
			for dep, crates := range p.Follow {
				if dep == "" || len(crates) == 0 {
					return nil, fmt.Errorf("%s: %s: follow needs a dependency and the crates that take its requirement", path, p.ID)
				}
				for _, c := range crates {
					if contains(p.Packages, c) {
						return nil, fmt.Errorf("%s: %s: follow: %s is already a pinned package", path, p.ID, c)
					}
				}
			}
		default:
			if p.File == "" || len(p.Packages) == 0 {
				return nil, fmt.Errorf("%s: %s: kind %s needs file and packages", path, p.ID, p.Kind)
			}
		}
		if len(p.Follow) > 0 && p.Kind != KindCargo {
			return nil, fmt.Errorf("%s: %s: follow is only for kind cargo", path, p.ID)
		}
		if len(p.Companions) > 0 && p.Kind != KindGoMod {
			return nil, fmt.Errorf("%s: %s: companions is only for kind gomod", path, p.ID)
		}
	}
	return &m, nil
}

// ForVariant returns the pin behind an instrumented variant: the one entry
// that lists the variant and whose source equals the variant's tracer
// coordinate.
func (m *Manifest) ForVariant(variant string, tracer registry.Package) (*Pin, error) {
	var hits []*Pin
	for i := range m.Pins {
		p := &m.Pins[i]
		if p.Source.Registry == tracer.Registry && p.Source.Package == tracer.Package && contains(p.Variants, variant) {
			hits = append(hits, p)
		}
	}
	if len(hits) != 1 {
		return nil, fmt.Errorf("%s: %d pins have source %s/%s and list the variant (need exactly 1)", variant, len(hits), tracer.Registry, tracer.Package)
	}
	return hits[0], nil
}

// Read returns the version the pin's file currently holds.
func (p *Pin) Read() (string, error) {
	v, err := p.read()
	if err != nil {
		return "", fmt.Errorf("%s: %w", p.ID, err)
	}
	return v, nil
}

// Check enforces what a pin's manifest must satisfy on main beyond
// resolving: for cargo, one copy of every pinned and followed crate in
// Cargo.lock. Other kinds have nothing to add. `cpm pins check` runs it;
// `cpm pins set` (back-test checkouts) does not.
func (p *Pin) Check() error {
	if p.Kind != KindCargo {
		return nil
	}
	if err := p.checkCargo(); err != nil {
		return fmt.Errorf("%s: %w", p.ID, err)
	}
	return nil
}

// Files lists the files Write touches, without writing: the manifest for
// arg/npm/pip/bundler, go.mod + go.sum for gomod, Cargo.toml + Cargo.lock
// for cargo. bump-agents.yml names them in the PR body before -apply runs.
func (p *Pin) Files() []string {
	switch p.Kind {
	case KindGoMod:
		return []string{filepath.Join(p.Dir, "go.mod"), filepath.Join(p.Dir, "go.sum")}
	case KindCargo:
		return []string{filepath.Join(p.Dir, "Cargo.toml"), filepath.Join(p.Dir, "Cargo.lock")}
	}
	return []string{p.File}
}

// Requirements returns the version requirement a release of a package
// declares on each of its dependencies, by name (versions.Client has one
// for crates.io). Write needs it for a cargo pin with Follow and for
// nothing else; nil is fine otherwise.
type Requirements func(ctx context.Context, p registry.Package, version string) (map[string]string, error)

// Write sets the pin to version and returns the files it touched. Kinds
// gomod and cargo run `go` and `cargo` in the module directory.
func (p *Pin) Write(ctx context.Context, version string, reqs Requirements) ([]string, error) {
	version = versions.Strip(version)
	if version == "" {
		return nil, fmt.Errorf("%s: empty version", p.ID)
	}
	files, err := p.write(ctx, version, reqs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.ID, err)
	}
	return files, nil
}

func (p *Pin) read() (string, error) {
	switch p.Kind {
	case KindArg:
		return p.readArg()
	case KindGoMod:
		return p.readGoMod()
	case KindNPM:
		return p.readNPM()
	case KindPip:
		return p.readPip()
	case KindBundler:
		return p.readBundler()
	case KindCargo:
		return p.readCargo()
	}
	return "", fmt.Errorf("unknown kind %q", p.Kind)
}

func (p *Pin) write(ctx context.Context, version string, reqs Requirements) ([]string, error) {
	switch p.Kind {
	case KindArg:
		return p.writeArg(version)
	case KindGoMod:
		return p.writeGoMod(version)
	case KindNPM:
		return p.writeNPM(version)
	case KindPip:
		return p.writePip(version)
	case KindBundler:
		return p.writeBundler(version)
	case KindCargo:
		return p.writeCargo(ctx, version, reqs)
	}
	return nil, fmt.Errorf("unknown kind %q", p.Kind)
}

// ---------------------------------------------------------------------------
// arg: `ARG NAME=<version>` or a regex with a (?<version>...) group; the
// first match is rewritten.

func (p *Pin) argRegexp() *regexp.Regexp {
	if p.Pattern != "" {
		return regexp.MustCompile(p.Pattern)
	}
	return regexp.MustCompile(`(?m)^ARG ` + regexp.QuoteMeta(p.Arg) + `=(?<version>\S+)`)
}

func (p *Pin) argMatch() (src string, start, end int, err error) {
	b, err := os.ReadFile(p.File)
	if err != nil {
		return "", 0, 0, err
	}
	src = string(b)
	re := p.argRegexp()
	loc := re.FindStringSubmatchIndex(src)
	idx := re.SubexpIndex("version")
	if loc == nil || idx < 0 || loc[2*idx] < 0 {
		return "", 0, 0, fmt.Errorf("pattern did not match in %s", p.File)
	}
	return src, loc[2*idx], loc[2*idx+1], nil
}

func (p *Pin) readArg() (string, error) {
	src, s, e, err := p.argMatch()
	if err != nil {
		return "", err
	}
	return src[s:e], nil
}

func (p *Pin) writeArg(version string) ([]string, error) {
	src, s, e, err := p.argMatch()
	if err != nil {
		return nil, err
	}
	return []string{p.File}, writeFile(p.File, src[:s]+version+src[e:])
}

// ---------------------------------------------------------------------------
// gomod: the family's modules are `go get` together, then `go mod tidy`.

func (p *Pin) readGoMod() (string, error) {
	path := filepath.Join(p.Dir, "go.mod")
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var vs []string
	for _, mod := range p.Modules {
		// Inside a require block or as a one-line `require mod vX`.
		re := regexp.MustCompile(`(?m)^\s*(?:require\s+)?` + regexp.QuoteMeta(mod) + ` (v\S+)`)
		m := re.FindStringSubmatch(string(b))
		if m == nil {
			return "", fmt.Errorf("%s not required in %s", mod, path)
		}
		vs = append(vs, versions.Strip(m[1]))
	}
	return vs[0], nil
}

func (p *Pin) writeGoMod(version string) ([]string, error) {
	args := []string{"get"}
	for _, mod := range p.Modules {
		args = append(args, mod+"@v"+version)
	}
	for _, mod := range p.Companions {
		args = append(args, mod+"@v"+version)
	}
	if err := run(p.Dir, "go", args...); err != nil {
		return nil, err
	}
	if err := run(p.Dir, "go", "mod", "tidy"); err != nil {
		return nil, err
	}
	// `go get` honours the version; `go mod tidy` need not. When a package
	// the graph needs lives in a module nothing requires yet, tidy adds that
	// module at its *latest* version, and if it requires a newer copy of the
	// pinned module MVS moves the pin right back up (see Companions). A pin
	// that did not take must fail here, before anything is built and filed
	// under a version that was never run.
	got, err := p.readGoMod()
	if err != nil {
		return nil, err
	}
	if got != version {
		return nil, fmt.Errorf("pinned %s to %s but after `go mod tidy` %s has %s — a module tidy added requires the newer version (`go mod why -m` names it; list it under companions if it shares the version line)", p.Modules[0], version, filepath.Join(p.Dir, "go.mod"), got)
	}
	return p.Files(), nil
}

// ---------------------------------------------------------------------------
// npm: package.json dependencies, rewritten with two-space indentation.

func (p *Pin) readNPM() (string, error) {
	var pkg struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	b, err := os.ReadFile(p.File)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		return "", fmt.Errorf("%s: %w", p.File, err)
	}
	for _, name := range p.Packages {
		if pkg.Dependencies[name] == "" {
			return "", fmt.Errorf("%s not in %s dependencies", name, p.File)
		}
	}
	return pkg.Dependencies[p.Packages[0]], nil
}

func (p *Pin) writeNPM(version string) ([]string, error) {
	// Rewrite only the dependency values so key order and the rest of the
	// document survive: the line `"name": "old"` becomes `"name": "new"`.
	b, err := os.ReadFile(p.File)
	if err != nil {
		return nil, err
	}
	src := string(b)
	for _, name := range p.Packages {
		re := regexp.MustCompile(`(?m)^(\s*` + regexp.QuoteMeta(jsonQuote(name)) + `\s*:\s*")([^"]*)(")`)
		if !re.MatchString(src) {
			return nil, fmt.Errorf("%s not in %s dependencies", name, p.File)
		}
		src = re.ReplaceAllString(src, "${1}"+version+"${3}")
	}
	return []string{p.File}, writeFile(p.File, src)
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// ---------------------------------------------------------------------------
// pip: every `pkg==version` line whose name matches one of the package globs.

var pipLineRe = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)==(\S+)`)

func (p *Pin) pipLines() (lines []string, hits []int, err error) {
	b, err := os.ReadFile(p.File)
	if err != nil {
		return nil, nil, err
	}
	globs := make([]*regexp.Regexp, len(p.Packages))
	for i, g := range p.Packages {
		globs[i] = globRegexp(g)
	}
	lines = strings.Split(string(b), "\n")
	for i, line := range lines {
		m := pipLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		for _, g := range globs {
			if g.MatchString(m[1]) {
				hits = append(hits, i)
				break
			}
		}
	}
	if len(hits) == 0 {
		return nil, nil, fmt.Errorf("no %s== line in %s", strings.Join(p.Packages, "|"), p.File)
	}
	return lines, hits, nil
}

func (p *Pin) readPip() (string, error) {
	lines, hits, err := p.pipLines()
	if err != nil {
		return "", err
	}
	return pipLineRe.FindStringSubmatch(lines[hits[0]])[2], nil
}

func (p *Pin) writePip(version string) ([]string, error) {
	lines, hits, err := p.pipLines()
	if err != nil {
		return nil, err
	}
	for _, i := range hits {
		m := pipLineRe.FindStringSubmatchIndex(lines[i])
		lines[i] = lines[i][:m[4]] + version + lines[i][m[5]:]
	}
	return []string{p.File}, writeFile(p.File, strings.Join(lines, "\n"))
}

// ---------------------------------------------------------------------------
// bundler: `gem 'name', 'version'` (single or double quotes).

func gemRegexp(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^(gem ['"]` + regexp.QuoteMeta(name) + `['"],\s*['"])([^'"]+)(['"])`)
}

func (p *Pin) readBundler() (string, error) {
	b, err := os.ReadFile(p.File)
	if err != nil {
		return "", err
	}
	var vs []string
	for _, gem := range p.Packages {
		m := gemRegexp(gem).FindStringSubmatch(string(b))
		if m == nil {
			return "", fmt.Errorf("gem '%s' not pinned in %s", gem, p.File)
		}
		vs = append(vs, m[2])
	}
	return vs[0], nil
}

func (p *Pin) writeBundler(version string) ([]string, error) {
	b, err := os.ReadFile(p.File)
	if err != nil {
		return nil, err
	}
	src := string(b)
	for _, gem := range p.Packages {
		re := gemRegexp(gem)
		if !re.MatchString(src) {
			return nil, fmt.Errorf("gem '%s' not pinned in %s", gem, p.File)
		}
		src = re.ReplaceAllString(src, "${1}"+version+"${3}")
	}
	return []string{p.File}, writeFile(p.File, src)
}

// ---------------------------------------------------------------------------
// cargo: Cargo.toml holds the requirement ("0.32"); Cargo.lock the exact
// version, which is what the relay reports and what we pin to. Writing sets
// an exact requirement and moves the whole family in the lock in one
// resolver pass (crates of a family require each other at the same version,
// so one at a time cannot resolve). The Dockerfile's `cargo build --locked`
// then applies unchanged.
//
// A crate can be in Cargo.lock twice: another dependency requires a version
// the relay's own requirement does not admit, and cargo links both. For the
// OpenTelemetry API crate that is fatal and silent: a tracer that installs
// its propagator in one copy's `global` and a relay that calls the other's
// builds, boots and delivers — with nothing propagated. So a pin whose
// crate requires the API crate exactly (datadog-opentelemetry) `follow`s
// it: Write gives the relay's own requirement on the followed crates the
// requirement the pinned release declares (crates.io reports it), and
// Check requires one copy of every pinned and followed crate in the lock.
// Reading resolves through the requirement when two copies are present,
// and `cargo update -p` is given `crate@version` (a bare name is ambiguous).

// cargoCrates lists the crates a pin writes: its packages, then the
// followed ones, in pins.yaml order.
func (p *Pin) cargoCrates() []string {
	crates := append([]string(nil), p.Packages...)
	for _, dep := range sortedKeys(p.Follow) {
		crates = append(crates, p.Follow[dep]...)
	}
	return crates
}

func cargoReqRegexp(crate string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^(` + regexp.QuoteMeta(crate) + `\s*=\s*(?:\{[^}]*?version\s*=\s*)?")([^"]+)(")`)
}

// cargoLockVersions lists every version of crate in Cargo.lock, in file
// order (cargo sorts entries by name, then version ascending).
func (p *Pin) cargoLockVersions(crate string) ([]string, error) {
	path := filepath.Join(p.Dir, "Cargo.lock")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	re := regexp.MustCompile(`\[\[package\]\]\nname = "` + regexp.QuoteMeta(crate) + `"\nversion = "([^"]+)"`)
	var vs []string
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		vs = append(vs, m[1])
	}
	if len(vs) == 0 {
		return nil, fmt.Errorf("%s not in %s", crate, path)
	}
	return vs, nil
}

// cargoLocked returns the version of crate the relay builds against: the
// one entry in Cargo.lock, or, when another dependency pulls in a second
// copy, the one that satisfies req, Cargo.toml's requirement for the crate
// — which then has to be exact (=X.Y.Z), as Write always leaves it.
func (p *Pin) cargoLocked(crate, req string) (string, error) {
	vs, err := p.cargoLockVersions(crate)
	if err != nil {
		return "", err
	}
	if len(vs) == 1 {
		return vs[0], nil
	}
	if want, exact := strings.CutPrefix(strings.TrimSpace(req), "="); exact {
		want = strings.TrimSpace(want)
		for _, v := range vs {
			if v == want {
				return v, nil
			}
		}
	}
	return "", fmt.Errorf("%s: Cargo.lock holds %s and Cargo.toml requires %q; pin an exact version (=X.Y.Z) to say which one the relay builds against",
		crate, strings.Join(vs, " and "), req)
}

// cargoReqs returns Cargo.toml's source and the requirement of every crate
// the pin writes (pinned and followed).
func (p *Pin) cargoReqs() (string, map[string]string, error) {
	path := filepath.Join(p.Dir, "Cargo.toml")
	b, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	src := string(b)
	reqs := map[string]string{}
	for _, crate := range p.cargoCrates() {
		m := cargoReqRegexp(crate).FindStringSubmatch(src)
		if m == nil {
			return "", nil, fmt.Errorf("%s not in %s", crate, path)
		}
		reqs[crate] = m[2]
	}
	return src, reqs, nil
}

func (p *Pin) readCargo() (string, error) {
	_, reqs, err := p.cargoReqs()
	if err != nil {
		return "", err
	}
	return p.cargoLocked(p.Packages[0], reqs[p.Packages[0]])
}

// checkCargo requires one copy of every pinned and followed crate in
// Cargo.lock: a second copy, pulled in by another dependency's requirement,
// means the tracer and the relay no longer share the API crate (see the
// section comment). Run by `cpm pins check`.
func (p *Pin) checkCargo() error {
	for _, crate := range p.cargoCrates() {
		vs, err := p.cargoLockVersions(crate)
		if err != nil {
			return err
		}
		if len(vs) > 1 {
			return fmt.Errorf("%s: Cargo.lock holds %s; the relay needs one copy — pin the version the other dependencies require (see %s)",
				crate, strings.Join(vs, " and "), filepath.Join(p.Dir, "Cargo.toml"))
		}
	}
	return nil
}

// writeCargo sets every pinned crate to `=version`, every followed crate
// to the requirement the pinned release declares on its dependency, and
// moves them all in Cargo.lock in one `cargo update`.
func (p *Pin) writeCargo(ctx context.Context, version string, reqs Requirements) ([]string, error) {
	path := filepath.Join(p.Dir, "Cargo.toml")
	src, current, err := p.cargoReqs()
	if err != nil {
		return nil, err
	}
	want := map[string]string{}
	for _, crate := range p.Packages {
		want[crate] = "=" + version
	}
	if len(p.Follow) > 0 {
		if reqs == nil {
			return nil, fmt.Errorf("follow needs a registry lookup for %s %s's requirements", p.Source.Package, version)
		}
		declared, err := reqs(ctx, p.Source, version)
		if err != nil {
			return nil, err
		}
		for _, dep := range sortedKeys(p.Follow) {
			req, ok := declared[dep]
			if !ok {
				return nil, fmt.Errorf("%s %s does not depend on %s (follow)", p.Source.Package, version, dep)
			}
			for _, crate := range p.Follow[dep] {
				want[crate] = req
			}
		}
	}
	args := []string{"update"}
	for _, crate := range p.cargoCrates() {
		// Name the lock entry being moved: once a second copy of the crate
		// is in the lock, a bare `-p crate` is ambiguous.
		locked, err := p.cargoLocked(crate, current[crate])
		if err != nil {
			return nil, err
		}
		src = cargoReqRegexp(crate).ReplaceAllString(src, "${1}"+want[crate]+"${3}")
		args = append(args, "-p", crate+"@"+locked)
	}
	if err := writeFile(path, src); err != nil {
		return nil, err
	}
	if err := run(p.Dir, "cargo", args...); err != nil {
		return nil, err
	}
	return p.Files(), nil
}

// ---------------------------------------------------------------------------

func run(dir, name string, args ...string) error {
	fmt.Fprintf(os.Stderr, "pins: (%s) %s %s\n", dir, name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func writeFile(path, s string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(s), info.Mode().Perm())
}

// globRegexp turns "opentelemetry-*" into an anchored regexp.
func globRegexp(g string) *regexp.Regexp {
	parts := strings.Split(g, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return regexp.MustCompile("^" + strings.Join(parts, ".*") + "$")
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string][]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
