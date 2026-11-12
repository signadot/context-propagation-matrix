// Package affected maps a set of changed repository paths (plus the registry
// and compose file before and after the change) to the variants whose
// measurement could differ, so a push to main re-runs only those and carries
// every other cell over from the previous release.
//
// The rules are deliberately conservative: anything the sink, driver,
// protobuf contract, shared images or the workflow itself depend on re-runs
// everything; paths that only affect aggregation or documentation re-run
// nothing; a path under a relay source re-runs the variants built from it.
package affected

import (
	"fmt"
	"path"
	"reflect"
	"sort"
	"strings"

	"github.com/signadot/context-propagation-matrix/internal/registry"
)

// Inputs describes one change.
type Inputs struct {
	// Paths changed between base and head, repository-relative with forward
	// slashes (as printed by `git diff --name-only`).
	Paths []string
	// Head is the registry at the new revision (required).
	Head *registry.Registry
	// Base is the registry at the old revision. Only consulted when
	// variants.yaml is among Paths; nil then means "could not be read" and
	// re-runs everything.
	Base *registry.Registry
	// HeadCompose and BaseCompose are docker-compose.yml decoded into generic
	// maps (anchors and merge keys resolved). Only consulted when
	// docker-compose.yml is among Paths; a nil BaseCompose then re-runs
	// everything.
	HeadCompose, BaseCompose map[string]any
}

// Result is the set of variants to re-measure.
type Result struct {
	// All is true when a shared component changed and every variant re-runs.
	All bool
	// Variants are the ids to run, in registry order (every id when All).
	Variants []string
	// Reasons explains each decision, one line per changed path or diff
	// finding, for the workflow log.
	Reasons []string
}

const (
	registryFile = "variants.yaml"
	composeFile  = "docker-compose.yml"
	workflowFile = ".github/workflows/matrix.yml"
)

// Prefixes (directories) and exact files whose change invalidates every
// measurement: they are built into, or drive, every variant's run.
var allPrefixes = []string{"driver/", "sink/", "proto/", "gen/", "docker/"}
var allFiles = map[string]bool{"go.mod": true, "go.sum": true, ".dockerignore": true, workflowFile: true}

// Prefixes and files that cannot change a raw measurement: documentation,
// annotations (republished separately), the aggregator and its tests, version
// discovery, other workflows, local tooling (incl. cpm try stack examples)
// and the ignored outputs.
var nonePrefixes = []string{"annotations/", ".github/", "cmd/", "examples/", "internal/", "matrixgen/", "results/", "scripts/"}
var noneFiles = map[string]bool{"run.sh": true, ".gitignore": true, ".env.example": true, "LICENSE": true, "pins.yaml": true}

