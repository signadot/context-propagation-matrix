package versions

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/signadot/context-propagation-matrix/internal/registry"
)

// canned is one registry answer. The 200 responses in this file are real
// ones, fetched in November 2026 and cut down to a few entries and to the
// fields around the ones that are read. Where an entry is made up, the test
// says so.
type canned struct {
	status int // 0 means 200
	header map[string]string
	body   string
}

// fakeRegistries answers requests by URL from canned responses, with a 404
// for any other URL, and records what was asked. Nothing reaches the network.
type fakeRegistries struct {
	t         *testing.T
	responses map[string]canned
	requests  []string // "METHOD URL", in order
	headers   []http.Header
}

func (f *fakeRegistries) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	f.requests = append(f.requests, req.Method+" "+req.URL.String())
	f.headers = append(f.headers, req.Header.Clone())
	if got := req.Header.Get("User-Agent"); got != userAgent {
		f.t.Errorf("%s: User-Agent = %q, want %q", req.URL, got, userAgent)
	}
	c, ok := f.responses[req.URL.String()]
	if !ok {
		c = canned{status: http.StatusNotFound, body: "not found"}
	}
	if c.status == 0 {
		c.status = http.StatusOK
	}
	resp := &http.Response{StatusCode: c.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(c.body)), Request: req}
	for k, v := range c.header {
		resp.Header.Set(k, v)
	}
	return resp, nil
}

func fakeClient(t *testing.T, responses map[string]canned) (*Client, *fakeRegistries) {
	t.Helper()
	// The token comes from the Client alone, whatever the machine has set.
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	f := &fakeRegistries{t: t, responses: responses}
	return &Client{HTTP: &http.Client{Transport: f}}, f
}

func at(t *testing.T, stamp string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

// checkDates compares got with want (version to RFC 3339 time) and checks
// that every time is in UTC.
func checkDates(t *testing.T, got map[string]time.Time, want map[string]string) {
	t.Helper()
	for v, stamp := range want {
		g, ok := got[v]
		if !ok {
			t.Errorf("%s: no date, want %s", v, stamp)
			continue
		}
		if !g.Equal(at(t, stamp)) {
			t.Errorf("%s: %s, want %s", v, g.Format(time.RFC3339Nano), stamp)
		}
		if g.Location() != time.UTC {
			t.Errorf("%s: %s is not in UTC", v, g)
		}
	}
	for v, g := range got {
		if _, ok := want[v]; !ok {
			t.Errorf("%s: unexpected date %s", v, g.Format(time.RFC3339Nano))
		}
	}
}

func TestReleaseDatesMaven(t *testing.T) {
	const base = "https://repo1.maven.org/maven2/io/opentelemetry/javaagent/opentelemetry-javaagent/"
	c, f := fakeClient(t, map[string]canned{
		base + "2.20.0/opentelemetry-javaagent-2.20.0.pom": {header: map[string]string{"Last-Modified": "Sun, 14 Sep 2025 21:05:52 GMT", "Content-Type": "text/xml"}},
		base + "2.21.0/opentelemetry-javaagent-2.21.0.pom": {header: map[string]string{"Last-Modified": "Mon, 20 Oct 2025 07:43:43 GMT", "Content-Type": "text/xml"}},
		// 9.9.9 is not published: Maven Central answers 404.
	})
	p := registry.Package{Registry: "maven", Package: "io.opentelemetry.javaagent:opentelemetry-javaagent"}
	// 2.20.0 is asked for under two spellings and looked up once.
	got, err := c.ReleaseDates(context.Background(), p, []string{"2.20.0", "v2.21.0", "9.9.9", "v2.20.0"})
	if err != nil {
		t.Fatalf("a version the registry does not have is not an error: %v", err)
	}
	checkDates(t, got, map[string]string{
		"2.20.0":  "2025-09-14T21:05:52Z",
		"v2.20.0": "2025-09-14T21:05:52Z",
		"v2.21.0": "2025-10-20T07:43:43Z",
	})
	want := []string{
		"HEAD " + base + "2.20.0/opentelemetry-javaagent-2.20.0.pom",
		"HEAD " + base + "2.21.0/opentelemetry-javaagent-2.21.0.pom",
		"HEAD " + base + "9.9.9/opentelemetry-javaagent-9.9.9.pom",
	}
	if !reflect.DeepEqual(f.requests, want) {
		t.Errorf("requests = %v, want %v", f.requests, want)
	}

	if _, err := c.ReleaseDates(context.Background(), registry.Package{Registry: "maven", Package: "no-colon"}, []string{"1.0.0"}); err == nil {
		t.Error("a maven package that is not group:artifact must be an error")
	}
}

// Some lookups fail, others succeed: the dates found come back with an
// error that says what failed.
func TestReleaseDatesPartialFailure(t *testing.T) {
	const base = "https://repo1.maven.org/maven2/com/datadoghq/dd-java-agent/"
	pom := func(v string) string { return base + v + "/dd-java-agent-" + v + ".pom" }
	c, _ := fakeClient(t, map[string]canned{
		pom("1.53.0"): {header: map[string]string{"Last-Modified": "Thu, 04 Sep 2025 09:47:12 GMT", "Content-Type": "text/xml"}},
		pom("1.52.0"): {status: http.StatusServiceUnavailable},
		pom("1.51.0"): {status: http.StatusServiceUnavailable},
		pom("1.50.0"): {status: http.StatusServiceUnavailable},
		pom("1.49.0"): {status: http.StatusServiceUnavailable},
		pom("1.48.0"): {status: http.StatusServiceUnavailable},
	})
	p := registry.Package{Registry: "maven", Package: "com.datadoghq:dd-java-agent"}
	got, err := c.ReleaseDates(context.Background(), p, []string{"1.53.0", "1.52.0", "1.51.0", "1.50.0", "1.49.0", "1.48.0"})
	checkDates(t, got, map[string]string{"1.53.0": "2025-09-04T09:47:12Z"})
	if err == nil {
		t.Fatal("want an error for the lookups that failed")
	}
	msg := err.Error()
	for _, want := range []string{"maven/com.datadoghq:dd-java-agent", pom("1.52.0") + ": HTTP 503", pom("1.50.0") + ": HTTP 503", "and 2 more"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error does not mention %q:\n%s", want, msg)
		}
	}
	// Only the first few failures are spelled out.
	if strings.Contains(msg, pom("1.48.0")) {
		t.Errorf("error lists every failure:\n%s", msg)
	}
}

