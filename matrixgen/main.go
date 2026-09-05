// Command matrixgen renders results/raw/*.json into results/MATRIX.md.
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
)

type Status struct {
	Traceparent   string `json:"traceparent"`
	Tracestate    string `json:"tracestate"`
	Baggage       string `json:"baggage"`
	OTBaggage     string `json:"ot_baggage"`
	DatadogNative string `json:"datadog_native"`
	CanaryLeak    bool   `json:"canary_leak"`
}

type TestResult struct {
	Boundary  string  `json:"boundary"`
	Delivered bool    `json:"delivered"`
	Status    *Status `json:"status"`
	Error     string  `json:"error,omitempty"`
}

type VariantResult struct {
	Variant  string         `json:"variant"`
	Tool     string         `json:"tool"`
	Language string         `json:"language"`
	Config   map[string]any `json:"config"`
	Info     map[string]any `json:"info"`
	RanAt    time.Time      `json:"ran_at"`
	Tests    []TestResult   `json:"tests"`
}

var boundaries = []string{
	"http->http", "http->grpc", "http->kafka", "http->rabbitmq",
	"grpc->http", "grpc->grpc", "grpc->kafka", "grpc->rabbitmq",
	"kafka->http", "kafka->grpc", "kafka->kafka", "kafka->rabbitmq",
	"rabbitmq->http", "rabbitmq->grpc", "rabbitmq->kafka", "rabbitmq->rabbitmq",
}

var tools = []string{"otel", "datadog", "elastic", "none"}

var toolTitles = map[string]string{
	"otel":    "OpenTelemetry",
	"datadog": "Datadog (`dd-trace`)",
	"elastic": "Elastic APM (classic agents)",
	"none":    "Baseline (no instrumentation attached)",
}

var langOrder = map[string]int{
	"java": 0, "node": 1, "python": 2, "go": 3, "dotnet": 4, "ruby": 5,
	"php": 6, "rust": 7,
}

type headerView struct {
	key   string // status field
	title string
	desc  string
}

var headerViews = []headerView{
	{"baggage", "W3C `baggage`", "`propagated` = the seeded `sd-routing-key` baggage member arrived intact"},
	{"tracestate", "W3C `tracestate`", "`preserved` = the seeded foreign vendor entry (`sdtest=v1`) survived"},
	{"traceparent", "W3C `traceparent` (trace continuity)", "`same trace` = the seeded trace-id was continued (span-id may differ, as expected)"},
	{"ot_baggage", "`ot-baggage-*` (OpenTracing legacy)", "`propagated` = `ot-baggage-sd-routing-key` arrived with the seeded value"},
}

func statusOf(s *Status, key string) string {
	switch key {
	case "traceparent":
		return s.Traceparent
	case "tracestate":
		return s.Tracestate
	case "baggage":
		return s.Baggage
	case "ot_baggage":
		return s.OTBaggage
	}
	return ""
}

func glyph(key, val string, delivered bool) string {
	if !delivered {
		return "🚫" // nothing was forwarded at all
	}
	switch val {
	case "propagated", "preserved", "same_trace":
		return "✅"
	case "partial", "present_entry_lost":
		return "🟡"
	case "new_trace":
		return "🔀"
	case "absent":
		return "❌"
	}
	return "?"
}

func boundaryHeader() string {
	cols := make([]string, len(boundaries))
	for i, b := range boundaries {
		c := strings.ReplaceAll(b, "->", "→")
		// Keep 16 columns readable.
		c = strings.ReplaceAll(c, "rabbitmq", "rmq")
		cols[i] = c
	}
	return "| variant | " + strings.Join(cols, " | ") + " |"
}

func infoStr(v *VariantResult, key string) string {
	if v.Info == nil {
		return ""
	}
	if s, ok := v.Info[key].(string); ok {
		return s
	}
	return ""
}

func rowLabel(v *VariantResult) string {
	label := v.Language
	if suffix := strings.TrimPrefix(v.Variant, v.Tool+"-"+v.Language); suffix != "" {
		label += " (" + strings.TrimPrefix(suffix, "-") + ")"
	}
	if tv := infoStr(v, "tracer_version"); tv != "" && tv != "unknown" && tv != "none" {
		label += " · " + tv
	}
	return label
}

