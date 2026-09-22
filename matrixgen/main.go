// Command matrixgen is kept for compatibility: `go run ./matrixgen` renders
// results/raw/*.json into results/MATRIX.md exactly as before, but now via the
// registry-driven aggregator (`go run ./cmd/cpm aggregate`), which is what CI
// and the website use. Prefer cpm directly; it also writes results.json.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/signadot/context-propagation-matrix/internal/annotations"
	"github.com/signadot/context-propagation-matrix/internal/raw"
	"github.com/signadot/context-propagation-matrix/internal/registry"
	"github.com/signadot/context-propagation-matrix/internal/render"
	"github.com/signadot/context-propagation-matrix/internal/results"
)

func main() {
	rawDir := flag.String("raw", "results/raw", "raw results directory")
	out := flag.String("out", "results/MATRIX.md", "output markdown file")
	regPath := flag.String("registry", "variants.yaml", "variants registry")
	annDir := flag.String("annotations", "annotations", "annotations directory")
	flag.Parse()

	reg, err := registry.Load(*regPath)
	if err != nil {
		fatal(err)
	}
	raws, err := raw.LoadDir(*rawDir)
	if err != nil {
		fatal(err)
	}
	res, err := results.Build(reg, raws, results.Run{Kind: "local", HarnessRepo: "signadot/context-propagation-matrix", Generator: "matrixgen"})
	if err != nil {
		fatal(err)
	}
	res.Run.RunID = "run-" + res.Run.FinishedAt.UTC().Format("2006-01-02")
	if anns, err := annotations.LoadDir(*annDir); err == nil {
		for _, p := range annotations.Apply(res, anns) {
			fmt.Fprintln(os.Stderr, p.String())
		}
	}
	if err := os.WriteFile(*out, []byte(render.Markdown(res)), 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %s (%d variants)\n", *out, res.Summary.VariantCount)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
