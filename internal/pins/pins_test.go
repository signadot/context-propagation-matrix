package pins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/signadot/context-propagation-matrix/internal/registry"
)

func write(t *testing.T, dir, name, body string) string {
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

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// roundTrip checks Read, then Write, then Read again, and that only the
// intended text moved.
func roundTrip(t *testing.T, p *Pin, wantBefore, to, wantFile string) {
	t.Helper()
	got, err := p.Read()
	if err != nil {
		t.Fatalf("%s: Read: %v", p.ID, err)
	}
	if got != wantBefore {
		t.Fatalf("%s: Read = %q, want %q", p.ID, got, wantBefore)
	}
	files, err := p.Write(to)
	if err != nil {
		t.Fatalf("%s: Write: %v", p.ID, err)
	}
	if len(files) == 0 {
		t.Fatalf("%s: Write returned no files", p.ID)
	}
	got, err = p.Read()
	if err != nil {
		t.Fatalf("%s: Read after write: %v", p.ID, err)
	}
	if want := strings.TrimPrefix(to, "v"); got != want { // Write strips a leading v
		t.Fatalf("%s: Read after write = %q, want %q", p.ID, got, want)
	}
	if body := read(t, files[0]); body != wantFile {
		t.Fatalf("%s: file after write:\n%s\nwant:\n%s", p.ID, body, wantFile)
	}
}

func TestArg(t *testing.T) {
	dir := t.TempDir()
	f := write(t, dir, "Dockerfile", "ARG OTHER=9\nARG DD_JAVA_AGENT_VERSION=1.65.1\nARG DD_JAVA_AGENT_VERSION\n")
	p := &Pin{ID: "dd", Kind: KindArg, File: f, Arg: "DD_JAVA_AGENT_VERSION"}
	roundTrip(t, p, "1.65.1", "v1.60.0", "ARG OTHER=9\nARG DD_JAVA_AGENT_VERSION=1.60.0\nARG DD_JAVA_AGENT_VERSION\n")

	f = write(t, dir, "php.Dockerfile", "RUN install-php-extensions opentelemetry-1.2.1\nRUN composer require open-telemetry/sdk:1.15.0\n")
	p = &Pin{ID: "ext", Kind: KindArg, File: f, Pattern: `install-php-extensions opentelemetry-(?<version>[\d.]+)`}
	roundTrip(t, p, "1.2.1", "1.1.0", "RUN install-php-extensions opentelemetry-1.1.0\nRUN composer require open-telemetry/sdk:1.15.0\n")

	p = &Pin{ID: "missing", Kind: KindArg, File: f, Arg: "NOPE"}
	if _, err := p.Read(); err == nil {
		t.Fatal("expected error for missing ARG")
	}
}

func TestNPM(t *testing.T) {
	dir := t.TempDir()
	f := write(t, dir, "package.json", "{\n  \"name\": \"x\",\n  \"dependencies\": {\n    \"@opentelemetry/auto-instrumentations-node\": \"0.80.0\"\n  }\n}\n")
	p := &Pin{ID: "otel", Kind: KindNPM, File: f, Packages: []string{"@opentelemetry/auto-instrumentations-node"}}
	roundTrip(t, p, "0.80.0", "0.79.0", "{\n  \"name\": \"x\",\n  \"dependencies\": {\n    \"@opentelemetry/auto-instrumentations-node\": \"0.79.0\"\n  }\n}\n")
}

func TestPip(t *testing.T) {
	dir := t.TempDir()
	f := write(t, dir, "requirements-otel.txt", "# comment\nopentelemetry-distro==0.65b0\nopentelemetry-instrumentation-flask==0.65b0\nflask==3.1.0\n")
	p := &Pin{ID: "otel", Kind: KindPip, File: f, Packages: []string{"opentelemetry-*"}}
	roundTrip(t, p, "0.65b0", "0.64b0", "# comment\nopentelemetry-distro==0.64b0\nopentelemetry-instrumentation-flask==0.64b0\nflask==3.1.0\n")
}

func TestBundler(t *testing.T) {
	dir := t.TempDir()
	f := write(t, dir, "Gemfile", "source 'https://rubygems.org'\ngem 'sinatra', '4.2.1'\ngem 'datadog', '2.42.0' # tracer\n")
	p := &Pin{ID: "dd", Kind: KindBundler, File: f, Packages: []string{"datadog"}}
	roundTrip(t, p, "2.42.0", "2.41.0", "source 'https://rubygems.org'\ngem 'sinatra', '4.2.1'\ngem 'datadog', '2.41.0' # tracer\n")
}

func TestGoModRead(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module x\n\ngo 1.26.0\n\nrequire (\n\tgithub.com/DataDog/dd-trace-go/v2 v2.10.1\n\tgithub.com/other/mod v1.0.0\n)\n\nrequire github.com/DataDog/dd-trace-go/orchestrion/all/v2 v2.10.1\n")
	p := &Pin{ID: "dd", Kind: KindGoMod, Dir: dir, Modules: []string{"github.com/DataDog/dd-trace-go/v2", "github.com/DataDog/dd-trace-go/orchestrion/all/v2"}}
	got, err := p.Read()
	if err != nil || got != "2.10.1" {
		t.Fatalf("Read = %q, %v", got, err)
	}
	p.Modules = []string{"github.com/DataDog/dd-trace-go/contrib/x/v2"}
	if _, err := p.Read(); err == nil || !strings.Contains(err.Error(), "not required") {
		t.Fatalf("expected not-required error, got %v", err)
	}
}

func TestCargoRead(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Cargo.toml", "[dependencies]\nopentelemetry = \"0.32\"\nopentelemetry_sdk = { version = \"0.32\", features = [\"rt-tokio\"] }\n")
	write(t, dir, "Cargo.lock", "[[package]]\nname = \"opentelemetry\"\nversion = \"0.32.0\"\n\n[[package]]\nname = \"opentelemetry_sdk\"\nversion = \"0.32.1\"\n")
	p := &Pin{ID: "otel", Kind: KindCargo, Dir: dir, Packages: []string{"opentelemetry", "opentelemetry_sdk"}}
	got, err := p.Read()
	if err != nil || got != "0.32.0" {
		t.Fatalf("Read = %q, %v", got, err)
	}
	// The requirement regexp must handle both the plain and the table form.
	for _, c := range p.Packages {
		if !cargoReqRegexp(c).MatchString(read(t, filepath.Join(dir, "Cargo.toml"))) {
			t.Fatalf("requirement regexp does not match %s", c)
		}
	}
}

