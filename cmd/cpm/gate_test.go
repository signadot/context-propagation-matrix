package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/signadot/context-propagation-matrix/internal/raw"
	"github.com/signadot/context-propagation-matrix/internal/registry"
	"github.com/signadot/context-propagation-matrix/internal/results"
)

// TestMain lets a test run cpm's real command line: started with
// CPM_TEST_MAIN=1 the test binary is cpm, so the exit status a test sees is
// the one a workflow step would.
func TestMain(m *testing.M) {
	if os.Getenv("CPM_TEST_MAIN") == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// cpm runs `cpm args...` in a child process, as a workflow step on a GitHub
// runner when actions is set, and returns its exit status and output.
func cpm(t *testing.T, actions bool, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), "CPM_TEST_MAIN=1", "GITHUB_ACTIONS=")
	if actions {
		cmd.Env = append(cmd.Env, "GITHUB_ACTIONS=true")
	}
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatalf("cpm %s: %v", strings.Join(args, " "), err)
	}
	return cmd.ProcessState.ExitCode(), string(out)
}

// Header names a relay's outbound call arrives with, as the sink records
// them: nothing but the client's own, the W3C set of a working agent, and
// everything the driver seeded (a relay that copies inbound headers).
func sendsPlain(out string) []string {
	if out == "http" || out == "grpc" {
		return []string{"content-type", "user-agent"}
	}
	return nil
}

func sendsTraced(out string) []string {
	return append(sendsPlain(out), "traceparent", "tracestate")
}

func sendsEverything(out string) []string {
	return append(sendsTraced(out), "baggage", "ot-baggage-sd-routing-key", "x-sd-canary")
}

// sendsRuntime is the .NET runtime with no agent: its HTTP and gRPC clients
// add the W3C headers themselves, its messaging clients nothing.
func sendsRuntime(out string) []string {
	if out == "http" || out == "grpc" {
		return sendsTraced(out)
	}
	return nil
}

// writeRaw writes dir/<variant>.json the way the driver does: one delivered
// test per boundary, carrying the header names sent(outbound) returns.
func writeRaw(t *testing.T, reg *registry.Registry, dir, variant string, sent func(out string) []string) {
	t.Helper()
	rv := reg.Variant(variant)
	if rv == nil {
		t.Fatalf("%s is not in variants.yaml", variant)
	}
	r := raw.VariantResult{Variant: variant, Tool: rv.Tool, Language: rv.Language,
		Info: map[string]any{"tracer_version": "1.0.0"}, RanAt: time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)}
	for _, b := range reg.Boundaries {
		_, out, _ := strings.Cut(b, "->")
		obs := &raw.Observed{Transport: out, Headers: map[string][]string{}}
		st := &raw.Status{Traceparent: "absent", Tracestate: "absent", Baggage: "absent", OTBaggage: "absent", DatadogNative: "absent"}
		for _, h := range sent(out) {
			obs.Headers[h] = []string{"x"}
			if h == "x-sd-canary" {
				st.CanaryLeak = true
			}
		}
		r.Tests = append(r.Tests, raw.TestResult{Boundary: b, TestID: variant + "--" + b, Delivered: true, Observed: obs, Status: st})
	}
	body, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, variant+".json", string(body))
}

// aggregate runs `cpm aggregate` over the raw files in rawDir and returns
// the path of the results.json it wrote.
func aggregate(t *testing.T, rawDir, kind, runID string, extra ...string) string {
	t.Helper()
	out := t.TempDir()
	args := append([]string{"aggregate", "-registry", "../../variants.yaml", "-compose", "../../docker-compose.yml",
		"-annotations", t.TempDir(), "-pins", "", "-raw", rawDir, "-out", out, "-kind", kind, "-run-id", runID}, extra...)
	if code, text := cpm(t, false, args...); code != 0 {
		t.Fatalf("cpm aggregate exited %d:\n%s", code, text)
	}
	return filepath.Join(out, "results.json")
}

// withoutVerdict copies the document at path without any variant's
// `invalid`, which is what one published before the field existed looks like.
func withoutVerdict(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	dropped := 0
	for _, v := range doc["variants"].([]any) {
		if m := v.(map[string]any); m["invalid"] != nil {
			delete(m, "invalid")
			dropped++
		}
	}
	if dropped == 0 {
		t.Fatalf("fixture: %s records no verdict to drop", path)
	}
	if body, err = json.Marshal(doc); err != nil {
		t.Fatal(err)
	}
	return writeFile(t, t.TempDir(), "results.json", string(body))
}

