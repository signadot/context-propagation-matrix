package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/signadot/context-propagation-matrix/internal/registry"
	"github.com/signadot/context-propagation-matrix/internal/versions"
)

// cmdDiscover enumerates back-test candidates: for every instrumented
// variant, the last N stable versions of its tracer package (from the
// registry coordinates in variants.yaml), minus the version the latest
// published results.json already measures, minus any (variant, version)
// pair that already has a `backtest-<variant>-<version>` GitHub Release.
//
//	cpm discover                                   # all public variants, depth 10
//	cpm discover -depth 5 -variants datadog-java,otel-python
//	cpm discover -include-secret                   # New Relic too
//	cpm discover -skip-published                   # consult GitHub Releases
//
// Output (stdout): JSON array of {variant, version, package, registry,
// current} suitable for a GitHub Actions matrix. Progress goes to stderr.
func cmdDiscover(args []string) error {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	regPath := regFlag(fs)
	resPath := fs.String("results", "results/latest/results.json", "results.json whose tracer versions are the current ones (optional)")
	depth := fs.Int("depth", 10, "how many older versions per variant")
	only := fs.String("variants", "", "comma-separated variant ids (default: every variant)")
	includeSecret := fs.Bool("include-secret", false, "include variants whose tool needs a secret")
	skipPublished := fs.Bool("skip-published", false, "skip pairs that already have a backtest-* release")
	repo := fs.String("repo", envOr("GITHUB_REPOSITORY", harnessRepo), "GitHub repository for -skip-published")
	max := fs.Int("max", 250, "cap on the number of pairs (GitHub Actions caps a matrix at 256 jobs)")
	fs.Parse(args)

	cs, err := loadCoords(*regPath, *resPath)
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, v := range strings.Split(*only, ",") {
		if v = strings.TrimSpace(v); v != "" {
			want[v] = true
		}
	}
	ctx := context.Background()
	client := &versions.Client{}

	published := map[string]bool{}
	if *skipPublished {
		tags, err := backtestReleases(ctx, client, *repo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not list releases (%v); not skipping\n", err)
		} else {
			for _, t := range tags {
				published[t] = true
			}
			fmt.Fprintf(os.Stderr, "skip-published: %d backtest releases exist\n", len(published))
		}
	}

	// Registries are queried once per distinct package, not per variant.
	cache := map[string][]string{}
	versionsFor := func(p registry.Package) []string {
		key := p.Registry + ":" + p.Package
		if vs, ok := cache[key]; ok {
			return vs
		}
		vs, err := client.List(ctx, p, *depth+5)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", key, err)
			vs = nil
		}
		cache[key] = vs
		return vs
	}

	type pair struct {
		Variant  string `json:"variant"`
		Version  string `json:"version"`
		Package  string `json:"package"`
		Registry string `json:"registry"`
		Current  string `json:"current"`
	}
	out := []pair{}
	for _, c := range cs {
		if len(want) > 0 && !want[c.Variant] {
			continue
		}
		if c.Secret != "" && !*includeSecret {
			continue
		}
		// Relays sometimes report "ddtrace 1.25.0" or "v2.10.1"; keep the number.
		current := versions.Number(c.Current)
		var older []string
		for _, v := range versionsFor(*c.Tracer) {
			if current == "" || versions.Compare(v, current) < 0 {
				older = append(older, v)
			}
			if len(older) == *depth {
				break
			}
		}
		for _, v := range older {
			if published["backtest-"+c.Variant+"-"+v] {
				continue
			}
			out = append(out, pair{c.Variant, v, c.Tracer.Package, c.Tracer.Registry, current})
		}
		cur := current
		if cur == "" {
			cur = "?"
		}
		fmt.Fprintf(os.Stderr, "%s: current %s → %d older candidate(s)\n", c.Variant, cur, len(older))
	}
	if len(out) > *max {
		fmt.Fprintf(os.Stderr, "warning: %d candidates, truncating to %d (newest versions first per variant)\n", len(out), *max)
		out = out[:*max]
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	return enc.Encode(out)
}

// backtestReleases lists the repo's backtest-* release tags (up to 1000).
func backtestReleases(ctx context.Context, client *versions.Client, repo string) ([]string, error) {
	headers := map[string]string{"Accept": "application/vnd.github+json"}
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		headers["Authorization"] = "Bearer " + t
	}
	var out []string
	for page := 1; page <= 10; page++ {
		var rel []struct {
			TagName string `json:"tag_name"`
		}
		if err := client.GetJSON(ctx, fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=100&page=%d", repo, page), headers, &rel); err != nil {
			return nil, err
		}
		if len(rel) == 0 {
			break
		}
		for _, r := range rel {
			if strings.HasPrefix(r.TagName, "backtest-") {
				out = append(out, r.TagName)
			}
		}
	}
	return out, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
