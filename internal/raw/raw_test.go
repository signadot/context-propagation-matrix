package raw

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The driver marshals these types, so loading a file a driver wrote and
// marshalling it again must give the same document back: no key gained,
// lost or renamed. Otherwise a tag edited here changes the raw format
// without anyone having decided to. Reads the hand-curated raw files tracked
// under results/, and the directory named by CPM_RAW_DIR when that is set.
func TestDriverFormatRoundTrips(t *testing.T) {
	dirs := []string{"../../results/experiments", "../../results/nokey"}
	if dir := os.Getenv("CPM_RAW_DIR"); dir != "" {
		dirs = append(dirs, dir)
	}
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.json"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no raw results in %s (%v)", dir, err)
		}
		identical := 0
		for _, f := range files {
			onDisk, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			v, err := LoadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			written, err := json.MarshalIndent(v, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			var want, got any
			if err := json.Unmarshal(onDisk, &want); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(written, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s: not the same document after loading and marshalling it", f)
			}
			if bytes.Equal(written, bytes.TrimRight(onDisk, "\n")) {
				identical++
			}
		}
		t.Logf("%s: %d files, %d of them byte-identical to what the driver would write", dir, len(files), identical)
	}
}

// What the driver leaves out, which the tracked files only partly show: an
// undelivered hop has no observed or status key, and a relay that reports no
// config has no config key.
func TestDriverFormatOmitsEmpty(t *testing.T) {
	b, err := json.Marshal(VariantResult{Variant: "none-go", Tests: []TestResult{
		{Boundary: "grpc->http", TestID: "none-go--grpc-http--3e26ef84", Seeded: &Seeded{}, Error: "send: refused"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var variant map[string]json.RawMessage
	if err := json.Unmarshal(b, &variant); err != nil {
		t.Fatal(err)
	}
	var tests []map[string]json.RawMessage
	if err := json.Unmarshal(variant["tests"], &tests); err != nil || len(tests) != 1 {
		t.Fatalf("tests: %v in %s", err, b)
	}
	for _, key := range []string{"config", "info"} {
		if _, ok := variant[key]; ok {
			t.Errorf("empty %s is written: %s", key, b)
		}
	}
	for _, key := range []string{"observed", "status"} {
		if _, ok := tests[0][key]; ok {
			t.Errorf("undelivered hop has a %s key: %s", key, b)
		}
	}
	if _, ok := tests[0]["seeded"]; !ok {
		t.Errorf("seeded is missing: %s", b)
	}
}
