package health

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// FetchMetrics pulls /metrics from baseURL and parses the relevant
// fields into a MetricsSnapshot.
//
// CapturedAt is intentionally left zero — the orchestrator stamps it
// once with the bundle-wide instant so all CapturedAt fields share a
// reference time. See pkg/health/bundle.go's UTC-everywhere note.
//
// A non-2xx response, network error, or unparseable body returns an
// error and a zero MetricsSnapshot — the caller should treat the
// section as absent rather than substituting zero values that look
// like real readings.
func FetchMetrics(ctx context.Context, client *http.Client, baseURL string) (MetricsSnapshot, error) {
	if client == nil {
		client = http.DefaultClient
	}
	url := strings.TrimRight(baseURL, "/") + "/metrics"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return MetricsSnapshot{}, fmt.Errorf("metrics: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return MetricsSnapshot{}, fmt.Errorf("metrics: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return MetricsSnapshot{}, fmt.Errorf("metrics: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes))
	if err != nil {
		return MetricsSnapshot{}, fmt.Errorf("metrics: read body: %w", err)
	}
	return ParseMetrics(string(body)), nil
}

// ParseMetrics walks Prometheus exposition text and pulls the v1
// MetricsSnapshot fields. Unknown metrics are ignored. Per-status
// model_requests counts are summed across all label permutations
// (model + status) — detectors care about totals at the bundle scope,
// not per-model breakdowns.
//
// Metric names match semstreams' `semstreams_agentic_*` namespace.
// Pinned against the real /metrics fixture under
// pkg/health/testdata/fixtures/metrics-real-2026-04-30/. The earlier
// `semspec_*` prefix was a guessed naming convention that produced
// silently-zeroed snapshots on every real run; caught 2026-04-30
// by the first end-to-end watch CLI exercise.
//
// LengthTruncationsTotal stays in the schema for additive-v1 compat
// but semstreams does not currently emit a length-truncation counter
// — the field will always be zero until upstream adds one.
//
// Pure: no I/O, deterministic given the input text. Safe to call
// directly from tests with golden Prometheus blobs.
func ParseMetrics(text string) MetricsSnapshot {
	var s MetricsSnapshot
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, labels, value, ok := splitMetricLine(line)
		if !ok {
			continue
		}
		switch name {
		case "semstreams_agentic_loop_active_loops":
			s.LoopActiveLoops = int64(value)
		case "semstreams_agentic_loop_context_utilization":
			s.LoopContextUtilization = value
		case "semstreams_agentic_model_requests_total":
			// Note: semstreams also exposes
			// `semstreams_agentic_model_errors_total{error_type=...}`
			// which tracks the same error count under a different
			// label scheme. We source ModelRequestsErrors from the
			// status="error" label here because it sits alongside
			// the totals on one counter — fewer rows to walk and
			// the two counters agree in every fixture observed
			// to date. If they drift upstream, switch the source.
			s.ModelRequestsTotal += int64(value)
			switch labels["status"] {
			case "error":
				s.ModelRequestsErrors += int64(value)
			case "timeout":
				s.ModelRequestsTimeouts += int64(value)
			}
		case "semstreams_agentic_loop_tool_results_truncated_total":
			s.ToolResultsTruncatedTotal += int64(value)
		case "semstreams_agentic_loop_context_compactions_total":
			s.ContextCompactionsTotal += int64(value)
		}
	}
	return s
}

// splitMetricLine parses one Prometheus exposition line into name,
// labels (may be nil), and value. Returns ok=false on malformed lines —
// the caller should skip them silently rather than fail the snapshot.
//
// Locates the value boundary AFTER the closing label brace so a label
// value containing whitespace (`{model="gemini 2.5",...}`) doesn't
// split early and silently corrupt the snapshot.
func splitMetricLine(line string) (name string, labels map[string]string, value float64, ok bool) {
	// Forms:
	//   metric_name 42
	//   metric_name{label="x",label2="y"} 42.5
	openBrace := strings.IndexByte(line, '{')
	closeBrace := -1
	if openBrace >= 0 {
		closeBrace = strings.IndexByte(line, '}')
		if closeBrace < 0 || closeBrace < openBrace {
			return "", nil, 0, false
		}
	}
	// Anchor the value scan past any closing label brace.
	valueRegion := line
	if closeBrace > 0 {
		valueRegion = line[closeBrace:]
	}
	relSpace := strings.LastIndexByte(valueRegion, ' ')
	if relSpace < 0 {
		return "", nil, 0, false
	}
	space := relSpace
	if closeBrace > 0 {
		space += closeBrace
	}
	rawValue := strings.TrimSpace(line[space+1:])
	v, err := strconv.ParseFloat(rawValue, 64)
	if err != nil {
		return "", nil, 0, false
	}
	if openBrace < 0 {
		return strings.TrimSpace(line[:space]), nil, v, true
	}
	name = strings.TrimSpace(line[:openBrace])
	labels = parseLabelSet(line[openBrace+1 : closeBrace])
	return name, labels, v, true
}

// parseLabelSet handles `k="v",k2="v2"` Prometheus label content. It is
// intentionally simple: no escape-sequence support beyond stripping the
// surrounding double-quotes. Real exposition libraries are heavier than
// the v1 detector set warrants.
func parseLabelSet(s string) map[string]string {
	if s == "" {
		return nil
	}
	out := make(map[string]string)
	for pair := range strings.SplitSeq(s, ",") {
		eq := strings.IndexByte(pair, '=')
		if eq <= 0 {
			continue
		}
		k := strings.TrimSpace(pair[:eq])
		v := strings.Trim(strings.TrimSpace(pair[eq+1:]), `"`)
		out[k] = v
	}
	return out
}
