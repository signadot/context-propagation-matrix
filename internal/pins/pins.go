// Package pins reads and moves the tracer/agent version pins listed in
// pins.yaml. The pins themselves live in the manifests next to each relay
// (go.mod, package.json, requirements-*.txt, Gemfile, Cargo.toml, Dockerfile
// ARGs); pins.yaml only says where they are, and this package writes them
// the way each ecosystem expects: `go get` + `go mod tidy`, `cargo update`
// behind an exact requirement, a JSON key, every `pkg==` line, a `gem` line,
// an `ARG`.
package pins

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	// kind: npm, pip, bundler (File) and cargo (Dir)
	Packages []string `yaml:"packages,omitempty" json:"packages,omitempty"`

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
		case KindCargo:
			if p.Dir == "" || len(p.Packages) == 0 {
				return nil, fmt.Errorf("%s: %s: kind cargo needs dir and packages", path, p.ID)
			}
		default:
			if p.File == "" || len(p.Packages) == 0 {
				return nil, fmt.Errorf("%s: %s: kind %s needs file and packages", path, p.ID, p.Kind)
			}
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

// Write sets the pin to version and returns the files it touched. Kinds
// gomod and cargo run `go` and `cargo` in the module directory.
func (p *Pin) Write(version string) ([]string, error) {
	version = versions.Strip(version)
	if version == "" {
		return nil, fmt.Errorf("%s: empty version", p.ID)
	}
	files, err := p.write(version)
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

func (p *Pin) write(version string) ([]string, error) {
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
		return p.writeCargo(version)
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
	if err := run(p.Dir, "go", args...); err != nil {
		return nil, err
	}
	if err := run(p.Dir, "go", "mod", "tidy"); err != nil {
		return nil, err
	}
	return []string{filepath.Join(p.Dir, "go.mod"), filepath.Join(p.Dir, "go.sum")}, nil
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

func cargoReqRegexp(crate string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^(` + regexp.QuoteMeta(crate) + `\s*=\s*(?:\{[^}]*?version\s*=\s*)?")([^"]+)(")`)
}

func (p *Pin) cargoLocked(crate string) (string, error) {
	path := filepath.Join(p.Dir, "Cargo.lock")
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	re := regexp.MustCompile(`\[\[package\]\]\nname = "` + regexp.QuoteMeta(crate) + `"\nversion = "([^"]+)"`)
	m := re.FindStringSubmatch(string(b))
	if m == nil {
		return "", fmt.Errorf("%s not in %s", crate, path)
	}
	return m[1], nil
}

func (p *Pin) readCargo() (string, error) {
	path := filepath.Join(p.Dir, "Cargo.toml")
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, crate := range p.Packages {
		if !cargoReqRegexp(crate).Match(b) {
			return "", fmt.Errorf("%s not in %s", crate, path)
		}
	}
	return p.cargoLocked(p.Packages[0])
}

func (p *Pin) writeCargo(version string) ([]string, error) {
	path := filepath.Join(p.Dir, "Cargo.toml")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	src := string(b)
	args := []string{"update"}
	for _, crate := range p.Packages {
		re := cargoReqRegexp(crate)
		if !re.MatchString(src) {
			return nil, fmt.Errorf("%s not in %s", crate, path)
		}
		src = re.ReplaceAllString(src, "${1}="+version+"${3}")
		args = append(args, "-p", crate)
	}
	if err := writeFile(path, src); err != nil {
		return nil, err
	}
	if err := run(p.Dir, "cargo", args...); err != nil {
		return nil, err
	}
	return []string{path, filepath.Join(p.Dir, "Cargo.lock")}, nil
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
