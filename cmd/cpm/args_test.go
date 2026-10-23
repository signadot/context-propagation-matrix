package main

import (
	"flag"
	"reflect"
	"testing"
)

// The workflows put flags after the positional argument (`cpm summary
// results.json -changes changes.json`); Go's flag package alone would treat
// `-changes` and its value as positionals.
func TestParsePositionals(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		n       int
		wantPos []string
		wantOut string
		wantErr bool
	}{
		{"flags first", []string{"-out", "c.json", "a.json", "b.json"}, 2, []string{"a.json", "b.json"}, "c.json", false},
		{"flags last", []string{"a.json", "b.json", "-out", "c.json"}, 2, []string{"a.json", "b.json"}, "c.json", false},
		{"interspersed", []string{"a.json", "-out", "c.json", "b.json"}, 2, []string{"a.json", "b.json"}, "c.json", false},
		{"no flags", []string{"a.json"}, 1, []string{"a.json"}, "", false},
		{"too many", []string{"a.json", "b.json"}, 1, nil, "", true},
		{"too few", []string{"-out", "c.json"}, 1, nil, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := flag.NewFlagSet(tc.name, flag.ContinueOnError)
			out := fs.String("out", "", "")
			pos, err := parsePositionals(fs, tc.args, tc.n)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if !reflect.DeepEqual(pos, tc.wantPos) {
				t.Errorf("positionals = %v, want %v", pos, tc.wantPos)
			}
			if *out != tc.wantOut {
				t.Errorf("-out = %q, want %q", *out, tc.wantOut)
			}
		})
	}
}
