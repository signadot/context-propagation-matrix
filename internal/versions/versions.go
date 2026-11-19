// Package versions lists the published versions of a tracer or agent in its
// upstream registry — Maven Central, npm, PyPI, the Go module proxy, NuGet,
// RubyGems, PECL, Packagist, crates.io, GitHub Releases, New Relic's download
// index — as plain version strings, newest first, pre-releases dropped.
//
// Coordinates are the registry/package pairs from variants.yaml (`tracer`) and
// pins.yaml (`source`), so `cpm discover` (older versions to back-test) and
// `cpm pins latest` (newer agent versions to bump to) share one client.
//
// The same client says when given versions were published (ReleaseDates, in
// dates.go), which `cpm versions` records in a variant's version index.
package versions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/signadot/context-propagation-matrix/internal/registry"
)

const userAgent = "context-propagation-matrix (+https://contextpropagation.io)"

// Client fetches version lists and release dates. The zero value is usable;
// Token (or $GITHUB_TOKEN, or $GH_TOKEN) raises the GitHub API rate limit
// for github-releases.
type Client struct {
	HTTP  *http.Client
	Token string
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// token is Token, else $GITHUB_TOKEN, else $GH_TOKEN. The last is what the
// workflows' publish steps set (for gh), and `cpm versions` looks up release
// dates from inside them.
func (c *Client) token() string {
	if c.Token != "" {
		return c.Token
	}
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	return os.Getenv("GH_TOKEN")
}

// Latest returns the newest stable version of p.
func (c *Client) Latest(ctx context.Context, p registry.Package) (string, error) {
	vs, err := c.List(ctx, p, 1)
	if err != nil {
		return "", err
	}
	if len(vs) == 0 {
		return "", fmt.Errorf("%s/%s: no stable versions", p.Registry, p.Package)
	}
	return vs[0], nil
}

// List returns up to n stable versions of p, newest first (n <= 0: all).
func (c *Client) List(ctx context.Context, p registry.Package, n int) ([]string, error) {
	fetch, ok := fetchers[p.Registry]
	if !ok {
		return nil, fmt.Errorf("unknown registry %q", p.Registry)
	}
	raw, err := fetch(ctx, c, p)
	if err != nil {
		return nil, fmt.Errorf("%s/%s: %w", p.Registry, p.Package, err)
	}
	vs := SortDesc(raw)
	if n > 0 && len(vs) > n {
		vs = vs[:n]
	}
	return vs, nil
}

// ---------------------------------------------------------------------------
// Version strings.

// Loose semver: 1.2.3, 1.2.3.4 (New Relic PHP), 0.65b0 (OTel Python betas are
// their stable channel), optionally v-prefixed.
var (
	stableRe     = regexp.MustCompile(`^v?(\d+)(\.\d+)*(b\d+)?$`)
	preReleaseRe = regexp.MustCompile(`(?i)(rc|alpha|beta|dev|snapshot|preview|nightly|-)`)
	otelBetaRe   = regexp.MustCompile(`^\d+\.\d+b\d+$`)
	numberRe     = regexp.MustCompile(`\d+(\.\d+)+(b\d+)?`)
)

// IsStable reports whether v is a release version: rejects rc/alpha/beta/dev/
// snapshot/preview/nightly and anything with a hyphen, except the OTel-Python
// "0.65b0" convention.
func IsStable(v string) bool {
	if v == "" {
		return false
	}
	if preReleaseRe.MatchString(v) && !otelBetaRe.MatchString(v) {
		return false
	}
	return stableRe.MatchString(v)
}

// Strip removes a leading "v".
func Strip(v string) string { return strings.TrimPrefix(v, "v") }

// Number extracts the version number from a relay's reported string
// ("ddtrace 1.25.0" -> "1.25.0", "v2.10.1" -> "2.10.1"); "" if none.
func Number(reported string) string { return Strip(numberRe.FindString(reported)) }

// Compare orders two loose-semver strings numerically by component, treating
// "b" as a separator so 0.65b0 < 0.65b1 < 0.66b0. Missing components are 0.
func Compare(a, b string) int {
	pa, pb := parts(a), parts(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func parts(v string) []int {
	fields := strings.FieldsFunc(Strip(v), func(r rune) bool { return r == '.' || r == 'b' })
	out := make([]int, len(fields))
	for i, f := range fields {
		out[i], _ = strconv.Atoi(f)
	}
	return out
}

// SortDesc de-duplicates, strips "v", drops unstable versions and sorts
// newest first.
func SortDesc(vs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range vs {
		v = Strip(v)
		if seen[v] || !IsStable(v) {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool { return Compare(out[i], out[j]) > 0 })
	return out
}

// ---------------------------------------------------------------------------
// Registries. Each returns raw version strings; List filters and sorts.

type fetcher func(ctx context.Context, c *Client, p registry.Package) ([]string, error)

var fetchers = map[string]fetcher{
	"maven":             fetchMaven,
	"npm":               fetchNPM,
	"pypi":              fetchPyPI,
	"goproxy":           fetchGoProxy,
	"nuget":             fetchNuGet,
	"rubygems":          fetchRubyGems,
	"pecl":              fetchPECL,
	"packagist":         fetchPackagist,
	"crates":            fetchCrates,
	"github-releases":   fetchGitHubReleases,
	"newrelic-download": fetchNewRelicDownload,
}

// Registries lists the supported registry names.
func Registries() []string {
	out := make([]string, 0, len(fetchers))
	for k := range fetchers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (c *Client) get(ctx context.Context, rawURL string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", rawURL, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

// GetJSON fetches a JSON document with the client's User-Agent (and no
// auth; callers add headers). Non-200 responses are errors.
func (c *Client) GetJSON(ctx context.Context, rawURL string, headers map[string]string, v any) error {
	if headers == nil {
		headers = map[string]string{}
	}
	if _, ok := headers["Accept"]; !ok {
		headers["Accept"] = "application/json"
	}
	b, err := c.get(ctx, rawURL, headers)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", rawURL, err)
	}
	return nil
}

func matchAll(re *regexp.Regexp, s string) []string {
	var out []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

var (
	xmlVersionRe = regexp.MustCompile(`<version>([^<]+)</version>`)
	peclVRe      = regexp.MustCompile(`<v>([^<]+)</v>`)
	// Relative ("10.13.0.2/") or absolute ("/php_agent/archive/10.13.0.2") hrefs.
	nrHrefRe = regexp.MustCompile(`href="(?:/php_agent/archive/)?(\d+\.\d+\.\d+\.\d+)/?"`)
)

// maven: pkg = "group:artifact"; maven-metadata.xml lists every version.
func fetchMaven(ctx context.Context, c *Client, p registry.Package) ([]string, error) {
	g, a, ok := strings.Cut(p.Package, ":")
	if !ok {
		return nil, fmt.Errorf("maven package must be group:artifact")
	}
	b, err := c.get(ctx, fmt.Sprintf("https://repo1.maven.org/maven2/%s/%s/maven-metadata.xml", strings.ReplaceAll(g, ".", "/"), a), nil)
	if err != nil {
		return nil, err
	}
	return matchAll(xmlVersionRe, string(b)), nil
}

func fetchNPM(ctx context.Context, c *Client, p registry.Package) ([]string, error) {
	// Scoped packages keep their "@"; the "/" is percent-encoded.
	name := strings.Replace(url.PathEscape(p.Package), "%40", "@", 1)
	var meta struct {
		Versions map[string]json.RawMessage `json:"versions"`
	}
	if err := c.GetJSON(ctx, "https://registry.npmjs.org/"+name, map[string]string{"Accept": "application/vnd.npm.install-v1+json"}, &meta); err != nil {
		return nil, err
	}
	return keys(meta.Versions), nil
}

func fetchPyPI(ctx context.Context, c *Client, p registry.Package) ([]string, error) {
	var meta struct {
		Releases map[string][]struct {
			Yanked bool `json:"yanked"`
		} `json:"releases"`
	}
	if err := c.GetJSON(ctx, fmt.Sprintf("https://pypi.org/pypi/%s/json", p.Package), nil, &meta); err != nil {
		return nil, err
	}
	var out []string
	for v, files := range meta.Releases {
		if len(files) == 0 {
			continue
		}
		allYanked := true
		for _, f := range files {
			if !f.Yanked {
				allYanked = false
				break
			}
		}
		if !allYanked {
			out = append(out, v)
		}
	}
	return out, nil
}

func fetchGoProxy(ctx context.Context, c *Client, p registry.Package) ([]string, error) {
	b, err := c.get(ctx, "https://proxy.golang.org/"+escapeModulePath(p.Package)+"/@v/list", nil)
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(b)), nil
}

func fetchNuGet(ctx context.Context, c *Client, p registry.Package) ([]string, error) {
	var meta struct {
		Versions []string `json:"versions"`
	}
	if err := c.GetJSON(ctx, fmt.Sprintf("https://api.nuget.org/v3-flatcontainer/%s/index.json", strings.ToLower(p.Package)), nil, &meta); err != nil {
		return nil, err
	}
	return meta.Versions, nil
}

func fetchRubyGems(ctx context.Context, c *Client, p registry.Package) ([]string, error) {
	var list []struct {
		Number     string `json:"number"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := c.GetJSON(ctx, fmt.Sprintf("https://rubygems.org/api/v1/versions/%s.json", p.Package), nil, &list); err != nil {
		return nil, err
	}
	var out []string
	for _, v := range list {
		if !v.Prerelease {
			out = append(out, v.Number)
		}
	}
	return out, nil
}

func fetchPECL(ctx context.Context, c *Client, p registry.Package) ([]string, error) {
	b, err := c.get(ctx, fmt.Sprintf("https://pecl.php.net/rest/r/%s/allreleases.xml", strings.ToLower(p.Package)), nil)
	if err != nil {
		return nil, err
	}
	return matchAll(peclVRe, string(b)), nil
}

func fetchPackagist(ctx context.Context, c *Client, p registry.Package) ([]string, error) {
	var meta struct {
		Packages map[string][]struct {
			Version string `json:"version"`
		} `json:"packages"`
	}
	if err := c.GetJSON(ctx, fmt.Sprintf("https://repo.packagist.org/p2/%s.json", p.Package), nil, &meta); err != nil {
		return nil, err
	}
	var out []string
	for _, r := range meta.Packages[p.Package] {
		out = append(out, r.Version)
	}
	return out, nil
}

func fetchCrates(ctx context.Context, c *Client, p registry.Package) ([]string, error) {
	var meta struct {
		Versions []struct {
			Num    string `json:"num"`
			Yanked bool   `json:"yanked"`
		} `json:"versions"`
	}
	if err := c.GetJSON(ctx, fmt.Sprintf("https://crates.io/api/v1/crates/%s/versions", p.Package), nil, &meta); err != nil {
		return nil, err
	}
	var out []string
	for _, v := range meta.Versions {
		if !v.Yanked {
			out = append(out, v.Num)
		}
	}
	return out, nil
}

// Requirements returns the version requirement one release of p declares on
// each of its regular (non-dev, non-build) dependencies, by package name —
// "=0.33.0", "^0.31.0" — as the registry records it. Only crates.io is
// supported: it is what `cpm pins` needs to move a crate's exact dependency
// alongside it (pins.yaml `follow`).
func (c *Client) Requirements(ctx context.Context, p registry.Package, version string) (map[string]string, error) {
	if p.Registry != "crates" {
		return nil, fmt.Errorf("%s/%s: dependency requirements are only known for crates", p.Registry, p.Package)
	}
	var meta struct {
		Dependencies []struct {
			Crate string `json:"crate_id"`
			Req   string `json:"req"`
			Kind  string `json:"kind"`
		} `json:"dependencies"`
	}
	url := fmt.Sprintf("https://crates.io/api/v1/crates/%s/%s/dependencies", p.Package, Strip(version))
	if err := c.GetJSON(ctx, url, nil, &meta); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, d := range meta.Dependencies {
		if d.Kind == "normal" {
			out[d.Crate] = d.Req
		}
	}
	return out, nil
}

// github-releases: pkg = "owner/repo"; TagPrefix is stripped from tag names
// and tags without it are ignored. Three pages of 100, newest first.
func fetchGitHubReleases(ctx context.Context, c *Client, p registry.Package) ([]string, error) {
	headers := map[string]string{"Accept": "application/vnd.github+json"}
	if t := c.token(); t != "" {
		headers["Authorization"] = "Bearer " + t
	}
	var out []string
	for page := 1; page <= 3; page++ {
		var rel []struct {
			TagName    string `json:"tag_name"`
			Draft      bool   `json:"draft"`
			Prerelease bool   `json:"prerelease"`
		}
		if err := c.GetJSON(ctx, fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=100&page=%d", p.Package, page), headers, &rel); err != nil {
			return nil, err
		}
		if len(rel) == 0 {
			break
		}
		for _, r := range rel {
			if r.Draft || r.Prerelease {
				continue
			}
			tag := r.TagName
			if p.TagPrefix != "" {
				if !strings.HasPrefix(tag, p.TagPrefix) {
					continue
				}
				tag = strings.TrimPrefix(tag, p.TagPrefix)
			}
			out = append(out, tag)
		}
	}
	return out, nil
}

// newrelic-download: https://download.newrelic.com/php_agent/archive/ is an
// autoindex of version directories; /release/ holds only the current one.
func fetchNewRelicDownload(ctx context.Context, c *Client, p registry.Package) ([]string, error) {
	if p.Package != "php_agent" {
		return nil, fmt.Errorf("newrelic-download: unknown package %q", p.Package)
	}
	b, err := c.get(ctx, "https://download.newrelic.com/php_agent/archive/", nil)
	if err != nil {
		return nil, err
	}
	return matchAll(nrHrefRe, string(b)), nil
}

// escapeModulePath applies the module proxy's case encoding: each upper-case
// letter becomes "!" + its lower-case form (github.com/DataDog/… →
// github.com/!data!dog/…), as golang.org/x/mod/module.EscapePath does.
func escapeModulePath(p string) string {
	var sb strings.Builder
	for _, r := range p {
		if 'A' <= r && r <= 'Z' {
			sb.WriteByte('!')
			sb.WriteRune(r + ('a' - 'A'))
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