// Compute applies the rules to one change.
func Compute(in Inputs) Result {
	var res Result
	hit := map[string]bool{}
	mark := func(ids ...string) {
		for _, id := range ids {
			hit[id] = true
		}
	}
	all := func(why string) {
		res.All = true
		res.Reasons = append(res.Reasons, why)
	}

	sawRegistry, sawCompose := false, false
	for _, p := range in.Paths {
		p = path.Clean(strings.TrimPrefix(p, "./"))
		switch {
		case p == registryFile:
			sawRegistry = true
		case p == composeFile:
			sawCompose = true
		case strings.HasSuffix(p, ".md"):
			res.Reasons = append(res.Reasons, p+": documentation, no variants")
		case allFiles[p] || hasPrefix(p, allPrefixes):
			all(p + ": shared component, every variant")
		case noneFiles[p] || hasPrefix(p, nonePrefixes):
			res.Reasons = append(res.Reasons, p+": aggregation/tooling only, no variants")
		case strings.HasPrefix(p, "relays/"):
			ids := relayVariants(p, in.Head)
			if len(ids) == 0 {
				all(p + ": relay path not under any variant source, every variant")
				continue
			}
			mark(ids...)
			res.Reasons = append(res.Reasons, p+": "+strings.Join(ids, ", "))
		default:
			all(p + ": unclassified path, every variant")
		}
	}

	if sawRegistry {
		if in.Base == nil {
			all(registryFile + ": previous revision unreadable, every variant")
		} else {
			n := len(res.Reasons)
			for _, hv := range in.Head.Variants {
				bv := in.Base.Variant(hv.ID)
				switch {
				case bv == nil:
					mark(hv.ID)
					res.Reasons = append(res.Reasons, registryFile+": "+hv.ID+" is new")
				case !reflect.DeepEqual(*hv, *bv):
					mark(hv.ID)
					res.Reasons = append(res.Reasons, registryFile+": "+hv.ID+" definition changed")
				}
			}
			if len(res.Reasons) == n {
				res.Reasons = append(res.Reasons, registryFile+": only dimensions changed (aggregation), no variants")
			}
		}
	}

	if sawCompose {
		if in.BaseCompose == nil {
			all(composeFile + ": previous revision unreadable, every variant")
		} else {
			ids, shared := composeDiff(in.BaseCompose, in.HeadCompose, in.Head)
			for _, why := range shared {
				all(composeFile + ": " + why + ", every variant")
			}
			for _, id := range ids {
				mark(id)
				res.Reasons = append(res.Reasons, composeFile+": service relay-"+id+" changed")
			}
		}
	}

	for _, v := range in.Head.SortedVariants() {
		if res.All || hit[v.ID] {
			res.Variants = append(res.Variants, v.ID)
		}
	}
	return res
}

// Since selects the variants whose inputs changed after they were last
// measured. measuredAt gives, by variant id, the harness commit of the
// variant's latest published measurement; a variant with no entry (or an
// empty one) has none and is always selected. diff computes the change from
// such a commit to the revision about to be measured and is called once per
// distinct commit.
//
// The change in one push is not enough to decide what to run, because an
// earlier run may not have measured what it was meant to: it was cancelled
// while queued behind another, one of its jobs failed, or the merge started
// no run at all (GitHub creates none for a merge made with the workflow's
// own token, which is how tracer bumps land). Diffing from each variant's
// own last measurement picks all of those up in the next run of any kind.
func Since(reg *registry.Registry, measuredAt map[string]string, diff func(base string) Result) Result {
	var res Result
	hit := map[string]bool{}
	var bases []string // distinct commits, in registry order of first use
	byBase := map[string][]string{}
	for _, v := range reg.SortedVariants() {
		base := measuredAt[v.ID]
		if base == "" {
			hit[v.ID] = true
			res.Reasons = append(res.Reasons, v.ID+": no published measurement")
			continue
		}
		if _, seen := byBase[base]; !seen {
			bases = append(bases, base)
		}
		byBase[base] = append(byBase[base], v.ID)
	}
	for _, base := range bases {
		d := diff(base)
		changed := map[string]bool{}
		for _, id := range d.Variants {
			changed[id] = true
		}
		var stale []string
		for _, id := range byBase[base] {
			if d.All || changed[id] {
				hit[id] = true
				stale = append(stale, id)
			}
		}
		if len(stale) == 0 {
			res.Reasons = append(res.Reasons, fmt.Sprintf("since %s: no change reaches the %d variant(s) last measured there", short(base), len(byBase[base])))
			continue
		}
		// The diff's reasons name every variant a path reaches; of those,
		// only the ones last measured at this commit are selected here.
		for _, why := range d.Reasons {
			res.Reasons = append(res.Reasons, "since "+short(base)+": "+why)
		}
		res.Reasons = append(res.Reasons, "since "+short(base)+": to measure, "+strings.Join(stale, ", "))
	}
	for _, v := range reg.SortedVariants() {
		if hit[v.ID] {
			res.Variants = append(res.Variants, v.ID)
		}
	}
	res.All = len(res.Variants) == len(reg.Variants)
	return res
}

