package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/signadot/context-propagation-matrix/internal/registry"
	"github.com/signadot/context-propagation-matrix/internal/results"
)

// fakeReleaseDates stands in for the tracer's registry: it answers from
// dates, with err when set, and records every call. A version missing from
// dates is one the registry does not list.
type fakeReleaseDates struct {
	dates map[string]time.Time
	err   error
	calls [][]string         // the versions asked for, per call
	pkgs  []registry.Package // the coordinate asked, per call
	ctxs  []context.Context
}

func (f *fakeReleaseDates) lookup(ctx context.Context, p registry.Package, vs []string) (map[string]time.Time, error) {
	f.calls = append(f.calls, append([]string(nil), vs...))
	f.pkgs = append(f.pkgs, p)
	f.ctxs = append(f.ctxs, ctx)
	out := map[string]time.Time{}
	for _, v := range vs {
		if t, ok := f.dates[v]; ok {
			out[v] = t
		}
	}
	return out, f.err
}

// useReleaseDates makes `cpm versions` look release dates up in f for the
// rest of the test, so that no test reaches a registry.
func useReleaseDates(t *testing.T, f *fakeReleaseDates) {
	t.Helper()
	real := lookupReleaseDates
	lookupReleaseDates = f.lookup
	t.Cleanup(func() { lookupReleaseDates = real })
}

// runVersions runs `cpm versions` for otel-java in this process and returns
// its error and what it wrote to stderr. A nil error is exit status 0: main
// exits 1 only for a command that returns one.
func runVersions(t *testing.T, args ...string) (error, string) {
	t.Helper()
	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	realOut, realErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = devnull, stderr
	runErr := cmdVersions(append([]string{"-variant", "otel-java", "-registry", "../../variants.yaml"}, args...))
	os.Stdout, os.Stderr = realOut, realErr
	devnull.Close()
	stderr.Close()
	text, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatal(err)
	}
	return runErr, string(text)
}

// releasedAt reads the released_at of every entry of an index, from
// index.json or from the index inside versions.json ("-" for none).
func releasedAt(t *testing.T, path string) map[string]string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var idx results.VersionIndex
	if filepath.Base(path) == results.BundleAsset {
		var bundle struct {
			Index *results.VersionIndex `json:"index"`
		}
		if err := json.Unmarshal(body, &bundle); err != nil || bundle.Index == nil {
			t.Fatalf("%s: no index (%v)", path, err)
		}
		idx = *bundle.Index
	} else if err := json.Unmarshal(body, &idx); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range idx.Versions {
		out[e.Version] = "-"
		if e.ReleasedAt != nil {
			out[e.Version] = e.ReleasedAt.Format(time.RFC3339Nano)
		}
	}
	return out
}

// checkReleasedAt checks the dates in index.json and that versions.json's
// index carries the same ones.
func checkReleasedAt(t *testing.T, dir string, want map[string]string) {
	t.Helper()
	if got := releasedAt(t, filepath.Join(dir, results.IndexAsset)); !reflect.DeepEqual(got, want) {
		t.Errorf("index.json released_at = %v, want %v", got, want)
	}
	if got := releasedAt(t, filepath.Join(dir, results.BundleAsset)); !reflect.DeepEqual(got, want) {
		t.Errorf("versions.json index released_at = %v, want %v", got, want)
	}
}

