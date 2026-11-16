// Package eval turns the headers the sink observed after a hop into the
// statuses defined in DESIGN.md ("Per-header status semantics").
//
// It is pure: no I/O, no registry. The driver calls Evaluate at run time and
// stores the result in the raw file. The aggregator calls Native, Span and
// Flags on the observed headers the raw file also stores, so those are
// filled in for results measured before they existed.
package eval

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/signadot/context-propagation-matrix/internal/raw"
)

// What the driver seeds besides the trace context: the baggage member (and
// ot-baggage-* suffix) that carries the routing key, the foreign tracestate
// entry, and the control header no instrumentation forwards. The driver
// seeds from these, so this is the one place to change them. They name no
// vendor and no product: nothing under test has a reason to treat them
// differently from any other application's.
const (
	RoutingKeyMember = "routing-key"
	TracestateEntry  = "cptest=v1"
	CanaryHeader     = "x-cp-canary"
)

// names is what to look for after a hop.
type names struct {
	member string // the baggage member that carries the routing key
	entry  string // the foreign tracestate entry
	ot     string // the ot-baggage-* header
	canary string // the control header
}

// namesOf returns the names a hop was seeded with, read from the headers the
// seed records. A result is judged against what it was sent, so one measured
// when the seeds had other names (they were renamed at matrix_version 2)
// still evaluates to the statuses recorded for it. A seed that records no
// headers is taken to have used today's.
func namesOf(seed raw.Seeded) names {
	n := names{RoutingKeyMember, TracestateEntry, "ot-baggage-" + RoutingKeyMember, Canary(seed.Headers)}
	for name, value := range seed.Headers {
		switch {
		case name == "tracestate":
			n.entry = value
		case name == "baggage":
			for member, v := range parseBaggage(value) {
				if v == seed.RoutingKey {
					n.member = member
				}
			}
		case strings.HasPrefix(name, "ot-baggage-"):
			n.ot = name
		}
	}
	return n
}

// Canary returns the name of the control header among the headers a hop was
// seeded with. It has always been called x-…-canary; when the seeded headers
// are not recorded, or hold none of that form, it is today's.
func Canary(seeded map[string]string) string {
	for name := range seeded {
		if strings.HasSuffix(name, "-canary") {
			return name
		}
	}
	return CanaryHeader
}

// The values Span returns.
const (
	NewSpan  = "new_span"
	SameSpan = "same_span"
)

// Evaluate classifies the observed headers h against what was seeded: the
// status the driver records for a delivered hop.
func Evaluate(h map[string][]string, seed raw.Seeded) *raw.Status {
	_, canary := first(h, namesOf(seed).canary)
	return &raw.Status{
		Traceparent: Traceparent(h, seed),
		Tracestate:  Tracestate(h, seed),
		Baggage:     Baggage(h, seed),
		OTBaggage:   OTBaggage(h, seed),
		// The driver has no registry, and datadog_native has always meant
		// x-datadog-trace-id whatever the tool under test.
		DatadogNative: Native(h, seed, []string{"x-datadog-trace-id"}),
		CanaryLeak:    canary,
	}
}

// first returns the first value of a header: for headers that may occur only
// once (traceparent) or that are not lists (ot-baggage-*, the native ones).
func first(h map[string][]string, key string) (string, bool) {
	vs := h[key]
	if len(vs) == 0 {
		return "", false
	}
	return vs[0], true
}

// joined returns every value of a list header combined with ",". A carrier
// may hold several fields of the same name, and RFC 9110 §5.3 reads them as
// one list in that order; tracestate and baggage are both defined that way,
// so a relay that splits one across two fields has still propagated it.
func joined(h map[string][]string, key string) (string, bool) {
	vs := h[key]
	if len(vs) == 0 {
		return "", false
	}
	return strings.Join(vs, ","), true
}

// traceparent holds the fields of a traceparent-formatted value
// (version-traceid-parentid-flags) that the evaluation reads.
type traceparent struct {
	traceID, parentID, flags string
}

// parseTraceparent splits a traceparent-formatted value. It does not
// validate: the evaluation compares ids, so anything that has a trace-id and
// a parent-id field parses, and flags is empty when that field is missing.
func parseTraceparent(v string) (traceparent, bool) {
	parts := strings.Split(strings.TrimSpace(v), "-")
	if len(parts) < 3 {
		return traceparent{}, false
	}
	tp := traceparent{traceID: parts[1], parentID: parts[2]}
	if len(parts) > 3 {
		tp.flags = parts[3]
	}
	return tp, true
}

// continued returns the observed traceparent when it carries the seeded
// trace id.
func continued(h map[string][]string, seed raw.Seeded) (traceparent, bool) {
	v, ok := first(h, "traceparent")
	if !ok {
		return traceparent{}, false
	}
	tp, ok := parseTraceparent(v)
	return tp, ok && strings.EqualFold(tp.traceID, seed.TraceID)
}

// Traceparent: same_trace | new_trace | absent.
func Traceparent(h map[string][]string, seed raw.Seeded) string {
	if _, ok := first(h, "traceparent"); !ok {
		return "absent"
	}
	if _, ok := continued(h, seed); ok {
		return "same_trace"
	}
	return "new_trace"
}