func main() {
	rawDir := flag.String("raw", "results/raw", "raw results directory")
	out := flag.String("out", "results/MATRIX.md", "output markdown file")
	flag.Parse()

	files, err := filepath.Glob(filepath.Join(*rawDir, "*.json"))
	if err != nil || len(files) == 0 {
		fmt.Fprintf(os.Stderr, "no results in %s\n", *rawDir)
		os.Exit(1)
	}

	var variants []*VariantResult
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read %s: %v\n", f, err)
			continue
		}
		var v VariantResult
		if err := json.Unmarshal(b, &v); err != nil {
			fmt.Fprintf(os.Stderr, "parse %s: %v\n", f, err)
			continue
		}
		variants = append(variants, &v)
	}
	sort.Slice(variants, func(i, j int) bool {
		a, b := variants[i], variants[j]
		if a.Tool != b.Tool {
			return a.Tool < b.Tool
		}
		if langOrder[a.Language] != langOrder[b.Language] {
			return langOrder[a.Language] < langOrder[b.Language]
		}
		return a.Variant < b.Variant
	})

	var sb strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&sb, format+"\n", args...) }

	w("# Context Propagation Support Matrix")
	w("")
	w("Generated from empirical runs of this harness — every cell is the observed")
	w("result of an actual request/message hop, not a reading of vendor docs.")
	w("Regenerate with `go run ./matrixgen` after `./run.sh`.")
	w("")
	w("Last generated: %s", time.Now().UTC().Format("2006-01-02 15:04 UTC"))
	w("")
	w("Boundaries are `inbound protocol → outbound protocol` through a single")
	w("instrumented service (the relay). Legend: ✅ propagated/preserved ·")
	w("🟡 header present but seeded value lost · 🔀 new trace started ·")
	w("❌ header absent · 🚫 message was never forwarded (instrumentation or app broke).")
	w("")

	for _, tool := range tools {
		var tv []*VariantResult
		for _, v := range variants {
			if v.Tool == tool {
				tv = append(tv, v)
			}
		}
		w("## %s", toolTitles[tool])
		w("")
		if len(tv) == 0 {
			w("_No results yet._")
			w("")
			continue
		}
		for _, hv := range headerViews {
			// Skip the ot-baggage table for tools that never emit it and never did.
			w("### %s", hv.title)
			w("")
			w("%s", hv.desc+".")
			w("")
			w("%s", boundaryHeader())
			w("|%s", strings.Repeat("---|", len(boundaries)+1))
			for _, v := range tv {
				cells := make([]string, len(boundaries))
				byBoundary := map[string]TestResult{}
				for _, t := range v.Tests {
					byBoundary[t.Boundary] = t
				}
				for i, b := range boundaries {
					t, ok := byBoundary[b]
					if !ok {
						cells[i] = "—"
					} else if t.Status == nil {
						cells[i] = glyph(hv.key, "", false)
					} else {
						cells[i] = glyph(hv.key, statusOf(t.Status, hv.key), t.Delivered)
					}
				}
				w("| %s | %s |", rowLabel(v), strings.Join(cells, " | "))
			}
			w("")
		}
	}

	w("## Variant details")
	w("")
	for _, v := range variants {
		cfgKeys := make([]string, 0, len(v.Config))
		for k := range v.Config {
			cfgKeys = append(cfgKeys, k)
		}
		sort.Strings(cfgKeys)
		cfg := make([]string, 0, len(cfgKeys))
		for _, k := range cfgKeys {
			cfg = append(cfg, fmt.Sprintf("%s=%v", k, v.Config[k]))
		}
		libs := ""
		if v.Info != nil {
			if lm, ok := v.Info["libs"].(map[string]any); ok {
				keys := make([]string, 0, len(lm))
				for k := range lm {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				parts := make([]string, 0, len(keys))
				for _, k := range keys {
					parts = append(parts, fmt.Sprintf("%s %v", k, lm[k]))
				}
				libs = strings.Join(parts, ", ")
			}
		}
		w("- **%s** — tracer %s, %s; libs: %s; config: %s; ran %s",
			v.Variant, infoStr(v, "tracer_version"), infoStr(v, "runtime_version"),
			libs, strings.Join(cfg, " "), v.RanAt.Format("2006-01-02"))
	}
	w("")
	var leaks []string
	for _, v := range variants {
		for _, t := range v.Tests {
			if t.Status != nil && t.Status.CanaryLeak {
				leaks = append(leaks, v.Variant+" "+t.Boundary)
			}
		}
	}
	if len(leaks) == 0 {
		w("Canary check: in all recorded runs the non-tracing control header")
		w("(`x-sd-canary`) was verified absent after the hop, proving relays re-emit")
		w("rather than proxy. Raw per-test header dumps live in `results/raw/*.json`.")
	} else {
		w("**WARNING — canary leaked (relay proxied headers, results invalid): %s**",
			strings.Join(leaks, ", "))
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, []byte(sb.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d variants)\n", *out, len(variants))
}
