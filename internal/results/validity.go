package results

import (
	"fmt"
	"strings"

	"github.com/signadot/context-propagation-matrix/internal/eval"
)

// Validity: a variant can run all its hops and still not be a measurement of
// its tool. Judge names the ways that happens. It reads only the hops (and
// the dimensions), so a document written before the verdict was recorded is
// judged the same way as a new one; `cpm aggregate` stores the verdict as
// Variant.Invalid and `cpm gate` decides from it what may be published.

// Problem kinds.
const (
	// The canary reached the sink: the relay copies inbound headers itself,
	// so a pass is the relay's doing, not the tool's.
	ProblemCanaryLeak = "canary_leak"
	// A hop arrived over a protocol other than its boundary's outbound one,
	// so its cells describe a boundary that was not exercised.
	ProblemTransportMismatch = "transport_mismatch"
	// An instrumented relay sent nothing its uninstrumented baseline does
	// not send as well: the tool did not attach, and its fails are not
	// findings about it.
	ProblemAgentInactive = "agent_inactive"
)

// Problem is one reason a variant's cells are not a result about its tool.
// Boundaries names the hops it was seen on; it is empty for agent_inactive,
// which is a statement about every delivered hop.
type Problem struct {
	Kind       string   `json:"kind"`
	Boundaries []string `json:"boundaries,omitempty"`
	Detail     string   `json:"detail"`
}

// otBaggagePrefix: OpenTracing baggage is one header per item, so tracers
// emit names the registry cannot list (Datadog's Go tracer turns the seeded
// W3C `team` member into `ot-baggage-team`).
const otBaggagePrefix = "ot-baggage-"

// tracingHeaders returns the header names that are a sign of tracing, lower
// case: the wire name of every header the matrix scores and every tool's
// native headers, both as the registry wrote them into the document. A
// header that is neither (and not ot-baggage-*) is never counted, whoever
// sent it: client libraries differ in the plain headers they send, and a
// name the registry does not know must not be what makes an agent look
// attached.
func (d *Dimensions) tracingHeaders() map[string]bool {
	names := map[string]bool{}
	for _, h := range d.Headers {
		names[strings.ToLower(h.WireName)] = true
	}
	for _, t := range d.Tools {
		for _, n := range t.NativeHeaders {
			names[strings.ToLower(n)] = true
		}
	}
	delete(names, "")
	return names
}

// tracingSeen returns the tracing header names the sink observed on h.
func tracingSeen(h *Hop, names map[string]bool) map[string]bool {
	seen := map[string]bool{}
	for k := range h.Observed {
		if k = strings.ToLower(k); names[k] || strings.HasPrefix(k, otBaggagePrefix) {
			seen[k] = true
		}
	}
	return seen
}

// canarySeen reports whether the canary arrived on h: what the driver
// concluded, or the header itself among what the sink recorded. The header
// is the control the driver seeds on every hop (driver/main.go), under the
// name this hop was seeded with: it was renamed on 16 November 2026.
func canarySeen(h *Hop) bool {
	if h.CanaryLeak {
		return true
	}
	canary := eval.Canary(h.Seeded)
	for k := range h.Observed {
		if strings.EqualFold(k, canary) {
			return true
		}
	}
	return false
}

// IsBaseline reports whether v is an uninstrumented variant: its tool is one
// the registry marks is_baseline in the document's dimensions.
func (r *Results) IsBaseline(v *Variant) bool {
	for _, t := range r.Dimensions.Tools {
		if t.ID == v.Tool {
			return t.IsBaseline
		}
	}
	return false
}

