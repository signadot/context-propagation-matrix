package pins

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/signadot/context-propagation-matrix/internal/registry"
)

// stubCargo puts a `cargo` on PATH that only records its arguments in
// dir/cargo.args, so cargo write paths can be tested without a toolchain or
// the network.
func stubCargo(t *testing.T, dir string) string {
	t.Helper()
	args := filepath.Join(dir, "cargo.args")
	bin := write(t, dir, "bin/cargo", "#!/bin/sh\nprintf '%s\\n' \"$*\" > '"+args+"'\n")
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return args
}

// stubBundle puts a `bundle` on PATH that appends its arguments to
// dir/bundle.args, one line per run, and fails a run whose arguments are a
// line of dir/bundle.fail, so bundler write paths (and a version that does
// not resolve) can be tested without Ruby or the network. It does not touch
// Gemfile.lock.
func stubBundle(t *testing.T, dir string) string {
	t.Helper()
	args := filepath.Join(dir, "bundle.args")
	fail := filepath.Join(dir, "bundle.fail")
	bin := write(t, dir, "bin/bundle", "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '"+args+"'\n! grep -qxF -- \"$*\" '"+fail+"' 2>/dev/null\n")
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return args
}

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
	files, err := p.Write(context.Background(), to, nil)
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

// gemLock is a Gemfile.lock as Bundler writes one for a Gemfile pinning
// sinatra 4.2.1, grpc 1.85.0 and datadog at the given version: a spec per
// gem (one per platform for a gem with precompiled variants), the gems
// each depends on indented below it, and the Gemfile's own requirements
// under DEPENDENCIES.
func gemLock(datadog string) string {
	return "GEM\n  remote: https://rubygems.org/\n  specs:\n" +
		"    datadog (" + datadog + ")\n      libdatadog (~> 46.1.0.1.1)\n      logger\n" +
		"    grpc (1.85.0-aarch64-linux-gnu)\n    grpc (1.85.0-x86_64-linux-gnu)\n" +
		"    libdatadog (46.1.0.1.1-aarch64-linux)\n    libdatadog (46.1.0.1.1-x86_64-linux)\n" +
		"    logger (1.7.0)\n    sinatra (4.2.1)\n      logger (>= 1.6.0)\n" +
		"\nPLATFORMS\n  aarch64-linux\n  x86_64-linux\n" +
		"\nDEPENDENCIES\n  datadog (= " + datadog + ")\n  grpc (= 1.85.0)\n  sinatra (= 4.2.1)\n" +
		"\nBUNDLED WITH\n   2.5.22\n"
}

func TestBundler(t *testing.T) {
	dir := t.TempDir()
	f := write(t, dir, "Gemfile", "source 'https://rubygems.org'\ngem 'sinatra', '4.2.1'\ngem 'grpc', '1.85.0'\ngem 'datadog', '2.42.0' # tracer\n")
	lock := write(t, dir, "Gemfile.lock", gemLock("2.42.0"))
	argsFile := stubBundle(t, dir)
	p := &Pin{ID: "dd", Kind: KindBundler, File: f, Packages: []string{"datadog"}}
	if got := p.Files(); len(got) != 2 || got[0] != f || got[1] != lock {
		t.Fatalf("Files = %v, want the Gemfile and its lock", got)
	}
	if err := p.Check(); err != nil {
		t.Fatalf("Check with the lock at the pinned version: %v", err)
	}
	roundTrip(t, p, "2.42.0", "2.41.0", "source 'https://rubygems.org'\ngem 'sinatra', '4.2.1'\ngem 'grpc', '1.85.0'\ngem 'datadog', '2.41.0' # tracer\n")
	// The lock is moved by Bundler, for the pinned gem only, in the
	// Gemfile's directory.
	if got, want := strings.TrimSpace(read(t, argsFile)), "lock --update datadog"; got != want {
		t.Fatalf("bundle ran with %q, want %q", got, want)
	}

	// The stub left the lock at 2.42.0 under a Gemfile that now pins 2.41.0,
	// the state a frozen install refuses: Check names both versions.
	if err := p.Check(); err == nil || !strings.Contains(err.Error(), "pins datadog 2.41.0") || !strings.Contains(err.Error(), "holds 2.42.0") {
		t.Fatalf("Check with the lock behind the Gemfile: got %v, want an error naming both versions", err)
	}
	write(t, dir, "Gemfile.lock", gemLock("2.41.0"))
	if err := p.Check(); err != nil {
		t.Fatalf("Check after the lock moved: %v", err)
	}

	// A gem with a spec per platform is one version. The lines that name a
	// gem as another's dependency (`libdatadog (~> 46.1.0.1.1)`) or under
	// DEPENDENCIES (`datadog (= 2.41.0)`) are not specs.
	for gem, want := range map[string]string{"grpc": "1.85.0", "libdatadog": "46.1.0.1.1", "datadog": "2.41.0", "nope": ""} {
		if got := strings.Join(gemLockVersions(read(t, lock), gem), ","); got != want {
			t.Errorf("gemLockVersions(%s) = %q, want %q", gem, got, want)
		}
	}
	p.Packages = []string{"grpc"}
	if err := p.Check(); err != nil {
		t.Fatalf("Check of a gem locked per platform: %v", err)
	}
}

