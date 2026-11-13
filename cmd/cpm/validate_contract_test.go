package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/signadot/context-propagation-matrix/internal/registry"
	"github.com/signadot/context-propagation-matrix/internal/results"
)

// latestRelease returns the registry and the path of a results.json
// aggregated from it, standing for the latest release.
func latestRelease(t *testing.T) (*registry.Registry, string) {
	t.Helper()
	reg, err := registry.Load("../../variants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rawDir := t.TempDir()
	writeRaw(t, reg, rawDir, "none-go", sendsPlain)
	return reg, aggregate(t, rawDir, "matrix", "run-2026-11-13")
}

// withoutProtocol copies the document at path as one written before
// protocol p existed: p and every boundary through it are gone from its
// dimensions. edit, when given, changes the copy further.
func withoutProtocol(t *testing.T, path, p string, edit func(doc map[string]any)) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	dims := doc["dimensions"].(map[string]any)
	var protocols, boundaries []any
	for _, x := range dims["protocols"].([]any) {
		if x.(map[string]any)["id"] != p {
			protocols = append(protocols, x)
		}
	}
	for _, b := range dims["boundaries"].([]any) {
		if in, to, _ := strings.Cut(b.(string), "->"); in != p && to != p {
			boundaries = append(boundaries, b)
		}
	}
	if len(protocols) == len(dims["protocols"].([]any)) || len(boundaries) == len(dims["boundaries"].([]any)) {
		t.Fatalf("fixture: %s is not a protocol of the document", p)
	}
	dims["protocols"], dims["boundaries"] = protocols, boundaries
	if edit != nil {
		edit(doc)
	}
	if body, err = json.Marshal(doc); err != nil {
		t.Fatal(err)
	}
	return writeFile(t, t.TempDir(), "results.json", string(body))
}

// matrix_version has to move when the cells a variant is scored on do.
// The comparison is with the latest release, which validate is given.
func TestContractErrors(t *testing.T) {
	reg, path := latestRelease(t)
	last := reg.ProtocolOrder[len(reg.ProtocolOrder)-1]
	load := func(path string) *results.Results {
		t.Helper()
		res, err := results.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	same := load(path)
	// The latest release as it would be if `last` had been added since.
	older := load(withoutProtocol(t, path, last, nil))
	bumped := *reg
	bumped.MatrixVersion++

	if errs := contractErrors(reg, same); errs != nil {
		t.Errorf("a registry the release was written from: %q", errs)
	}
	errs := contractErrors(reg, older)
	if len(errs) != 1 {
		t.Fatalf("a protocol added, matrix_version unchanged: want one error, got %q", errs)
	}
	for _, want := range []string{"run-2026-11-13", "Bump matrix_version", "protocols added: " + last, "boundaries added: ", last + "->" + last} {
		if !strings.Contains(errs[0], want) {
			t.Errorf("the error does not say %q:\n%s", want, errs[0])
		}
	}
	if errs := contractErrors(&bumped, older); errs != nil {
		t.Errorf("a protocol added and matrix_version bumped: %q", errs)
	}
	if errs := contractErrors(&bumped, same); errs != nil {
		t.Errorf("matrix_version bumped for a change this cannot see (what an outcome means): %q", errs)
	}

	// A document from before run.matrix_version existed was measured under 1.
	unversioned := load(withoutProtocol(t, path, last, func(doc map[string]any) {
		delete(doc["run"].(map[string]any), "matrix_version")
	}))
	if unversioned.Run.MatrixVersion != 0 {
		t.Fatalf("fixture: the document still carries matrix_version %d", unversioned.Run.MatrixVersion)
	}
	first := *reg
	first.MatrixVersion = 1
	if errs := contractErrors(&first, unversioned); len(errs) != 1 || !strings.Contains(errs[0], "is still 1") {
		t.Errorf("a protocol added since a document with no matrix_version: %q", errs)
	}

	// The version never goes back: a lower one would be taken for the
	// contract of the results already published under it.
	ahead := *same
	ahead.Run.MatrixVersion = reg.MatrixVersion + 1
	if errs := contractErrors(reg, &ahead); len(errs) != 1 || !strings.Contains(errs[0], "lower than") {
		t.Errorf("matrix_version below the release's: %q", errs)
	}
}

// The same through the command line, as verify.yml runs it: `cpm validate`
// exits 1 and says what to do.
func TestValidateFailsWhenTheContractMovedAndMatrixVersionDidNot(t *testing.T) {
	reg, path := latestRelease(t)
	last := reg.ProtocolOrder[len(reg.ProtocolOrder)-1]
	validate := func(results string) (int, string) {
		return cpm(t, false, "validate", "-registry", "../../variants.yaml", "-compose", "../../docker-compose.yml",
			"-annotations", t.TempDir(), "-results", results)
	}

	if code, out := validate(path); code != 0 || strings.Contains(out, "matrix_version") {
		t.Fatalf("the registry the release was written from: exit %d\n%s", code, out)
	}
	code, out := validate(withoutProtocol(t, path, last, nil))
	if code != 1 {
		t.Errorf("a release measured without %s: exit %d, want 1\n%s", last, code, out)
	}
	for _, want := range []string{"Bump matrix_version in variants.yaml", "protocols added: " + last} {
		if !strings.Contains(out, want) {
			t.Errorf("the output does not say %q:\n%s", want, out)
		}
	}
}
