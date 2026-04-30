package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goldenMetrics is a minimal Prometheus exposition body matching the
// real semstreams namespace. The shape mirrors what
// pkg/health/testdata/fixtures/metrics-real-2026-04-30/metrics.txt
// captures from a live run, condensed to just the lines the parser
// reads plus comment/garbage lines that should be skipped.
const goldenMetrics = `# HELP semstreams_agentic_loop_active_loops Active agent loops
# TYPE semstreams_agentic_loop_active_loops gauge
semstreams_agentic_loop_active_loops 7
# HELP semstreams_agentic_loop_context_utilization Most recent ctx utilization
# TYPE semstreams_agentic_loop_context_utilization gauge
semstreams_agentic_loop_context_utilization 0.42
# HELP semstreams_agentic_model_requests_total Model HTTP requests
# TYPE semstreams_agentic_model_requests_total counter
semstreams_agentic_model_requests_total{model="gemini-2.5-pro",status="success"} 25
semstreams_agentic_model_requests_total{model="gemini-2.5-pro",status="error"} 3
semstreams_agentic_model_requests_total{model="qwen3-14b",status="timeout"} 2
semstreams_agentic_loop_tool_results_truncated_total 1
semstreams_agentic_loop_context_compactions_total 0
some_unrelated_metric 9999
malformed_line_no_value
`

func TestParseMetrics_GoldenBlob(t *testing.T) {
	got := ParseMetrics(goldenMetrics)
	if got.LoopActiveLoops != 7 {
		t.Errorf("LoopActiveLoops = %d, want 7", got.LoopActiveLoops)
	}
	if got.LoopContextUtilization != 0.42 {
		t.Errorf("LoopContextUtilization = %v, want 0.42", got.LoopContextUtilization)
	}
	// 25+3+2 across labels.
	if got.ModelRequestsTotal != 30 {
		t.Errorf("ModelRequestsTotal = %d, want 30", got.ModelRequestsTotal)
	}
	if got.ModelRequestsErrors != 3 {
		t.Errorf("ModelRequestsErrors = %d, want 3", got.ModelRequestsErrors)
	}
	if got.ModelRequestsTimeouts != 2 {
		t.Errorf("ModelRequestsTimeouts = %d, want 2", got.ModelRequestsTimeouts)
	}
	if got.LengthTruncationsTotal != 0 {
		// semstreams doesn't emit a length-truncation counter; field
		// stays in schema for additive-v1 compat. Pin zero so a future
		// upstream addition doesn't silently change behavior.
		t.Errorf("LengthTruncationsTotal = %d, want 0 (semstreams does not emit)", got.LengthTruncationsTotal)
	}
	if got.ToolResultsTruncatedTotal != 1 {
		t.Errorf("ToolResultsTruncatedTotal = %d, want 1", got.ToolResultsTruncatedTotal)
	}
}

// TestParseMetrics_RealFixture pins the parser against the actual
// /metrics output from a healthy Gemini @easy run on 2026-04-30. The
// previous synthetic-only test masked a silent-data-loss bug where
// the parser used a `semspec_*` prefix that semstreams never emits;
// this test would have failed loudly on the wrong prefix and is the
// canonical example of why parsers of upstream wire format need a
// real fixture (see CLAUDE.md Testing Patterns).
func TestParseMetrics_RealFixture(t *testing.T) {
	path := filepath.Join("testdata", "fixtures", "metrics-real-2026-04-30", "metrics.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	got := ParseMetrics(string(data))

	// At capture time the run had: 12 active loops, ctx utilization
	// ~0.0034, multiple model_requests labels including some errors.
	// We pin the load-bearing fields rather than every detail so a
	// future fixture refresh doesn't have to recompute exact totals.
	if got.LoopActiveLoops <= 0 {
		t.Errorf("LoopActiveLoops should be > 0 from real fixture, got %d", got.LoopActiveLoops)
	}
	if got.LoopContextUtilization <= 0 {
		t.Errorf("LoopContextUtilization should be > 0 from real fixture, got %v", got.LoopContextUtilization)
	}
	if got.ModelRequestsTotal <= 0 {
		t.Errorf("ModelRequestsTotal should be > 0 from real fixture, got %d", got.ModelRequestsTotal)
	}
	// The fixture includes status="error" entries; pin that error
	// classification reaches the snapshot.
	if got.ModelRequestsErrors <= 0 {
		t.Errorf("ModelRequestsErrors should be > 0 (fixture has error rows), got %d", got.ModelRequestsErrors)
	}
}

func TestParseMetrics_EmptyAndCommentOnlyAreSafe(t *testing.T) {
	if got := ParseMetrics(""); got.LoopActiveLoops != 0 {
		t.Errorf("empty body: got non-zero %+v", got)
	}
	commentOnly := "# only comments\n# TYPE foo gauge\n"
	if got := ParseMetrics(commentOnly); got.LoopActiveLoops != 0 {
		t.Errorf("comment-only body: got non-zero %+v", got)
	}
}

func TestFetchMetrics_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(goldenMetrics))
	}))
	defer srv.Close()

	snap, err := FetchMetrics(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("FetchMetrics: %v", err)
	}
	if snap.LoopActiveLoops != 7 {
		t.Errorf("LoopActiveLoops not parsed: %+v", snap)
	}
	// FetchMetrics deliberately leaves CapturedAt zero — the
	// orchestrator stamps a single bundle-wide instant. See
	// pkg/health/metrics.go FetchMetrics doc comment.
	if !snap.CapturedAt.IsZero() {
		t.Errorf("CapturedAt should be zero (orchestrator stamps it); got %v", snap.CapturedAt)
	}
}

func TestParseMetrics_LabelValueWithSpace(t *testing.T) {
	// Regression: splitMetricLine used to LastIndexByte(' ') on the
	// full line, which split inside `model="gemini 2.5"` and silently
	// returned ok=false (zero-snapshot). Anchor past the closing brace.
	input := `semstreams_agentic_model_requests_total{model="gemini 2.5",status="success"} 25` + "\n"
	got := ParseMetrics(input)
	if got.ModelRequestsTotal != 25 {
		t.Errorf("ModelRequestsTotal = %d, want 25 (label-value-with-space regression)", got.ModelRequestsTotal)
	}
}

func TestFetchMetrics_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := FetchMetrics(context.Background(), srv.Client(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("expected HTTP 500 error, got %v", err)
	}
}