// Without a lock the image cannot be built, and `bundle lock` would create
// one for the wrong platforms: Check fails, and Write fails before it
// changes the Gemfile or runs bundle.
func TestBundlerNoLock(t *testing.T) {
	dir := t.TempDir()
	body := "gem 'datadog', '2.42.0'\n"
	f := write(t, dir, "Gemfile", body)
	argsFile := stubBundle(t, dir)
	p := &Pin{ID: "dd", Kind: KindBundler, File: f, Packages: []string{"datadog"}}
	if err := p.Check(); err == nil || !strings.Contains(err.Error(), "Gemfile.lock") {
		t.Fatalf("Check without a lock: got %v, want an error naming it", err)
	}
	if _, err := p.Write(context.Background(), "2.41.0", nil); err == nil || !strings.Contains(err.Error(), "Gemfile.lock") {
		t.Fatalf("Write without a lock: got %v, want an error naming it", err)
	}
	if read(t, f) != body {
		t.Fatal("a failed Write changed the Gemfile")
	}
	if _, err := os.Stat(argsFile); err == nil {
		t.Fatalf("bundle ran without a lock: %s", read(t, argsFile))
	}
	// A lock that does not list the pinned gem at all.
	write(t, dir, "Gemfile.lock", "GEM\n  specs:\n    sinatra (4.2.1)\n      datadog (2.42.0)\n\nDEPENDENCIES\n  datadog (= 2.42.0)\n")
	if err := p.Check(); err == nil || !strings.Contains(err.Error(), "does not list it") {
		t.Fatalf("Check with a lock that lacks the gem: got %v", err)
	}
}

// A version Bundler cannot resolve is an error naming the command. Nothing
// is tried a second time: resolving the whole Gemfile again would move the
// application's gems inside a tracer change.
func TestBundlerLockFails(t *testing.T) {
	dir := t.TempDir()
	f := write(t, dir, "Gemfile", "gem 'opentelemetry-sdk', '1.13.1'\ngem 'opentelemetry-instrumentation-rack', '0.31.2'\n")
	write(t, dir, "Gemfile.lock", "GEM\n  specs:\n    opentelemetry-sdk (1.13.1)\n")
	argsFile := stubBundle(t, dir)
	p := &Pin{ID: "otel", Kind: KindBundler, File: f, Packages: []string{"opentelemetry-sdk"}}

	write(t, dir, "bundle.fail", "lock --update opentelemetry-sdk\n")
	if _, err := p.Write(context.Background(), "1.2.1", nil); err == nil || !strings.Contains(err.Error(), "bundle lock --update opentelemetry-sdk") {
		t.Fatalf("Write of a version that does not resolve: got %v, want the command that failed", err)
	}
	if got, want := read(t, argsFile), "lock --update opentelemetry-sdk\n"; got != want {
		t.Fatalf("bundle ran with %q, want %q", got, want)
	}

	// No bundle on PATH is an error too, not a Gemfile moved without its lock.
	t.Setenv("PATH", t.TempDir())
	if _, err := p.Write(context.Background(), "1.4.0", nil); err == nil || !strings.Contains(err.Error(), "bundle lock --update opentelemetry-sdk") {
		t.Fatalf("Write without bundle on PATH: got %v, want the command that could not start", err)
	}
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
	if err := p.Check(); err != nil {
		t.Fatalf("Check with one copy: %v", err)
	}
}