func TestLoadAndForVariant(t *testing.T) {
	dir := t.TempDir()
	f := write(t, dir, "pins.yaml", `
pins:
  - id: dd-java-agent
    kind: arg
    file: relays/java/Dockerfile
    arg: DD_JAVA_AGENT_VERSION
    source: { registry: maven, package: "com.datadoghq:dd-java-agent" }
    variants: [datadog-java, datadog-java-defaults]
  - id: dd-trace-go
    kind: gomod
    dir: relays/go
    modules: [github.com/DataDog/dd-trace-go/v2]
    source: { registry: goproxy, package: github.com/DataDog/dd-trace-go/v2 }
    variants: [datadog-go]
  - id: otel-dotnet-auto
    kind: arg
    file: relays/dotnet/Dockerfile
    arg: OTEL_DOTNET_AUTO_VERSION
    source: { registry: github-releases, package: open-telemetry/opentelemetry-dotnet-instrumentation, tag_prefix: v }
    variants: [otel-dotnet]
`)
	m, err := Load(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Pins) != 3 || m.Pins[2].Source.TagPrefix != "v" {
		t.Fatalf("Load = %+v", m.Pins)
	}
	p, err := m.ForVariant("datadog-java-defaults", registry.Package{Registry: "maven", Package: "com.datadoghq:dd-java-agent"})
	if err != nil || p.ID != "dd-java-agent" {
		t.Fatalf("ForVariant = %v, %v", p, err)
	}
	// Right source, variant not listed.
	if _, err := m.ForVariant("datadog-java-other", registry.Package{Registry: "maven", Package: "com.datadoghq:dd-java-agent"}); err == nil {
		t.Fatal("expected error for unlisted variant")
	}
	// Variant listed, source differs.
	if _, err := m.ForVariant("datadog-go", registry.Package{Registry: "goproxy", Package: "github.com/DataDog/orchestrion"}); err == nil {
		t.Fatal("expected error for mismatched source")
	}

	for name, body := range map[string]string{
		"dup":      "pins:\n  - {id: a, kind: arg, file: f, arg: X, source: {registry: r, package: p}}\n  - {id: a, kind: arg, file: f, arg: X, source: {registry: r, package: p}}\n",
		"kind":     "pins:\n  - {id: a, kind: pipx, file: f, packages: [x], source: {registry: r, package: p}}\n",
		"nosource": "pins:\n  - {id: a, kind: arg, file: f, arg: X}\n",
		"group":    "pins:\n  - {id: a, kind: arg, file: f, pattern: 'x-(\\d+)', source: {registry: r, package: p}}\n",
		"gomod":    "pins:\n  - {id: a, kind: gomod, dir: d, source: {registry: r, package: p}}\n",
	} {
		f := write(t, dir, name+".yaml", body)
		if _, err := Load(f); err == nil {
			t.Errorf("%s: expected Load error", name)
		}
	}
}
