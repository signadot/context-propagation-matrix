package versions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/signadot/context-propagation-matrix/internal/registry"
)

// Release dates: when a registry published a version. `cpm versions` records
// it as the `released_at` of a variant's version index.
//
// The lookups are kept apart from the fetchers in versions.go, even where
// they ask for the same document again, so that nothing here can change what
// `cpm discover` and `cpm pins latest` see.

// ReleaseDates returns when each of the given versions of p was published,
// keyed by the version as given. A version the registry does not list, or
// gives no date for, is left out. Versions are matched with a leading "v"
// removed on both sides. Times are in UTC.
//
// When some lookups fail and others succeed, the dates found are returned
// together with an error saying what failed, so the caller can keep the
// partial result.
func (c *Client) ReleaseDates(ctx context.Context, p registry.Package, versions []string) (map[string]time.Time, error) {
	lookup, ok := daters[p.Registry]
	if !ok {
		if _, listed := fetchers[p.Registry]; listed {
			return nil, fmt.Errorf("release dates are not available for %s", p.Registry)
		}
		return nil, fmt.Errorf("unknown registry %q", p.Registry)
	}
	// One lookup per distinct version, however it is spelled.
	var want []string
	seen := map[string]bool{}
	for _, v := range versions {
		if s := Strip(v); s != "" && !seen[s] {
			seen[s] = true
			want = append(want, s)
		}
	}
	out := map[string]time.Time{}
	if len(want) == 0 {
		return out, nil
	}
	dates, err := lookup(ctx, c, p, want)
	for _, v := range versions {
		if t, ok := dates[Strip(v)]; ok {
			out[v] = t.UTC()
		}
	}
	if err != nil {
		err = fmt.Errorf("%s/%s: %w", p.Registry, p.Package, err)
	}
	return out, err
}

// HasReleaseDates reports whether ReleaseDates can ask the named registry.
func HasReleaseDates(name string) bool {
	_, ok := daters[name]
	return ok
}

// A dater returns the publication time of each wanted version (already
// stripped of "v") that the registry lists, keyed the same way, and an error
// for what it could not look up. Both may be non-empty.
type dater func(ctx context.Context, c *Client, p registry.Package, want []string) (map[string]time.Time, error)

// nuget has none: no variant's tracer is published there.
var daters = map[string]dater{
	"maven":             datesMaven,
	"npm":               datesNPM,
	"pypi":              datesPyPI,
	"goproxy":           datesGoProxy,
	"rubygems":          datesRubyGems,
	"pecl":              datesPECL,
	"packagist":         datesPackagist,
	"crates":            datesCrates,
	"github-releases":   datesGitHubReleases,
	"newrelic-download": datesNewRelicDownload,
}

// found gathers the dates of the wanted versions while a registry's answer
// is read, and what went wrong on the way.
type found struct {
	want  map[string]bool
	dates map[string]time.Time
	errs  []error
}

func newFound(want []string) *found {
	f := &found{want: map[string]bool{}, dates: map[string]time.Time{}}
	for _, v := range want {
		f.want[v] = true
	}
	return f
}

// add records that version was published at stamp, a time in the given
// layout (read as UTC when the layout has no zone). Versions nobody asked
// for and empty stamps are ignored. A registry may list a version more than
// once, one entry per platform or per file: the earliest stands.
func (f *found) add(version, stamp, layout string) {
	v := Strip(version)
	if !f.want[v] || stamp == "" {
		return
	}
	t, err := time.Parse(layout, stamp)
	if err != nil {
		f.errs = append(f.errs, fmt.Errorf("%s: %w", v, err))
		return
	}
	f.set(v, t)
}

func (f *found) set(v string, t time.Time) {
	if old, ok := f.dates[v]; !ok || t.Before(old) {
		f.dates[v] = t.UTC()
	}
}

func (f *found) fail(err error) { f.errs = append(f.errs, err) }

// missing counts the wanted versions that have no date yet.
func (f *found) missing() int { return len(f.want) - len(f.dates) }

// expired reports whether ctx is done, and if so records that as the reason
// the lookup stopped short. Registries asked once per version check it
// before each request.
func (f *found) expired(ctx context.Context) bool {
	if ctx.Err() == nil {
		return false
	}
	f.fail(fmt.Errorf("stopped with %d version(s) undated: %w", f.missing(), ctx.Err()))
	return true
}

