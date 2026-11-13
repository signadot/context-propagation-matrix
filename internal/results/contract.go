package results

import (
	"fmt"
	"sort"
	"strings"

	"github.com/signadot/context-propagation-matrix/internal/registry"
)

// The measurement contract is what variants.yaml's matrix_version counts:
// the cells a variant is scored on (headers × boundaries) and what each
// outcome means. A document records the dimensions it was written with, so
// the part of the contract that is data can be compared with the registry.

// ContractDiff lists how the contract in reg differs from the one the
// document with dimensions d was written under, one line per difference;
// nil when it does not.
//
// Protocols, boundaries and headers are compared as sets: their order is
// the column order, which is presentation. For a header in both, the fields
// a cell's outcome is computed from are compared (wire_name, raw_field,
// success, partial, new_trace); its display name, question and spec link
// are not part of the contract.
//
// A change in what an outcome means that leaves all of these alone (the
// evaluation code, the seeded values) cannot be seen here. matrix_version is
// still bumped by hand for those.
func ContractDiff(reg *registry.Registry, d *Dimensions) []string {
	var out []string
	var docProtocols, docHeaders []string
	for _, p := range d.Protocols {
		docProtocols = append(docProtocols, p.ID)
	}
	was := map[string]*registry.Header{}
	for _, h := range d.Headers {
		docHeaders = append(docHeaders, h.ID)
		was[h.ID] = h
	}
	out = append(out, setDiff("protocols", docProtocols, reg.ProtocolOrder)...)
	out = append(out, setDiff("boundaries", d.Boundaries, reg.Boundaries)...)
	out = append(out, setDiff("headers", docHeaders, reg.HeaderOrder)...)

	for _, id := range reg.HeaderOrder {
		old, now := was[id], reg.Headers[id]
		if old == nil {
			continue
		}
		for _, f := range []struct {
			name     string
			old, now []string
		}{
			{"wire_name", []string{old.WireName}, []string{now.WireName}},
			{"raw_field", []string{old.RawField}, []string{now.RawField}},
			{"success", old.Success, now.Success},
			{"partial", old.Partial, now.Partial},
			{"new_trace", old.NewTrace, now.NewTrace},
		} {
			if a, b := sorted(f.old), sorted(f.now); strings.Join(a, "\x00") != strings.Join(b, "\x00") {
				out = append(out, fmt.Sprintf("header %s: %s was [%s], is [%s]", id, f.name, strings.Join(a, ", "), strings.Join(b, ", ")))
			}
		}
	}
	return out
}

// setDiff reports what was added to and removed from a set, in the order
// the lists give.
func setDiff(what string, old, now []string) []string {
	in := func(list []string) map[string]bool {
		m := map[string]bool{}
		for _, s := range list {
			m[s] = true
		}
		return m
	}
	inOld, inNow := in(old), in(now)
	var added, removed []string
	for _, s := range now {
		if !inOld[s] {
			added = append(added, s)
		}
	}
	for _, s := range old {
		if !inNow[s] {
			removed = append(removed, s)
		}
	}
	var out []string
	if len(added) > 0 {
		out = append(out, fmt.Sprintf("%s added: %s", what, strings.Join(added, ", ")))
	}
	if len(removed) > 0 {
		out = append(out, fmt.Sprintf("%s removed: %s", what, strings.Join(removed, ", ")))
	}
	return out
}

func sorted(list []string) []string {
	out := append([]string{}, list...)
	sort.Strings(out)
	return out
}