// Two copies of a crate in Cargo.lock (another dependency requires a
// version the relay's requirement does not admit): Read resolves through
// an exact requirement, refuses a loose one, and Check refuses both.
func TestCargoTwoCopies(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Cargo.lock", "[[package]]\nname = \"opentelemetry\"\nversion = \"0.32.0\"\n\n[[package]]\nname = \"opentelemetry\"\nversion = \"0.33.1\"\n\n[[package]]\nname = \"other\"\nversion = \"1.0.0\"\n")
	p := &Pin{ID: "otel", Kind: KindCargo, Dir: dir, Packages: []string{"opentelemetry"}}

	write(t, dir, "Cargo.toml", "[dependencies]\nopentelemetry = \"=0.33.1\"\n")
	if got, err := p.Read(); err != nil || got != "0.33.1" {
		t.Fatalf("Read with exact requirement = %q, %v; want 0.33.1", got, err)
	}
	if err := p.Check(); err == nil || !strings.Contains(err.Error(), "0.32.0 and 0.33.1") {
		t.Fatalf("Check with two copies: got %v, want an error naming both", err)
	}

	write(t, dir, "Cargo.toml", "[dependencies]\nopentelemetry = \"0.33\"\n")
	if _, err := p.Read(); err == nil || !strings.Contains(err.Error(), "exact") {
		t.Fatalf("Read with a loose requirement and two copies: got %v, want an error asking for an exact one", err)
	}

	write(t, dir, "Cargo.toml", "[dependencies]\nopentelemetry = \"=0.31.0\"\n")
	if _, err := p.Read(); err == nil {
		t.Fatal("Read with an exact requirement matching neither copy: want an error")
	}
}

