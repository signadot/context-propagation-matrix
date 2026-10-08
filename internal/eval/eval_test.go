package eval

import (
	"encoding/base64"
	"testing"

	"github.com/signadot/context-propagation-matrix/internal/raw"
)

var seed = raw.Seeded{
	TraceID:    "239b89fcef379d4793e9097d24404872",
	SpanID:     "a9afda86883fab17",
	RoutingKey: "rk-3e26ef84",
}

// ddLow64 is the decimal low 64 bits of seed.TraceID (93e9097d24404872).
const ddLow64 = "10658060426232744050"

type hdrs = map[string][]string

func TestTraceparent(t *testing.T) {
	for name, tc := range map[string]struct {
		h    hdrs
		want string
	}{
		"absent":            {hdrs{}, "absent"},
		"same trace":        {hdrs{"traceparent": {"00-239b89fcef379d4793e9097d24404872-2138c1a30d31ad21-01"}}, "same_trace"},
		"same trace, upper": {hdrs{"traceparent": {"00-239B89FCEF379D4793E9097D24404872-2138c1a30d31ad21-01"}}, "same_trace"},
		"new trace":         {hdrs{"traceparent": {"00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"}}, "new_trace"},
		"garbage":           {hdrs{"traceparent": {"nonsense"}}, "new_trace"},
		"empty value list":  {hdrs{"traceparent": {}}, "absent"},
	} {
		if got := Traceparent(tc.h, seed); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestTracestate(t *testing.T) {
	for name, tc := range map[string]struct {
		h    hdrs
		want string
	}{
		"absent":              {hdrs{}, "absent"},
		"verbatim":            {hdrs{"tracestate": {"sdtest=v1"}}, "preserved"},
		"vendor prepended":    {hdrs{"tracestate": {"dd=s:1;t.dm:-0, sdtest=v1"}}, "preserved"},
		"entry lost":          {hdrs{"tracestate": {"dd=s:1"}}, "present_entry_lost"},
		"split across fields": {hdrs{"tracestate": {"dd=s:1", "sdtest=v1"}}, "preserved"},
	} {
		if got := Tracestate(tc.h); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestBaggage(t *testing.T) {
	for name, tc := range map[string]struct {
		h    hdrs
		want string
	}{
		"absent":              {hdrs{}, "absent"},
		"verbatim":            {hdrs{"baggage": {"sd-routing-key=rk-3e26ef84,team=alpha"}}, "propagated"},
		"reordered, spaces":   {hdrs{"baggage": {"team=alpha , sd-routing-key = rk-3e26ef84"}}, "propagated"},
		"member properties":   {hdrs{"baggage": {"sd-routing-key=rk-3e26ef84;prop=1"}}, "propagated"},
		"key missing":         {hdrs{"baggage": {"team=alpha"}}, "partial"},
		"wrong value":         {hdrs{"baggage": {"sd-routing-key=rk-other"}}, "partial"},
		"split across fields": {hdrs{"baggage": {"team=alpha", "sd-routing-key=rk-3e26ef84"}}, "propagated"},
		"duplicate key":       {hdrs{"baggage": {"sd-routing-key=stale,sd-routing-key=rk-3e26ef84"}}, "propagated"},
	} {
		if got := Baggage(tc.h, seed); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestOTBaggage(t *testing.T) {
	for name, tc := range map[string]struct {
		h    hdrs
		want string
	}{
		"absent":      {hdrs{}, "absent"},
		"propagated":  {hdrs{"ot-baggage-sd-routing-key": {"rk-3e26ef84"}}, "propagated"},
		"wrong value": {hdrs{"ot-baggage-sd-routing-key": {"rk-other"}}, "partial"},
	} {
		if got := OTBaggage(tc.h, seed); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestEvaluateCanary(t *testing.T) {
	if Evaluate(hdrs{}, seed).CanaryLeak {
		t.Error("canary reported without the header")
	}
	if !Evaluate(hdrs{"x-sd-canary": {"id"}}, seed).CanaryLeak {
		t.Error("canary leak not detected")
	}
}

func nrHeader(traceID string) string {
	return base64.StdEncoding.EncodeToString([]byte(`{"v":[0,1],"d":{"ty":"App","tr":"` + traceID + `","sa":true}}`))
}

func TestNative(t *testing.T) {
	dd := []string{"x-datadog-trace-id", "x-datadog-parent-id"}
	el := []string{"elastic-apm-traceparent"}
	nr := []string{"newrelic"}
	for name, tc := range map[string]struct {
		h      hdrs
		native []string
		want   string
	}{
		"tool without native headers": {hdrs{"x-datadog-trace-id": {ddLow64}}, nil, ""},
		"datadog absent":              {hdrs{}, dd, "absent"},
		"datadog same trace":          {hdrs{"x-datadog-trace-id": {ddLow64}}, dd, "present_same_trace"},
		"datadog new trace":           {hdrs{"x-datadog-trace-id": {"42"}}, dd, "present_new_trace"},
		"datadog parent only":         {hdrs{"x-datadog-parent-id": {"42"}}, dd, "present"},
		"elastic same trace":          {hdrs{"elastic-apm-traceparent": {"00-239b89fcef379d4793e9097d24404872-b54da95b3407fd1b-01"}}, el, "present_same_trace"},
		"elastic new trace":           {hdrs{"elastic-apm-traceparent": {"00-0af7651916cd43dd8448eb211c80319c-b54da95b3407fd1b-01"}}, el, "present_new_trace"},
		"newrelic same trace":         {hdrs{"newrelic": {nrHeader(seed.TraceID)}}, nr, "present_same_trace"},
		"newrelic new trace":          {hdrs{"newrelic": {nrHeader("0af7651916cd43dd8448eb211c80319c")}}, nr, "present_new_trace"},
		"newrelic undecodable":        {hdrs{"newrelic": {"%%%"}}, nr, "present"},
		"other vendor header":         {hdrs{"sentry-trace": {"x"}}, []string{"sentry-trace"}, "present"},
	} {
		if got := Native(tc.h, seed, tc.native); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

// TestCommittedRawResults re-evaluates every recorded hop under
// results/raw and checks the statuses the driver wrote at run time still
// come out the same: moving the evaluation must not change a single cell.
func TestCommittedRawResults(t *testing.T) {
	vs, err := raw.LoadDir("../../results/raw")
	if err != nil {
		t.Skip(err)
	}
	n := 0
	for _, v := range vs {
		for _, tr := range v.Tests {
			if tr.Status == nil || tr.Observed == nil || tr.Seeded == nil {
				continue
			}
			got := Evaluate(tr.Observed.Headers, *tr.Seeded)
			if *got != *tr.Status {
				t.Errorf("%s %s: re-evaluated %+v, recorded %+v", v.Variant, tr.Boundary, *got, *tr.Status)
			}
			n++
		}
	}
	if n == 0 {
		t.Fatal("no hops evaluated")
	}
	t.Logf("%d recorded hops re-evaluated identically", n)
}
