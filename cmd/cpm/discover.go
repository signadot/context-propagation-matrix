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
	"github.com/signadot/context-propagation-matrix/internal/results"
	"github.com/signadot/context-propagation-matrix/internal/versions"
)

// cmdDiscover enumerates back-test candidates: for every instrumented
// variant, the last N stable versions of its tracer package (from the
// registry coordinates in variants.yaml), minus the version the latest
// published results.json already measures (tracer.pinned), minus versions
// the pin's backtest policy in pins.yaml rules out (too old to build, never
// shipped for the whole family), minus — with -skip-published — any
// (variant, version) pair already measured: filed in the variant's
// `variant-<id>` release (by a matrix run or a back-test) or published as a
// `backtest-<variant>-<version>` release.
//
// Depth is a window over the tracer's release history (the newest N versions
// older than current), not a count of gaps, so a monthly run with a small
// depth keeps up with ordinary bumps and a large depth back-fills further.
// When the candidates exceed -max (GitHub caps a matrix at 256 jobs) they
// are taken round-robin across variants, newest first, so every variant
// gets its most recent gaps filled and repeating the dispatch finishes the
// rest.
//
//	cpm discover                                   # all public variants, depth 10
//	cpm discover -depth 5 -variants datadog-java,otel-python
//	cpm discover -include-secret                   # New Relic too
//	cpm discover -skip-published                   # consult GitHub Releases
//	cpm discover -depth 5                          # without -skip-published: every pair again (a refresh)
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
	skipPublished := fs.Bool("skip-published", false, "skip pairs already measured (filed in the variant-* release or with a backtest-* release)")
	repo := fs.String("repo", envOr("GITHUB_REPOSITORY", harnessRepo), "GitHub repository for -skip-published")
	max := fs.Int("max", 250, "cap on the number of pairs (GitHub Actions caps a matrix at 256 jobs)")
	pinsPath := fs.String("pins", "pins.yaml", "pins manifest, for each pin's backtest policy (\"\" to ignore)")
	fs.Parse(args)

	cs, err := loadCoords(*regPath, *resPath)
	if err != nil {
		return err
	}
	var manifest *pins.Manifest
	if *pinsPath != "" {
		if manifest, err = pins.Load(*pinsPath); err != nil {
			return err
		}
	}
	want := map[string]bool{}
	for _, v := range strings.Split(*only, ",") {
		if v = strings.TrimSpace(v); v != "" {
			want[v] = true
		}
	}
	ctx := context.Background()
	client := &versions.Client{}

	// Pairs that need no back-test: backtest-* release tags, plus every
	// version in each variant-* release's index.json — which also holds the
	// versions matrix runs measured, so a version the matrix measured before
	// a bump is not measured again as a back-test (its matrix entry stays in
	// the index).
	published := map[string]bool{}    // backtest-<variant>-<version> tags
	var indexAssets map[string]string // variant id → index.json asset URL
	if *skipPublished {
		rels, err := publishedReleases(ctx, client, *repo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not list releases (%v); not skipping\n", err)
		} else {
			for _, t := range rels.backtests {
				published[t] = true
			}
			indexAssets = rels.indexes
			fmt.Fprintf(os.Stderr, "skip-published: %d backtest releases, %d variant releases\n", len(published), len(indexAssets))
		}
	}
	filedVersions := func(variant string) map[string]bool {
		url, ok := indexAssets[variant]
		if !ok {
			return nil
		}
		filed, err := indexVersions(ctx, client, url)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: variant-%s/index.json: %v; not skipping its filed versions\n", variant, err)
			return nil
		}
		return filed
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
	var perVariant [][]pair // candidates per variant, newest first
	total := 0
	for _, c := range cs {
		if len(want) > 0 && !want[c.Variant] {
			continue
		}
		if c.Secret != "" && !*includeSecret {
			continue
		}
		// Relays sometimes report "ddtrace 1.25.0" or "v2.10.1"; keep the number.
		current := versions.Number(c.Current)
		var policy *pins.BacktestPolicy
		if manifest != nil {
			if pin, err := manifest.ForVariant(c.Variant, *c.Tracer); err == nil {
				policy = pin.Backtest
			}
		}
		var older []string
		excluded := 0
		for _, v := range versionsFor(*c.Tracer) {
			if current != "" && versions.Compare(v, current) >= 0 {
				continue
			}
			if !policy.Allows(v) {
				excluded++
				continue
			}
			older = append(older, v)
			if len(older) == *depth {
				break
			}
		}
		filed := filedVersions(c.Variant)
		var mine []pair
		for _, v := range older {
			if published["backtest-"+c.Variant+"-"+v] || filed[v] {
				continue
			}
			mine = append(mine, pair{c.Variant, v, c.Tracer.Package, c.Tracer.Registry, current})
		}
		cur := current
		if cur == "" {
			cur = "?"
		}
		fmt.Fprintf(os.Stderr, "%s: current %s → %d older candidate(s), %d excluded by policy, %d already measured\n", c.Variant, cur, len(older), excluded, len(older)-len(mine))
		if len(mine) > 0 {
			perVariant = append(perVariant, mine)
			total += len(mine)
		}
	}
	out := []pair{}
	if total <= *max {
		for _, mine := range perVariant {
			out = append(out, mine...)
		}
	} else {
		// Round-robin: the newest gap of every variant before anyone's second.
		fmt.Fprintf(os.Stderr, "warning: %d candidates, taking %d round-robin across %d variants (newest first); dispatch again for the rest\n", total, *max, len(perVariant))
		for i := 0; len(out) < *max; i++ {
			for _, mine := range perVariant {
				if i < len(mine) && len(out) < *max {
					out = append(out, mine[i])
				}
			}
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	return enc.Encode(out)
}

// releases is what -skip-published needs from the repo's GitHub Releases.
type releases struct {
	backtests []string          // backtest-<variant>-<version> tags
	indexes   map[string]string // variant id → API URL of variant-<id>'s index.json asset
}

func githubHeaders(accept string) map[string]string {
	headers := map[string]string{"Accept": accept}
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		headers["Authorization"] = "Bearer " + t
	}
	return headers
}

