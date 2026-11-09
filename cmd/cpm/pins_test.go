package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/signadot/context-propagation-matrix/internal/pins"
	"github.com/signadot/context-propagation-matrix/internal/registry"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// fakeRegistry answers Latest from a map keyed registry:package; a missing
// key is a lookup error (the registry is down).
func fakeRegistry(newest map[string]string) latestFunc {
	return func(_ context.Context, p registry.Package) (string, error) {
		v, ok := newest[sourceKey(p)]
		if !ok {
			return "", errors.New("HTTP 503")
		}
		return v, nil
	}
}

// onePinManifest writes a manifest for one pin of each kind, with the given
// current version in its file, and returns it with the pin's source.
func onePinManifest(t *testing.T, dir, kind, current string) *pins.Manifest {
	t.Helper()
	src := registry.Package{Registry: "reg", Package: "pkg"}
	p := pins.Pin{ID: "pin", Kind: kind, Source: src, Variants: []string{"v-a", "v-b"}}
	switch kind {
	case pins.KindArg:
		p.File = writeFile(t, dir, "Dockerfile", "ARG X_VERSION="+current+"\n")
		p.Arg = "X_VERSION"
	case pins.KindNPM:
		p.File = writeFile(t, dir, "package.json", "{\n  \"dependencies\": {\n    \"pkg\": \""+current+"\"\n  }\n}\n")
		p.Packages = []string{"pkg"}
	case pins.KindPip:
		p.File = writeFile(t, dir, "requirements.txt", "pkg=="+current+"\npkg-extra=="+current+"\n")
		p.Packages = []string{"pkg*"}
	case pins.KindBundler:
		p.File = writeFile(t, dir, "Gemfile", "gem 'pkg', '"+current+"'\n")
		p.Packages = []string{"pkg"}
	case pins.KindGoMod:
		writeFile(t, dir, "go.mod", "module x\n\nrequire (\n\texample.com/pkg v"+current+"\n\texample.com/pkg/sdk v"+current+"\n)\n")
		p.Dir = dir
		p.Modules = []string{"example.com/pkg", "example.com/pkg/sdk"}
	case pins.KindCargo:
		writeFile(t, dir, "Cargo.toml", "[dependencies]\npkg = \"="+current+"\"\n")
		writeFile(t, dir, "Cargo.lock", "[[package]]\nname = \"pkg\"\nversion = \""+current+"\"\n")
		p.Dir = dir
		p.Packages = []string{"pkg"}
	default:
		t.Fatalf("unknown kind %s", kind)
	}
	return &pins.Manifest{Pins: []pins.Pin{p}}
}

// Every kind is considered, and the bump decision depends only on the
// comparison of the stripped current and newest versions.
func TestFindBumpsEveryKind(t *testing.T) {
	kinds := []string{pins.KindArg, pins.KindNPM, pins.KindPip, pins.KindBundler, pins.KindGoMod, pins.KindCargo}
	cases := []struct {
		name    string
		current string
		newest  string
		want    string // "" = no bump
	}{
		{"behind", "1.65.1", "1.68.1", "1.68.1"},
		{"equal", "1.68.1", "1.68.1", ""},
		{"registry behind (yanked latest)", "1.68.1", "1.67.0", ""},
		{"v-prefixed registry value", "2.10.1", "v2.12.0", "2.12.0"},
		{"more components", "12.10.0.39", "12.12.0.41", "12.12.0.41"},
		{"minor rolls over", "1.9.0", "1.10.0", "1.10.0"},
		{"otel python beta channel", "0.65b0", "0.66b2", "0.66b2"},
		{"otel python same", "0.66b2", "0.66b2", ""},
	}
	for _, kind := range kinds {
		for _, tc := range cases {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				m := onePinManifest(t, t.TempDir(), kind, tc.current)
				got, err := findBumps(context.Background(), m, "", fakeRegistry(map[string]string{"reg:pkg": tc.newest}), nil)
				if err != nil {
					t.Fatal(err)
				}
				if tc.want == "" {
					if len(got) != 0 {
						t.Fatalf("bumps = %+v, want none", got)
					}
					return
				}
				if len(got) != 1 {
					t.Fatalf("bumps = %+v, want one", got)
				}
				b := got[0]
				if b.ID != "pin" || b.Kind != kind || b.From != tc.current || b.To != tc.want {
					t.Errorf("bump = %+v, want pin/%s %s -> %s", b, kind, tc.current, tc.want)
				}
				if !reflect.DeepEqual(b.Variants, []string{"v-a", "v-b"}) {
					t.Errorf("variants = %v", b.Variants)
				}
				if want := m.Pins[0].Files(); !reflect.DeepEqual(b.Files, want) || b.File != want[0] {
					t.Errorf("files = %v / %q, want %v", b.Files, b.File, want)
				}
			})
		}
	}
}

