package annotations

import (
	"fmt"

	"github.com/signadot/context-propagation-matrix/internal/registry"
	"github.com/signadot/context-propagation-matrix/internal/results"
)

// Reannotate replaces the annotations of a single-variant document with the
// ones the YAML in anns attaches to its cells today.
//
// A document's cells are the measurement and never change; its annotations
// are the explanation and belong to the YAML. A per-version document is
// filed once, with the annotations of that moment, so a note written or
// corrected afterwards never reached the versions it explains — a fix
// recorded as `fixed_in: 0.4.0` showed only on the version measured after
// the note was written. The per-version bundle is re-annotated on every
// publish instead, through this function.
//
// The document is padded with every other registered variant, marked
// Missing, so that Apply judges the YAML exactly as it does when a back-test
// aggregates one variant: a selector naming a registered variant that is
// not in the document has nothing to check, one naming no registered
// variant is stale (a fatal problem, returned for the caller to report).
// Resolution is therefore judged on this variant alone: a note whose cells
// all pass in this version is attached to them as resolved, with the YAML's
// fixed_in or, failing that, the version the document records.
//
// doc is modified in place: only its Annotations change.
func Reannotate(doc *results.Results, reg *registry.Registry, anns []*Annotation) ([]Problem, error) {
	if len(doc.Variants) != 1 || doc.Variants[0].Missing {
		return nil, fmt.Errorf("reannotate: want a single-variant document, got %d variant(s)", len(doc.Variants))
	}
	v := doc.Variants[0]
	if reg.Variant(v.ID) == nil {
		return nil, fmt.Errorf("reannotate: %s is not in the registry", v.ID)
	}
	padded := &results.Results{SchemaVersion: doc.SchemaVersion, Run: doc.Run, Dimensions: doc.Dimensions}
	for _, rv := range reg.SortedVariants() {
		if rv.ID == v.ID {
			padded.Variants = append(padded.Variants, v)
			continue
		}
		padded.Variants = append(padded.Variants, &results.Variant{
			ID: rv.ID, Tool: rv.Tool, Language: rv.Language, Flavour: rv.Flavour, DisplayName: rv.DisplayName,
			Missing: true, Hops: []results.Hop{}, Cells: []results.Cell{},
		})
	}
	problems := Apply(padded, anns)
	// Extract keeps the annotations that touch the variant, as it does when
	// a run or back-test document is filed under its version.
	ex, err := padded.Extract(v.ID)
	if err != nil {
		return problems, err
	}
	doc.Annotations = ex.Annotations
	return problems, nil
}
