package eval

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/signadot/context-propagation-matrix/internal/raw"
)

var seed = raw.Seeded{
	TraceID:    "239b89fcef379d4793e9097d24404872",
	SpanID:     "a9afda86883fab17",
	RoutingKey: "rk-3e26ef84",
}

const (
	// What the driver sends for seed, and the same trace continued by a
	// relay that started its own span.
	seededTraceparent = "00-239b89fcef379d4793e9097d24404872-a9afda86883fab17-01"
	newSpan           = "00-239b89fcef379d4793e9097d24404872-2138c1a30d31ad21-01"
	otherTrace        = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	otherTraceID      = "0af7651916cd43dd8448eb211c80319c"

	// ddLow64 is the low 64 bits of seed.TraceID (93e9097d24404872) in
	// decimal.
	ddLow64 = "10658060426232744050"
)

type hdrs = map[string][]string

func TestTraceparent(t *testing.T) {
	for name, tc := range map[string]struct {
		h    hdrs
		want string
	}{
		"absent":                      {hdrs{}, "absent"},
		"no values":                   {hdrs{"traceparent": {}}, "absent"},
		"same trace, new span":        {hdrs{"traceparent": {newSpan}}, "same_trace"},
		"same trace, seeded span":     {hdrs{"traceparent": {seededTraceparent}}, "same_trace"},
		"same trace, upper case":      {hdrs{"traceparent": {strings.ToUpper(newSpan)}}, "same_trace"},
		"same trace, padded":          {hdrs{"traceparent": {" " + newSpan + " "}}, "same_trace"},
		"same trace, no flags":        {hdrs{"traceparent": {"00-239b89fcef379d4793e9097d24404872-2138c1a30d31ad21"}}, "same_trace"},
		"new trace":                   {hdrs{"traceparent": {otherTrace}}, "new_trace"},
		"not a traceparent":           {hdrs{"traceparent": {"nonsense"}}, "new_trace"},
		"trace id alone":              {hdrs{"traceparent": {seed.TraceID}}, "new_trace"},
		"no parent id field":          {hdrs{"traceparent": {"00-239b89fcef379d4793e9097d24404872"}}, "new_trace"},
		"empty value":                 {hdrs{"traceparent": {""}}, "new_trace"},
		"only the first value counts": {hdrs{"traceparent": {otherTrace, newSpan}}, "new_trace"},
	} {
		if got := Traceparent(tc.h, seed); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

// Span and Flags only speak about a traceparent that is same_trace.
func TestSpanAndFlags(t *testing.T) {
	for name, tc := range map[string]struct {
		h           hdrs
		span, flags string
	}{
		"absent":                      {hdrs{}, "", ""},
		"new trace":                   {hdrs{"traceparent": {otherTrace}}, "", ""},
		"new trace, seeded span id":   {hdrs{"traceparent": {"00-" + otherTraceID + "-a9afda86883fab17-01"}}, "", ""},
		"not a traceparent":           {hdrs{"traceparent": {"nonsense"}}, "", ""},
		"no parent id field":          {hdrs{"traceparent": {"00-239b89fcef379d4793e9097d24404872"}}, "", ""},
		"new span, sampled":           {hdrs{"traceparent": {newSpan}}, NewSpan, "01"},
		"new span, not sampled":       {hdrs{"traceparent": {"00-239b89fcef379d4793e9097d24404872-2138c1a30d31ad21-00"}}, NewSpan, "00"},
		"new span, other flag bits":   {hdrs{"traceparent": {"00-239b89fcef379d4793e9097d24404872-2138c1a30d31ad21-03"}}, NewSpan, "03"},
		"new span, no flags field":    {hdrs{"traceparent": {"00-239b89fcef379d4793e9097d24404872-2138c1a30d31ad21"}}, NewSpan, ""},
		"passed through verbatim":     {hdrs{"traceparent": {seededTraceparent}}, SameSpan, "01"},
		"passed through, upper case":  {hdrs{"traceparent": {strings.ToUpper(seededTraceparent)}}, SameSpan, "01"},
		"seeded span, flags cleared":  {hdrs{"traceparent": {"00-239b89fcef379d4793e9097d24404872-a9afda86883fab17-00"}}, SameSpan, "00"},
		"only the first value counts": {hdrs{"traceparent": {newSpan, seededTraceparent}}, NewSpan, "01"},
	} {
		if got := Span(tc.h, seed); got != tc.span {
			t.Errorf("%s: span %q, want %q", name, got, tc.span)
		}
		if got := Flags(tc.h, seed); got != tc.flags {
			t.Errorf("%s: flags %q, want %q", name, got, tc.flags)
		}
	}
}

func TestTracestate(t *testing.T) {
	for name, tc := range map[string]struct {
		h    hdrs
		want string
	}{
		"absent":                     {hdrs{}, "absent"},
		"no values":                  {hdrs{"tracestate": {}}, "absent"},
		"verbatim":                   {hdrs{"tracestate": {"cptest=v1"}}, "preserved"},
		"vendor entry prepended":     {hdrs{"tracestate": {"dd=s:1;t.dm:-0, cptest=v1"}}, "preserved"},
		"entry lost":                 {hdrs{"tracestate": {"dd=s:1"}}, "present_entry_lost"},
		"value changed":              {hdrs{"tracestate": {"cptest=v2"}}, "present_entry_lost"},
		"empty value":                {hdrs{"tracestate": {""}}, "present_entry_lost"},
		"second field has the entry": {hdrs{"tracestate": {"dd=s:1", "cptest=v1"}}, "preserved"},
		"first field has the entry":  {hdrs{"tracestate": {"cptest=v1", "dd=s:1"}}, "preserved"},
		"no field has the entry":     {hdrs{"tracestate": {"dd=s:1", "es=s:1"}}, "present_entry_lost"},
	} {
		if got := Tracestate(tc.h, seed); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestBaggage(t *testing.T) {
	for name, tc := range map[string]struct {
		h    hdrs
		want string
	}{
		"absent":                        {hdrs{}, "absent"},
		"no values":                     {hdrs{"baggage": {}}, "absent"},
		"verbatim":                      {hdrs{"baggage": {"routing-key=rk-3e26ef84,team=alpha"}}, "propagated"},
		"reordered, spaces":             {hdrs{"baggage": {"team=alpha , routing-key = rk-3e26ef84"}}, "propagated"},
		"member properties":             {hdrs{"baggage": {"routing-key=rk-3e26ef84;prop=1,team=alpha"}}, "propagated"},
		"key missing":                   {hdrs{"baggage": {"team=alpha"}}, "partial"},
		"wrong value":                   {hdrs{"baggage": {"routing-key=rk-other"}}, "partial"},
		"empty value":                   {hdrs{"baggage": {""}}, "partial"},
		"second field has the key":      {hdrs{"baggage": {"team=alpha", "routing-key=rk-3e26ef84"}}, "propagated"},
		"first field has the key":       {hdrs{"baggage": {"routing-key=rk-3e26ef84", "team=alpha"}}, "propagated"},
		"no field has the key":          {hdrs{"baggage": {"team=alpha", "env=test"}}, "partial"},
		"repeated key, seeded last":     {hdrs{"baggage": {"routing-key=stale,routing-key=rk-3e26ef84"}}, "propagated"},
		"repeated key, seeded first":    {hdrs{"baggage": {"routing-key=rk-3e26ef84,routing-key=stale"}}, "partial"},
		"repeated key in a later field": {hdrs{"baggage": {"routing-key=rk-3e26ef84", "routing-key=stale"}}, "partial"},
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
		"absent":                      {hdrs{}, "absent"},
		"no values":                   {hdrs{"ot-baggage-routing-key": {}}, "absent"},
		"another ot-baggage key":      {hdrs{"ot-baggage-team": {"alpha"}}, "absent"},
		"propagated":                  {hdrs{"ot-baggage-routing-key": {"rk-3e26ef84"}}, "propagated"},
		"wrong value":                 {hdrs{"ot-baggage-routing-key": {"rk-other"}}, "partial"},
		"only the first value counts": {hdrs{"ot-baggage-routing-key": {"rk-other", "rk-3e26ef84"}}, "partial"},
	} {
		if got := OTBaggage(tc.h, seed); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestEvaluate(t *testing.T) {
	got := Evaluate(hdrs{
		"traceparent":            {newSpan},
		"tracestate":             {"dd=s:1,cptest=v1"},
		"baggage":                {"team=alpha"},
		"ot-baggage-routing-key": {"rk-3e26ef84"},
		"x-datadog-trace-id":     {ddLow64},
	}, seed)
	want := raw.Status{Traceparent: "same_trace", Tracestate: "preserved", Baggage: "partial",
		OTBaggage: "propagated", DatadogNative: "present_same_trace"}
	if *got != want {
		t.Errorf("got %+v, want %+v", *got, want)
	}

	// Nothing arrived: every header absent, and no canary.
	want = raw.Status{Traceparent: "absent", Tracestate: "absent", Baggage: "absent",
		OTBaggage: "absent", DatadogNative: "absent"}
	if got := Evaluate(hdrs{}, seed); *got != want {
		t.Errorf("empty: got %+v, want %+v", *got, want)
	}

	if !Evaluate(hdrs{"x-cp-canary": {"otel-node--http-http--3e26ef84"}}, seed).CanaryLeak {
		t.Error("canary leak not detected")
	}
}

// A result is judged against the names it was seeded with, which the seed
// records: one measured before a rename keeps its statuses, and what counts
// for it is what it was sent, not what the driver sends today.
func TestNamesComeFromTheSeed(t *testing.T) {
	earlier := raw.Seeded{TraceID: seed.TraceID, SpanID: seed.SpanID, RoutingKey: seed.RoutingKey,
		Headers: map[string]string{
			"traceparent":         "00-" + seed.TraceID + "-" + seed.SpanID + "-01",
			"tracestate":          "acme=v7",
			"baggage":             "acme-key=" + seed.RoutingKey + ",team=alpha",
			"ot-baggage-acme-key": seed.RoutingKey,
			"x-acme-canary":       "otel-node--http-http--3e26ef84",
		}}
	then := hdrs{
		"traceparent":         {newSpan},
		"tracestate":          {"dd=s:1,acme=v7"},
		"baggage":             {"team=alpha,acme-key=rk-3e26ef84"},
		"ot-baggage-acme-key": {"rk-3e26ef84"},
		"x-acme-canary":       {"otel-node--http-http--3e26ef84"},
	}
	now := hdrs{
		"traceparent":            {newSpan},
		"tracestate":             {"dd=s:1,cptest=v1"},
		"baggage":                {"team=alpha,routing-key=rk-3e26ef84"},
		"ot-baggage-routing-key": {"rk-3e26ef84"},
		"x-cp-canary":            {"otel-node--http-http--3e26ef84"},
	}
	arrived := raw.Status{Traceparent: "same_trace", Tracestate: "preserved", Baggage: "propagated",
		OTBaggage: "propagated", DatadogNative: "absent", CanaryLeak: true}
	lost := raw.Status{Traceparent: "same_trace", Tracestate: "present_entry_lost", Baggage: "partial",
		OTBaggage: "absent", DatadogNative: "absent"}

	for name, tc := range map[string]struct {
		h    hdrs
		seed raw.Seeded
		want raw.Status
	}{
		"earlier names, earlier seed":  {then, earlier, arrived},
		"today's names, earlier seed":  {now, earlier, lost},
		"earlier names, today's seed":  {then, seed, lost},
		"today's names, today's seed":  {now, seed, arrived},
		"today's names, recorded seed": {now, raw.Seeded{TraceID: seed.TraceID, RoutingKey: seed.RoutingKey, Headers: map[string]string{"tracestate": TracestateEntry, "baggage": RoutingKeyMember + "=" + seed.RoutingKey + ",team=alpha", "ot-baggage-" + RoutingKeyMember: seed.RoutingKey, CanaryHeader: "t"}}, arrived},
	} {
		if got := Evaluate(tc.h, tc.seed); *got != tc.want {
			t.Errorf("%s: got %+v, want %+v", name, *got, tc.want)
		}
	}

	if got := Canary(earlier.Headers); got != "x-acme-canary" {
		t.Errorf("Canary of the earlier seed: got %q", got)
	}
	if got := Canary(nil); got != CanaryHeader {
		t.Errorf("Canary of no headers: got %q, want %q", got, CanaryHeader)
	}
}

func nrHeader(traceID string) string {
	return base64.StdEncoding.EncodeToString([]byte(`{"v":[0,1],"d":{"ty":"App","ac":"12","ap":"34","tr":"` + traceID + `","sa":true}}`))
}

func TestNative(t *testing.T) {
	// native_headers as variants.yaml lists them.
	dd := []string{"x-datadog-trace-id", "x-datadog-parent-id", "x-datadog-sampling-priority", "x-datadog-tags"}
	el := []string{"elastic-apm-traceparent"}
	nr := []string{"newrelic"}

	nrSame := nrHeader(seed.TraceID)
	if !strings.HasSuffix(nrSame, "=") {
		t.Fatalf("test payload must need padding for the unpadded case to mean anything: %s", nrSame)
	}

	for name, tc := range map[string]struct {
		h      hdrs
		native []string
		want   string
	}{
		"no native headers (otel, none)": {hdrs{"x-datadog-trace-id": {ddLow64}, "newrelic": {nrSame}}, nil, ""},
		"no decoder for the tool's list": {hdrs{"sentry-trace": {"x"}}, []string{"sentry-trace"}, ""},
		"another tool's header":          {hdrs{"x-datadog-trace-id": {ddLow64}}, el, "absent"},

		"datadog absent":           {hdrs{}, dd, "absent"},
		"datadog no values":        {hdrs{"x-datadog-trace-id": {}}, dd, "absent"},
		"datadog same trace":       {hdrs{"x-datadog-trace-id": {ddLow64}, "x-datadog-parent-id": {"42"}}, dd, "present_same_trace"},
		"datadog padded":           {hdrs{"x-datadog-trace-id": {" " + ddLow64 + " "}}, dd, "present_same_trace"},
		"datadog new trace":        {hdrs{"x-datadog-trace-id": {"42"}}, dd, "present_new_trace"},
		"datadog high 64 bits":     {hdrs{"x-datadog-trace-id": {"2565860727139769671"}}, dd, "present_new_trace"},
		"datadog hex, not decimal": {hdrs{"x-datadog-trace-id": {"93e9097d24404872"}}, dd, "present_new_trace"},
		"datadog empty value":      {hdrs{"x-datadog-trace-id": {""}}, dd, "present_new_trace"},
		"datadog parent id only":   {hdrs{"x-datadog-parent-id": {"42"}, "x-datadog-sampling-priority": {"1"}}, dd, "absent"},

		"elastic absent":            {hdrs{"traceparent": {newSpan}}, el, "absent"},
		"elastic same trace":        {hdrs{"elastic-apm-traceparent": {"00-239b89fcef379d4793e9097d24404872-b54da95b3407fd1b-01"}}, el, "present_same_trace"},
		"elastic upper case":        {hdrs{"elastic-apm-traceparent": {"00-239B89FCEF379D4793E9097D24404872-B54DA95B3407FD1B-01"}}, el, "present_same_trace"},
		"elastic new trace":         {hdrs{"elastic-apm-traceparent": {otherTrace}}, el, "present_new_trace"},
		"elastic not a traceparent": {hdrs{"elastic-apm-traceparent": {"nonsense"}}, el, "present_new_trace"},
		"elastic empty value":       {hdrs{"elastic-apm-traceparent": {""}}, el, "present_new_trace"},

		"newrelic absent":          {hdrs{"traceparent": {newSpan}}, nr, "absent"},
		"newrelic same trace":      {hdrs{"newrelic": {nrSame}}, nr, "present_same_trace"},
		"newrelic unpadded":        {hdrs{"newrelic": {strings.TrimRight(nrSame, "=")}}, nr, "present_same_trace"},
		"newrelic upper case id":   {hdrs{"newrelic": {nrHeader(strings.ToUpper(seed.TraceID))}}, nr, "present_same_trace"},
		"newrelic new trace":       {hdrs{"newrelic": {nrHeader(otherTraceID)}}, nr, "present_new_trace"},
		"newrelic not base64":      {hdrs{"newrelic": {"%%%"}}, nr, "present_new_trace"},
		"newrelic not json":        {hdrs{"newrelic": {base64.StdEncoding.EncodeToString([]byte("not json"))}}, nr, "present_new_trace"},
		"newrelic no trace id":     {hdrs{"newrelic": {base64.StdEncoding.EncodeToString([]byte(`{"v":[0,1],"d":{"ty":"App"}}`))}}, nr, "present_new_trace"},
		"newrelic d not an object": {hdrs{"newrelic": {base64.StdEncoding.EncodeToString([]byte(`{"d":"` + seed.TraceID + `"}`))}}, nr, "present_new_trace"},
		"newrelic empty value":     {hdrs{"newrelic": {""}}, nr, "present_new_trace"},
	} {
		if got := Native(tc.h, seed, tc.native); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}

	// A seeded trace id that is not 128 bits of hex has no low 64 bits to
	// compare with: present, but never the same trace.
	for _, id := range []string{"", "239b89fcef379d47", "239b89fcef379d47zzzzzzzzzzzzzzzz"} {
		s := raw.Seeded{TraceID: id}
		if got := Native(hdrs{"x-datadog-trace-id": {ddLow64}}, s, dd); got != "present_new_trace" {
			t.Errorf("seeded trace id %q: got %q, want present_new_trace", id, got)
		}
	}
}

// TestRecordedRawResults replays the evaluation over raw results the driver
// has written and requires every status it recorded to come out the same
// now, so a change to this package cannot quietly re-grade a measured hop.
// It reads the hand-curated raw files tracked under results/, and the
// directory named by CPM_RAW_DIR when that is set (a release's raw.tar.gz
// unpacked, or results/raw after `./run.sh fetch -raw`).
func TestRecordedRawResults(t *testing.T) {
	dirs := []string{"../../results/experiments", "../../results/nokey"}
	if dir := os.Getenv("CPM_RAW_DIR"); dir != "" {
		dirs = append(dirs, dir)
	}
	for _, dir := range dirs {
		t.Run(dir, func(t *testing.T) {
			vs, err := raw.LoadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			hops, repeated := 0, 0
			for _, v := range vs {
				for _, tr := range v.Tests {
					if tr.Status == nil || tr.Observed == nil || tr.Seeded == nil {
						continue
					}
					hops++
					if len(tr.Observed.Headers["tracestate"]) > 1 || len(tr.Observed.Headers["baggage"]) > 1 {
						repeated++
					}
					if got := Evaluate(tr.Observed.Headers, *tr.Seeded); *got != *tr.Status {
						t.Errorf("%s %s: re-evaluated %+v, recorded %+v", v.Variant, tr.Boundary, *got, *tr.Status)
					}
				}
			}
			if hops == 0 {
				t.Fatal("no delivered hops to re-evaluate")
			}
			t.Logf("%d variants, %d delivered hops re-evaluated; %d of them have a repeated tracestate or baggage field", len(vs), hops, repeated)
		})
	}
}
