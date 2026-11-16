package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/signadot/context-propagation-matrix/internal/annotations"
	"github.com/signadot/context-propagation-matrix/internal/registry"
	"github.com/signadot/context-propagation-matrix/internal/results"
)

// cmdVersions maintains the per-variant, per-version documents behind the
// `variant-{id}` releases (see internal/results/perversion.go). It only
// touches files; scripts/publish-versions.sh moves them to and from the
// release.
//
//	cpm versions -variant ID -out DIR [-index DIR/index.json] [-bundle DIR/versions.json]
//	             [-results results.json [-version X]] [-present a.json,b.json] [doc.json ...]
//
// With -results, the variant's measurement is written to DIR/{version}.json
// and indexed; the version is the pinned tracer version recorded in the
// document, or -version when given (a back-test tag knows what it pinned
// even when an old document does not say). Positional documents are
// existing per-version files (named {version}.json) to (re)index, which is
// how a release is rebuilt and how an index that lost a race catches up.
// -present lists the asset names on the release; entries for assets not in
// it and not written now are dropped.
//
// DIR/versions.json, the bundle of the index and every document, is
// rewritten as well: the existing bundle is read, the documents written or
// given now are put in, and what the index no longer lists is dropped. The
// bundle must end up holding a document for every indexed version, so when
// it does not (first bundle, or a document only on the release) the missing
// versions are reported and the caller passes their documents as
// positionals. Prints one JSON line per document written or indexed.
//
// Every document in the bundle is then re-annotated from -annotations
// against -registry (annotations.Reannotate): the cells stay as measured,
// the annotations become those the current YAML attaches to them, so a note
// written or corrected after a version was measured reaches that version's
// page at the next publish of the variant. With neither -results nor
// documents, an existing bundle is just re-annotated and rewritten, which is
// what republish.yml does for every variant when annotations/ changes.
// -annotations "" turns re-annotation off.
func cmdVersions(args []string) error {
	fs := flag.NewFlagSet("versions", flag.ExitOnError)
	variant := fs.String("variant", "", "variant id (required)")
	outDir := fs.String("out", "", "directory for {version}.json, index.json and versions.json (required)")
	indexPath := fs.String("index", "", "existing index.json to update (default: <out>/index.json if present)")
	bundlePath := fs.String("bundle", "", "existing versions.json to update (default: <out>/versions.json if present)")
	resultsPath := fs.String("results", "", "results.json to take the variant's measurement from")
	version := fs.String("version", "", "canonical version for -results (default: the document's tracer.pinned)")
	present := fs.String("present", "", "comma-separated asset names on the release; prune entries for any other asset")
	force := fs.Bool("force", false, "with -results: publish the variant even when the document carried it over from an earlier run")
	regPath := fs.String("registry", "variants.yaml", "variants registry, for re-annotation")
	annDir := fs.String("annotations", "annotations", "annotations directory to re-annotate every bundled document from (\"\" to leave annotations as filed)")
	fs.Parse(args)
	if *variant == "" || *outDir == "" {
		return fmt.Errorf("versions: -variant and -out are required")
	}
	if *indexPath == "" {
		*indexPath = filepath.Join(*outDir, results.IndexAsset)
	}
	if *bundlePath == "" {
		*bundlePath = filepath.Join(*outDir, results.BundleAsset)
	}

	// Re-annotation needs the registry (to pad a single-variant document
	// with the variants it lacks) and the YAML. Both are read once; fatal
	// problems are the YAML's business, reported once each and not fatal
	// here: validate -strict and aggregate guard the YAML on every pull
	// request, and a stale selector attaches to nothing.
	var reg *registry.Registry
	var anns []*annotations.Annotation
	if *annDir != "" {
		var err error
		if reg, err = registry.Load(*regPath); err != nil {
			return fmt.Errorf("versions: %w", err)
		}
		if anns, err = annotations.LoadDir(*annDir); err != nil {
			return fmt.Errorf("versions: %w", err)
		}
	}
	reported := map[string]bool{}
	reannotate := func(doc *results.Results) error {
		if reg == nil {
			return nil
		}
		problems, err := annotations.Reannotate(doc, reg, anns)
		if err != nil {
			return err
		}
		for _, p := range problems {
			if p.Fatal && !reported[p.Message] {
				reported[p.Message] = true
				fmt.Fprintf(os.Stderr, "versions: %s: %s\n", *variant, p.String())
			}
		}
		return nil
	}

	type written struct {
		Variant string `json:"variant"`
		Version string `json:"version"`
		Asset   string `json:"asset"`
		Source  string `json:"source"`
	}
	var (
		idx    *results.VersionIndex
		bundle *results.VersionBundle
		wrote  []written
		enc    = json.NewEncoder(os.Stdout)
	)
	ensureIndex := func(v *results.Variant) error {
		if idx != nil {
			return nil
		}
		var err error
		if idx, err = results.LoadVersionIndex(*indexPath, v); err != nil {
			return err
		}
		bundle, err = results.LoadVersionBundle(*bundlePath, v)
		return err
	}

	// Existing documents first, the fresh measurement last: when -results
	// files a version that is also given as a document (the release copy of
	// the version being re-measured), the measurement made now must win.
	for _, path := range fs.Args() {
		doc, err := results.Load(path)
		if err != nil {
			return err
		}
		if len(doc.Variants) != 1 || doc.Variants[0].ID != *variant || doc.Variants[0].Missing {
			return fmt.Errorf("versions: %s is not a single-variant document for %s", path, *variant)
		}
		v := doc.Variants[0]
		ver := strings.TrimSuffix(filepath.Base(path), ".json")
		if cv := v.CanonicalVersion(); cv != "" && cv != ver {
			return fmt.Errorf("versions: %s is filed as %s but records version %s", path, ver, cv)
		}
		if err := ensureIndex(v); err != nil {
			return err
		}
		e, err := doc.Entry(ver)
		if err != nil {
			return err
		}
		idx.Upsert(e)
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		bundle.Put(ver, raw)
		wrote = append(wrote, written{*variant, ver, e.Asset, path})
	}

	if *resultsPath != "" {
		res, err := results.Load(*resultsPath)
		if err != nil {
			return err
		}
		v := res.Variant(*variant)
		if v == nil || v.Missing {
			return fmt.Errorf("versions: %s is not measured in %s", *variant, *resultsPath)
		}
		if !res.MeasuredHere(v) && !*force {
			return fmt.Errorf("versions: %s in %s was carried over from %s; publish it from that release (or -force)", *variant, res.Run.RunID, v.MeasuredIn)
		}
		ver := *version
		if ver == "" {
			ver = v.CanonicalVersion()
		}
		if ver == "" {
			reported := ""
			if v.Tracer != nil {
				reported = v.Tracer.Version
			}
			return fmt.Errorf("versions: %s has no pinned version and reports %q; pass -version", *variant, reported)
		}
		doc, err := res.Extract(*variant)
		if err != nil {
			return err
		}
		if v.Tracer != nil && v.Tracer.Pinned == "" {
			v.Tracer.Pinned = ver // the document should say what it is filed under
		}
		// The asset is filed with the same annotations the bundle will hold:
		// judged on this variant alone, not on the whole run it came from.
		if err := reannotate(doc); err != nil {
			return err
		}
		asset, err := results.AssetName(ver)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			return err
		}
		assetPath := filepath.Join(*outDir, asset)
		if err := doc.Write(assetPath); err != nil {
			return err
		}
		if err := ensureIndex(v); err != nil {
			return err
		}
		e, err := doc.Entry(ver)
		if err != nil {
			return err
		}
		idx.Upsert(e)
		raw, err := os.ReadFile(assetPath) // the bundle carries the asset byte for byte
		if err != nil {
			return err
		}
		bundle.Put(ver, raw)
		wrote = append(wrote, written{*variant, ver, asset, *resultsPath})
	}

	if idx == nil {
		// Nothing filed: re-annotate what the release already holds. The
		// bundle must be there; the registry stands in for a document to
		// name the variant, and carries the display metadata as Load does.
		if _, err := os.Stat(*bundlePath); err != nil || reg == nil {
			return fmt.Errorf("versions: nothing to do (give -results or documents, or an existing %s to re-annotate)", results.BundleAsset)
		}
		rv := reg.Variant(*variant)
		if rv == nil {
			return fmt.Errorf("versions: %s is not in %s", *variant, *regPath)
		}
		if err := ensureIndex(&results.Variant{ID: rv.ID, Tool: rv.Tool, Language: rv.Language, DisplayName: rv.DisplayName}); err != nil {
			return err
		}
	}
	if *present != "" {
		keep := map[string]bool{}
		for _, a := range strings.Split(*present, ",") {
			if a = strings.TrimSpace(a); a != "" {
				keep[a] = true
			}
		}
		for _, w := range wrote {
			keep[w.Asset] = true
		}
		for _, a := range idx.Prune(keep) {
			fmt.Fprintf(os.Stderr, "versions: dropped %s from the index: not on the release\n", a)
		}
	}
	if missing := bundle.Sync(idx); len(missing) > 0 {
		return fmt.Errorf("versions: %s lacks the document for %s; pass %s as positional documents",
			results.BundleAsset, strings.Join(missing, ", "), assetList(missing))
	}
	if reg != nil {
		if err := bundle.Each(func(_ string, doc *results.Results) error { return reannotate(doc) }); err != nil {
			return fmt.Errorf("versions: %w", err)
		}
		fmt.Fprintf(os.Stderr, "versions: %s: %d document(s) re-annotated from %s\n", *variant, len(bundle.Documents), *annDir)
	}
	if err := os.MkdirAll(filepath.Dir(*indexPath), 0o755); err != nil {
		return err
	}
	if err := idx.Write(*indexPath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*bundlePath), 0o755); err != nil {
		return err
	}
	if err := bundle.Write(*bundlePath); err != nil {
		return err
	}
	for _, w := range wrote {
		if err := enc.Encode(w); err != nil {
			return err
		}
	}
	return nil
}

// assetList renders versions as the asset names a caller would download.
func assetList(versions []string) string {
	names := make([]string, len(versions))
	for i, v := range versions {
		names[i] = v + ".json"
	}
	return strings.Join(names, " ")
}
