package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/signadot/context-propagation-matrix/internal/results"
)

// cmdGate decides whether a results.json may be published. matrix.yml and
// backtest.yml run it between aggregating and the first step that publishes;
// a non-zero exit stops the release, the per-version filing and the site
// rebuild.
//
//	cpm gate [-reran "id id ..."] [-baseline results.json] <results.json>
//
// Every measured variant is judged again from its hops (results.Judge), not
// from the verdict `cpm aggregate` stored, so a document published before
// the verdict existed is gated the same way. What a problem does depends on
// where the variant was measured and on the kind of run:
//
//   - A variant carried over from an earlier release only warns. The run
//     that measured it was the one to judge it, and failing now would hold
//     back every variant this run did measure without un-publishing that
//     one. Measured here means what the document records (measured_in,
//     run.reran) unless -reran names the ids.
//   - canary_leak and transport_mismatch on a variant measured here block:
//     they are faults of the relay or the harness, never a result.
//   - A boundary with no hop on a variant measured here blocks
//     (boundary_not_run). The driver runs its own protocol list, and this
//     is a boundary of variants.yaml it did not run: its cells are not_run,
//     which is no result either. It is not one of results.Judge's verdicts
//     and is not recorded as `invalid`, since the cells say it themselves.
//   - agent_inactive on a variant measured here blocks, except in a
//     back-test (run.kind), where it warns. That an old version does not
//     attach on today's runtime is worth recording once, and the document
//     carries the verdict; left unpublished and unfiled, the pair would be
//     proposed again by every `cpm discover -skip-published`.
//
// Problems are printed one per line, as ::error:: / ::warning:: workflow
// commands on a GitHub runner. The exit status is 1 when anything blocks.
func cmdGate(args []string) error {
	fs := flag.NewFlagSet("gate", flag.ExitOnError)
	reran := fs.String("reran", "", "variant ids measured in this run (space/comma separated); default: what the document records")
	baselinePath := fs.String("baseline", "", "results.json to take a language's uninstrumented baseline from when the document has none (a back-test holds one variant)")
	pos, err := parsePositionals(fs, args, 1)
	if err != nil {
		return fmt.Errorf(`usage: cpm gate [-reran "id id ..."] [-baseline results.json] <results.json>`)
	}
	res, err := results.Load(pos[0])
	if err != nil {
		return err
	}
	baseline, err := loadBaseline(*baselinePath)
	if err != nil {
		return err
	}
	var measured map[string]bool
	if ids := strings.FieldsFunc(*reran, func(r rune) bool { return r == ' ' || r == ',' || r == '\n' }); len(ids) > 0 {
		measured = map[string]bool{}
		for _, id := range ids {
			measured[id] = true
		}
	}
	if n := gate(os.Stdout, res, baseline, measured); n > 0 {
		return fmt.Errorf("gate: %d problem(s) block publishing %s", n, res.Run.RunID)
	}
	return nil
}

// gate prints every problem found in res and returns how many of them block
// publishing. measured is the set of variant ids measured in this run; nil
// means ask the document.
func gate(w io.Writer, res, baseline *results.Results, measured map[string]bool) int {
	say := func(level, format string, a ...any) {
		if os.Getenv("GITHUB_ACTIONS") == "true" {
			fmt.Fprintf(w, "::%s::gate: %s\n", level, fmt.Sprintf(format, a...))
			return
		}
		fmt.Fprintf(w, "gate: %s: %s\n", level, fmt.Sprintf(format, a...))
	}
	var judged, here, blocking, warnings int
	unbased := map[string]bool{} // languages judged without a baseline
	// The run a carried-over variant came from; -reran can leave out a
	// variant the document says this run measured.
	carriedFrom := func(v *results.Variant) string {
		if v.MeasuredIn == "" || v.MeasuredIn == res.Run.RunID {
			return "an earlier run"
		}
		return v.MeasuredIn
	}
	for _, v := range res.Variants {
		if v.Missing {
			continue
		}
		judged++
		mine := res.MeasuredHere(v)
		if measured != nil {
			mine = measured[v.ID]
		}
		if mine {
			here++
		}
		if missed := notRun(v); len(missed) > 0 {
			detail := fmt.Sprintf("no hop on %d of %d boundaries (%s): the driver did not run what variants.yaml lists",
				len(missed), len(v.Hops), strings.Join(missed, ", "))
			if mine {
				blocking++
				say("error", "%s: %s: %s", v.ID, problemBoundaryNotRun, detail)
			} else {
				warnings++
				say("warning", "%s: %s: %s (carried over from %s, which was the run to judge it; not blocking)", v.ID, problemBoundaryNotRun, detail, carriedFrom(v))
			}
		}
		b := res.BaselineFor(v, baseline)
		if b == nil && !res.IsBaseline(v) {
			unbased[v.Language] = true
		}
		for _, p := range res.Judge(v, b) {
			switch {
			case !mine:
				warnings++
				say("warning", "%s: %s: %s (carried over from %s, which was the run to judge it; not blocking)", v.ID, p.Kind, p.Detail, carriedFrom(v))
			case p.Kind == results.ProblemAgentInactive && res.Run.Kind == "backtest":
				warnings++
				say("warning", "%s: %s: %s (a back-test is published and filed with this verdict)", v.ID, p.Kind, p.Detail)
			default:
				blocking++
				say("error", "%s: %s: %s", v.ID, p.Kind, p.Detail)
			}
		}
	}
	if len(unbased) > 0 {
		langs := make([]string, 0, len(unbased))
		for l := range unbased {
			langs = append(langs, l)
		}
		sort.Strings(langs)
		warnings++
		say("warning", "no uninstrumented baseline for %s: agent_inactive there was judged as \"no tracing header at all\", which misses an inactive agent on a runtime that sends them itself (-baseline <the latest run's results.json> supplies one)", strings.Join(langs, ", "))
	}
	fmt.Fprintf(w, "gate: %s (%s): %d variant(s) judged, %d measured in this run: %d blocking, %d warning(s)\n",
		res.Run.RunID, res.Run.Kind, judged, here, blocking, warnings)
	return blocking
}

// problemBoundaryNotRun is the gate's name for a variant with a boundary no
// hop ran on.
const problemBoundaryNotRun = "boundary_not_run"

// notRun returns the boundaries of v that have no hop: the ones whose cells
// `cpm aggregate` wrote as not_run because the raw result had no test for
// them. An undelivered hop did run, and is not one of them.
func notRun(v *results.Variant) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range v.Cells {
		if c.Outcome == results.OutcomeNotRun && !seen[c.Boundary] {
			seen[c.Boundary] = true
			out = append(out, c.Boundary)
		}
	}
	return out
}

// loadBaseline reads the optional -baseline document of gate and aggregate.
func loadBaseline(path string) (*results.Results, error) {
	if path == "" {
		return nil, nil
	}
	return results.Load(path)
}

// recordValidity stores every variant's verdict in res (Variant.Invalid) for
// cmdAggregate and reports what it stored. It never fails on a verdict:
// whether an invalid measurement may be published is cpm gate's decision,
// and a local run should get its results.json either way.
func recordValidity(res *results.Results, baselinePath string) error {
	baseline, err := loadBaseline(baselinePath)
	if err != nil {
		return err
	}
	res.SetValidity(baseline)
	for _, v := range res.Variants {
		for _, p := range v.Invalid {
			fmt.Fprintf(os.Stderr, "aggregate: %s is not a valid measurement: %s: %s\n", v.ID, p.Kind, p.Detail)
		}
	}
	return nil
}
