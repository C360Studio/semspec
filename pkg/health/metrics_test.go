package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// goldenMetrics is a minimal Prometheus exposition body shaped like
// what semspec actually emits — gauges, per-status request counts,
// failure-shape counters, plus comments and noise to confirm the
// parser ignores them.
const goldenMetrics = `# HELP semspec_loop_active_loops Active agent loops
# TYPE semspec_loop_active_loops gauge
semspec_loop_active_loops 7
# HELP semspec_loop_context_utilization Most recent ctx utilization
# TYPE semspec_loop_context_utilization gauge
semspec_loop_context_utilization 0.42
# HELP semspec_model_requests_total Model HTTP requests
# TYPE semspec_model_requests_total counter
semspec_model_requests_total{model="gemini-2.5-pro",status="success"} 25
semspec_model_requests_total{model="gemini-2.5-pro",status="error"} 3
semspec_model_requests_total{model="qwen3-14b",status="timeout"} 2
semspec_length_truncations_total 4
semspec_tool_results_truncated_total 1
semspec_context_compactions_total 0
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
	if got.LengthTruncationsTotal != 4 {
		t.Errorf("LengthTruncationsTotal = %d, want 4", got.LengthTruncationsTotal)
	}
	if got.ToolResultsTruncatedTotal != 1 {
		t.Errorf("ToolResultsTruncatedTotal = %d, want 1", got.ToolResultsTruncatedTotal)
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
	if snap.CapturedAt.IsZero() {
		t.Error("CapturedAt should be set")
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
