// Package render turns a results document into MATRIX.md (a release asset,
// results/latest/MATRIX.md locally).
package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/signadot/context-propagation-matrix/internal/results"
)

// Markdown renders the human-readable matrix. It is deterministic for a
// given results document (the run id, not wall-clock time, is printed).
func Markdown(res *results.Results) string {
	var sb strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&sb, format+"\n", args...) }

	w("# Context Propagation Support Matrix")
	w("")
	w("Generated from empirical runs of this harness — every cell is the observed")
	w("result of an actual request/message hop, not a reading of vendor docs.")
	w("Regenerate with `go run ./cmd/cpm aggregate` after `./run.sh`.")
	w("")
	w("Run: `%s` · %d variants · %d hops · %d cells · finished %s",
		res.Run.RunID, res.Summary.VariantCount, res.Summary.HopCount, res.Summary.CellCount,
		res.Run.FinishedAt.UTC().Format("2006-01-02 15:04 UTC"))
	w("")
	w("Boundaries are `inbound protocol → outbound protocol` through a single")
	w("instrumented service (the relay). Legend: ✅ propagated/preserved ·")
	w("🟡 header present but seeded value lost · 🔀 new trace started ·")
	w("❌ header absent · 🚫 message was never forwarded (instrumentation or app broke).")
	w("")
	w("The `setup` column says what attaching the tool required:")
	for _, t := range res.Dimensions.SetupTiers {
		w("- `%s` — %s", t.ID, strings.TrimSpace(t.Description))
	}
	w("")

	cols := make([]string, len(res.Dimensions.Boundaries))
	for i, b := range res.Dimensions.Boundaries {
		c := strings.ReplaceAll(b, "->", "→")
		cols[i] = strings.ReplaceAll(c, "rabbitmq", "rmq")
	}
	header := "| variant | setup | " + strings.Join(cols, " | ") + " |"
	rule := "|" + strings.Repeat("---|", len(cols)+2)

	for _, tool := range res.Dimensions.Tools {
		var tv []*results.Variant
		for _, v := range res.Variants {
			if v.Tool == tool.ID && !v.Missing {
				tv = append(tv, v)
			}
		}
		w("## %s", tool.DisplayName)
		w("")
		if len(tv) == 0 {
			w("_No results yet._")
			w("")
			continue
		}
		for _, h := range res.Dimensions.Headers {
			w("### %s", h.DisplayName)
			w("")
			w("%s", h.Question)
			w("")
			w("%s", header)
			w("%s", rule)
			for _, v := range tv {
				cells := make([]string, 0, len(res.Dimensions.Boundaries))
				for _, c := range v.Cells {
					if c.Header == h.ID {
						cells = append(cells, c.Outcome.Glyph())
					}
				}
				tier := "—"
				if v.SetupTier != nil {
					tier = *v.SetupTier
				}
				w("| %s | %s | %s |", rowLabel(v), tier, strings.Join(cells, " | "))
			}
			w("")
		}
	}

	if len(res.Annotations) > 0 {
		w("## Why the non-green cells are non-green")
		w("")
		w("Each annotation below is attached to the cells it explains in `results.json`;")
		w("the full write-ups live in `annotations/*.yaml` and on contextpropagation.io.")
		w("")
		for _, a := range res.Annotations {
			status := a.Status
			if a.FixedIn != "" {
				status += " (fixed in " + a.FixedIn + ")"
			}
			w("- **%s** — %s _[%s; %d cells]_", a.Title, a.Cause, status, len(a.Cells))
			for _, l := range a.Links {
				w("  - [%s](%s)", l.Title, l.URL)
			}
		}
		w("")
	}

	w("## Variant details")
	w("")
	for _, v := range res.Variants {
		if v.Missing {
			w("- **%s** — _not run in this run_", v.ID)
			continue
		}
		cfgKeys := make([]string, 0, len(v.Config))
		for k := range v.Config {
			cfgKeys = append(cfgKeys, k)
		}
		sort.Strings(cfgKeys)
		cfg := make([]string, 0, len(cfgKeys))
		for _, k := range cfgKeys {
			cfg = append(cfg, k+"="+v.Config[k])
		}
		libKeys := make([]string, 0, len(v.Libs))
		for k := range v.Libs {
			libKeys = append(libKeys, k)
		}
		sort.Strings(libKeys)
		libs := make([]string, 0, len(libKeys))
		for _, k := range libKeys {
			libs = append(libs, k+" "+v.Libs[k])
		}
		tracer := "none"
		if v.Tracer != nil {
			tracer = v.Tracer.Name + " " + v.Tracer.Version
		}
		if v.Weaver != nil && v.Weaver.Version != "" {
			tracer += " via " + v.Weaver.Name + " " + v.Weaver.Version
		}
		w("- **%s** — tracer %s, %s; libs: %s; config: %s; ran %s",
			v.ID, tracer, v.Runtime, strings.Join(libs, ", "), strings.Join(cfg, " "), v.RanAt.Format("2006-01-02"))
	}
	w("")
	if len(res.Summary.CanaryLeaks) == 0 {
		w("Canary check: in all recorded runs the non-tracing control header")
		w("(`x-sd-canary`) was verified absent after the hop, proving relays re-emit")
		w("rather than proxy. Raw per-test header dumps live in this release's `raw.tar.gz`")
		w("(`raw/{variant}.json`) and, per hop, under `variants[].hops[].observed_headers` in `results.json`.")
	} else {
		w("**WARNING — canary leaked (relay proxied headers, results invalid): %s**",
			strings.Join(res.Summary.CanaryLeaks, ", "))
	}
	return sb.String()
}

func rowLabel(v *results.Variant) string {
	label := v.Language
	if v.Flavour != "default" {
		label += " (" + v.Flavour + ")"
	}
	if v.Tracer != nil && v.Tracer.Version != "" && v.Tracer.Version != "unknown" && v.Tracer.Version != "none" {
		label += " · " + v.Tracer.Version
	}
	return label
}
