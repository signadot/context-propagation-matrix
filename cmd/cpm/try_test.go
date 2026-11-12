package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/signadot/context-propagation-matrix/internal/registry"
	"github.com/signadot/context-propagation-matrix/internal/results"
)

func TestParseTrySpec(t *testing.T) {
	good := `
variant: datadog-python
tracer: 4.16.4
libs: { confluent-kafka: 2.15.0 }
env: { DD_KAFKA_PROPAGATION_ENABLED: "true" }
protocols: [http, kafka]
`
	s, err := parseTrySpec([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if s.Variant != "datadog-python" || s.Tracer != "4.16.4" || s.Libs["confluent-kafka"] != "2.15.0" || len(s.Protocols) != 2 {
		t.Fatalf("parsed %+v", s)
	}
	for name, bad := range map[string]string{
		"no base":       "tracer: 1.0.0\n",
		"unknown field": "variant: otel-go\ntracers: 1.0.0\n",
		"bad version":   "variant: otel-go\ntracer: latest\n",
		"bad lib":       "variant: otel-go\nlibs: { x: '>=1.0' }\n",
		"bad env":       "variant: otel-go\nenv: { 'A-B': x }\n",
	} {
		if _, err := parseTrySpec([]byte(bad)); err == nil {
			t.Errorf("%s: accepted %q", name, bad)
		}
	}
}

func TestTryResolve(t *testing.T) {
	reg, err := registry.Load("../../variants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		spec trySpec
		want string
		err  string
	}{
		"by id":           {trySpec{Variant: "otel-go"}, "otel-go", ""},
		"default flavour": {trySpec{Tool: "datadog", Language: "python"}, "datadog-python", ""},
		"named flavour":   {trySpec{Tool: "otel", Language: "go", Flavour: "compiletime"}, "otel-go-compiletime", ""},
		"unknown id":      {trySpec{Variant: "nope"}, "", "unknown variant"},
		"unknown flavour": {trySpec{Tool: "otel", Language: "go", Flavour: "nope"}, "", "registered: otel-go"},
		"unknown pair":    {trySpec{Tool: "otel", Language: "cobol"}, "", "no variant"},
	}
	for name, c := range cases {
		v, err := c.spec.resolve(reg)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%s: err %v, want %q", name, err, c.err)
			}
			continue
		}
		if err != nil || v.ID != c.want {
			t.Errorf("%s: got %v %v, want %s", name, v, err, c.want)
		}
	}
}

func TestLibManifestsRealTree(t *testing.T) {
	reg, err := registry.Load("../../variants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]string{
		// Shared Python dir: app requirements plus this tool's, not the others'.
		"datadog-python": {"relays/python/requirements-datadog.txt", "relays/python/requirements.txt"},
		// Node: the tool's package.json plus the shared app one.
		"otel-node": {"relays/node/package.json", "relays/node/tools/otel/package.json"},
		"otel-go":   {"relays/go/otel/go.mod"},
		"otel-java": {"relays/java/pom.xml"},
	}
	for id, want := range cases {
		got := libManifests("../..", reg.Variant(id))
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v, want %v", id, got, want)
		}
	}
}

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, body := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestLibPinRewrites(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"r/requirements.txt": "flask==3.1.3\nconfluent-kafka==2.16.1\n",
		"r/package.json":     "{\n  \"dependencies\": {\n    \"kafkajs\": \"2.2.4\"\n  }\n}\n",
		"r/Gemfile":          "gem 'bunny', '3.5.0'\n",
		"r/Relay.csproj":     `<PackageReference Include="Confluent.Kafka" Version="2.16.0" />` + "\n",
		"r/composer.json":    `{"require": {"php-amqplib/php-amqplib": "3.7.5"}}` + "\n",
		"r/pom.xml": `<properties><grpc.version>1.85.0</grpc.version></properties>
<dependency><artifactId>grpc-stub</artifactId>
  <version>${grpc.version}</version></dependency>
<dependency><artifactId>kafka-clients</artifactId><version>4.1.0</version></dependency>
`,
	})
	t.Chdir(root)
	cases := []struct{ file, lib, from, to, wantLine string }{
		{"r/requirements.txt", "confluent-kafka", "2.16.1", "2.15.0", "confluent-kafka==2.15.0"},
		{"r/package.json", "kafkajs", "2.2.4", "2.2.3", `"kafkajs": "2.2.3"`},
		{"r/Gemfile", "bunny", "3.5.0", "3.4.0", "gem 'bunny', '3.4.0'"},
		{"r/Relay.csproj", "Confluent.Kafka", "2.16.0", "2.15.0", `Version="2.15.0"`},
		{"r/composer.json", "php-amqplib/php-amqplib", "3.7.5", "3.7.0", `"php-amqplib/php-amqplib": "3.7.0"`},
		{"r/pom.xml", "kafka-clients", "4.1.0", "4.0.0", "<version>4.0.0</version>"},
		{"r/pom.xml", "io.grpc:grpc-stub", "1.85.0", "1.80.0", "<grpc.version>1.80.0</grpc.version>"},
	}
	for _, c := range cases {
		p := libPin(root, c.file, c.lib)
		if p == nil {
			t.Errorf("%s in %s: not found", c.lib, c.file)
			continue
		}
		if got, _ := p.Read(); got != c.from {
			t.Errorf("%s: read %q, want %q", c.lib, got, c.from)
		}
		if _, err := p.Write(context.Background(), c.to, nil); err != nil {
			t.Errorf("%s: write: %v", c.lib, err)
			continue
		}
		b, _ := os.ReadFile(c.file)
		if !strings.Contains(string(b), c.wantLine) {
			t.Errorf("%s: %s lacks %q:\n%s", c.lib, c.file, c.wantLine, b)
		}
	}
	for _, f := range []string{"r/requirements.txt", "r/package.json", "r/Gemfile", "r/Relay.csproj", "r/composer.json", "r/pom.xml"} {
		if p := libPin(root, f, "not-a-dependency"); p != nil {
			t.Errorf("%s: matched a library it does not pin", f)
		}
	}
}