// result is what a dater returns. A registry that is down fails every
// version the same way, so only the first few errors are spelled out.
func (f *found) result() (map[string]time.Time, error) {
	const show = 3
	errs := f.errs
	if len(errs) > show {
		errs = append(errs[:show:show], fmt.Errorf("and %d more", len(errs)-show))
	}
	return f.dates, errors.Join(errs...)
}

// maxBody bounds a response read into memory, as get does. The largest
// documents read here are PyPI's for ddtrace (41 MB in November 2026) and
// npm's full packument for dd-trace (6 MB).
const maxBody = 64 << 20

// statusError is a response with a status other than 200.
type statusError struct {
	URL  string
	Code int
}

func (e *statusError) Error() string { return fmt.Sprintf("%s: HTTP %d", e.URL, e.Code) }

// absent reports whether err is a registry saying it has no such document:
// 404, or 410 as a Go module proxy may answer. For a lookup by version that
// means the version was not published there, which is not a failure.
func absent(err error) bool {
	var se *statusError
	return errors.As(err, &se) && (se.Code == http.StatusNotFound || se.Code == http.StatusGone)
}

// request is get with a method and the response headers: Maven Central says
// when a file was published only in Last-Modified, and a HEAD is enough to
// read it. A status other than 200 is a *statusError, so that "no such
// version" can be told from an outage. A body over maxBody is an error
// rather than a truncated document.
func (c *Client) request(ctx context.Context, method, rawURL string, headers map[string]string) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, &statusError{URL: rawURL, Code: resp.StatusCode}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", rawURL, err)
	}
	if len(b) > maxBody {
		return nil, nil, fmt.Errorf("%s: response is larger than %d MB", rawURL, maxBody>>20)
	}
	return b, resp.Header, nil
}

// requestJSON is GetJSON over request.
func (c *Client) requestJSON(ctx context.Context, rawURL string, headers map[string]string, v any) error {
	h := map[string]string{"Accept": "application/json"}
	for k, val := range headers {
		h[k] = val
	}
	b, _, err := c.request(ctx, http.MethodGet, rawURL, h)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", rawURL, err)
	}
	return nil
}

// maven: maven-metadata.xml has no dates. The Last-Modified of a version's
// .pom is when Maven Central received it. One request per version.
func datesMaven(ctx context.Context, c *Client, p registry.Package, want []string) (map[string]time.Time, error) {
	g, a, ok := strings.Cut(p.Package, ":")
	if !ok {
		return nil, fmt.Errorf("maven package must be group:artifact")
	}
	f := newFound(want)
	for _, v := range want {
		if f.expired(ctx) {
			break
		}
		pom := fmt.Sprintf("https://repo1.maven.org/maven2/%s/%s/%s/%s-%s.pom", strings.ReplaceAll(g, ".", "/"), a, url.PathEscape(v), a, url.PathEscape(v))
		_, h, err := c.request(ctx, http.MethodHead, pom, nil)
		if absent(err) {
			continue
		}
		if err != nil {
			f.fail(err)
			continue
		}
		modified := h.Get("Last-Modified")
		if modified == "" {
			continue
		}
		t, err := http.ParseTime(modified)
		if err != nil {
			f.fail(fmt.Errorf("%s: Last-Modified: %w", pom, err))
			continue
		}
		f.set(v, t)
	}
	return f.result()
}

// npm: the full packument maps every version to its publication time under
// `time` (beside "created" and "modified"). The abbreviated document
// fetchNPM asks for leaves `time` out.
func datesNPM(ctx context.Context, c *Client, p registry.Package, want []string) (map[string]time.Time, error) {
	// Scoped packages keep their "@"; the "/" is percent-encoded.
	name := strings.Replace(url.PathEscape(p.Package), "%40", "@", 1)
	var meta struct {
		// Values are strings, except "unpublished", an object.
		Time map[string]any `json:"time"`
	}
	if err := c.requestJSON(ctx, "https://registry.npmjs.org/"+name, nil, &meta); err != nil {
		return nil, err
	}
	f := newFound(want)
	for v, at := range meta.Time {
		if s, ok := at.(string); ok {
			f.add(v, s, time.RFC3339)
		}
	}
	return f.result()
}