func invalidKinds(t *testing.T, path, variant string) string {
	t.Helper()
	res, err := results.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, p := range res.Variant(variant).Invalid {
		kinds = append(kinds, p.Kind)
	}
	return strings.Join(kinds, ",")
}

// The exit status of `cpm gate` under both policies, on documents made by
// `cpm aggregate` from raw driver output: a matrix run is refused for any
// problem on a variant it measured and only warned about one it carried
// over; a back-test is refused for a canary leak (or a wrong transport) but
// published with a warning when the agent did not attach.
func TestGateExitStatus(t *testing.T) {
	reg, err := registry.Load("../../variants.yaml")
	if err != nil {
		t.Fatal(err)
	}

	// The latest run: baselines and working agents.
	latestRaw := t.TempDir()
	writeRaw(t, reg, latestRaw, "none-java", sendsPlain)
	writeRaw(t, reg, latestRaw, "otel-java", sendsTraced)
	writeRaw(t, reg, latestRaw, "elastic-java", sendsTraced)
	writeRaw(t, reg, latestRaw, "none-dotnet", sendsRuntime)
	latest := aggregate(t, latestRaw, "matrix", "run-1")
	if code, out := cpm(t, true, "gate", latest); code != 0 || strings.Contains(out, "::") {
		t.Errorf("valid matrix run: exit %d, want 0 and no annotation:\n%s", code, out)
	}

	t.Run("matrix", func(t *testing.T) {
		// run-2 measures elastic-java with an agent that did not attach.
		rawDir := t.TempDir()
		writeRaw(t, reg, rawDir, "none-java", sendsPlain)
		writeRaw(t, reg, rawDir, "otel-java", sendsTraced)
		writeRaw(t, reg, rawDir, "elastic-java", sendsPlain)
		doc := aggregate(t, rawDir, "matrix", "run-2", "-prev", latest, "-reran", "elastic-java")
		if got := invalidKinds(t, doc, "elastic-java"); got != results.ProblemAgentInactive {
			t.Errorf("aggregate recorded invalid = %q on elastic-java", got)
		}
		code, out := cpm(t, true, "gate", "-reran", "elastic-java", doc)
		if code != 1 || !strings.Contains(out, "::error::gate: elastic-java: agent_inactive: ") {
			t.Errorf("inactive agent measured in this run: exit %d, want 1 with an ::error::\n%s", code, out)
		}
		// Without -reran the document's own record of what it measured decides.
		if code, _ := cpm(t, false, "gate", doc); code != 1 {
			t.Errorf("same, measured set from the document: exit %d, want 1", code)
		}
		// With it, the flag does: elastic-java is then not this run's to answer for.
		code, out = cpm(t, true, "gate", "-reran", "otel-java", doc)
		if code != 0 || !strings.Contains(out, "::warning::gate: elastic-java: agent_inactive: ") {
			t.Errorf("-reran without the inactive variant: exit %d, want 0 with a ::warning::\n%s", code, out)
		}
		// The verdict is judged from the hops, not read back: a document
		// published before aggregate recorded it is refused all the same.
		code, out = cpm(t, true, "gate", withoutVerdict(t, doc))
		if code != 1 || !strings.Contains(out, "::error::gate: elastic-java: agent_inactive: ") {
			t.Errorf("document without a recorded verdict: exit %d, want 1 with an ::error::\n%s", code, out)
		}

		// run-3 carries that elastic-java over and measures only otel-java.
		next := aggregate(t, rawDir, "matrix", "run-3", "-prev", doc, "-reran", "otel-java")
		code, out = cpm(t, true, "gate", "-reran", "otel-java", next)
		if code != 0 || !strings.Contains(out, "::warning::gate: elastic-java: agent_inactive: ") ||
			!strings.Contains(out, "carried over from run-2") || strings.Contains(out, "::error::") {
			t.Errorf("inactive agent carried over: exit %d, want 0 with a ::warning::\n%s", code, out)
		}
		if code, _ := cpm(t, false, "gate", next); code != 0 {
			t.Errorf("same, measured set from the document: exit %d, want 0", code)
		}

		// A canary leak on a measured variant blocks like any other problem.
		writeRaw(t, reg, rawDir, "otel-java", sendsEverything)
		leaky := aggregate(t, rawDir, "matrix", "run-4", "-prev", doc, "-reran", "otel-java")
		code, out = cpm(t, true, "gate", "-reran", "otel-java", leaky)
		if code != 1 || !strings.Contains(out, "::error::gate: otel-java: canary_leak: ") {
			t.Errorf("canary leak measured in this run: exit %d, want 1 with an ::error::\n%s", code, out)
		}
	})

	t.Run("backtest", func(t *testing.T) {
		// One variant per document; the baseline comes from the latest run.
		rawDir := t.TempDir()
		writeRaw(t, reg, rawDir, "elastic-java", sendsPlain)
		doc := aggregate(t, rawDir, "backtest", "backtest-elastic-java-1.53.0", "-baseline", latest)
		if got := invalidKinds(t, doc, "elastic-java"); got != results.ProblemAgentInactive {
			t.Errorf("aggregate -baseline recorded invalid = %q on elastic-java", got)
		}
		code, out := cpm(t, true, "gate", "-baseline", latest, doc)
		if code != 0 || !strings.Contains(out, "::warning::gate: elastic-java: agent_inactive: ") || strings.Contains(out, "::error::") {
			t.Errorf("inactive agent in a back-test: exit %d, want 0 with a ::warning::\n%s", code, out)
		}
		// Off a runner the same lines are plain text.
		if code, out := cpm(t, false, "gate", "-baseline", latest, doc); code != 0 || strings.Contains(out, "::") ||
			!strings.Contains(out, "gate: warning: elastic-java: agent_inactive: ") {
			t.Errorf("not on a runner: exit %d\n%s", code, out)
		}
		// No baseline to compare with: the output says so.
		if code, out := cpm(t, true, "gate", doc); code != 0 || !strings.Contains(out, "::warning::gate: no uninstrumented baseline for java") {
			t.Errorf("no baseline: exit %d, want 0 and a note that none was available\n%s", code, out)
		}

		writeRaw(t, reg, rawDir, "elastic-java", sendsEverything)
		leaky := aggregate(t, rawDir, "backtest", "backtest-elastic-java-1.52.0", "-baseline", latest)
		code, out = cpm(t, true, "gate", "-baseline", latest, leaky)
		if code != 1 || !strings.Contains(out, "::error::gate: elastic-java: canary_leak: ") {
			t.Errorf("canary leak in a back-test: exit %d, want 1 with an ::error::\n%s", code, out)
		}

		// A working agent, but the sink saw the Kafka-outbound hops arrive over HTTP.
		writeRaw(t, reg, rawDir, "elastic-java", sendsTraced)
		body, err := os.ReadFile(filepath.Join(rawDir, "elastic-java.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), `"transport":"kafka"`) {
			t.Fatalf("fixture: no Kafka-outbound test in %s", body)
		}
		writeFile(t, rawDir, "elastic-java.json", strings.ReplaceAll(string(body), `"transport":"kafka"`, `"transport":"http"`))
		mismatched := aggregate(t, rawDir, "backtest", "backtest-elastic-java-1.51.0", "-baseline", latest)
		code, out = cpm(t, true, "gate", "-baseline", latest, mismatched)
		if code != 1 || !strings.Contains(out, "::error::gate: elastic-java: transport_mismatch: ") {
			t.Errorf("wrong transport in a back-test: exit %d, want 1 with an ::error::\n%s", code, out)
		}

		writeRaw(t, reg, rawDir, "elastic-java", sendsTraced)
		good := aggregate(t, rawDir, "backtest", "backtest-elastic-java-1.54.0", "-baseline", latest)
		if code, out := cpm(t, true, "gate", "-baseline", latest, good); code != 0 || strings.Contains(out, "::") {
			t.Errorf("valid back-test: exit %d, want 0 and no annotation:\n%s", code, out)
		}

		// Where the runtime sends tracing headers itself, only the baseline
		// tells an agent that did not attach from one that did, so the
		// verdict aggregate records depends on being given it.
		dotnetRaw := t.TempDir()
		writeRaw(t, reg, dotnetRaw, "otel-dotnet", sendsRuntime)
		based := aggregate(t, dotnetRaw, "backtest", "backtest-otel-dotnet-1.0.0", "-baseline", latest)
		if got := invalidKinds(t, based, "otel-dotnet"); got != results.ProblemAgentInactive {
			t.Errorf("aggregate -baseline recorded invalid = %q on a .NET agent that sent only what the runtime sends", got)
		}
		unbased := aggregate(t, dotnetRaw, "backtest", "backtest-otel-dotnet-1.0.0")
		if got := invalidKinds(t, unbased, "otel-dotnet"); got != "" {
			t.Errorf("aggregate without -baseline recorded invalid = %q on the same hops, want none: nothing to compare with", got)
		}
	})
}
