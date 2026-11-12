package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/signadot/context-propagation-matrix/internal/pins"
	"github.com/signadot/context-propagation-matrix/internal/registry"
	"github.com/signadot/context-propagation-matrix/internal/results"
	"github.com/signadot/context-propagation-matrix/internal/versions"
)

// trySpec is a user's stack: one registered variant as the base, with the
// tracer version, library versions and relay environment they actually run.
//
//	# mystack.yaml
//	variant: datadog-python        # or tool + language (+ flavour)
//	tracer: 4.16.4                 # optional: tracer/agent version
//	libs:                          # optional: package -> version, in the relay's manifests
//	  confluent-kafka: 2.15.0
//	env:                           # optional: extra relay environment
//	  DD_KAFKA_PROPAGATION_ENABLED: "true"
//	protocols: [http, kafka]       # optional: only report boundaries touching these
type trySpec struct {
	Name      string            `yaml:"name"`
	Variant   string            `yaml:"variant"`
	Tool      string            `yaml:"tool"`
	Language  string            `yaml:"language"`
	Flavour   string            `yaml:"flavour"`
	Tracer    string            `yaml:"tracer"`
	Libs      map[string]string `yaml:"libs"`
	Env       map[string]string `yaml:"env"`
	Protocols []string          `yaml:"protocols"`
}

func parseTrySpec(b []byte) (*trySpec, error) {
	var s trySpec
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("stack file: %w", err)
	}
	if s.Variant == "" && (s.Tool == "" || s.Language == "") {
		return nil, fmt.Errorf("stack file: set variant, or tool and language")
	}
	for k, v := range s.Libs {
		if strings.TrimSpace(k) == "" || !versionish.MatchString(v) {
			return nil, fmt.Errorf("stack file: libs.%s: %q is not a version", k, v)
		}
	}
	if s.Tracer != "" && !versionish.MatchString(s.Tracer) {
		return nil, fmt.Errorf("stack file: tracer: %q is not a version", s.Tracer)
	}
	for k := range s.Env {
		if !envName.MatchString(k) {
			return nil, fmt.Errorf("stack file: env: %q is not a valid variable name", k)
		}
	}
	return &s, nil
}