// BaselineFor returns the measured uninstrumented variant of v's language
// (`none-<language>`) to judge v against: the one in r when r has it, as a
// matrix run does, otherwise the one in other, which may be nil (a back-test
// document holds a single variant, and other is then the latest run). It
// returns nil when neither has one and when v is itself a baseline.
func (r *Results) BaselineFor(v *Variant, other *Results) *Variant {
	if r.IsBaseline(v) {
		return nil
	}
	for _, doc := range []*Results{r, other} {
		if doc == nil {
			continue
		}
		for _, b := range doc.Variants {
			if !b.Missing && b.Language == v.Language && doc.IsBaseline(b) {
				return b
			}
		}
	}
	return nil
}

// Judge returns what makes v's measurement invalid, nil when nothing does.
// Only delivered hops are evidence: an undelivered one (PHP serves no gRPC)
// observed nothing to judge.
//
// agent_inactive is for instrumented variants only, and means that on every
// delivered hop the tracing headers observed are a subset of what baseline
// (see BaselineFor) shows on the same boundary. The comparison has to be per
// boundary and against the baseline rather than against nothing: the .NET
// runtime sends traceparent, tracestate and baggage on its HTTP and gRPC
// clients with no agent installed, and on http->http a working OpenTelemetry
// .NET agent sends exactly those names too. With a nil baseline, or on a
// boundary the baseline did not deliver, the comparison is with the empty
// set — "no tracing header at all" — which cannot see an inactive agent on
// such a runtime.
func (r *Results) Judge(v, baseline *Variant) []Problem {
	if v.Missing {
		return nil
	}
	names := r.Dimensions.tracingHeaders()
	base := map[string]map[string]bool{}
	if baseline != nil {
		for i := range baseline.Hops {
			if h := &baseline.Hops[i]; h.Delivered {
				base[h.Boundary] = tracingSeen(h, names)
			}
		}
	}

	var leaks, mismatches, arrivals []string
	delivered, own := 0, false
	for i := range v.Hops {
		h := &v.Hops[i]
		if !h.Delivered {
			continue
		}
		delivered++
		if canarySeen(h) {
			leaks = append(leaks, h.Boundary)
		}
		if h.Transport != h.Outbound {
			mismatches = append(mismatches, h.Boundary)
			over := h.Transport
			if over == "" {
				over = "an unrecorded transport"
			}
			arrivals = append(arrivals, h.Boundary+" over "+over)
		}
		for name := range tracingSeen(h, names) {
			if !base[h.Boundary][name] {
				own = true
			}
		}
	}

	var out []Problem
	if len(leaks) > 0 {
		out = append(out, Problem{Kind: ProblemCanaryLeak, Boundaries: leaks,
			Detail: fmt.Sprintf("the control header reached the sink on %d of %d delivered hop(s): the relay forwards inbound headers itself", len(leaks), delivered)})
	}
	if len(mismatches) > 0 {
		out = append(out, Problem{Kind: ProblemTransportMismatch, Boundaries: mismatches,
			Detail: fmt.Sprintf("%d of %d delivered hop(s) did not arrive over the boundary's outbound protocol: %s", len(mismatches), delivered, strings.Join(arrivals, ", "))})
	}
	// No delivered hop is no evidence either way, not an inactive agent.
	if !own && delivered > 0 && !r.IsBaseline(v) {
		p := Problem{Kind: ProblemAgentInactive}
		if baseline != nil {
			from := ""
			if baseline.MeasuredIn != "" {
				from = " (measured in " + baseline.MeasuredIn + ")"
			}
			p.Detail = fmt.Sprintf("no tracing header beyond what the uninstrumented %s%s sends on any of %d delivered hop(s)", baseline.ID, from, delivered)
		} else {
			p.Detail = fmt.Sprintf("no tracing header on any of %d delivered hop(s); no uninstrumented %s baseline was available to compare with", delivered, v.Language)
		}
		out = append(out, p)
	}
	return out
}

// SetValidity records Judge's verdict on every variant of r, taking
// baselines from r itself and then from other (nil when r is a full run).
func (r *Results) SetValidity(other *Results) {
	for _, v := range r.Variants {
		v.Invalid = r.Judge(v, r.BaselineFor(v, other))
	}
}