// publishedReleases pages through every release of the repo, until the API
// returns an empty page, and collects the backtest-* tags and each
// variant-* release's index.json asset. The repo holds one release per
// measured (variant, version), so the list is long and keeps growing; a
// fixed page bound would silently drop the oldest back-tests.
// Drafts are ignored: a publish step that fails part-way (the API's rate
// limit, say) leaves an empty draft carrying the tag name, and a draft is
// not a measurement.
func publishedReleases(ctx context.Context, client *versions.Client, repo string) (*releases, error) {
	out := &releases{indexes: map[string]string{}}
	for page := 1; ; page++ {
		var rel []struct {
			TagName string `json:"tag_name"`
			Draft   bool   `json:"draft"`
			Assets  []struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			} `json:"assets"`
		}
		if err := client.GetJSON(ctx, fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=100&page=%d", repo, page), githubHeaders("application/vnd.github+json"), &rel); err != nil {
			return nil, err
		}
		if len(rel) == 0 {
			break
		}
		for _, r := range rel {
			switch {
			case r.Draft:
			case strings.HasPrefix(r.TagName, "backtest-"):
				out.backtests = append(out.backtests, r.TagName)
			case strings.HasPrefix(r.TagName, "variant-"):
				for _, a := range r.Assets {
					if a.Name == "index.json" {
						out.indexes[strings.TrimPrefix(r.TagName, "variant-")] = a.URL
					}
				}
			}
		}
	}
	return out, nil
}

// indexVersions downloads a variant-<id> release's index.json (the asset API
// URL, which redirects to the content) and returns the versions it files.
func indexVersions(ctx context.Context, client *versions.Client, assetURL string) (map[string]bool, error) {
	var idx struct {
		SchemaVersion int `json:"schema_version"`
		Versions      []struct {
			Version string `json:"version"`
		} `json:"versions"`
	}
	if err := client.GetJSON(ctx, assetURL, githubHeaders("application/octet-stream"), &idx); err != nil {
		return nil, err
	}
	if idx.SchemaVersion != results.VersionIndexSchema {
		return nil, fmt.Errorf("schema_version %d, want %d", idx.SchemaVersion, results.VersionIndexSchema)
	}
	out := map[string]bool{}
	for _, v := range idx.Versions {
		out[v.Version] = true
	}
	return out, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
