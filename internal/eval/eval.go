// Package eval turns the headers the sink observed after a hop into the
// per-header statuses defined in DESIGN.md ("Per-header status semantics").
//
// It is pure: no I/O, no registry. The driver calls Evaluate at run time;
// the aggregator calls Native with the tool's native_headers from
// variants.yaml, re-reading the observed headers stored in the raw results.
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

// RoutingKeyMember is the baggage member (and ot-baggage-* suffix) the
// driver seeds; TracestateEntry is the foreign tracestate entry it seeds.
const (
	RoutingKeyMember = "sd-routing-key"
	TracestateEntry  = "sdtest=v1"
	CanaryHeader     = "x-sd-canary"
)

// Evaluate classifies the observed headers h against what was seeded.
func Evaluate(h map[string][]string, seed raw.Seeded) *raw.Status {
	_, canary := first(h, CanaryHeader)
	return &raw.Status{
		Traceparent:   Traceparent(h, seed),
		Tracestate:    Tracestate(h),
		Baggage:       Baggage(h, seed),
		OTBaggage:     OTBaggage(h, seed),
		DatadogNative: datadog(h, seed),
		CanaryLeak:    canary,
	}
}

// first returns the first value of a header. Used where the spec allows only
// one occurrence (traceparent) or where we seeded a single value.
func first(h map[string][]string, key string) (string, bool) {
	vs := h[key]
	if len(vs) == 0 {
		return "", false
	}
	return vs[0], true
}

// joined returns every value of a list-valued header combined with ",", as
// RFC 9110 §5.3 allows for repeated fields and as both W3C specs require
// (baggage §3.3.1, trace-context §3.3.1.1: multiple headers form one list).
func joined(h map[string][]string, key string) (string, bool) {
	vs := h[key]
	if len(vs) == 0 {
		return "", false
	}
	return strings.Join(vs, ","), true
}

// traceIDOf returns the trace-id field of a traceparent-formatted value.
func traceIDOf(v string) string {
	parts := strings.Split(strings.TrimSpace(v), "-")
	if len(parts) < 3 {
		return ""
	}
	return parts[1]
}

func continuity(sameTrace bool, same, other string) string {
	if sameTrace {
		return same
	}
	return other
}

// Traceparent: same_trace | new_trace | absent.
func Traceparent(h map[string][]string, seed raw.Seeded) string {
	v, ok := first(h, "traceparent")
	if !ok {
		return "absent"
	}
	return continuity(strings.EqualFold(traceIDOf(v), seed.TraceID), "same_trace", "new_trace")
}

// Tracestate: preserved | present_entry_lost | absent.
func Tracestate(h map[string][]string) string {
	v, ok := joined(h, "tracestate")
	if !ok {
		return "absent"
	}
	for _, entry := range strings.Split(v, ",") {
		if strings.TrimSpace(entry) == TracestateEntry {
			return "preserved"
		}
	}
	return "present_entry_lost"
}

// baggageHas reports whether the W3C baggage list v has a member key=want,
// ignoring member properties (";...") and surrounding whitespace.
func baggageHas(v, key, want string) bool {
	for _, member := range strings.Split(v, ",") {
		member, _, _ = strings.Cut(member, ";")
		k, val, ok := strings.Cut(member, "=")
		if ok && strings.TrimSpace(k) == key && strings.TrimSpace(val) == want {
			return true
		}
	}
	return false
}

// Baggage: propagated | partial | absent.
func Baggage(h map[string][]string, seed raw.Seeded) string {
	v, ok := joined(h, "baggage")
	if !ok {
		return "absent"
	}
	if baggageHas(v, RoutingKeyMember, seed.RoutingKey) {
		return "propagated"
	}
	return "partial"
}

// OTBaggage: propagated | partial | absent.
func OTBaggage(h map[string][]string, seed raw.Seeded) string {
	vs := h["ot-baggage-"+RoutingKeyMember]
	if len(vs) == 0 {
		return "absent"
	}
	for _, v := range vs {
		if v == seed.RoutingKey {
			return "propagated"
		}
	}
	return "partial"
}

// Native reports the tool's own (non-W3C) propagation for one hop, given the
// tool's native_headers from variants.yaml:
//
//   - "present_same_trace" / "present_new_trace" when a native header we know
//     how to decode is present (x-datadog-trace-id, elastic-apm-traceparent,
//     newrelic) and does / does not carry the seeded trace;
//   - "present" when another listed native header arrived;
//   - "absent" when none did;
//   - "" when the tool has no native headers (OTel, the baseline).
func Native(h map[string][]string, seed raw.Seeded, nativeHeaders []string) string {
	if len(nativeHeaders) == 0 {
		return ""
	}
	present := false
	for _, name := range nativeHeaders {
		if _, ok := first(h, name); !ok {
			continue
		}
		present = true
		var st string
		switch name {
		case "x-datadog-trace-id":
			st = datadog(h, seed)
		case "elastic-apm-traceparent":
			st = elastic(h, seed)
		case "newrelic":
			st = newrelic(h, seed)
		}
		if st != "" && st != "absent" {
			return st
		}
	}
	if present {
		return "present"
	}
	return "absent"
}

// datadog compares x-datadog-trace-id, the decimal low 64 bits of the
// 128-bit trace id.
func datadog(h map[string][]string, seed raw.Seeded) string {
	v, ok := first(h, "x-datadog-trace-id")
	if !ok {
		return "absent"
	}
	if len(seed.TraceID) != 32 {
		return "present_new_trace"
	}
	b, err := hex.DecodeString(seed.TraceID[16:])
	if err != nil {
		return "present_new_trace"
	}
	want := strconv.FormatUint(binary.BigEndian.Uint64(b), 10)
	return continuity(strings.TrimSpace(v) == want, "present_same_trace", "present_new_trace")
}

// elastic compares elastic-apm-traceparent, which has traceparent's format.
func elastic(h map[string][]string, seed raw.Seeded) string {
	v, ok := first(h, "elastic-apm-traceparent")
	if !ok {
		return "absent"
	}
	return continuity(strings.EqualFold(traceIDOf(v), seed.TraceID), "present_same_trace", "present_new_trace")
}

// newrelic decodes the base64 JSON `newrelic` header and compares its
// trace id ("d.tr").
func newrelic(h map[string][]string, seed raw.Seeded) string {
	v, ok := first(h, "newrelic")
	if !ok {
		return "absent"
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v))
	if err != nil {
		return "present"
	}
	var payload struct {
		D struct {
			TR string `json:"tr"`
		} `json:"d"`
	}
	if json.Unmarshal(b, &payload) != nil || payload.D.TR == "" {
		return "present"
	}
	return continuity(strings.EqualFold(payload.D.TR, seed.TraceID), "present_same_trace", "present_new_trace")
}