// Span says whether a hop that continued the seeded trace did so with a span
// of its own: NewSpan when the observed parent-id differs from the seeded
// span id, SameSpan when it is the seeded one. An active tracer always
// starts a span on the inbound side, so SameSpan means the seeded
// traceparent was passed through verbatim: the trace id survived without
// any instrumentation taking part, which same_trace alone cannot tell apart
// from propagation. Empty when the traceparent is not same_trace.
func Span(h map[string][]string, seed raw.Seeded) string {
	tp, ok := continued(h, seed)
	if !ok {
		return ""
	}
	if strings.EqualFold(tp.parentID, seed.SpanID) {
		return SameSpan
	}
	return NewSpan
}

// Flags returns the trace-flags field of a traceparent that continued the
// seeded trace, as observed. The driver seeds "01" (sampled); a relay that
// sends "00" keeps the trace id but marks the trace not sampled, which is
// what the services after it go by when deciding whether to record it.
// Empty when the traceparent is not same_trace or has no flags field.
func Flags(h map[string][]string, seed raw.Seeded) string {
	tp, ok := continued(h, seed)
	if !ok {
		return ""
	}
	return tp.flags
}

// Tracestate: preserved | present_entry_lost | absent.
func Tracestate(h map[string][]string, seed raw.Seeded) string {
	v, ok := joined(h, "tracestate")
	if !ok {
		return "absent"
	}
	want := namesOf(seed).entry
	for _, entry := range strings.Split(v, ",") {
		if strings.TrimSpace(entry) == want {
			return "preserved"
		}
	}
	return "present_entry_lost"
}

// parseBaggage reads a W3C baggage list into key/value pairs, dropping
// member properties (";...") and surrounding whitespace. When a key is
// repeated the last member wins, as it does in the parsers a service
// downstream of the relay would read the header with.
func parseBaggage(v string) map[string]string {
	out := map[string]string{}
	for _, member := range strings.Split(v, ",") {
		member, _, _ = strings.Cut(member, ";")
		if k, val, ok := strings.Cut(member, "="); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(val)
		}
	}
	return out
}

// Baggage: propagated | partial | absent.
func Baggage(h map[string][]string, seed raw.Seeded) string {
	v, ok := joined(h, "baggage")
	if !ok {
		return "absent"
	}
	if parseBaggage(v)[namesOf(seed).member] == seed.RoutingKey {
		return "propagated"
	}
	return "partial"
}

// OTBaggage: propagated | partial | absent.
func OTBaggage(h map[string][]string, seed raw.Seeded) string {
	v, ok := first(h, namesOf(seed).ot)
	if !ok {
		return "absent"
	}
	if v == seed.RoutingKey {
		return "propagated"
	}
	return "partial"
}

// nativeDecoders holds, for each vendor header whose trace id this package
// can read, the function that says whether a value carries the seeded trace.
// A value that cannot be decoded does not: the header arrived and is not on
// the seeded trace, as a malformed traceparent counts as new_trace.
var nativeDecoders = map[string]func(v string, seed raw.Seeded) bool{
	"x-datadog-trace-id":      datadogSameTrace,
	"elastic-apm-traceparent": elasticSameTrace,
	"newrelic":                newRelicSameTrace,
}

// Native reports whether the tool's own (non-W3C) trace header continued the
// seeded trace on one hop, given the tool's native_headers from
// variants.yaml: present_same_trace | present_new_trace | absent. Only the
// listed headers that carry a trace id are looked at (x-datadog-parent-id on
// its own says nothing about the trace). Empty when there is none, which is
// the case for tools without native headers (OTel, the baselines) and for a
// tool whose native headers have no decoder here yet.
func Native(h map[string][]string, seed raw.Seeded, nativeHeaders []string) string {
	status := ""
	for _, name := range nativeHeaders {
		sameTrace, ok := nativeDecoders[name]
		if !ok {
			continue
		}
		v, present := first(h, name)
		switch {
		case !present:
			if status == "" {
				status = "absent"
			}
		case sameTrace(v, seed):
			return "present_same_trace"
		default:
			status = "present_new_trace"
		}
	}
	return status
}

// datadogSameTrace compares x-datadog-trace-id, which is the low 64 bits of
// the 128-bit trace id in decimal.
func datadogSameTrace(v string, seed raw.Seeded) bool {
	if len(seed.TraceID) != 32 {
		return false
	}
	b, err := hex.DecodeString(seed.TraceID[16:])
	if err != nil {
		return false
	}
	return strings.TrimSpace(v) == strconv.FormatUint(binary.BigEndian.Uint64(b), 10)
}

// elasticSameTrace compares elastic-apm-traceparent, the name Elastic agents
// used before W3C trace context and still send; its value has traceparent's
// format.
func elasticSameTrace(v string, seed raw.Seeded) bool {
	tp, ok := parseTraceparent(v)
	return ok && strings.EqualFold(tp.traceID, seed.TraceID)
}

// newRelicSameTrace compares the trace id ("d.tr") in the base64-encoded
// JSON payload of the newrelic header.
func newRelicSameTrace(v string, seed raw.Seeded) bool {
	// Padding carries no information, so a payload sent without it still
	// decodes.
	b, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(strings.TrimSpace(v), "="))
	if err != nil {
		return false
	}
	var payload struct {
		D struct {
			TR string `json:"tr"`
		} `json:"d"`
	}
	if json.Unmarshal(b, &payload) != nil {
		return false
	}
	return strings.EqualFold(payload.D.TR, seed.TraceID)
}
