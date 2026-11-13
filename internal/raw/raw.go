// Package raw defines the per-variant JSON the driver writes to
// results/raw/<variant>.json. This is the harness's internal format; the
// published contract is internal/results (schema v2).
//
// The driver marshals these types and the aggregator unmarshals them, so a
// tag changed here changes what the driver writes: internal/affected re-runs
// every variant for a change to this package for that reason.
package raw

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Status struct {
	Traceparent string `json:"traceparent"`
	Tracestate  string `json:"tracestate"`
	Baggage     string `json:"baggage"`
	OTBaggage   string `json:"ot_baggage"`
	// DatadogNative is x-datadog-trace-id continuity as the driver has
	// always recorded it, for every tool. The published vendor_native is no
	// longer taken from it: the aggregator evaluates each tool's own native
	// header from the observed headers (eval.Native). The driver still
	// writes it so raw files of every age have one shape.
	DatadogNative string `json:"datadog_native"`
	CanaryLeak    bool   `json:"canary_leak"`
}

// Field returns the status value for a header's raw_field name.
func (s *Status) Field(name string) (string, bool) {
	switch name {
	case "traceparent":
		return s.Traceparent, true
	case "tracestate":
		return s.Tracestate, true
	case "baggage":
		return s.Baggage, true
	case "ot_baggage":
		return s.OTBaggage, true
	case "datadog_native":
		return s.DatadogNative, true
	}
	return "", false
}

type Seeded struct {
	TraceID    string            `json:"trace_id"`
	SpanID     string            `json:"span_id"`
	RoutingKey string            `json:"routing_key"`
	Headers    map[string]string `json:"headers"`
}

type Observed struct {
	Transport string              `json:"transport"`
	Headers   map[string][]string `json:"headers"`
}

// The omitempty tags below are the driver's: an undelivered hop has no
// observed or status key, and a relay that reports no config has no config
// key. They decide what the driver writes, not what the aggregator accepts.
type TestResult struct {
	Boundary  string    `json:"boundary"`
	TestID    string    `json:"test_id"`
	Delivered bool      `json:"delivered"`
	Seeded    *Seeded   `json:"seeded,omitempty"`
	Observed  *Observed `json:"observed,omitempty"`
	Status    *Status   `json:"status,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type VariantResult struct {
	Variant  string         `json:"variant"`
	Tool     string         `json:"tool"`
	Language string         `json:"language"`
	Config   map[string]any `json:"config,omitempty"`
	Info     map[string]any `json:"info,omitempty"`
	RanAt    time.Time      `json:"ran_at"`
	Tests    []TestResult   `json:"tests"`
}

// InfoString returns a string field of the relay's /info payload.
func (v *VariantResult) InfoString(key string) string {
	if v.Info == nil {
		return ""
	}
	if s, ok := v.Info[key].(string); ok {
		return s
	}
	return ""
}

// Libs returns the relay's reported client library versions, sorted by key.
func (v *VariantResult) Libs() map[string]string {
	out := map[string]string{}
	if v.Info == nil {
		return out
	}
	lm, ok := v.Info["libs"].(map[string]any)
	if !ok {
		return out
	}
	for k, val := range lm {
		out[k] = fmt.Sprint(val)
	}
	return out
}

// ConfigStrings flattens Config to string values.
func (v *VariantResult) ConfigStrings() map[string]string {
	out := map[string]string{}
	for k, val := range v.Config {
		out[k] = fmt.Sprint(val)
	}
	return out
}

// LoadDir reads every *.json in dir, sorted by variant id.
func LoadDir(dir string) ([]*VariantResult, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no raw results in %s", dir)
	}
	var out []*VariantResult
	for _, f := range files {
		v, err := LoadFile(f)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Variant < out[j].Variant })
	return out, nil
}

// LoadFile reads one raw result file.
func LoadFile(path string) (*VariantResult, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v VariantResult
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if v.Variant == "" {
		return nil, fmt.Errorf("%s: missing variant id", path)
	}
	return &v, nil
}
