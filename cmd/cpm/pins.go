package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/signadot/context-propagation-matrix/internal/pins"
	"github.com/signadot/context-propagation-matrix/internal/registry"
	"github.com/signadot/context-propagation-matrix/internal/versions"
)

const pinsFile = "pins.yaml"

// cmdPins reads and moves the tracer/agent pins listed in pins.yaml.
//
//	cpm pins [list]                      every pin with its current version (JSON)
//	cpm pins check                       pins resolve + every instrumented variant has exactly one
//	cpm pins set -variant V -version X   backtest.yml: move the pin behind V (scratch checkout)
//	cpm pins latest [-only id] [-apply]  bump-agents.yml: newer upstream versions, every kind; -apply rewrites
func cmdPins(args []string) error {
	sub := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list":
		return pinsList(args, false)
	case "check":
		return pinsList(args, true)
	case "set":
		return pinsSet(args)
	case "latest":
		return pinsLatest(args)
	}
	return fmt.Errorf("usage: cpm pins <list|check|set|latest> [flags]")
}

func pinsFlag(fs *flag.FlagSet) *string {
	return fs.String("pins", pinsFile, "path to pins.yaml")
}

type pinState struct {
	ID       string           `json:"id"`
	Kind     string           `json:"kind"`
	Current  string           `json:"current"`
	Source   registry.Package `json:"source"`
	Variants []string         `json:"variants"`
}

// pinsList prints every pin with the version its file holds. With check, it
// also requires each pin's manifest to pass pins.Check (one copy of every
// cargo crate in Cargo.lock) and every instrumented variant in variants.yaml
// to resolve to exactly one pin (and every listed variant to exist), exiting
// non-zero on any failure — the verify.yml gate.
func pinsList(args []string, check bool) error {
	fs := flag.NewFlagSet("pins", flag.ExitOnError)
	pinsPath := pinsFlag(fs)
	regPath := regFlag(fs)
	fs.Parse(args)

	m, err := pins.Load(*pinsPath)
	if err != nil {
		return err
	}
	failed := false
	var out []pinState
	for i := range m.Pins {
		p := &m.Pins[i]
		cur, err := p.Read()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			failed = true
			continue
		}
		if check {
			if err := p.Check(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				failed = true
			}
		}
		out = append(out, pinState{p.ID, p.Kind, cur, p.Source, p.Variants})
	}
	if check {
		cs, err := loadCoords(*regPath, "")
		if err != nil {
			return err
		}
		known := map[string]bool{}
		for _, c := range cs {
			known[c.Variant] = true
			p, err := m.ForVariant(c.Variant, *c.Tracer)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				failed = true
				continue
			}
			cur := "?"
			for _, o := range out {
				if o.ID == p.ID {
					cur = o.Current
				}
			}
			fmt.Fprintf(os.Stderr, "%s: %s (%s) pins %s\n", c.Variant, p.ID, p.Kind, cur)
		}
		for _, p := range m.Pins {
			for _, v := range p.Variants {
				if !known[v] {
					fmt.Fprintf(os.Stderr, "%s: variant %s is not an instrumented variant in variants.yaml\n", p.ID, v)
					failed = true
				}
			}
		}
		if failed {
			return fmt.Errorf("pins check failed")
		}
		fmt.Fprintf(os.Stderr, "pins: %d pins, %d instrumented variants, all resolved\n", len(m.Pins), len(cs))
	} else if failed {
		return fmt.Errorf("some pins did not resolve")
	}
	return writeJSON(out)
}

