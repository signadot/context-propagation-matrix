// Command cpm is the harness CLI: it reads variants.yaml and turns raw driver
// output into the published results contract.
//
//	cpm list [-tool otel] [-secret|-no-secret] [-json]
//	cpm validate [-results results/latest/results.json]
//	cpm aggregate [-raw results/raw] [-out results/latest] [-run-id id] [-prev results.json]
//	cpm diff <from.json> <to.json> [-out changes.json]
//	cpm summary <results.json> [-changes changes.json]
//	cpm annotations mark-fixed <id> -fixed-in <version>
//	cpm annotations resolved <results.json>
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/signadot/context-propagation-matrix/internal/annotations"
	"github.com/signadot/context-propagation-matrix/internal/raw"
	"github.com/signadot/context-propagation-matrix/internal/registry"
	"github.com/signadot/context-propagation-matrix/internal/render"
	"github.com/signadot/context-propagation-matrix/internal/results"
)

const harnessRepo = "signadot/context-propagation-matrix"

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "list":
		err = cmdList(os.Args[2:])
	case "validate":
		err = cmdValidate(os.Args[2:])
	case "aggregate":
		err = cmdAggregate(os.Args[2:])
	case "diff":
		err = cmdDiff(os.Args[2:])
	case "summary":
		err = cmdSummary(os.Args[2:])
	case "coords":
		err = cmdCoords(os.Args[2:])
	case "annotations":
		err = cmdAnnotations(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: cpm <list|validate|aggregate|diff|summary> [flags]

  list       print variant ids from variants.yaml (one per line, or -json)
  validate   check variants.yaml, docker-compose.yml and annotations/ agree
  aggregate  results/raw/*.json -> results.json + MATRIX.md (+ changes.json)
  diff       compare two results.json files into changes.json
  summary    markdown summary of a results.json for CI/Slack
  coords     tracer package coordinates per variant as JSON (for version discovery)
  annotations mark-fixed <id> -fixed-in <version>   record an upstream fix in annotations/
  annotations resolved <results.json>              list resolved-but-unmarked annotations (id<TAB>version)`)
	os.Exit(2)
}

// ---------------------------------------------------------------------------

func cmdAnnotations(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cpm annotations <mark-fixed|resolved> ...")
	}
	switch args[0] {
	case "mark-fixed":
		fs := flag.NewFlagSet("annotations mark-fixed", flag.ExitOnError)
		dir := fs.String("annotations", "annotations", "annotations directory")
		ver := fs.String("fixed-in", "", "tracer version the fix shipped in (required)")
		// Allow `mark-fixed <id> -fixed-in v` as well as flags-first: the id
		// is the single positional argument wherever it appears.
		id, rest := "", []string{}
		for i := 1; i < len(args); i++ {
			if !strings.HasPrefix(args[i], "-") && id == "" {
				id = args[i]
				continue
			}
			rest = append(rest, args[i])
			if (args[i] == "-fixed-in" || args[i] == "--fixed-in" || args[i] == "-annotations" || args[i] == "--annotations") && i+1 < len(args) {
				i++
				rest = append(rest, args[i])
			}
		}
		fs.Parse(rest)
		if id == "" || fs.NArg() != 0 || *ver == "" {
			return fmt.Errorf("usage: cpm annotations mark-fixed <id> -fixed-in <version>")
		}
		file, err := annotations.MarkFixed(*dir, id, *ver)
		if err != nil {
			return err
		}
		fmt.Printf("%s: %s -> status: fixed, fixed_in: %s\n", file, id, *ver)
		return nil
	case "resolved":
		// For automation: annotations the aggregator marked resolved whose
		// YAML does not yet say fixed. One "id<TAB>fixed_in" per line.
		fs := flag.NewFlagSet("annotations resolved", flag.ExitOnError)
		fs.Parse(args[1:])
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: cpm annotations resolved <results.json>")
		}
		res, err := results.Load(fs.Arg(0))
		if err != nil {
			return err
		}
		for _, a := range res.Annotations {
			if a.Resolved && a.Status != "fixed" {
				fmt.Printf("%s\t%s\n", a.ID, a.FixedIn)
			}
		}
		return nil
	}
	return fmt.Errorf("unknown annotations subcommand %q", args[0])
}

func regFlag(fs *flag.FlagSet) *string {
	return fs.String("registry", "variants.yaml", "path to variants.yaml")
}

// ---------------------------------------------------------------------------

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	regPath := regFlag(fs)
	tool := fs.String("tool", "", "only variants of this tool")
	lang := fs.String("language", "", "only variants of this language")
	secret := fs.Bool("secret", false, "only variants whose tool requires a secret")
	noSecret := fs.Bool("no-secret", false, "only variants that need no secret")
	asJSON := fs.Bool("json", false, "emit a JSON array (for GitHub Actions matrices)")
	fs.Parse(args)

	reg, err := registry.Load(*regPath)
	if err != nil {
		return err
	}
	var ids []string
	for _, v := range reg.Variants {
		if *tool != "" && v.Tool != *tool {
			continue
		}
		if *lang != "" && v.Language != *lang {
			continue
		}
		needs := reg.RequiresSecret(v) != ""
		if *secret && !needs || *noSecret && needs {
			continue
		}
		ids = append(ids, v.ID)
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(ids)
	}
	for _, id := range ids {
		fmt.Println(id)
	}
	return nil
}

// ---------------------------------------------------------------------------

// cmdCoords prints, for every instrumented variant, the tracer (and weaver)
// package coordinates plus the version observed in the latest results, so
// versions/discover.mjs can enumerate back-test candidates without parsing
// YAML.
func cmdCoords(args []string) error {
	fs := flag.NewFlagSet("coords", flag.ExitOnError)
	regPath := regFlag(fs)
	resPath := fs.String("results", "results/latest/results.json", "results.json for current versions (optional)")
	fs.Parse(args)

	reg, err := registry.Load(*regPath)
	if err != nil {
		return err
	}
	current := map[string]string{}
	if res, err := results.Load(*resPath); err == nil {
		for _, v := range res.Variants {
			if v.Tracer != nil {
				current[v.ID] = v.Tracer.Version
			}
		}
	}
	type coord struct {
		Variant  string            `json:"variant"`
		Tool     string            `json:"tool"`
		Language string            `json:"language"`
		Tracer   *registry.Package `json:"tracer"`
		Weaver   *registry.Package `json:"weaver,omitempty"`
		Current  string            `json:"current_version,omitempty"`
		Secret   string            `json:"requires_secret,omitempty"`
	}
	var out []coord
	for _, v := range reg.SortedVariants() {
		if v.Tracer == nil {
			continue
		}
		out = append(out, coord{v.ID, v.Tool, v.Language, v.Tracer, v.Weaver, current[v.ID], reg.RequiresSecret(v)})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	return enc.Encode(out)
}

// ---------------------------------------------------------------------------

type composeFile struct {
	Services map[string]struct {
		Image    string   `yaml:"image"`
		Profiles []string `yaml:"profiles"`
	} `yaml:"services"`
}

func loadCompose(path string) (*composeFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c composeFile
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

func brokerVersions(c *composeFile) map[string]string {
	out := map[string]string{}
	for _, name := range []string{"kafka", "rabbitmq"} {
		if s, ok := c.Services[name]; ok && s.Image != "" {
			out[name] = s.Image
		}
	}
	return out
}

func cmdValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	regPath := regFlag(fs)
	composePath := fs.String("compose", "docker-compose.yml", "compose file")
	annDir := fs.String("annotations", "annotations", "annotations directory")
	resPath := fs.String("results", "results/latest/results.json", "results.json to validate annotations against (skipped if missing)")
	strict := fs.Bool("strict", false, "treat annotation warnings as errors")
	fs.Parse(args)

	reg, err := registry.Load(*regPath)
	if err != nil {
		return err
	}
	fmt.Printf("registry: %d variants, %d tools, %d languages, %d boundaries, %d headers\n",
		len(reg.Variants), len(reg.Tools), len(reg.Languages), len(reg.Boundaries), len(reg.Headers))

	var errs []string
	compose, err := loadCompose(*composePath)
	if err != nil {
		return err
	}
	for _, v := range reg.Variants {
		svc, ok := compose.Services[v.ComposeService()]
		if !ok {
			errs = append(errs, fmt.Sprintf("compose: missing service %s for variant %s", v.ComposeService(), v.ID))
			continue
		}
		if len(svc.Profiles) != 1 || svc.Profiles[0] != v.ID {
			errs = append(errs, fmt.Sprintf("compose: service %s must have profiles: [%s], got %v", v.ComposeService(), v.ID, svc.Profiles))
		}
		if _, err := os.Stat(filepath.Dir(*regPath) + "/" + v.Source); err != nil {
			errs = append(errs, fmt.Sprintf("%s: source dir %s does not exist", v.ID, v.Source))
		}
	}
	for name := range compose.Services {
		if strings.HasPrefix(name, "relay-") && reg.Variant(strings.TrimPrefix(name, "relay-")) == nil {
			errs = append(errs, fmt.Sprintf("compose: service %s has no entry in variants.yaml", name))
		}
	}
	fmt.Printf("compose: %d relay services checked\n", len(reg.Variants))

	anns, err := annotations.LoadDir(*annDir)
	if err != nil {
		return err
	}
	fmt.Printf("annotations: %d loaded\n", len(anns))
	if res, err := results.Load(*resPath); err == nil {
		problems := annotations.Apply(res, anns)
		for _, p := range problems {
			fmt.Println("  " + p.String())
			if p.Fatal || *strict {
				errs = append(errs, p.String())
			}
		}
		fmt.Printf("annotations: matched against %s (%d problems)\n", *resPath, len(problems))
		resolved := 0
		for _, a := range res.Annotations {
			if a.Resolved {
				resolved++
			}
		}
		if resolved > 0 {
			fmt.Printf("annotations: %d resolved (every described cell now passes)\n", resolved)
		}
		// Coverage: failing cells nobody has explained yet. Informational —
		// a new variant legitimately arrives unexplained — but surfaced here
		// and in the Slack summary so an explanation gets written.
		if un := annotations.Unexplained(res); len(un) > 0 {
			fmt.Printf("annotations: %d non-pass cell(s) have no annotation:\n", len(un))
			for _, line := range unexplainedLines(un, 20) {
				fmt.Println("  " + line)
			}
		} else {
			fmt.Println("annotations: every non-pass cell is explained")
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		fmt.Printf("annotations: %s not found, selector check skipped\n", *resPath)
	}

	if len(errs) > 0 {
		return fmt.Errorf("%d validation error(s):\n  - %s", len(errs), strings.Join(errs, "\n  - "))
	}
	fmt.Println("ok")
	return nil
}

// ---------------------------------------------------------------------------

func cmdAggregate(args []string) error {
	fs := flag.NewFlagSet("aggregate", flag.ExitOnError)
	regPath := regFlag(fs)
	rawDir := fs.String("raw", "results/raw", "raw results directory")
	outDir := fs.String("out", "results/latest", "output directory (results.json, MATRIX.md, changes.json)")
	runID := fs.String("run-id", "", "run id (default: run-YYYY-MM-DD from the latest raw result)")
	kind := fs.String("kind", "local", "run kind: matrix | backtest | local")
	commit := fs.String("commit", os.Getenv("GITHUB_SHA"), "harness commit sha")
	notes := fs.String("notes", "", "free-text run notes")
	composePath := fs.String("compose", "docker-compose.yml", "compose file (for broker versions)")
	annDir := fs.String("annotations", "annotations", "annotations directory")
	prev := fs.String("prev", "", "previous results.json to diff against (writes changes.json)")
	legacyMD := fs.String("matrix-md", "", "also write MATRIX.md to this path (legacy location)")
	fs.Parse(args)

	reg, err := registry.Load(*regPath)
	if err != nil {
		return err
	}
	raws, err := raw.LoadDir(*rawDir)
	if err != nil {
		return err
	}
	run := results.Run{Kind: *kind, HarnessRepo: harnessRepo, HarnessCommit: *commit, Notes: *notes,
		Generator: "cpm aggregate (schema v" + fmt.Sprint(results.SchemaVersion) + ")"}
	if c, err := loadCompose(*composePath); err == nil {
		run.BrokerVersions = brokerVersions(c)
	}
	res, err := results.Build(reg, raws, run)
	if err != nil {
		return err
	}
	if *runID == "" {
		*runID = "run-" + res.Run.FinishedAt.UTC().Format("2006-01-02")
	}
	res.Run.RunID = *runID

	if anns, err := annotations.LoadDir(*annDir); err != nil {
		return err
	} else if len(anns) > 0 {
		for _, p := range annotations.Apply(res, anns) {
			fmt.Fprintln(os.Stderr, p.String())
			if p.Fatal {
				return fmt.Errorf("annotation validation failed")
			}
		}
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	if err := res.Write(filepath.Join(*outDir, "results.json")); err != nil {
		return err
	}
	md := render.Markdown(res)
	if err := os.WriteFile(filepath.Join(*outDir, "MATRIX.md"), []byte(md), 0o644); err != nil {
		return err
	}
	if *legacyMD != "" {
		if err := os.WriteFile(*legacyMD, []byte(md), 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("wrote %s/results.json and MATRIX.md (%s: %d variants, %d hops, %d cells)\n",
		*outDir, res.Run.RunID, res.Summary.VariantCount, res.Summary.HopCount, res.Summary.CellCount)

	if *prev != "" {
		from, err := results.Load(*prev)
		if err != nil {
			return err
		}
		ch := results.Diff(from, res)
		if err := ch.Write(filepath.Join(*outDir, "changes.json")); err != nil {
			return err
		}
		fmt.Printf("wrote %s/changes.json (%d cells changed: %d improved, %d regressed; %d version bumps)\n",
			*outDir, len(ch.ChangedCells), ch.Improved, ch.Regressed, len(ch.VersionBumps))
	}
	return nil
}

// ---------------------------------------------------------------------------

func cmdDiff(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	out := fs.String("out", "", "write changes.json here (default: stdout)")
	fs.Parse(args)
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: cpm diff <from.json> <to.json> [-out changes.json]")
	}
	from, err := results.Load(fs.Arg(0))
	if err != nil {
		return err
	}
	to, err := results.Load(fs.Arg(1))
	if err != nil {
		return err
	}
	ch := results.Diff(from, to)
	if *out != "" {
		return ch.Write(*out)
	}
	return json.NewEncoder(os.Stdout).Encode(ch)
}

// ---------------------------------------------------------------------------

func cmdSummary(args []string) error {
	fs := flag.NewFlagSet("summary", flag.ExitOnError)
	changesPath := fs.String("changes", "", "changes.json to include")
	siteURL := fs.String("site", "https://contextpropagation.io", "site base URL for permalinks")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: cpm summary <results.json> [-changes changes.json]")
	}
	res, err := results.Load(fs.Arg(0))
	if err != nil {
		return err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Context propagation matrix — %s\n\n", res.Run.RunID)
	fmt.Fprintf(&sb, "%d variants · %d hops · %d cells · finished %s\n\n",
		res.Summary.VariantCount, res.Summary.HopCount, res.Summary.CellCount, res.Run.FinishedAt.UTC().Format(time.RFC3339))
	if len(res.Summary.CanaryLeaks) > 0 {
		fmt.Fprintf(&sb, "**CANARY LEAK — results invalid:** %s\n\n", strings.Join(res.Summary.CanaryLeaks, ", "))
	}
	sb.WriteString("| header | pass | partial | new trace | absent | undelivered |\n|---|---|---|---|---|---|\n")
	for _, h := range res.Dimensions.Headers {
		c := res.Summary.TotalsByHeader[h.ID]
		fmt.Fprintf(&sb, "| %s | %d | %d | %d | %d | %d |\n", h.ID, c.Pass, c.Partial, c.NewTrace, c.Fail, c.Undelivered)
	}
	var missing []string
	for _, v := range res.Variants {
		if v.Missing {
			missing = append(missing, v.ID)
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(&sb, "\nNot run: %s\n", strings.Join(missing, ", "))
	}
	if *changesPath != "" {
		b, err := os.ReadFile(*changesPath)
		if err != nil {
			return err
		}
		var ch results.Changes
		if err := json.Unmarshal(b, &ch); err != nil {
			return err
		}
		fmt.Fprintf(&sb, "\n### Changes since %s\n\n", ch.FromRun)
		if ch.Empty() {
			sb.WriteString("No cell, variant or version changes.\n")
		} else {
			for _, vb := range ch.VersionBumps {
				fmt.Fprintf(&sb, "- %s: %s %s → %s\n", vb.Variant, vb.Package, vb.From, vb.To)
			}
			sort.Slice(ch.ChangedCells, func(i, j int) bool { return ch.ChangedCells[i].CellID < ch.ChangedCells[j].CellID })
			for _, cc := range ch.ChangedCells {
				arrow := "⬇️"
				if cc.Improvement {
					arrow = "⬆️"
				}
				v := res.Variant(cc.Variant)
				link := fmt.Sprintf("%s/results/%s/%s/#%s-%s", *siteURL, v.Tool, v.Language, cc.Header, strings.ReplaceAll(cc.Boundary, "->", "-"))
				fmt.Fprintf(&sb, "- %s [%s](%s): %s → %s\n", arrow, cc.CellID, link, cc.From.Glyph(), cc.To.Glyph())
			}
			for _, a := range ch.AddedVariants {
				fmt.Fprintf(&sb, "- ➕ new variant %s\n", a)
			}
			for _, r := range ch.RemovedVariants {
				fmt.Fprintf(&sb, "- ➖ removed variant %s\n", r)
			}
		}
	}
	var resolved []string
	for _, a := range res.Annotations {
		if a.Resolved {
			resolved = append(resolved, fmt.Sprintf("%s (fixed in %s, status still `%s`)", a.ID, a.FixedIn, a.Status))
		}
	}
	if len(resolved) > 0 {
		sb.WriteString("\n### Annotations whose cells now all pass\n\n")
		for _, r := range resolved {
			fmt.Fprintf(&sb, "- ✅ %s\n", r)
		}
		sb.WriteString("\nRecord `status: fixed` / `fixed_in` in `annotations/` (`cpm annotations mark-fixed <id> -fixed-in <version>`).\n")
	}
	if un := annotations.Unexplained(res); len(un) > 0 {
		fmt.Fprintf(&sb, "\n### %d failing cell(s) without an explanation\n\n", len(un))
		for _, line := range unexplainedLines(un, 12) {
			fmt.Fprintf(&sb, "- %s\n", line)
		}
		sb.WriteString("\nAdd an annotation under `annotations/` so the site can explain them.\n")
	}
	fmt.Print(sb.String())
	return nil
}

// unexplainedLines groups cell ids ("variant/header/in-out") by variant into
// at most max lines: "variant: header/in-out, header/in-out …".
func unexplainedLines(cells []string, max int) []string {
	byVariant := map[string][]string{}
	var order []string
	for _, id := range cells {
		v, rest, ok := strings.Cut(id, "/")
		if !ok {
			v, rest = id, ""
		}
		if _, seen := byVariant[v]; !seen {
			order = append(order, v)
		}
		byVariant[v] = append(byVariant[v], rest)
	}
	var out []string
	for i, v := range order {
		if i == max {
			out = append(out, fmt.Sprintf("… and %d more variant(s)", len(order)-max))
			break
		}
		out = append(out, fmt.Sprintf("%s: %s", v, strings.Join(byVariant[v], ", ")))
	}
	return out
}