func TestFindBumpsSelection(t *testing.T) {
	dir := t.TempDir()
	df := writeFile(t, dir, "Dockerfile", "ARG A_VERSION=1.0.0\nARG B_VERSION=2.0.0\nARG C_VERSION=3.0.0\n")
	m := &pins.Manifest{Pins: []pins.Pin{
		{ID: "a", Kind: pins.KindArg, File: df, Arg: "A_VERSION", Source: registry.Package{Registry: "maven", Package: "g:a"}},
		{ID: "b", Kind: pins.KindArg, File: df, Arg: "B_VERSION", Source: registry.Package{Registry: "npm", Package: "b"}},
		{ID: "c", Kind: pins.KindArg, File: df, Arg: "C_VERSION", Source: registry.Package{Registry: "crates", Package: "c"}},
	}}
	reg := fakeRegistry(map[string]string{"maven:g:a": "1.1.0", "npm:b": "2.0.0"}) // c: lookup fails

	ids := func(bs []pinBump) []string {
		var out []string
		for _, b := range bs {
			out = append(out, b.ID)
		}
		return out
	}

	// All pins: a is behind, b is current, c's registry is down (skipped, not fatal).
	got, err := findBumps(context.Background(), m, "", reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids(got), []string{"a"}) {
		t.Errorf("bumps = %v, want [a]", ids(got))
	}

	// -only narrows to one pin.
	got, err = findBumps(context.Background(), m, "b", reg, nil)
	if err != nil || len(got) != 0 {
		t.Errorf("-only b: bumps = %v, err = %v; want none", ids(got), err)
	}
	got, err = findBumps(context.Background(), m, "a", reg, nil)
	if err != nil || !reflect.DeepEqual(ids(got), []string{"a"}) {
		t.Errorf("-only a: bumps = %v, err = %v; want [a]", ids(got), err)
	}

	// -only with an unknown id is an error (the apply job would otherwise
	// commit nothing).
	if _, err := findBumps(context.Background(), m, "nope", reg, nil); err == nil || !strings.Contains(err.Error(), "no pin") {
		t.Errorf("-only nope: err = %v, want no-pin error", err)
	}

	// A pin that does not resolve in its file is an error, but the other
	// bumps are still returned (and printed) so the run shows what it found.
	m.Pins = append(m.Pins, pins.Pin{ID: "d", Kind: pins.KindArg, File: df, Arg: "MISSING", Source: registry.Package{Registry: "npm", Package: "b"}})
	got, err = findBumps(context.Background(), m, "", reg, nil)
	if err == nil || !strings.Contains(err.Error(), "did not resolve") {
		t.Errorf("unresolved pin: err = %v", err)
	}
	if !reflect.DeepEqual(ids(got), []string{"a"}) {
		t.Errorf("unresolved pin: bumps = %v, want [a]", ids(got))
	}
}