// pinsSet moves the pin behind a variant to a version, in place. Used by
// backtest.yml in a scratch checkout; nothing is committed.
func pinsSet(args []string) error {
	fs := flag.NewFlagSet("pins set", flag.ExitOnError)
	pinsPath := pinsFlag(fs)
	regPath := regFlag(fs)
	variant := fs.String("variant", "", "variant id (required)")
	version := fs.String("version", "", "tracer version to pin (required)")
	fs.Parse(args)
	if *variant == "" || *version == "" {
		return fmt.Errorf("cpm pins set needs -variant and -version")
	}
	m, err := pins.Load(*pinsPath)
	if err != nil {
		return err
	}
	cs, err := loadCoords(*regPath, "")
	if err != nil {
		return err
	}
	var c *coord
	for i := range cs {
		if cs[i].Variant == *variant {
			c = &cs[i]
		}
	}
	if c == nil {
		return fmt.Errorf("%s: not an instrumented variant in variants.yaml", *variant)
	}
	p, err := m.ForVariant(*variant, *c.Tracer)
	if err != nil {
		return err
	}
	from, err := p.Read()
	if err != nil {
		return err
	}
	files, err := p.Write(*version)
	if err != nil {
		return err
	}
	to := versions.Strip(*version)
	fmt.Fprintf(os.Stderr, "pins: %s: %s %s -> %s (%s)\n", *variant, p.ID, from, to, strings.Join(files, ", "))
	return writeJSON(struct {
		Variant string   `json:"variant"`
		ID      string   `json:"id"`
		Kind    string   `json:"kind"`
		From    string   `json:"from"`
		To      string   `json:"to"`
		Files   []string `json:"files"`
	}{*variant, p.ID, p.Kind, from, to, files})
}

// pinBump is one entry of `cpm pins latest`'s output, the JSON
// bump-agents.yml consumes. File is Files[0] (the shape older runs read).
type pinBump struct {
	ID       string           `json:"id"`
	Kind     string           `json:"kind"`
	File     string           `json:"file"`
	Files    []string         `json:"files"`
	From     string           `json:"from"`
	To       string           `json:"to"`
	Source   registry.Package `json:"source"`
	Variants []string         `json:"variants"`
	// URL is the package's page in its registry; ReleasesURL the GitHub
	// releases page (variants.yaml `github:`, or the package itself for
	// github-releases sources). Either may be empty.
	URL         string `json:"url,omitempty"`
	ReleasesURL string `json:"releases_url,omitempty"`
}

// latestFunc is the registry lookup findBumps uses; tests substitute one.
type latestFunc func(ctx context.Context, p registry.Package) (string, error)

// findBumps compares every pin (or the one named by only) with the newest
// stable version its registry publishes and returns those that are behind.
// Versions are compared with a leading "v" stripped on both sides, the way
// the registries report them. A failed lookup skips that pin (and is
// reported on stderr) so one registry outage does not block the others; a
// pin that does not resolve in its file is an error.
func findBumps(ctx context.Context, m *pins.Manifest, only string, latest latestFunc, github map[string]string) ([]pinBump, error) {
	bumps := []pinBump{}
	found := false
	var errs []string
	for i := range m.Pins {
		p := &m.Pins[i]
		if only != "" && p.ID != only {
			continue
		}
		found = true
		current, err := p.Read()
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		current = versions.Strip(current)
		newest, err := latest(ctx, p.Source)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: lookup failed: %v\n", p.ID, err)
			continue
		}
		newest = versions.Strip(newest)
		if versions.Compare(newest, current) <= 0 {
			continue
		}
		files := p.Files()
		bumps = append(bumps, pinBump{
			ID: p.ID, Kind: p.Kind, File: files[0], Files: files,
			From: current, To: newest, Source: p.Source, Variants: p.Variants,
			URL:         packageURL(p.Source),
			ReleasesURL: releasesURL(p.Source, github[sourceKey(p.Source)]),
		})
	}
	if only != "" && !found {
		return nil, fmt.Errorf("no pin %q in pins.yaml", only)
	}
	if len(errs) > 0 {
		return bumps, fmt.Errorf("some pins did not resolve:\n%s", strings.Join(errs, "\n"))
	}
	return bumps, nil
}

