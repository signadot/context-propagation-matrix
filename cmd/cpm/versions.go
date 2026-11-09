package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/signadot/context-propagation-matrix/internal/results"
)

// cmdVersions maintains the per-variant, per-version documents behind the
// `variant-{id}` releases (see internal/results/perversion.go). It only
// touches files; scripts/publish-versions.sh moves them to and from the
// release.
//
//	cpm versions -variant ID -out DIR [-index DIR/index.json]
//	             [-results results.json [-version X]] [-present a.json,b.json] [doc.json ...]
//
// With -results, the variant's measurement is written to DIR/{version}.json
// and indexed; the version is the pinned tracer version recorded in the
// document, or -version when given (a back-test tag knows what it pinned
// even when an old document does not say). Positional documents are
// existing per-version files (named {version}.json) to (re)index, which is
// how a release is rebuilt and how an index that lost a race catches up.
// -present lists the asset names on the release; entries for assets not in
// it and not written now are dropped. Prints one JSON line per document
// written or indexed.
func cmdVersions(args []string) error {
	fs := flag.NewFlagSet("versions", flag.ExitOnError)
	variant := fs.String("variant", "", "variant id (required)")
	outDir := fs.String("out", "", "directory for {version}.json and index.json (required)")
	indexPath := fs.String("index", "", "existing index.json to update (default: <out>/index.json if present)")
	resultsPath := fs.String("results", "", "results.json to take the variant's measurement from")
	version := fs.String("version", "", "canonical version for -results (default: the document's tracer.pinned)")
	present := fs.String("present", "", "comma-separated asset names on the release; prune entries for any other asset")
	force := fs.Bool("force", false, "with -results: publish the variant even when the document carried it over from an earlier run")
	fs.Parse(args)
	if *variant == "" || *outDir == "" {
		return fmt.Errorf("versions: -variant and -out are required")
	}
	if *indexPath == "" {
		*indexPath = filepath.Join(*outDir, "index.json")
	}

	type written struct {
		Variant string `json:"variant"`
		Version string `json:"version"`
		Asset   string `json:"asset"`
		Source  string `json:"source"`
	}
	var (
		idx   *results.VersionIndex
		wrote []written
		enc   = json.NewEncoder(os.Stdout)
	)
	ensureIndex := func(v *results.Variant) error {
		if idx != nil {
			return nil
		}
		var err error
		idx, err = results.LoadVersionIndex(*indexPath, v)
		return err
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
		asset, err := results.AssetName(ver)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			return err
		}
		if err := doc.Write(filepath.Join(*outDir, asset)); err != nil {
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
		wrote = append(wrote, written{*variant, ver, asset, *resultsPath})
	}

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
		wrote = append(wrote, written{*variant, ver, e.Asset, path})
	}

	if idx == nil {
		return fmt.Errorf("versions: nothing to do (give -results or documents)")
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
	if err := os.MkdirAll(filepath.Dir(*indexPath), 0o755); err != nil {
		return err
	}
	if err := idx.Write(*indexPath); err != nil {
		return err
	}
	for _, w := range wrote {
		if err := enc.Encode(w); err != nil {
			return err
		}
	}
	return nil
}