// A lookup that has run out of time stops asking and says so.
func TestReleaseDatesStopsWhenContextIsDone(t *testing.T) {
	c, f := fakeClient(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, p := range []registry.Package{
		{Registry: "maven", Package: "g:a"},
		{Registry: "goproxy", Package: "example.com/m"},
		{Registry: "pecl", Package: "opentelemetry"},
	} {
		got, err := c.ReleaseDates(ctx, p, []string{"1.0.0", "1.1.0"})
		if err == nil || !strings.Contains(err.Error(), "context canceled") || len(got) != 0 {
			t.Errorf("%s: dates = %v, err = %v; want none and the context's error", p.Registry, got, err)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("requests made after the context was done: %v", f.requests)
	}
}

func TestReleaseDatesNPM(t *testing.T) {
	// The full packument, not the abbreviated one fetchNPM reads.
	const packument = `{"_id":"@opentelemetry/auto-instrumentations-node","_rev":"125-1839667e614aef9dbc14b10b94572647","name":"@opentelemetry/auto-instrumentations-node",
"dist-tags":{"latest":"0.83.1"},
"versions":{"0.83.0":{"name":"@opentelemetry/auto-instrumentations-node","version":"0.83.0"},"0.83.1":{"name":"@opentelemetry/auto-instrumentations-node","version":"0.83.1"}},
"time":{"created":"2021-04-05T19:01:24.695Z","modified":"2026-11-12T19:41:19.615Z","0.15.0":"2021-04-05T19:01:25.103Z","0.82.0":"2026-10-20T21:41:58.239Z","0.83.0":"2026-11-05T21:39:23.591Z","0.83.1":"2026-11-12T19:41:19.229Z"},
"license":"Apache-2.0"}`
	const u = "https://registry.npmjs.org/@opentelemetry%2Fauto-instrumentations-node"
	c, f := fakeClient(t, map[string]canned{u: {body: packument}})
	p := registry.Package{Registry: "npm", Package: "@opentelemetry/auto-instrumentations-node"}
	got, err := c.ReleaseDates(context.Background(), p, []string{"0.83.1", "v0.82.0", "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	checkDates(t, got, map[string]string{
		"0.83.1":  "2026-11-12T19:41:19.229Z",
		"v0.82.0": "2026-10-20T21:41:58.239Z",
	})
	if want := []string{"GET " + u}; !reflect.DeepEqual(f.requests, want) {
		t.Fatalf("requests = %v, want %v", f.requests, want)
	}
	if accept := f.headers[0].Get("Accept"); strings.Contains(accept, "vnd.npm.install") {
		t.Errorf("Accept = %q asks for the abbreviated packument, which has no dates", accept)
	}
}

func TestReleaseDatesPyPI(t *testing.T) {
	// 0.49.0rc1 is the one ddtrace release whose first listed file is not
	// the first uploaded.
	const doc = `{"info":{"name":"ddtrace","version":"4.17.1"},"last_serial":43795653,
"releases":{
"0.49.0rc1":[
{"comment_text":"","filename":"ddtrace-0.49.0rc1-cp27-cp27m-macosx_10_9_x86_64.whl","packagetype":"bdist_wheel","python_version":"cp27","size":816119,"upload_time":"2021-05-18T19:20:04","upload_time_iso_8601":"2021-05-18T19:20:04.470783Z","yanked":false,"yanked_reason":null},
{"comment_text":"","filename":"ddtrace-0.49.0rc1-cp27-cp27m-manylinux2010_i686.whl","packagetype":"bdist_wheel","python_version":"cp27","size":1605250,"upload_time":"2021-05-18T18:28:14","upload_time_iso_8601":"2021-05-18T18:28:14.795529Z","yanked":false,"yanked_reason":null},
{"comment_text":"","filename":"ddtrace-0.49.0rc1-cp27-cp27m-manylinux2010_x86_64.whl","packagetype":"bdist_wheel","python_version":"cp27","size":1665487,"upload_time":"2021-05-18T18:28:17","upload_time_iso_8601":"2021-05-18T18:28:17.978965Z","yanked":false,"yanked_reason":null}],
"2.21.0":[
{"comment_text":null,"filename":"ddtrace-2.21.0-cp310-cp310-macosx_12_0_universal2.whl","packagetype":"bdist_wheel","python_version":"cp310","size":4438529,"upload_time":"2025-02-14T17:07:44","upload_time_iso_8601":"2025-02-14T17:07:44.152309Z","yanked":false,"yanked_reason":null},
{"comment_text":null,"filename":"ddtrace-2.21.0-cp310-cp310-macosx_12_0_x86_64.whl","packagetype":"bdist_wheel","python_version":"cp310","size":3076281,"upload_time":"2025-02-14T17:07:48","upload_time_iso_8601":"2025-02-14T17:07:48.646169Z","yanked":false,"yanked_reason":null}]
},"urls":[],"vulnerabilities":[]}`
	c, f := fakeClient(t, map[string]canned{"https://pypi.org/pypi/ddtrace/json": {body: doc}})
	p := registry.Package{Registry: "pypi", Package: "ddtrace"}
	got, err := c.ReleaseDates(context.Background(), p, []string{"v0.49.0rc1", "2.21.0", "2.22.0"})
	if err != nil {
		t.Fatal(err)
	}
	checkDates(t, got, map[string]string{
		"v0.49.0rc1": "2021-05-18T18:28:14.795529Z", // the second file listed
		"2.21.0":     "2025-02-14T17:07:44.152309Z",
	})
	if len(f.requests) != 1 {
		t.Errorf("requests = %v, want one", f.requests)
	}
}

func TestReleaseDatesGoProxy(t *testing.T) {
	const base = "https://proxy.golang.org/github.com/!data!dog/dd-trace-go/v2/@v/"
	c, f := fakeClient(t, map[string]canned{
		base + "v2.3.0.info": {body: `{"Version":"v2.3.0","Time":"2025-10-20T12:57:22Z","Origin":{"VCS":"git","URL":"https://github.com/DataDog/dd-trace-go","Hash":"c95df190b2cdc14186f67b3845d133435cde6c74","Ref":"refs/tags/v2.3.0"}}`},
		// The proxy answers 404 for a version that does not exist; the
		// GOPROXY protocol allows 410 as well.
		base + "v2.999.0.info": {status: http.StatusGone, body: "not found"},
	})
	p := registry.Package{Registry: "goproxy", Package: "github.com/DataDog/dd-trace-go/v2"}
	got, err := c.ReleaseDates(context.Background(), p, []string{"v2.3.0", "2.998.0", "2.999.0"})
	if err != nil {
		t.Fatal(err)
	}
	checkDates(t, got, map[string]string{"v2.3.0": "2025-10-20T12:57:22Z"})
	want := []string{"GET " + base + "v2.3.0.info", "GET " + base + "v2.998.0.info", "GET " + base + "v2.999.0.info"}
	if !reflect.DeepEqual(f.requests, want) {
		t.Errorf("requests = %v, want %v", f.requests, want)
	}
}

func TestReleaseDatesRubyGems(t *testing.T) {
	// libdatadog publishes one gem per platform for a version; the "ruby"
	// one, listed last, is the first created.
	const list = `[
{"authors":"Datadog, Inc.","built_at":"1980-01-02T00:00:00.000Z","created_at":"2026-11-13T20:06:32.859Z","downloads_count":1256,"number":"49.0.0.1.1","platform":"arm64-darwin","prerelease":false,"licenses":["Apache-2.0"]},
{"authors":"Datadog, Inc.","built_at":"1980-01-02T00:00:00.000Z","created_at":"2026-11-13T20:06:27.105Z","downloads_count":289,"number":"49.0.0.1.1","platform":"aarch64-linux","prerelease":false,"licenses":["Apache-2.0"]},
{"authors":"Datadog, Inc.","built_at":"1980-01-02T00:00:00.000Z","created_at":"2026-11-13T20:06:21.889Z","downloads_count":18664,"number":"49.0.0.1.1","platform":"x86_64-linux","prerelease":false,"licenses":["Apache-2.0"]},
{"authors":"Datadog, Inc.","built_at":"1980-01-02T00:00:00.000Z","created_at":"2026-11-13T20:06:16.585Z","downloads_count":2619,"number":"49.0.0.1.1","platform":"ruby","prerelease":false,"licenses":["Apache-2.0"]},
{"authors":"Datadog, Inc.","built_at":"1980-01-02T00:00:00.000Z","created_at":"2026-11-09T17:12:04.693Z","downloads_count":3842,"number":"48.0.0.1.1","platform":"arm64-darwin","prerelease":false,"licenses":["Apache-2.0"]}
]`
	c, _ := fakeClient(t, map[string]canned{"https://rubygems.org/api/v1/versions/libdatadog.json": {body: list}})
	p := registry.Package{Registry: "rubygems", Package: "libdatadog"}
	got, err := c.ReleaseDates(context.Background(), p, []string{"49.0.0.1.1", "v48.0.0.1.1", "47.0.0.1.1"})
	if err != nil {
		t.Fatal(err)
	}
	checkDates(t, got, map[string]string{
		"49.0.0.1.1":  "2026-11-13T20:06:16.585Z",
		"v48.0.0.1.1": "2026-11-09T17:12:04.693Z",
	})
}

func TestReleaseDatesCrates(t *testing.T) {
	const doc = `{"versions":[
{"id":3656968,"crate":"datadog-opentelemetry","num":"0.6.0","updated_at":"2026-10-30T13:58:17.536409Z","created_at":"2026-10-30T13:58:17.536409Z","yanked":false,"yank_message":null,"license":"Apache-2.0"},
{"id":3626811,"crate":"datadog-opentelemetry","num":"0.5.4","updated_at":"2026-10-27T17:40:17.489600Z","created_at":"2026-10-27T17:40:17.489600Z","yanked":false,"yank_message":null,"license":"Apache-2.0"},
{"id":3616457,"crate":"datadog-opentelemetry","num":"0.5.3","updated_at":"2026-10-27T09:20:16.080883Z","created_at":"2026-10-26T18:50:11.496336Z","yanked":true,"yank_message":null,"license":"Apache-2.0"}
],"meta":{"total":16,"next_page":null}}`
	c, _ := fakeClient(t, map[string]canned{"https://crates.io/api/v1/crates/datadog-opentelemetry/versions": {body: doc}})
	p := registry.Package{Registry: "crates", Package: "datadog-opentelemetry"}
	got, err := c.ReleaseDates(context.Background(), p, []string{"v0.6.0", "0.5.3", "0.0.9"})
	if err != nil {
		t.Fatal(err)
	}
	checkDates(t, got, map[string]string{
		"v0.6.0": "2026-10-30T13:58:17.536409Z",
		"0.5.3":  "2026-10-26T18:50:11.496336Z", // yanked, and still dated
	})
}

func TestReleaseDatesPECL(t *testing.T) {
	const release = `<?xml version="1.0" encoding="UTF-8" ?>
<r xmlns="http://pear.php.net/dtd/rest.release"
    xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
    xmlns:xlink="http://www.w3.org/1999/xlink"
    xsi:schemaLocation="http://pear.php.net/dtd/rest.release
    http://pear.php.net/dtd/rest.release.xsd">
 <p xlink:href="/rest/p/opentelemetry">opentelemetry</p>
 <c>pecl.php.net</c>
 <v>1.2.1</v>
 <st>stable</st>
 <l>Apache 2.0</l>
 <m>brettmc</m>
 <s>OpenTelemetry auto-instrumentation support extension</s>
 <d>https://github.com/open-telemetry/opentelemetry-php-instrumentation</d>
 <da>2025-10-02 06:00:17</da>
 <n>See https://github.com/open-telemetry/opentelemetry-php-instrumentation/releases/tag/1.2.1</n>
 <f>33392</f>
 <g>https://pecl.php.net/get/opentelemetry-1.2.1</g>
 <x xlink:href="package.1.2.1.xml"/>
</r>`
	c, f := fakeClient(t, map[string]canned{"https://pecl.php.net/rest/r/opentelemetry/1.2.1.xml": {body: release}})
	// The REST paths are lower-case whatever the coordinate says.
	p := registry.Package{Registry: "pecl", Package: "OpenTelemetry"}
	got, err := c.ReleaseDates(context.Background(), p, []string{"v1.2.1", "9.9.9"})
	if err != nil {
		t.Fatal(err)
	}
	checkDates(t, got, map[string]string{"v1.2.1": "2025-10-02T06:00:17Z"})
	want := []string{"GET https://pecl.php.net/rest/r/opentelemetry/1.2.1.xml", "GET https://pecl.php.net/rest/r/opentelemetry/9.9.9.xml"}
	if !reflect.DeepEqual(f.requests, want) {
		t.Errorf("requests = %v, want %v", f.requests, want)
	}
}

func TestReleaseDatesNewRelicDownload(t *testing.T) {
	const index = `<!DOCTYPE HTML>
<html>
	<head>
		<meta charset="UTF-8">
		<title>Index of /php_agent/archive</title>
	</head>
	<body>
		<h1>Index of /php_agent/archive</h1>
		<table>
			<thead>
				<tr>
					<th>Name</th>
					<th>Time</th>
					<th>Size</th>
				</tr>
			</thead>
			<tbody>
	<tr>
		<td><a href="/php_agent">../</a></td>
		<td></td>
		<td></td>
	</tr>

				
					
	<tr>
		<td><a href="/php_agent/archive/1.2.1.609">1.2.1.609/</a></td>
		<td>28-Jun-2016 17:04</td>
		<td></td>
	</tr>

				
					
	<tr>
		<td><a href="/php_agent/archive/12.8.0.37">12.8.0.37/</a></td>
		<td>29-Jun-2026 16:05</td>
		<td></td>
	</tr>

				
					
	<tr>
		<td><a href="/php_agent/archive/12.9.0.38">12.9.0.38/</a></td>
		<td>27-Jul-2026 14:31</td>
		<td></td>
	</tr>

				
					
	<tr>
		<td><a href="/php_agent/archive/9.9.0.260">9.9.0.260/</a></td>
		<td>01-Apr-2020 17:54</td>
		<td></td>
	</tr>

				
				
			</tbody>
		</table>
	</body>
</html>`
	c, _ := fakeClient(t, map[string]canned{"https://download.newrelic.com/php_agent/archive/": {body: index}})
	p := registry.Package{Registry: "newrelic-download", Package: "php_agent"}
	got, err := c.ReleaseDates(context.Background(), p, []string{"12.9.0.38", "v12.8.0.37", "9.9.0.260", "12.7.0.36"})
	if err != nil {
		t.Fatal(err)
	}
	checkDates(t, got, map[string]string{
		"12.9.0.38":  "2026-07-27T14:31:00Z",
		"v12.8.0.37": "2026-06-29T16:05:00Z",
		"9.9.0.260":  "2020-04-01T17:54:00Z",
	})

	if _, err := c.ReleaseDates(context.Background(), registry.Package{Registry: "newrelic-download", Package: "other"}, []string{"1.0.0.0"}); err == nil {
		t.Error("newrelic-download knows one package; any other must be an error")
	}
}

func TestReleaseDatesPackagist(t *testing.T) {
	// The first two entries are real. The third is made up, to cover what
	// the minified format allows and this package has not done: a version
	// tagged on the same commit as the one before it repeats no `time`.
	const doc = `{"minified":"composer/2.0","packages":{"open-telemetry/opentelemetry-auto-slim":[
{"name":"open-telemetry/opentelemetry-auto-slim","description":"OpenTelemetry auto-instrumentation for Slim Framework.","version":"1.5.1","version_normalized":"1.5.1.0","type":"library","time":"2026-09-02T12:13:51+00:00","published-time":"2026-10-24T20:01:27+00:00"},
{"version":"1.5.0","version_normalized":"1.5.0.0","time":"2026-06-08T12:54:17+00:00","published-time":"2026-07-15T15:54:18+00:00"},
{"version":"v1.4.9","version_normalized":"1.4.9.0"}
]},"security-advisories":[]}`
	c, _ := fakeClient(t, map[string]canned{"https://repo.packagist.org/p2/open-telemetry/opentelemetry-auto-slim.json": {body: doc}})
	p := registry.Package{Registry: "packagist", Package: "open-telemetry/opentelemetry-auto-slim"}
	got, err := c.ReleaseDates(context.Background(), p, []string{"v1.5.1", "1.5.0", "1.4.9", "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	checkDates(t, got, map[string]string{
		"v1.5.1": "2026-09-02T12:13:51Z",
		"1.5.0":  "2026-06-08T12:54:17Z",
		"1.4.9":  "2026-06-08T12:54:17Z",
	})
}

// ghRelease is one release as the GitHub API returns it, cut down to these
// fields. The values passed in the tests below are the real ones.
func ghRelease(repo string, id int, tag, name string, prerelease bool, created, published string) string {
	return fmt.Sprintf(`{"url":"https://api.github.com/repos/%[1]s/releases/%[2]d","html_url":"https://github.com/%[1]s/releases/tag/%[3]s",`+
		`"id":%[2]d,"tag_name":%[3]q,"name":%[4]q,"draft":false,"prerelease":%[5]t,"created_at":%[6]q,"published_at":%[7]q}`,
		repo, id, tag, name, prerelease, created, published)
}

func ghPage(repo string, page int) string {
	return fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=100&page=%d", repo, page)
}

func ghTag(repo, tag string) string {
	return "https://api.github.com/repos/" + repo + "/releases/tags/" + tag
}

// With a TagPrefix: tags without it are other products' releases, and
// paging stops at the page that dates the last wanted version.
func TestReleaseDatesGitHubTagPrefix(t *testing.T) {
	const repo = "newrelic/newrelic-dotnet-agent"
	page1 := "[" + strings.Join([]string{
		ghRelease(repo, 450452079, "v10.58.0", "v10.58.0", false, "2026-11-18T19:24:34Z", "2026-11-18T19:24:56Z"),
		ghRelease(repo, 198620079, "v10.36.0", "v10.36.0", false, "2025-02-05T16:04:51Z", "2025-02-05T16:05:13Z"),
		ghRelease(repo, 198170249, "AzureSiteExtension_v1.6.0", "New Relic Azure Site Extension v1.6.0 Release", false, "2025-02-03T17:25:26Z", "2025-02-03T19:06:26Z"),
		ghRelease(repo, 196204140, "v10.35.0", "v10.35.0", false, "2025-01-22T20:58:57Z", "2025-01-22T20:59:18Z"),
	}, ",") + "]"
	c, f := fakeClient(t, map[string]canned{ghPage(repo, 1): {body: page1}, ghPage(repo, 2): {body: "[]"}})
	c.Token = "s3cret"
	p := registry.Package{Registry: "github-releases", Package: repo, TagPrefix: "v"}
	got, err := c.ReleaseDates(context.Background(), p, []string{"10.58.0", "v10.35.0"})
	if err != nil {
		t.Fatal(err)
	}
	checkDates(t, got, map[string]string{
		"10.58.0":  "2026-11-18T19:24:56Z", // published_at, not created_at
		"v10.35.0": "2025-01-22T20:59:18Z",
	})
	if want := []string{"GET " + ghPage(repo, 1)}; !reflect.DeepEqual(f.requests, want) {
		t.Fatalf("requests = %v, want %v: every wanted version was on the first page", f.requests, want)
	}
	if h := f.headers[0]; h.Get("Authorization") != "Bearer s3cret" || h.Get("Accept") != "application/vnd.github+json" {
		t.Errorf("headers = %v, want the listing's Authorization and Accept", h)
	}

	// 1.6.0 is the Azure extension's version, not the agent's: its tag has
	// no "v" prefix, so the listing does not date it, and the agent has no
	// v1.6.0 tag.
	f.requests = nil
	got, err = c.ReleaseDates(context.Background(), p, []string{"1.6.0"})
	if err != nil || len(got) != 0 {
		t.Errorf("dates = %v, err = %v; want none: the tag lacks the prefix", got, err)
	}
	if want := []string{"GET " + ghPage(repo, 1), "GET " + ghPage(repo, 2), "GET " + ghTag(repo, "v1.6.0")}; !reflect.DeepEqual(f.requests, want) {
		t.Errorf("requests = %v, want %v", f.requests, want)
	}
}

// Without a TagPrefix the tag is the version, with or without a "v". A
// version the pages do not hold is asked for by tag, under both spellings.
func TestReleaseDatesGitHubTagFallback(t *testing.T) {
	const repo = "DataDog/dd-trace-php"
	page1 := "[" + strings.Join([]string{
		ghRelease(repo, 450251690, "2.0.0", "2.0.0", false, "2026-11-18T14:47:34Z", "2026-11-18T17:05:02Z"),
		ghRelease(repo, 444792262, "1.27.2", "1.27.2", false, "2026-11-13T22:25:48Z", "2026-11-13T23:37:26Z"),
	}, ",") + "]"
	c, f := fakeClient(t, map[string]canned{
		ghPage(repo, 1): {body: page1},
		ghPage(repo, 2): {body: "[]"},
		// dd-trace-php tagged its first releases "v0.2.5"; later ones "1.27.2".
		ghTag(repo, "0.2.5"):  {status: http.StatusNotFound, body: `{"message":"Not Found","documentation_url":"https://docs.github.com/rest/releases/releases#get-a-release-by-tag-name","status":"404"}`},
		ghTag(repo, "v0.2.5"): {body: ghRelease(repo, 13581212, "v0.2.5", "0.2.5", true, "2018-10-22T09:36:51Z", "2018-10-22T10:57:09Z")},
	})
	p := registry.Package{Registry: "github-releases", Package: repo}
	got, err := c.ReleaseDates(context.Background(), p, []string{"1.27.2", "0.2.5", "0.0.1"})
	if err != nil {
		t.Fatalf("a tag that does not exist is not an error: %v", err)
	}
	checkDates(t, got, map[string]string{
		"1.27.2": "2026-11-13T23:37:26Z",
		"0.2.5":  "2018-10-22T10:57:09Z", // a pre-release, and still dated
	})
	want := []string{
		"GET " + ghPage(repo, 1), "GET " + ghPage(repo, 2),
		"GET " + ghTag(repo, "0.2.5"), "GET " + ghTag(repo, "v0.2.5"),
		"GET " + ghTag(repo, "0.0.1"), "GET " + ghTag(repo, "v0.0.1"),
	}
	if !reflect.DeepEqual(f.requests, want) {
		t.Errorf("requests = %v, want %v", f.requests, want)
	}
	if auth := f.headers[0].Get("Authorization"); auth != "" {
		t.Errorf("Authorization = %q with no token", auth)
	}

	// With a TagPrefix only the prefixed tag is tried.
	const dotnet = "DataDog/dd-trace-dotnet"
	c, f = fakeClient(t, map[string]canned{
		ghPage(dotnet, 1):        {body: "[" + ghRelease(dotnet, 446945195, "v3.58.0", "3.58.0", false, "2026-11-13T19:25:47Z", "2026-11-16T13:43:42Z") + "]"},
		ghPage(dotnet, 2):        {body: "[]"},
		ghTag(dotnet, "v3.20.0"): {body: ghRelease(dotnet, 228854919, "v3.20.0", "3.20.0", false, "2025-06-30T13:31:32Z", "2025-06-30T17:04:25Z")},
	})
	p = registry.Package{Registry: "github-releases", Package: dotnet, TagPrefix: "v"}
	got, err = c.ReleaseDates(context.Background(), p, []string{"3.20.0", "3.19.0"})
	if err != nil {
		t.Fatal(err)
	}
	checkDates(t, got, map[string]string{"3.20.0": "2025-06-30T17:04:25Z"})
	want = []string{"GET " + ghPage(dotnet, 1), "GET " + ghPage(dotnet, 2), "GET " + ghTag(dotnet, "v3.20.0"), "GET " + ghTag(dotnet, "v3.19.0")}
	if !reflect.DeepEqual(f.requests, want) {
		t.Errorf("requests = %v, want %v", f.requests, want)
	}
}

// A page that fails (the rate limit, say) ends the lookup: what the pages
// before it dated is returned with the error, and no tag is asked for.
func TestReleaseDatesGitHubPartialFailure(t *testing.T) {
	const repo = "DataDog/dd-trace-dotnet"
	full := make([]string, 0, 100)
	full = append(full, ghRelease(repo, 446945195, "v3.58.0", "3.58.0", false, "2026-11-13T19:25:47Z", "2026-11-16T13:43:42Z"))
	for len(full) < cap(full) {
		full = append(full, ghRelease(repo, 431998950, "v3.57.0", "3.57.0", false, "2026-11-02T19:49:28Z", "2026-11-02T21:55:08Z"))
	}
	c, f := fakeClient(t, map[string]canned{
		ghPage(repo, 1): {body: "[" + strings.Join(full, ",") + "]"},
		ghPage(repo, 2): {status: http.StatusForbidden},
	})
	p := registry.Package{Registry: "github-releases", Package: repo, TagPrefix: "v"}
	got, err := c.ReleaseDates(context.Background(), p, []string{"3.58.0", "2.40.0"})
	checkDates(t, got, map[string]string{"3.58.0": "2026-11-16T13:43:42Z"})
	if err == nil || !strings.Contains(err.Error(), ghPage(repo, 2)+": HTTP 403") {
		t.Errorf("err = %v, want the failed page", err)
	}
	if want := []string{"GET " + ghPage(repo, 1), "GET " + ghPage(repo, 2)}; !reflect.DeepEqual(f.requests, want) {
		t.Errorf("requests = %v, want %v: nothing is asked after a failure", f.requests, want)
	}
}

func TestReleaseDatesUnavailable(t *testing.T) {
	c, f := fakeClient(t, nil)
	_, err := c.ReleaseDates(context.Background(), registry.Package{Registry: "nuget", Package: "Datadog.Trace"}, []string{"3.0.0"})
	if err == nil || !strings.Contains(err.Error(), "release dates are not available for nuget") {
		t.Errorf("nuget: err = %v", err)
	}
	_, err = c.ReleaseDates(context.Background(), registry.Package{Registry: "nope", Package: "x"}, []string{"1.0.0"})
	if err == nil || !strings.Contains(err.Error(), `unknown registry "nope"`) {
		t.Errorf("unknown registry: err = %v", err)
	}
	// Nothing to look up: no request.
	got, err := c.ReleaseDates(context.Background(), registry.Package{Registry: "npm", Package: "dd-trace"}, nil)
	if err != nil || len(got) != 0 {
		t.Errorf("no versions: dates = %v, err = %v", got, err)
	}
	if len(f.requests) != 0 {
		t.Errorf("requests = %v, want none", f.requests)
	}
}

// A response that is not the document expected is an error, not an empty
// answer. These bodies are made up.
func TestReleaseDatesBadDocument(t *testing.T) {
	c, _ := fakeClient(t, map[string]canned{
		"https://registry.npmjs.org/dd-trace":         {body: "<html>maintenance</html>"},
		"https://rubygems.org/api/v1/versions/x.json": {status: http.StatusInternalServerError},
		"https://crates.io/api/v1/crates/x/versions":  {body: `{"versions":[{"num":"1.0.0","created_at":"yesterday"}]}`},
	})
	for _, p := range []registry.Package{
		{Registry: "npm", Package: "dd-trace"},
		{Registry: "rubygems", Package: "x"},
		{Registry: "crates", Package: "x"},
		{Registry: "pypi", Package: "x"}, // 404: the package itself is missing
	} {
		got, err := c.ReleaseDates(context.Background(), p, []string{"1.0.0"})
		if err == nil || len(got) != 0 {
			t.Errorf("%s: dates = %v, err = %v; want an error", p.Registry, got, err)
		}
	}
}

// Every registry a variant's tracer is published in has a release-date
// lookup: a variant added with a new registry needs one before its versions
// can be dated.
func TestEveryTracerRegistryHasReleaseDates(t *testing.T) {
	reg, err := registry.Load("../../variants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	tracers := 0
	for _, v := range reg.Variants {
		if v.Tracer == nil {
			continue
		}
		tracers++
		if _, ok := daters[v.Tracer.Registry]; !ok {
			t.Errorf("%s: no release-date lookup for registry %q (add one to daters in dates.go)", v.ID, v.Tracer.Registry)
		}
	}
	if tracers == 0 {
		t.Fatal("variants.yaml lists no variant with a tracer")
	}
	// Every lookup is for a registry the version listing knows.
	for name := range daters {
		if _, ok := fetchers[name]; !ok {
			t.Errorf("daters has %q, which is not a registry in fetchers", name)
		}
	}
}

func TestTokenFallsBackToGHToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	c := &Client{}
	if got := c.token(); got != "" {
		t.Errorf("token = %q with nothing set", got)
	}
	// What a workflow's publish step has: GH_TOKEN only.
	t.Setenv("GH_TOKEN", "from-gh-token")
	if got := c.token(); got != "from-gh-token" {
		t.Errorf("token = %q, want $GH_TOKEN when $GITHUB_TOKEN is empty", got)
	}
	t.Setenv("GITHUB_TOKEN", "from-github-token")
	if got := c.token(); got != "from-github-token" {
		t.Errorf("token = %q, want $GITHUB_TOKEN to win", got)
	}
	c.Token = "explicit"
	if got := c.token(); got != "explicit" {
		t.Errorf("token = %q, want the Client's own", got)
	}
}
