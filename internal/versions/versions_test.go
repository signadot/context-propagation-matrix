package versions

import (
	"reflect"
	"testing"
)

func TestIsStable(t *testing.T) {
	for v, want := range map[string]bool{
		"1.2.3":          true,
		"v1.2.3":         true,
		"12.10.0.39":     true, // New Relic PHP
		"0.65b0":         true, // OTel Python beta channel
		"2.0.0-rc.1":     false,
		"1.0.0-beta.2":   false,
		"3.0.0.dev1":     false,
		"1.2.3-SNAPSHOT": false,
		"1.2.3.preview":  false,
		"nightly":        false,
		"":               false,
		"1.2.3+build":    false,
	} {
		if got := IsStable(v); got != want {
			t.Errorf("IsStable(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"v1.2.3", "1.2.3", 0},
		{"1.2.3", "1.2.4", -1},
		{"1.10.0", "1.9.9", 1},
		{"1.2", "1.2.0", 0},
		{"1.2.0.1", "1.2", 1},
		{"0.65b0", "0.65b1", -1},
		{"0.65b1", "0.66b0", -1},
		{"2.10.1", "2.10.0", 1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSortDesc(t *testing.T) {
	in := []string{"1.2.3", "v1.10.0", "1.2.3", "2.0.0-rc.1", "1.9.9", "0.0.1-beta", "1.10.0"}
	want := []string{"1.10.0", "1.9.9", "1.2.3"}
	if got := SortDesc(in); !reflect.DeepEqual(got, want) {
		t.Errorf("SortDesc = %v, want %v", got, want)
	}
}

func TestNumber(t *testing.T) {
	for in, want := range map[string]string{
		"ddtrace 1.25.0": "1.25.0",
		"v2.10.1":        "2.10.1",
		"open-telemetry/sdk 1.15.0 (ext-opentelemetry 1.2.1)": "1.15.0",
		"0.65b0":              "0.65b0",
		"newrelic 12.10.0.39": "12.10.0.39",
		"unknown":             "",
	} {
		if got := Number(in); got != want {
			t.Errorf("Number(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEscapeModulePath(t *testing.T) {
	if got := escapeModulePath("github.com/DataDog/dd-trace-go/v2"); got != "github.com/!data!dog/dd-trace-go/v2" {
		t.Errorf("escapeModulePath = %q", got)
	}
	if got := escapeModulePath("go.opentelemetry.io/otel"); got != "go.opentelemetry.io/otel" {
		t.Errorf("escapeModulePath = %q", got)
	}
}

func TestNewRelicIndexRegexp(t *testing.T) {
	html := `<a href="/php_agent">..</a> <a href="/php_agent/archive/10.13.0.2">x</a> <a href="12.10.0.39/">y</a> <a href="/php_agent/archive/latest">z</a>`
	got := matchAll(nrHrefRe, html)
	want := []string{"10.13.0.2", "12.10.0.39"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("newrelic hrefs = %v, want %v", got, want)
	}
}