var (
	versionish = regexp.MustCompile(`^v?[0-9][0-9A-Za-z.+_-]*$`)
	envName    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// resolve picks the base variant: by id, or by tool + language + flavour
// (flavour defaults to "default").
func (s *trySpec) resolve(reg *registry.Registry) (*registry.Variant, error) {
	if s.Variant != "" {
		v := reg.Variant(s.Variant)
		if v == nil {
			return nil, fmt.Errorf("unknown variant %q (see: cpm list)", s.Variant)
		}
		return v, nil
	}
	flavour := s.Flavour
	if flavour == "" {
		flavour = "default"
	}
	var ids []string
	for _, v := range reg.SortedVariants() {
		if v.Tool == s.Tool && v.Language == s.Language {
			if v.Flavour == flavour {
				return v, nil
			}
			ids = append(ids, v.ID)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no variant for tool %q and language %q (see: cpm list)", s.Tool, s.Language)
	}
	return nil, fmt.Errorf("no %q flavour of %s/%s; registered: %s", flavour, s.Tool, s.Language, strings.Join(ids, ", "))
}

// libManifests lists the manifests a library version for v may live in:
// the variant's own source directory and the shared app manifests at the
// relay root (relays/node/package.json, relays/python/requirements.txt).
// Shared directories hold every tool's files, so other tools' requirement
// files are left out.
func libManifests(root string, v *registry.Variant) []string {
	parts := strings.Split(filepath.ToSlash(v.Source), "/")
	dirs := []string{v.Source}
	if len(parts) > 2 && parts[0] == "relays" {
		dirs = append(dirs, filepath.Join(parts[0], parts[1]))
	}
	seen := map[string]bool{}
	var out []string
	for _, d := range dirs {
		entries, err := os.ReadDir(filepath.Join(root, d))
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !isManifest(name, v.Tool) {
				continue
			}
			p := filepath.Join(d, name)
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

func isManifest(name, tool string) bool {
	switch {
	case name == "requirements.txt", name == "requirements-"+tool+".txt":
		return true
	case strings.HasPrefix(name, "requirements"):
		return false
	case name == "package.json", name == "Gemfile", name == "go.mod", name == "Cargo.toml",
		name == "pom.xml", name == "composer.json", strings.HasSuffix(name, ".csproj"):
		return true
	}
	return false
}

// libPin describes where lib's version sits in one manifest, as a pin the
// pins package can read and rewrite. It returns nil when the manifest does
// not pin lib.
func libPin(root, file, lib string) *pins.Pin {
	id := "lib:" + lib
	base := filepath.Base(file)
	var p *pins.Pin
	switch {
	case strings.HasPrefix(base, "requirements"):
		p = &pins.Pin{ID: id, Kind: pins.KindPip, File: file, Packages: []string{lib}}
	case base == "package.json":
		p = &pins.Pin{ID: id, Kind: pins.KindNPM, File: file, Packages: []string{lib}}
	case base == "Gemfile":
		p = &pins.Pin{ID: id, Kind: pins.KindBundler, File: file, Packages: []string{lib}}
	case base == "go.mod":
		p = &pins.Pin{ID: id, Kind: pins.KindGoMod, Dir: filepath.Dir(file), Modules: []string{lib}}
	case base == "Cargo.toml":
		p = &pins.Pin{ID: id, Kind: pins.KindCargo, Dir: filepath.Dir(file), Packages: []string{lib}}
	case strings.HasSuffix(base, ".csproj"):
		p = &pins.Pin{ID: id, Kind: pins.KindArg, File: file,
			Pattern: `<PackageReference\s+Include="` + regexp.QuoteMeta(lib) + `"\s+Version="(?<version>[^"]+)"`}
	case base == "composer.json":
		p = &pins.Pin{ID: id, Kind: pins.KindArg, File: file,
			Pattern: `"` + regexp.QuoteMeta(lib) + `"\s*:\s*"(?<version>[^"]+)"`}
	case base == "pom.xml":
		p = pomPin(root, file, lib)
	}
	if p == nil {
		return nil
	}
	if _, err := p.Read(); err != nil {
		return nil
	}
	return p
}

// pomPin finds artifactId lib (or groupId:artifactId) in a pom. A literal
// <version> is rewritten in place; a ${property} version is followed to the
// property, which may be shared with other artifacts (grpc.version).
func pomPin(root, file, lib string) *pins.Pin {
	artifact := lib
	if i := strings.LastIndex(lib, ":"); i >= 0 {
		artifact = lib[i+1:]
	}
	b, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		return nil
	}
	lit := `(?s)<artifactId>` + regexp.QuoteMeta(artifact) + `</artifactId>\s*<version>(?<version>[^<$]+)</version>`
	if regexp.MustCompile(lit).Match(b) {
		return &pins.Pin{ID: "lib:" + lib, Kind: pins.KindArg, File: file, Pattern: lit}
	}
	ref := regexp.MustCompile(`(?s)<artifactId>` + regexp.QuoteMeta(artifact) + `</artifactId>\s*<version>\$\{([^}]+)\}</version>`)
	m := ref.FindSubmatch(b)
	if m == nil {
		return nil
	}
	prop := regexp.QuoteMeta(string(m[1]))
	return &pins.Pin{ID: "lib:" + lib + " (via ${" + string(m[1]) + "})", Kind: pins.KindArg, File: file,
		Pattern: `<` + prop + `>(?<version>[^<]+)</` + prop + `>`}
}

// composeOverride adds env to the relay's environment.
func composeOverride(service string, env map[string]string) ([]byte, error) {
	doc := map[string]any{"services": map[string]any{service: map[string]any{"environment": env}}}
	return yaml.Marshal(doc)
}

// ---------------------------------------------------------------------------
// Comparison

type tryDiff struct {
	Header, Inbound, Outbound string
	Mine, Published           results.Outcome
}

type tryHeader struct {
	ID                                     string
	MinePass, MineOf, PublishedPass, PubOf int
}

type tryReport struct {
	Headers     []tryHeader
	Diffs       []tryDiff
	Regressions int // cells that pass in the published run and not in yours
	Gains       int
}

func touches(c results.Cell, protocols []string) bool {
	if len(protocols) == 0 {
		return true
	}
	for _, p := range protocols {
		if c.Inbound == p || c.Outbound == p {
			return true
		}
	}
	return false
}

func measured(o results.Outcome) bool { return o != "" && o != "undelivered" && o != "not_run" }

// compareTry compares your cells with the published ones (pub may be nil).
func compareTry(headers []string, mine, pub *results.Variant, protocols []string) tryReport {
	var r tryReport
	pubCells := map[string]results.Outcome{}
	if pub != nil {
		for _, c := range pub.Cells {
			pubCells[c.Header+"|"+c.Inbound+"|"+c.Outbound] = c.Outcome
		}
	}
	for _, h := range headers {
		th := tryHeader{ID: h}
		for _, c := range mine.Cells {
			if c.Header != h || !touches(c, protocols) {
				continue
			}
			if measured(c.Outcome) {
				th.MineOf++
				if c.Outcome == "pass" {
					th.MinePass++
				}
			}
			p, ok := pubCells[h+"|"+c.Inbound+"|"+c.Outbound]
			if !ok {
				continue
			}
			if p != c.Outcome && measured(p) && measured(c.Outcome) {
				r.Diffs = append(r.Diffs, tryDiff{h, c.Inbound, c.Outbound, c.Outcome, p})
				if p == "pass" {
					r.Regressions++
				} else if c.Outcome == "pass" {
					r.Gains++
				}
			}
		}
		// The published score is the published variant's own, over its cells.
		if pub != nil {
			for _, c := range pub.Cells {
				if c.Header == h && touches(c, protocols) && measured(c.Outcome) {
					th.PubOf++
					if c.Outcome == "pass" {
						th.PublishedPass++
					}
				}
			}
		}
		r.Headers = append(r.Headers, th)
	}
	return r
}

func (r tryReport) markdown(title, pubLabel string, havePub bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n", title)
	if havePub {
		fmt.Fprintf(&b, "| header | your stack | published (%s) |\n|---|---|---|\n", pubLabel)
	} else {
		b.WriteString("| header | your stack |\n|---|---|\n")
	}
	for _, h := range r.Headers {
		if havePub {
			fmt.Fprintf(&b, "| %s | %d/%d pass | %d/%d pass |\n", h.ID, h.MinePass, h.MineOf, h.PublishedPass, h.PubOf)
		} else {
			fmt.Fprintf(&b, "| %s | %d/%d pass |\n", h.ID, h.MinePass, h.MineOf)
		}
	}
	if !havePub {
		return b.String()
	}
	if len(r.Diffs) == 0 {
		b.WriteString("\nEvery measured cell matches the published result.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "\n%d cell(s) differ from the published result (%d now fail, %d now pass):\n\n", len(r.Diffs), r.Regressions, r.Gains)
	for _, d := range r.Diffs {
		fmt.Fprintf(&b, "- `%s` %s → %s: **%s** (published: %s)\n", d.Header, d.Inbound, d.Outbound, d.Mine, d.Published)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Command

// cmdTry measures a user's stack: the base variant with their tracer and
// library versions and relay environment, in a throwaway git worktree of
// HEAD (the checkout is never modified), then compares it with the latest
// published run.
func cmdTry(args []string) error {
	fs := flag.NewFlagSet("try", flag.ExitOnError)
	outFlag := fs.String("out", "", "where to write report.md, results.json and the raw result (default results/try/<name>)")
	baseline := fs.String("baseline", "", "published results.json to compare with (default: the latest run-* release, via gh)")
	dryRun := fs.Bool("dry-run", false, "apply the stack in the worktree and print the changes; build and run nothing")
	keep := fs.Bool("keep", false, "keep the worktree and the containers afterwards")
	failOnRegression := fs.Bool("fail-on-regression", false, "exit 1 when a cell that passes in the published run does not pass for your stack (CI)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: cpm try <stack.yaml> [-dry-run] [-baseline results.json] [-out dir] [-keep] [-fail-on-regression]")
		fs.PrintDefaults()
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fs.Usage()
		return fmt.Errorf("cpm try needs a stack file")
	}
	specPath := args[0]
	fs.Parse(args[1:])

	b, err := os.ReadFile(specPath)
	if err != nil {
		return err
	}
	spec, err := parseTrySpec(b)
	if err != nil {
		return err
	}
	root, err := gitOut("", "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("cpm try runs inside the harness repository: %w", err)
	}
	reg, err := registry.Load(filepath.Join(root, "variants.yaml"))
	if err != nil {
		return err
	}
	v, err := spec.resolve(reg)
	if err != nil {
		return err
	}
	for _, p := range spec.Protocols {
		if _, ok := reg.Protocols[p]; !ok {
			return fmt.Errorf("protocols: unknown protocol %q", p)
		}
	}
	name := spec.Name
	if name == "" {
		name = v.ID
		if spec.Tracer != "" {
			name += "-" + versions.Strip(spec.Tracer)
		}
	}
	name = regexp.MustCompile(`[^A-Za-z0-9._-]+`).ReplaceAllString(name, "-")
	out := *outFlag
	if out == "" {
		out = filepath.Join(root, "results", "try", name)
	}
	out, _ = filepath.Abs(out)
	if *baseline != "" {
		*baseline, _ = filepath.Abs(*baseline)
	}
	if secret := reg.RequiresSecret(v); secret != "" && os.Getenv(secret) == "" && !*dryRun {
		return fmt.Errorf("%s needs %s in the environment", v.ID, secret)
	}

	if dirty, _ := gitOut(root, "status", "--porcelain", "--untracked-files=no"); dirty != "" {
		fmt.Fprintln(os.Stderr, "try: note: uncommitted changes are not included; the stack is applied to HEAD")
	}
	wt, err := os.MkdirTemp("", "cpm-try-")
	if err != nil {
		return err
	}
	if _, err := gitOut(root, "worktree", "add", "--detach", "--quiet", wt, "HEAD"); err != nil {
		return err
	}
	if *keep {
		fmt.Fprintf(os.Stderr, "try: worktree kept at %s\n", wt)
	} else {
		defer func() {
			gitOut(root, "worktree", "remove", "--force", wt)
		}()
	}

	// Apply the stack.
	var changes []string
	if spec.Tracer != "" {
		if v.Tracer == nil {
			return fmt.Errorf("%s has no tracer to pin", v.ID)
		}
		m, err := pins.Load(filepath.Join(wt, pinsFile))
		if err != nil {
			return err
		}
		p, err := m.ForVariant(v.ID, *v.Tracer)
		if err != nil {
			return err
		}
		if err := inDir(wt, func() error {
			from, err := p.Read()
			if err != nil {
				return err
			}
			client := &versions.Client{}
			if _, err := p.Write(context.Background(), spec.Tracer, client.Requirements); err != nil {
				return err
			}
			changes = append(changes, fmt.Sprintf("tracer %s: %s -> %s", v.Tracer.Package, from, versions.Strip(spec.Tracer)))
			return nil
		}); err != nil {
			return err
		}
	}
	libs := make([]string, 0, len(spec.Libs))
	for l := range spec.Libs {
		libs = append(libs, l)
	}
	sort.Strings(libs)
	manifests := libManifests(wt, v)
	for _, lib := range libs {
		want := spec.Libs[lib]
		found := false
		for _, f := range manifests {
			if err := inDir(wt, func() error {
				p := libPin(wt, f, lib)
				if p == nil {
					return nil
				}
				found = true
				from, _ := p.Read()
				if _, err := p.Write(context.Background(), want, nil); err != nil {
					return err
				}
				changes = append(changes, fmt.Sprintf("%s in %s: %s -> %s", strings.TrimPrefix(p.ID, "lib:"), f, from, versions.Strip(want)))
				return nil
			}); err != nil {
				return err
			}
		}
		if !found {
			return fmt.Errorf("libs.%s: not pinned in %s — the %s relay does not use it (a different client library needs relay code; see CONTRIBUTING.md)",
				lib, strings.Join(manifests, ", "), v.ID)
		}
	}
	files := []string{"-f", "docker-compose.yml"}
	if len(spec.Env) > 0 {
		ov, err := composeOverride(v.ComposeService(), spec.Env)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(wt, "compose.try.yml"), ov, 0o644); err != nil {
			return err
		}
		files = append(files, "-f", "compose.try.yml")
		keys := make([]string, 0, len(spec.Env))
		for k := range spec.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			changes = append(changes, fmt.Sprintf("env %s=%s", k, spec.Env[k]))
		}
	}
	fmt.Fprintf(os.Stderr, "try: %s (%s)\n", v.ID, name)
	if len(changes) == 0 {
		fmt.Fprintln(os.Stderr, "try:   no changes: measuring the variant as registered")
	}
	for _, c := range changes {
		fmt.Fprintln(os.Stderr, "try:   "+c)
	}
	if *dryRun {
		diff, _ := gitOut(wt, "diff", "--stat")
		if diff != "" {
			fmt.Fprintln(os.Stderr, diff)
		}
		return nil
	}

	// Measure, as run.sh does, under a compose project of its own.
	project := "cpm-try-" + regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(strings.ToLower(v.ID), "-")
	dc := func(a ...string) error {
		full := append([]string{"compose", "-p", project}, files...)
		return run(wt, "docker", append(full, a...)...)
	}
	for _, d := range []string{"results/raw", "results/data"} {
		os.MkdirAll(filepath.Join(wt, d), 0o755)
	}
	if !*keep {
		// Relays and the driver sit behind compose profiles, which a plain
		// `down` leaves running: name them.
		defer dc("--profile", v.ID, "--profile", "driver", "down", "-v", "--remove-orphans")
	}
	fmt.Fprintln(os.Stderr, "try: starting kafka, rabbitmq and the sink")
	if err := dc("up", "-d", "--build", "--wait", "kafka", "rabbitmq", "sink"); err != nil {
		return err
	}
	if err := dc("build", "driver"); err != nil {
		return err
	}
	for _, topic := range []string{"relay-in-" + v.ID, "sink-in"} {
		dc("exec", "-T", "kafka", "/opt/kafka/bin/kafka-topics.sh", "--bootstrap-server", "localhost:9092",
			"--create", "--if-not-exists", "--topic", topic, "--partitions", "1", "--replication-factor", "1")
	}
	fmt.Fprintf(os.Stderr, "try: building and starting %s\n", v.ComposeService())
	if err := dc("up", "-d", "--build", v.ComposeService()); err != nil {
		return fmt.Errorf("build/start %s: %w", v.ComposeService(), err)
	}
	if reg.RequiresSecret(v) != "" {
		fmt.Fprintln(os.Stderr, "try: priming (the agent connects lazily; first pass discarded)")
		dc("run", "--rm", "driver", "-variant", v.ID)
	}
	if err := dc("run", "--rm", "driver", "-variant", v.ID); err != nil {
		return fmt.Errorf("driver: %w", err)
	}

	// Aggregate in the worktree, then compare.
	var res *results.Results
	if err := inDir(wt, func() error {
		if err := cmdAggregate([]string{"-raw", "results/raw", "-out", "results/try", "-kind", "local",
			"-run-id", "try-" + name, "-notes", "cpm try: " + strings.Join(changes, "; ")}); err != nil {
			return err
		}
		var err error
		res, err = results.Load("results/try/results.json")
		return err
	}); err != nil {
		return err
	}
	mine := findVariant(res, v.ID)
	if mine == nil || mine.Missing {
		return fmt.Errorf("no result for %s", v.ID)
	}
	if spec.Tracer != "" && mine.Tracer != nil && !strings.Contains(mine.Tracer.Version, versions.Strip(spec.Tracer)) {
		fmt.Fprintf(os.Stderr, "try: WARNING: the relay reported tracer %q, requested %s — the pin may not have taken effect\n",
			mine.Tracer.Version, versions.Strip(spec.Tracer))
	}

	var pub *results.Variant
	pubLabel := ""
	pubPath := *baseline
	if pubPath == "" {
		if err := run(wt, "scripts/fetch-latest.sh"); err == nil {
			pubPath = filepath.Join(wt, "results", "latest", "results.json")
		} else {
			fmt.Fprintln(os.Stderr, "try: could not fetch the latest published run; reporting your stack only")
		}
	}
	if pubPath != "" {
		if pr, err := results.Load(pubPath); err == nil {
			if pv := findVariant(pr, v.ID); pv != nil && !pv.Missing {
				pub = pv
				pubLabel = pr.Run.RunID
				if pv.Tracer != nil {
					pubLabel = fmt.Sprintf("%s, %s %s", pr.Run.RunID, pv.Tracer.Package.Package, pv.CanonicalVersion())
				}
			}
		} else {
			fmt.Fprintf(os.Stderr, "try: baseline %s: %v\n", pubPath, err)
		}
	}

	headers := make([]string, 0, len(res.Dimensions.Headers))
	for _, h := range res.Dimensions.Headers {
		headers = append(headers, h.ID)
	}
	report := compareTry(headers, mine, pub, spec.Protocols)
	title := v.ID
	if len(changes) > 0 {
		title += " with " + strings.Join(changes, "; ")
	}
	md := report.markdown(title, pubLabel, pub != nil)

	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for src, dst := range map[string]string{
		filepath.Join(wt, "results", "try", "results.json"): "results.json",
		filepath.Join(wt, "results", "raw", v.ID+".json"):   v.ID + ".raw.json",
	} {
		if b, err := os.ReadFile(src); err == nil {
			os.WriteFile(filepath.Join(out, dst), b, 0o644)
		}
	}
	os.WriteFile(filepath.Join(out, "report.md"), []byte(md), 0o644)
	fmt.Println(md)
	fmt.Fprintf(os.Stderr, "try: wrote %s\n", out)
	if *failOnRegression && report.Regressions > 0 {
		return fmt.Errorf("%d cell(s) pass in the published run but not for your stack", report.Regressions)
	}
	return nil
}

func findVariant(r *results.Results, id string) *results.Variant {
	for i := range r.Variants {
		if r.Variants[i].ID == id {
			return r.Variants[i]
		}
	}
	return nil
}

// inDir runs f with the working directory set to dir (pins paths are
// relative to the repository root).
func inDir(dir string, f func() error) error {
	prev, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(dir); err != nil {
		return err
	}
	defer os.Chdir(prev)
	return f()
}

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(b)), nil
}

func run(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