func TestComposeOverride(t *testing.T) {
	b, err := composeOverride("relay-otel-go", map[string]string{"OTEL_PROPAGATORS": "tracecontext"})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Services map[string]struct {
			Environment map[string]string `yaml:"environment"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Services["relay-otel-go"].Environment["OTEL_PROPAGATORS"] != "tracecontext" {
		t.Fatalf("override %s", b)
	}
}

func cells(header string, outcomes map[string]results.Outcome) []results.Cell {
	var cs []results.Cell
	for b, o := range outcomes {
		in, out, _ := strings.Cut(b, "->")
		cs = append(cs, results.Cell{Header: header, Inbound: in, Outbound: out, Boundary: b, Outcome: o})
	}
	return cs
}

func TestCompareTry(t *testing.T) {
	mine := &results.Variant{ID: "x", Cells: cells("baggage", map[string]results.Outcome{
		"http->http":   "pass",
		"http->kafka":  "fail",      // regression
		"kafka->http":  "pass",      // gain
		"grpc->grpc":   "not_run",   // not measured: ignored
		"kafka->kafka": "new_trace", // same as published
	})}
	pub := &results.Variant{ID: "x", Cells: cells("baggage", map[string]results.Outcome{
		"http->http":   "pass",
		"http->kafka":  "pass",
		"kafka->http":  "fail",
		"grpc->grpc":   "pass",
		"kafka->kafka": "new_trace",
	})}
	r := compareTry([]string{"baggage"}, mine, pub, nil)
	if r.Regressions != 1 || r.Gains != 1 || len(r.Diffs) != 2 {
		t.Fatalf("report %+v", r)
	}
	h := r.Headers[0]
	if h.MinePass != 2 || h.MineOf != 4 || h.PublishedPass != 3 || h.PubOf != 5 {
		t.Fatalf("counts %+v", h)
	}
	md := r.markdown("x", "run-1", true)
	if !strings.Contains(md, "1 now fail, 1 now pass") || !strings.Contains(md, "http → kafka: **fail** (published: pass)") {
		t.Fatalf("markdown:\n%s", md)
	}

	// Protocol filter: only boundaries touching http.
	r = compareTry([]string{"baggage"}, mine, pub, []string{"http"})
	if r.Headers[0].MineOf != 3 || len(r.Diffs) != 2 {
		t.Fatalf("filtered %+v", r)
	}
	// No baseline: counts only.
	r = compareTry([]string{"baggage"}, mine, nil, nil)
	if len(r.Diffs) != 0 || !strings.Contains(r.markdown("x", "", false), "2/4 pass") {
		t.Fatalf("no baseline %+v", r)
	}
}