func sourceKey(p registry.Package) string { return p.Registry + ":" + p.Package }

// githubRepos maps every tracer/weaver coordinate in variants.yaml to its
// `github:` owner/repo, for the releases link in PR bodies. Best effort: an
// unreadable variants.yaml just means no links.
func githubRepos(regPath string) map[string]string {
	out := map[string]string{}
	reg, err := registry.Load(regPath)
	if err != nil {
		return out
	}
	for _, v := range reg.Variants {
		for _, p := range []*registry.Package{v.Tracer, v.Weaver} {
			if p != nil && p.GitHub != "" {
				out[sourceKey(*p)] = p.GitHub
			}
		}
	}
	return out
}

// packageURL is the package's page in its registry, "" if unknown.
func packageURL(p registry.Package) string {
	switch p.Registry {
	case "maven":
		g, a, ok := strings.Cut(p.Package, ":")
		if !ok {
			return ""
		}
		return "https://central.sonatype.com/artifact/" + g + "/" + a
	case "npm":
		return "https://www.npmjs.com/package/" + p.Package
	case "pypi":
		return "https://pypi.org/project/" + p.Package + "/"
	case "goproxy":
		return "https://pkg.go.dev/" + p.Package
	case "nuget":
		return "https://www.nuget.org/packages/" + p.Package
	case "rubygems":
		return "https://rubygems.org/gems/" + p.Package
	case "pecl":
		return "https://pecl.php.net/package/" + p.Package
	case "packagist":
		return "https://packagist.org/packages/" + p.Package
	case "crates":
		return "https://crates.io/crates/" + p.Package
	case "github-releases":
		return "https://github.com/" + p.Package + "/releases"
	case "newrelic-download":
		return "https://download.newrelic.com/" + p.Package + "/archive/"
	}
	return ""
}

// releasesURL is the GitHub releases page for a source: github (variants.yaml
// `github:` owner/repo) when known, else the package itself when the source
// is github-releases, else "".
func releasesURL(p registry.Package, github string) string {
	if github == "" && p.Registry == "github-releases" {
		github = p.Package
	}
	if github == "" {
		return ""
	}
	return "https://github.com/" + github + "/releases"
}

// pinsLatest lists (and with -apply writes) newer upstream versions of every
// pin in pins.yaml, whatever its kind: tracers are moved only by
// bump-agents.yml (Dependabot ignores them, see .github/dependabot.yml).
// -apply writes through Pin.Write, the same path as `cpm pins set`, so a
// gomod family moves together and `go mod tidy` / `cargo update` run.
// Output is the JSON bump-agents.yml consumes (pinBump).
func pinsLatest(args []string) error {
	fs := flag.NewFlagSet("pins latest", flag.ExitOnError)
	pinsPath := pinsFlag(fs)
	regPath := regFlag(fs)
	only := fs.String("only", "", "only this pin id")
	apply := fs.Bool("apply", false, "rewrite the pin(s) in place")
	fs.Parse(args)

	m, err := pins.Load(*pinsPath)
	if err != nil {
		return err
	}
	client := &versions.Client{}
	ctx := context.Background()
	bumps, findErr := findBumps(ctx, m, *only, client.Latest, githubRepos(*regPath))
	if *apply {
		for i := range bumps {
			b := &bumps[i]
			p := pinByID(m, b.ID)
			files, err := p.Write(b.To)
			if err != nil {
				return err
			}
			b.Files, b.File = files, files[0]
			fmt.Fprintf(os.Stderr, "%s: %s -> %s (%s)\n", b.ID, b.From, b.To, strings.Join(files, ", "))
		}
	}
	if err := writeJSON(bumps); err != nil {
		return err
	}
	return findErr
}

func pinByID(m *pins.Manifest, id string) *pins.Pin {
	for i := range m.Pins {
		if m.Pins[i].ID == id {
			return &m.Pins[i]
		}
	}
	return nil
}

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
