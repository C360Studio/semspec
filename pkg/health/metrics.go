package health

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// FetchMetrics pulls /metrics from baseURL and parses the relevant
// fields into a MetricsSnapshot. Sets CapturedAt to the request
// completion time (UTC).
//
// A non-2xx response, network error, or unparseable body returns an
// error and a zero MetricsSnapshot with CapturedAt unset — the caller
// should treat the section as absent rather than substituting zero
// values that look like real readings.
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return MetricsSnapshot{}, fmt.Errorf("metrics: read body: %w", err)
	}
	snap := ParseMetrics(string(body))
	snap.CapturedAt = time.Now().UTC()
	return snap, nil
}

// ParseMetrics walks Prometheus exposition text and pulls the v1
// MetricsSnapshot fields. Unknown metrics are ignored. Per-status
// model_requests counts are summed across all label permutations
// (model + status) — detectors care about totals at the bundle scope,
// not per-model breakdowns.
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
		case "semspec_loop_active_loops":
			s.LoopActiveLoops = int64(value)
		case "semspec_loop_context_utilization":
			s.LoopContextUtilization = value
		case "semspec_model_requests_total":
			s.ModelRequestsTotal += int64(value)
			if labels["status"] == "error" {
				s.ModelRequestsErrors += int64(value)
			}
			if labels["status"] == "timeout" {
				s.ModelRequestsTimeouts += int64(value)
			}
		case "semspec_length_truncations_total":
			s.LengthTruncationsTotal += int64(value)
		case "semspec_tool_results_truncated_total":
			s.ToolResultsTruncatedTotal += int64(value)
		case "semspec_context_compactions_total":
			s.ContextCompactionsTotal += int64(value)
		}
	}
	return s
}

// splitMetricLine parses one Prometheus exposition line into name,
// labels (may be nil), and value. Returns ok=false on malformed lines —
// the caller should skip them silently rather than fail the snapshot.
func splitMetricLine(line string) (name string, labels map[string]string, value float64, ok bool) {
	// Forms:
	//   metric_name 42
	//   metric_name{label="x",label2="y"} 42.5
	openBrace := strings.IndexByte(line, '{')
	space := strings.LastIndexByte(line, ' ')
	if space < 0 {
		return "", nil, 0, false
	}
	rawValue := line[space+1:]
	v, err := strconv.ParseFloat(rawValue, 64)
	if err != nil {
		return "", nil, 0, false
	}
	if openBrace < 0 || openBrace > space {
		return strings.TrimSpace(line[:space]), nil, v, true
	}
	closeBrace := strings.IndexByte(line, '}')
	if closeBrace < 0 || closeBrace > space {
		return "", nil, 0, false
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
