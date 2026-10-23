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
//	cpm pins latest [-only id] [-apply]  bump-agents.yml: newer kind: arg versions; -apply rewrites
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
// also requires every instrumented variant in variants.yaml to resolve to
// exactly one pin (and every listed variant to exist), exiting non-zero on
// any failure — the verify.yml gate.
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

// pinsLatest lists (and with -apply writes) newer upstream versions of the
// kind: arg pins — the ones Dependabot cannot see. The other kinds are
// Dependabot's; proposing them here would open duplicate PRs. Output is the
// JSON bump-agents.yml consumes: id, file, from, to, source, variants.
func pinsLatest(args []string) error {
	fs := flag.NewFlagSet("pins latest", flag.ExitOnError)
	pinsPath := pinsFlag(fs)
	only := fs.String("only", "", "only this pin id")
	apply := fs.Bool("apply", false, "rewrite the pin(s) in place")
	fs.Parse(args)

	m, err := pins.Load(*pinsPath)
	if err != nil {
		return err
	}
	type bump struct {
		ID       string           `json:"id"`
		File     string           `json:"file"`
		From     string           `json:"from"`
		To       string           `json:"to"`
		Source   registry.Package `json:"source"`
		Variants []string         `json:"variants"`
	}
	bumps := []bump{}
	client := &versions.Client{}
	ctx := context.Background()
	failed := false
	for i := range m.Pins {
		p := &m.Pins[i]
		if p.Kind != pins.KindArg || (*only != "" && p.ID != *only) {
			continue
		}
		current, err := p.Read()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			failed = true
			continue
		}
		newest, err := client.Latest(ctx, p.Source)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: lookup failed: %v\n", p.ID, err)
			continue
		}
		if versions.Compare(newest, current) <= 0 {
			continue
		}
		bumps = append(bumps, bump{p.ID, p.File, current, newest, p.Source, p.Variants})
		if *apply {
			if _, err := p.Write(newest); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "%s: %s -> %s (%s)\n", p.ID, current, newest, p.File)
		}
	}
	if err := writeJSON(bumps); err != nil {
		return err
	}
	if failed {
		return fmt.Errorf("some pins did not resolve")
	}
	return nil
}

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
