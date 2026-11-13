package results

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/signadot/context-propagation-matrix/internal/registry"
)

// written returns the dimensions a document aggregated from reg carries, as
// copies the test can edit without touching reg.
func written(reg *registry.Registry) *Dimensions {
	d := dimensions(reg)
	d.Boundaries = append([]string{}, d.Boundaries...)
	for i, p := range d.Protocols {
		c := *p
		d.Protocols[i] = &c
	}
	for i, h := range d.Headers {
		c := *h
		c.Success = append([]string{}, h.Success...)
		c.Partial = append([]string{}, h.Partial...)
		c.NewTrace = append([]string{}, h.NewTrace...)
		d.Headers[i] = &c
	}
	return &d
}

// crossing lists the boundaries of reg that have protocol p on either side,
// in the registry's order.
func crossing(reg *registry.Registry, p string) []string {
	var out []string
	for _, b := range reg.Boundaries {
		if in, to, _ := strings.Cut(b, "->"); in == p || to == p {
			out = append(out, b)
		}
	}
	return out
}

// ContractDiff against the checked-in registry: each case is a document
// written under a registry that differed from today's in one way.
func TestContractDiff(t *testing.T) {
	reg, err := registry.Load("../../variants.yaml")
	if err != nil {
		t.Fatal(err)
	}
	last := reg.ProtocolOrder[len(reg.ProtocolOrder)-1]
	header := reg.HeaderOrder[0]
	scored := reg.Headers[header]
	if len(scored.Partial) == 0 {
		t.Fatalf("fixture: header %s has no partial statuses, so the case that drops them proves nothing", header)
	}

	cases := []struct {
		name string
		edit func(d *Dimensions)
		want []string
	}{
		{"written under this registry", func(d *Dimensions) {}, nil},
		{"the same things in another order", func(d *Dimensions) {
			slices.Reverse(d.Protocols)
			slices.Reverse(d.Boundaries)
			slices.Reverse(d.Headers)
			for _, h := range d.Headers {
				slices.Reverse(h.Success)
				slices.Reverse(h.Partial)
				slices.Reverse(h.NewTrace)
			}
		}, nil},
		{"only what a header is called and asked", func(d *Dimensions) {
			d.Headers[0].DisplayName += " (renamed)"
			d.Headers[0].Question += " Really?"
			d.Headers[0].Spec += "#moved"
			d.Protocols[0].DisplayName += " (renamed)"
		}, nil},
		{"before the registry's last protocol was added", func(d *Dimensions) {
			d.Protocols = d.Protocols[:len(d.Protocols)-1]
			var kept []string
			for _, b := range d.Boundaries {
				if in, to, _ := strings.Cut(b, "->"); in != last && to != last {
					kept = append(kept, b)
				}
			}
			d.Boundaries = kept
		}, []string{
			"protocols added: " + last,
			"boundaries added: " + strings.Join(crossing(reg, last), ", "),
		}},
		{"with a protocol the registry no longer has", func(d *Dimensions) {
			d.Protocols = append(d.Protocols, &registry.Protocol{ID: "nats"})
			d.Boundaries = append(d.Boundaries, "nats->nats", last+"->nats")
		}, []string{
			"protocols removed: nats",
			"boundaries removed: nats->nats, " + last + "->nats",
		}},
		{"before the registry's first header was added", func(d *Dimensions) {
			d.Headers = d.Headers[1:]
		}, []string{"headers added: " + header}},
		{"with a header the registry no longer has", func(d *Dimensions) {
			d.Headers = append(d.Headers, &registry.Header{ID: "b3", WireName: "b3", Success: []string{"propagated"}})
		}, []string{"headers removed: b3"}},
		{"when one more status counted as a pass", func(d *Dimensions) {
			d.Headers[0].Success = append(d.Headers[0].Success, "zz-was-a-pass")
		}, []string{fmt.Sprintf("header %s: success was [%s], is [%s]", header,
			strings.Join(append(sorted(scored.Success), "zz-was-a-pass"), ", "), strings.Join(sorted(scored.Success), ", "))}},
		{"when a status that is now partial was not", func(d *Dimensions) {
			d.Headers[0].Partial = nil
		}, []string{fmt.Sprintf("header %s: partial was [], is [%s]", header, strings.Join(sorted(scored.Partial), ", "))}},
		{"when the header was read from another field under another name", func(d *Dimensions) {
			d.Headers[0].WireName = "x-old-name"
			d.Headers[0].RawField = "old_field"
		}, []string{
			fmt.Sprintf("header %s: wire_name was [x-old-name], is [%s]", header, scored.WireName),
			fmt.Sprintf("header %s: raw_field was [old_field], is [%s]", header, scored.RawField),
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := written(reg)
			c.edit(d)
			if got := ContractDiff(reg, d); !reflect.DeepEqual(got, c.want) {
				t.Errorf("ContractDiff:\n got  %q\n want %q", got, c.want)
			}
		})
	}
}