func TestFindBumpsLinks(t *testing.T) {
	dir := t.TempDir()
	df := writeFile(t, dir, "Dockerfile", "ARG A_VERSION=1.0.0\nARG B_VERSION=1.0.0\nARG C_VERSION=1.0.0\n")
	m := &pins.Manifest{Pins: []pins.Pin{
		{ID: "a", Kind: pins.KindArg, File: df, Arg: "A_VERSION", Source: registry.Package{Registry: "maven", Package: "com.datadoghq:dd-java-agent"}},
		{ID: "b", Kind: pins.KindArg, File: df, Arg: "B_VERSION", Source: registry.Package{Registry: "github-releases", Package: "DataDog/dd-trace-dotnet", TagPrefix: "v"}},
		{ID: "c", Kind: pins.KindArg, File: df, Arg: "C_VERSION", Source: registry.Package{Registry: "newrelic-download", Package: "php_agent"}},
	}}
	reg := fakeRegistry(map[string]string{
		"maven:com.datadoghq:dd-java-agent":       "1.1.0",
		"github-releases:DataDog/dd-trace-dotnet": "1.1.0",
		"newrelic-download:php_agent":             "1.1.0.0",
	})
	github := map[string]string{"maven:com.datadoghq:dd-java-agent": "DataDog/dd-trace-java"}
	got, err := findBumps(context.Background(), m, "", reg, github)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]string{
		"a": {"https://central.sonatype.com/artifact/com.datadoghq/dd-java-agent", "https://github.com/DataDog/dd-trace-java/releases"},
		"b": {"https://github.com/DataDog/dd-trace-dotnet/releases", "https://github.com/DataDog/dd-trace-dotnet/releases"},
		"c": {"https://download.newrelic.com/php_agent/archive/", ""},
	}
	for _, b := range got {
		if w := want[b.ID]; b.URL != w[0] || b.ReleasesURL != w[1] {
			t.Errorf("%s: url = %q, releases = %q; want %q, %q", b.ID, b.URL, b.ReleasesURL, w[0], w[1])
		}
	}
}

func TestPackageURL(t *testing.T) {
	cases := map[registry.Package]string{
		{Registry: "maven", Package: "io.opentelemetry.javaagent:opentelemetry-javaagent"}: "https://central.sonatype.com/artifact/io.opentelemetry.javaagent/opentelemetry-javaagent",
		{Registry: "maven", Package: "no-colon"}:                                           "",
		{Registry: "npm", Package: "@opentelemetry/auto-instrumentations-node"}:            "https://www.npmjs.com/package/@opentelemetry/auto-instrumentations-node",
		{Registry: "pypi", Package: "ddtrace"}:                                             "https://pypi.org/project/ddtrace/",
		{Registry: "goproxy", Package: "github.com/DataDog/dd-trace-go/v2"}:                "https://pkg.go.dev/github.com/DataDog/dd-trace-go/v2",
		{Registry: "rubygems", Package: "newrelic_rpm"}:                                    "https://rubygems.org/gems/newrelic_rpm",
		{Registry: "pecl", Package: "opentelemetry"}:                                       "https://pecl.php.net/package/opentelemetry",
		{Registry: "packagist", Package: "open-telemetry/opentelemetry-auto-slim"}:         "https://packagist.org/packages/open-telemetry/opentelemetry-auto-slim",
		{Registry: "crates", Package: "datadog-opentelemetry"}:                             "https://crates.io/crates/datadog-opentelemetry",
		{Registry: "github-releases", Package: "elastic/apm-agent-php", TagPrefix: "v"}:    "https://github.com/elastic/apm-agent-php/releases",
		{Registry: "newrelic-download", Package: "php_agent"}:                              "https://download.newrelic.com/php_agent/archive/",
		{Registry: "unknown", Package: "x"}:                                                "",
	}
	for p, want := range cases {
		if got := packageURL(p); got != want {
			t.Errorf("packageURL(%s/%s) = %q, want %q", p.Registry, p.Package, got, want)
		}
	}
}