func TestVersionsReleaseDates(t *testing.T) {
	reg, err := registry.Load("../../variants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rawDir := t.TempDir()
	writeRaw(t, reg, rawDir, "otel-java", sendsTraced)
	doc := aggregate(t, rawDir, "backtest", "backtest-otel-java-2.0.0")
	// file runs `cpm versions` to file doc as the given version in dir, the
	// way a publish does, re-annotating from a directory with no annotations.
	annDir := t.TempDir()
	file := func(dir, version string, extra ...string) (error, string) {
		return runVersions(t, append([]string{"-out", dir, "-annotations", annDir, "-results", doc, "-version", version}, extra...)...)
	}

	// The registry's times have sub-second precision and need not be in UTC.
	cest := time.FixedZone("CEST", 2*60*60)
	fake := &fakeReleaseDates{dates: map[string]time.Time{
		"2.0.0": time.Date(2024, 1, 12, 20, 15, 30, 987654321, cest),
		"2.1.0": time.Date(2024, 2, 16, 9, 0, 1, 500, time.UTC),
	}}
	useReleaseDates(t, fake)
	dir := t.TempDir()

	// A first version: its date is looked up, for the variant's tracer
	// coordinate, and written to both files.
	runErr, stderr := file(dir, "2.0.0")
	if runErr != nil {
		t.Fatalf("versions: %v\n%s", runErr, stderr)
	}
	if want := [][]string{{"2.0.0"}}; !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("lookups = %v, want %v", fake.calls, want)
	}
	if want := *reg.Variant("otel-java").Tracer; fake.pkgs[0] != want {
		t.Errorf("looked up %+v, want the variant's tracer %+v", fake.pkgs[0], want)
	}
	if deadline, ok := fake.ctxs[0].Deadline(); !ok || time.Until(deadline) > releaseDatesTimeout {
		t.Errorf("the lookup has no overall deadline within %s", releaseDatesTimeout)
	}
	// UTC, to the second.
	checkReleasedAt(t, dir, map[string]string{"2.0.0": "2024-01-12T18:15:30Z"})
	if !strings.Contains(stderr, "versions: otel-java: release date found for 1 of 1 version(s)") {
		t.Errorf("stderr does not say what was found:\n%s", stderr)
	}

	// The date is the index's alone: the per-version document, on the
	// release and in the bundle, says what it said without the lookup.
	undated := t.TempDir()
	if runErr, stderr := file(undated, "2.0.0", "-release-dates=false"); runErr != nil {
		t.Fatalf("versions: %v\n%s", runErr, stderr)
	}
	for _, d := range []string{dir, undated} {
		asset, err := os.ReadFile(filepath.Join(d, "2.0.0.json"))
		if err != nil {
			t.Fatal(err)
		}
		bundle, err := results.LoadVersionBundle(filepath.Join(d, results.BundleAsset), &results.Variant{ID: "otel-java"})
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(asset, []byte("released_at")) || bytes.Contains(bundle.Documents["2.0.0"], []byte("released_at")) {
			t.Errorf("%s: a document has a released_at: it belongs to the index entry only", d)
		}
	}
	for _, name := range []string{"2.0.0.json", results.BundleAsset} {
		with, without := readJSON(t, filepath.Join(dir, name)), readJSON(t, filepath.Join(undated, name))
		if name == results.BundleAsset {
			with, without = with["documents"].(map[string]any), without["documents"].(map[string]any)
		}
		if !reflect.DeepEqual(with, without) {
			t.Errorf("%s: the documents differ with and without the lookup", name)
		}
	}

	// A second version: only it is looked up. The registry now says
	// something else about 2.0.0, which has its date and keeps it.
	fake.dates["2.0.0"] = time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
	if runErr, stderr = file(dir, "2.1.0"); runErr != nil {
		t.Fatalf("versions: %v\n%s", runErr, stderr)
	}
	if want := [][]string{{"2.0.0"}, {"2.1.0"}}; !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("lookups = %v, want %v: a dated entry is not looked up again", fake.calls, want)
	}
	checkReleasedAt(t, dir, map[string]string{"2.0.0": "2024-01-12T18:15:30Z", "2.1.0": "2024-02-16T09:00:01Z"})

	// 2.0.0 measured again: the entry is replaced and keeps its date, and
	// with nothing undated the registry is not asked at all.
	if runErr, stderr = file(dir, "2.0.0"); runErr != nil {
		t.Fatalf("versions: %v\n%s", runErr, stderr)
	}
	if len(fake.calls) != 2 {
		t.Errorf("lookups = %v, want no third one", fake.calls)
	}
	if strings.Contains(stderr, "release date") {
		t.Errorf("stderr mentions release dates with nothing to look up:\n%s", stderr)
	}
	checkReleasedAt(t, dir, map[string]string{"2.0.0": "2024-01-12T18:15:30Z", "2.1.0": "2024-02-16T09:00:01Z"})

	// The registry is down: the version is filed all the same, without a
	// date, and the command succeeds.
	fake.err = errors.New("maven/io.opentelemetry.javaagent:opentelemetry-javaagent: HTTP 503\nand 2 more")
	if runErr, stderr = file(dir, "2.2.0"); runErr != nil {
		t.Fatalf("a failed lookup failed the command: %v\n%s", runErr, stderr)
	}
	checkReleasedAt(t, dir, map[string]string{"2.0.0": "2024-01-12T18:15:30Z", "2.1.0": "2024-02-16T09:00:01Z", "2.2.0": "-"})
	for _, want := range []string{
		"versions: otel-java: release dates: maven/io.opentelemetry.javaagent:opentelemetry-javaagent: HTTP 503; and 2 more\n",
		"versions: otel-java: release date found for 0 of 1 version(s)\n",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "2.2.0.json")); err != nil {
		t.Errorf("2.2.0 was not filed: %v", err)
	}

	// The next publish tries the undated entry again, with the version it
	// files. Some lookups fail and others succeed: what was found is kept,
	// and the error is still only a warning.
	fake.dates["2.2.0"] = time.Date(2024, 3, 15, 11, 30, 0, 0, time.UTC)
	if runErr, stderr = file(dir, "2.3.0"); runErr != nil {
		t.Fatalf("versions: %v\n%s", runErr, stderr)
	}
	if got := fake.calls[len(fake.calls)-1]; !sameSet(got, []string{"2.2.0", "2.3.0"}) {
		t.Errorf("lookup = %v, want the two undated versions, 2.2.0 and 2.3.0", got)
	}
	checkReleasedAt(t, dir, map[string]string{"2.0.0": "2024-01-12T18:15:30Z", "2.1.0": "2024-02-16T09:00:01Z", "2.2.0": "2024-03-15T11:30:00Z", "2.3.0": "-"})
	if !strings.Contains(stderr, "release date found for 1 of 2 version(s)") || !strings.Contains(stderr, "release dates: maven/") {
		t.Errorf("stderr should report the partial result and the error:\n%s", stderr)
	}

	// Nothing to file: an existing bundle is re-annotated and its undated
	// entries looked up (what republish.yml and rebuild-versions.sh run).
	fake.err = nil
	fake.dates["2.3.0"] = time.Date(2024, 4, 15, 0, 0, 0, 0, time.UTC)
	if runErr, stderr = runVersions(t, "-out", dir, "-annotations", annDir); runErr != nil {
		t.Fatalf("versions with nothing to file: %v\n%s", runErr, stderr)
	}
	if got := fake.calls[len(fake.calls)-1]; !reflect.DeepEqual(got, []string{"2.3.0"}) {
		t.Errorf("lookup = %v, want [2.3.0]", got)
	}
	all := map[string]string{"2.0.0": "2024-01-12T18:15:30Z", "2.1.0": "2024-02-16T09:00:01Z", "2.2.0": "2024-03-15T11:30:00Z", "2.3.0": "2024-04-15T00:00:00Z"}
	checkReleasedAt(t, dir, all)
	// The same with re-annotation off: the dates alone are something to do.
	// Here there are none left to find, and the files are rewritten as they
	// stand.
	if runErr, stderr = runVersions(t, "-out", dir, "-annotations", ""); runErr != nil {
		t.Fatalf("versions with nothing to file or re-annotate: %v\n%s", runErr, stderr)
	}
	checkReleasedAt(t, dir, all)

	t.Run("off", func(t *testing.T) {
		fake := &fakeReleaseDates{dates: map[string]time.Time{"2.0.0": time.Date(2024, 1, 12, 0, 0, 0, 0, time.UTC)}}
		useReleaseDates(t, fake)
		dir := t.TempDir()
		runErr, stderr := file(dir, "2.0.0", "-release-dates=false")
		if runErr != nil {
			t.Fatalf("versions: %v\n%s", runErr, stderr)
		}
		if len(fake.calls) != 0 {
			t.Errorf("lookups = %v with -release-dates=false", fake.calls)
		}
		if strings.Contains(stderr, "release date") {
			t.Errorf("stderr mentions release dates:\n%s", stderr)
		}
		checkReleasedAt(t, dir, map[string]string{"2.0.0": "-"})
		for _, name := range []string{results.IndexAsset, results.BundleAsset} {
			body, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(body, []byte("released_at")) {
				t.Errorf("%s has a released_at key", name)
			}
		}
		// Nothing to file and nothing to look up or re-annotate: as before.
		if runErr, _ := runVersions(t, "-out", dir, "-annotations", "", "-release-dates=false"); runErr == nil || !strings.Contains(runErr.Error(), "nothing to do") {
			t.Errorf("err = %v, want nothing to do", runErr)
		}
	})

	// A registry file that only the dates wanted (re-annotation is off) and
	// that cannot be read is a warning too.
	t.Run("no registry", func(t *testing.T) {
		fake := &fakeReleaseDates{dates: map[string]time.Time{"2.0.0": time.Date(2024, 1, 12, 0, 0, 0, 0, time.UTC)}}
		useReleaseDates(t, fake)
		dir := t.TempDir()
		runErr, stderr := runVersions(t, "-out", dir, "-annotations", "", "-results", doc, "-version", "2.0.0", "-registry", filepath.Join(dir, "missing.yaml"))
		if runErr != nil {
			t.Fatalf("versions: %v\n%s", runErr, stderr)
		}
		if len(fake.calls) != 0 {
			t.Errorf("lookups = %v without a tracer coordinate", fake.calls)
		}
		if !strings.Contains(stderr, "versions: otel-java: release dates: ") || !strings.Contains(stderr, "missing.yaml") {
			t.Errorf("stderr does not say why there are no dates:\n%s", stderr)
		}
		checkReleasedAt(t, dir, map[string]string{"2.0.0": "-"})
	})
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return v
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	in := map[string]bool{}
	for _, x := range a {
		in[x] = true
	}
	for _, x := range b {
		if !in[x] {
			return false
		}
	}
	return true
}