// Union merges two selections: a variant either one selects is selected.
func Union(reg *registry.Registry, a, b Result) Result {
	hit := map[string]bool{}
	for _, id := range a.Variants {
		hit[id] = true
	}
	for _, id := range b.Variants {
		hit[id] = true
	}
	res := Result{Reasons: append(append([]string{}, a.Reasons...), b.Reasons...)}
	for _, v := range reg.SortedVariants() {
		if a.All || b.All || hit[v.ID] {
			res.Variants = append(res.Variants, v.ID)
		}
	}
	res.All = a.All || b.All || len(res.Variants) == len(reg.Variants)
	return res
}

func short(rev string) string {
	if len(rev) > 7 {
		return rev[:7]
	}
	return rev
}

func hasPrefix(p string, prefixes []string) bool {
	for _, pre := range prefixes {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

// relayVariants maps a path under relays/ to variants. A path inside a
// variant's source directory affects that variant, and the deepest source
// wins: relays/node/tools/datadog/package.json belongs to the Datadog Node
// variants even though the baseline's source, relays/node, contains it too.
//
// A path is also shared with every variant whose source sits below one of
// its ancestor directories, searched from the nearest ancestor upwards and
// no higher than the source that owns the path (or relays/ when none does):
// relays/node/Dockerfile, relays/node/package.json and relays/node/app.js are
// built into all the Node variants, relays/ruby/app/relay.rb into all the
// Ruby ones. Both rules apply together, so a file in relays/node that is not
// under relays/node/tools/<tool> selects the baseline and the tools alike.
// An empty result means no variant source is anywhere under the path's
// directory and the caller falls back to re-running everything.
func relayVariants(p string, reg *registry.Registry) []string {
	hit := map[string]bool{}

	owner := ""
	for _, v := range reg.Variants {
		if strings.HasPrefix(p, v.Source+"/") && len(v.Source) > len(owner) {
			owner = v.Source
		}
	}
	for _, v := range reg.Variants {
		if owner != "" && v.Source == owner {
			hit[v.ID] = true
		}
	}

	for dir := path.Dir(p); dir != "relays" && dir != "." && dir != "/"; dir = path.Dir(dir) {
		if owner != "" && !strings.HasPrefix(dir+"/", owner+"/") {
			break // above the source that owns p
		}
		found := false
		for _, v := range reg.Variants {
			if strings.HasPrefix(v.Source, dir+"/") {
				hit[v.ID] = true
				found = true
			}
		}
		if found {
			break
		}
	}

	ids := make([]string, 0, len(hit))
	for id := range hit {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// composeDiff compares the two compose documents. A changed relay-<id>
// service (anchors and merge keys already resolved, so an edited
// x-relay-common-env shows up in every relay) selects that variant; a
// changed broker, sink or driver service, or any other top-level key except
// the x-* extension fields, is a shared change.
func composeDiff(base, head map[string]any, reg *registry.Registry) (ids []string, shared []string) {
	keys := map[string]bool{}
	for k := range base {
		keys[k] = true
	}
	for k := range head {
		keys[k] = true
	}
	for _, k := range sortedKeys(keys) {
		if k == "services" || strings.HasPrefix(k, "x-") {
			continue
		}
		if !reflect.DeepEqual(base[k], head[k]) {
			shared = append(shared, fmt.Sprintf("top-level %q changed", k))
		}
	}
	bs, _ := base["services"].(map[string]any)
	hs, _ := head["services"].(map[string]any)
	names := map[string]bool{}
	for k := range bs {
		names[k] = true
	}
	for k := range hs {
		names[k] = true
	}
	for _, name := range sortedKeys(names) {
		if reflect.DeepEqual(bs[name], hs[name]) {
			continue
		}
		if id, ok := strings.CutPrefix(name, "relay-"); ok {
			if reg.Variant(id) != nil {
				ids = append(ids, id)
			}
			// A removed relay service has no variant to run.
			continue
		}
		shared = append(shared, fmt.Sprintf("service %q changed", name))
	}
	return ids, shared
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