// A pin with follow: Write sets the pinned crate exact, gives the followed
// crates the requirement the release declares on their dependency, and
// moves them all in one `cargo update`; Check covers the followed crates.
func TestCargoFollow(t *testing.T) {
	dir := t.TempDir()
	toml := write(t, dir, "Cargo.toml", "[dependencies]\ndatadog-opentelemetry = \"=0.6.0\"\nopentelemetry = \"=0.33.0\"\nopentelemetry-http = { version = \"=0.33.0\", default-features = false }\n")
	write(t, dir, "Cargo.lock", "[[package]]\nname = \"datadog-opentelemetry\"\nversion = \"0.6.0\"\n\n[[package]]\nname = \"opentelemetry\"\nversion = \"0.33.0\"\n\n[[package]]\nname = \"opentelemetry-http\"\nversion = \"0.33.0\"\n")
	argsFile := stubCargo(t, dir)
	p := &Pin{
		ID: "datadog-rust", Kind: KindCargo, Dir: dir,
		Packages: []string{"datadog-opentelemetry"},
		Follow:   map[string][]string{"opentelemetry": {"opentelemetry", "opentelemetry-http"}},
		Source:   registry.Package{Registry: "crates", Package: "datadog-opentelemetry"},
	}
	if err := p.Check(); err != nil {
		t.Fatalf("Check with one copy of each: %v", err)
	}

	var asked []string
	reqs := func(_ context.Context, pkg registry.Package, version string) (map[string]string, error) {
		asked = append(asked, pkg.Registry+"/"+pkg.Package+"@"+version)
		return map[string]string{"opentelemetry": "^0.32.0", "opentelemetry_sdk": "^0.32.1"}, nil
	}
	files, err := p.Write(context.Background(), "v0.5.4", reqs)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if want := []string{"crates/datadog-opentelemetry@0.5.4"}; strings.Join(asked, ",") != strings.Join(want, ",") {
		t.Fatalf("requirements asked for %v, want %v", asked, want)
	}
	if len(files) != 2 || files[0] != toml {
		t.Fatalf("Write files = %v", files)
	}
	if got, want := read(t, toml), "[dependencies]\ndatadog-opentelemetry = \"=0.5.4\"\nopentelemetry = \"^0.32.0\"\nopentelemetry-http = { version = \"^0.32.0\", default-features = false }\n"; got != want {
		t.Fatalf("Cargo.toml after write:\n%s\nwant:\n%s", got, want)
	}
	if got, want := strings.TrimSpace(read(t, argsFile)), "update -p datadog-opentelemetry@0.6.0 -p opentelemetry@0.33.0 -p opentelemetry-http@0.33.0"; got != want {
		t.Fatalf("cargo ran with %q, want %q", got, want)
	}

	// No lookup, or a release that does not depend on the followed crate:
	// an error before anything is written.
	before := read(t, toml)
	if _, err := p.Write(context.Background(), "0.5.3", nil); err == nil || !strings.Contains(err.Error(), "follow") {
		t.Fatalf("Write without a lookup: got %v, want an error about follow", err)
	}
	none := func(context.Context, registry.Package, string) (map[string]string, error) {
		return map[string]string{"serde": "^1"}, nil
	}
	if _, err := p.Write(context.Background(), "0.5.3", none); err == nil || !strings.Contains(err.Error(), "does not depend on opentelemetry") {
		t.Fatalf("Write for a release without the dependency: got %v", err)
	}
	if read(t, toml) != before {
		t.Fatal("a failed Write changed Cargo.toml")
	}

	// A second copy of a followed crate fails Check like a pinned one.
	write(t, dir, "Cargo.lock", "[[package]]\nname = \"datadog-opentelemetry\"\nversion = \"0.6.0\"\n\n[[package]]\nname = \"opentelemetry\"\nversion = \"0.33.0\"\n\n[[package]]\nname = \"opentelemetry-http\"\nversion = \"0.32.0\"\n\n[[package]]\nname = \"opentelemetry-http\"\nversion = \"0.33.0\"\n")
	if err := p.Check(); err == nil || !strings.Contains(err.Error(), "opentelemetry-http: Cargo.lock holds 0.32.0 and 0.33.0") {
		t.Fatalf("Check with two copies of a followed crate: got %v", err)
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
  - id: datadog-rust
    kind: cargo
    dir: relays/rust/datadog
    packages: [datadog-opentelemetry]
    follow:
      opentelemetry: [opentelemetry, opentelemetry-http]
    source: { registry: crates, package: datadog-opentelemetry }
    variants: [datadog-rust]
`)
	m, err := Load(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Pins) != 4 || m.Pins[2].Source.TagPrefix != "v" {
		t.Fatalf("Load = %+v", m.Pins)
	}
	if got := m.Pins[3].cargoCrates(); strings.Join(got, " ") != "datadog-opentelemetry opentelemetry opentelemetry-http" {
		t.Fatalf("cargoCrates = %v", got)
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
		// follow is cargo-only, needs crates, and cannot name a pinned package.
		"follow-kind":    "pins:\n  - {id: a, kind: arg, file: f, arg: X, follow: {x: [y]}, source: {registry: r, package: p}}\n",
		"follow-empty":   "pins:\n  - {id: a, kind: cargo, dir: d, packages: [x], follow: {y: []}, source: {registry: r, package: p}}\n",
		"follow-overlap": "pins:\n  - {id: a, kind: cargo, dir: d, packages: [x, y], follow: {z: [y]}, source: {registry: r, package: p}}\n",
		// companions is gomod-only and cannot name a pinned module.
		"companions-kind":    "pins:\n  - {id: a, kind: cargo, dir: d, packages: [x], companions: [y], source: {registry: r, package: p}}\n",
		"companions-overlap": "pins:\n  - {id: a, kind: gomod, dir: d, modules: [x, y], companions: [y], source: {registry: r, package: p}}\n",
	} {
		f := write(t, dir, name+".yaml", body)
		if _, err := Load(f); err == nil {
			t.Errorf("%s: expected Load error", name)
		}
	}
}

func TestBacktestPolicyAllows(t *testing.T) {
	var none *BacktestPolicy
	if !none.Allows("0.0.1") {
		t.Error("nil policy must allow everything")
	}
	p := &BacktestPolicy{Min: "1.0.1", Skip: []string{"1.2.0", "v1.3.0"}}
	for v, want := range map[string]bool{"1.0.0": false, "0.5.0": false, "1.0.1": true, "1.1.0": true, "1.2.0": false, "v1.2.0": false, "1.3.0": false, "1.4.0": true} {
		if got := p.Allows(v); got != want {
			t.Errorf("Allows(%s) = %v, want %v", v, got, want)
		}
	}
	// The repo's pins.yaml parses with its policies.
	m, err := Load("../../pins.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"otelc", "otel-rust"} {
		found := false
		for _, p := range m.Pins {
			if p.ID == id && p.Backtest != nil {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: expected a backtest policy in pins.yaml", id)
		}
	}
}