// pypi: a release is a set of files, each with its upload time. The first
// file uploaded is when the version appeared.
func datesPyPI(ctx context.Context, c *Client, p registry.Package, want []string) (map[string]time.Time, error) {
	var meta struct {
		Releases map[string][]struct {
			Uploaded string `json:"upload_time_iso_8601"`
		} `json:"releases"`
	}
	if err := c.requestJSON(ctx, fmt.Sprintf("https://pypi.org/pypi/%s/json", p.Package), nil, &meta); err != nil {
		return nil, err
	}
	f := newFound(want)
	for v, files := range meta.Releases {
		for _, file := range files {
			f.add(v, file.Uploaded, time.RFC3339)
		}
	}
	return f.result()
}

// goproxy: a version's .info carries the time of the tagged commit, which is
// what the proxy records for a version. It is usually the same day as the
// release. One request per version.
func datesGoProxy(ctx context.Context, c *Client, p registry.Package, want []string) (map[string]time.Time, error) {
	f := newFound(want)
	for _, v := range want {
		if f.expired(ctx) {
			break
		}
		var info struct {
			Time string `json:"Time"`
		}
		err := c.requestJSON(ctx, "https://proxy.golang.org/"+escapeModulePath(p.Package)+"/@v/v"+url.PathEscape(v)+".info", nil, &info)
		if absent(err) {
			continue
		}
		if err != nil {
			f.fail(err)
			continue
		}
		f.add(v, info.Time, time.RFC3339)
	}
	return f.result()
}

// rubygems: one entry per version and platform, each with its created_at.
func datesRubyGems(ctx context.Context, c *Client, p registry.Package, want []string) (map[string]time.Time, error) {
	var list []struct {
		Number    string `json:"number"`
		CreatedAt string `json:"created_at"`
	}
	if err := c.requestJSON(ctx, fmt.Sprintf("https://rubygems.org/api/v1/versions/%s.json", p.Package), nil, &list); err != nil {
		return nil, err
	}
	f := newFound(want)
	for _, v := range list {
		f.add(v.Number, v.CreatedAt, time.RFC3339)
	}
	return f.result()
}

// pecl: the REST document of a release has <da>2025-10-02 06:00:17</da>.
// It names no zone and is read as UTC, so the date can be off by one for a
// release made near midnight. One request per version.
func datesPECL(ctx context.Context, c *Client, p registry.Package, want []string) (map[string]time.Time, error) {
	f := newFound(want)
	for _, v := range want {
		if f.expired(ctx) {
			break
		}
		b, _, err := c.request(ctx, http.MethodGet, fmt.Sprintf("https://pecl.php.net/rest/r/%s/%s.xml", strings.ToLower(p.Package), url.PathEscape(v)), nil)
		if absent(err) {
			continue
		}
		if err != nil {
			f.fail(err)
			continue
		}
		if m := peclDateRe.FindSubmatch(b); m != nil {
			f.add(v, strings.TrimSpace(string(m[1])), "2006-01-02 15:04:05")
		}
	}
	return f.result()
}

// packagist: `time` per version. The p2 document is minified: an entry
// repeats a key only when its value differs from the entry before it, and
// says "__unset" when the key is gone.
func datesPackagist(ctx context.Context, c *Client, p registry.Package, want []string) (map[string]time.Time, error) {
	var meta struct {
		Packages map[string][]struct {
			Version string  `json:"version"`
			Time    *string `json:"time"`
		} `json:"packages"`
	}
	if err := c.requestJSON(ctx, fmt.Sprintf("https://repo.packagist.org/p2/%s.json", p.Package), nil, &meta); err != nil {
		return nil, err
	}
	f := newFound(want)
	at := ""
	for _, r := range meta.Packages[p.Package] {
		if r.Time != nil {
			at = *r.Time
		}
		if at == "__unset" {
			at = ""
		}
		f.add(r.Version, at, time.RFC3339)
	}
	return f.result()
}

// crates: created_at per version. Yanked versions keep theirs.
func datesCrates(ctx context.Context, c *Client, p registry.Package, want []string) (map[string]time.Time, error) {
	var meta struct {
		Versions []struct {
			Num       string `json:"num"`
			CreatedAt string `json:"created_at"`
		} `json:"versions"`
	}
	if err := c.requestJSON(ctx, fmt.Sprintf("https://crates.io/api/v1/crates/%s/versions", p.Package), nil, &meta); err != nil {
		return nil, err
	}
	f := newFound(want)
	for _, v := range meta.Versions {
		f.add(v.Num, v.CreatedAt, time.RFC3339)
	}
	return f.result()
}

// github-releases: published_at of each release, from the listing
// fetchGitHubReleases reads (three pages of 100, newest first, the same
// TagPrefix rule and Authorization). Pre-releases count: a version that was
// measured has a date whatever its channel. Drafts have no published_at.
// Paging stops as soon as every wanted version has a date, which for the
// usual case, one new version, is the first page.
//
// A wanted version the pages do not hold (more than 300 releases ago) is
// asked for by tag: TagPrefix + version, and without a TagPrefix also "v" +
// version. The first request that fails for a reason other than "no such
// tag" ends the lookup, so a rate limit is not answered with more requests.
func datesGitHubReleases(ctx context.Context, c *Client, p registry.Package, want []string) (map[string]time.Time, error) {
	headers := map[string]string{"Accept": "application/vnd.github+json"}
	if t := c.token(); t != "" {
		headers["Authorization"] = "Bearer " + t
	}
	type release struct {
		TagName     string `json:"tag_name"`
		PublishedAt string `json:"published_at"`
	}
	f := newFound(want)
	for page := 1; page <= 3 && f.missing() > 0; page++ {
		var rel []release
		if err := c.requestJSON(ctx, fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=100&page=%d", p.Package, page), headers, &rel); err != nil {
			f.fail(err)
			return f.result()
		}
		if len(rel) == 0 {
			break
		}
		for _, r := range rel {
			tag := r.TagName
			if p.TagPrefix != "" {
				if !strings.HasPrefix(tag, p.TagPrefix) {
					continue
				}
				tag = strings.TrimPrefix(tag, p.TagPrefix)
			}
			f.add(tag, r.PublishedAt, time.RFC3339)
		}
	}
	for _, v := range want {
		if _, ok := f.dates[v]; ok {
			continue
		}
		tags := []string{p.TagPrefix + v}
		if p.TagPrefix == "" {
			tags = append(tags, "v"+v)
		}
		for _, tag := range tags {
			if f.expired(ctx) {
				return f.result()
			}
			var r release
			err := c.requestJSON(ctx, fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", p.Package, url.PathEscape(tag)), headers, &r)
			if absent(err) {
				continue
			}
			if err != nil {
				f.fail(err)
				return f.result()
			}
			f.add(v, r.PublishedAt, time.RFC3339)
			break
		}
	}
	return f.result()
}

// newrelic-download: each row of the archive's autoindex is a version
// directory and its modification time, "27-Jul-2026 14:31". The page names
// no zone and the time is read as UTC. For the releases compared, the
// project's GitHub release was published the same day, about an hour later.
func datesNewRelicDownload(ctx context.Context, c *Client, p registry.Package, want []string) (map[string]time.Time, error) {
	if p.Package != "php_agent" {
		return nil, fmt.Errorf("newrelic-download: unknown package %q", p.Package)
	}
	b, _, err := c.request(ctx, http.MethodGet, "https://download.newrelic.com/php_agent/archive/", nil)
	if err != nil {
		return nil, err
	}
	f := newFound(want)
	for _, m := range nrRowRe.FindAllSubmatch(b, -1) {
		f.add(string(m[1]), string(m[2]), "2-Jan-2006 15:04")
	}
	return f.result()
}

var (
	peclDateRe = regexp.MustCompile(`<da>([^<]+)</da>`)
	// A version directory's link (href as in nrHrefRe), then the next cell.
	nrRowRe = regexp.MustCompile(`href="(?:/php_agent/archive/)?(\d+\.\d+\.\d+\.\d+)/?"[^>]*>[^<]*</a>\s*</td>\s*<td>\s*(\d{1,2}-[A-Za-z]{3}-\d{4} \d{2}:\d{2})\s*</td>`)
)
