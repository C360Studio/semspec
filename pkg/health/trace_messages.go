package health

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// FetchTraceMessages walks the supplied loops, extracts each loop's
// trace_id from LoopEntity.Metadata (semstreams beta.43+ stamps this
// via stampTraceIDFromCtx), hits /message-logger/trace/{traceID} for
// each, and returns the per-loop trace dumps keyed by loop_id.
//
// Loops without a trace_id in metadata are silently skipped — the
// LoopEntity field arrived in beta.43 and older bundles or pre-beta.43
// loops simply lack it. Wedge investigation against an older snapshot
// falls back to grepping the messages section by hand.
//
// Returns whatever trace dumps succeeded; per-trace failures are
// returned as CaptureErrors via the collector so the bundle records
// "we tried trace X and got status Y" rather than silently omitting.
//
// Body is preserved opaque (json.RawMessage) for the same reason as
// KVEntry.Value and JetStreamSnapshot.JSZ — forward-compat with the
// upstream message-logger response shape.
func FetchTraceMessages(ctx context.Context, client *http.Client, baseURL string, loops []KVEntry, collector *errCollector) map[string]TraceMessages {
	if client == nil {
		client = http.DefaultClient
	}
	if len(loops) == 0 {
		return nil
	}
	out := make(map[string]TraceMessages)
	seen := make(map[string]bool) // dedupe trace_ids — many loops share one
	for _, loop := range loops {
		if strings.HasPrefix(loop.Key, completeLoopMarkerPrefix) {
			// Skip terminal-state markers; the live entry carries the
			// metadata and we'd just hit /trace/X twice.
			continue
		}
		traceID := extractTraceID(loop.Value)
		if traceID == "" {
			continue
		}
		if seen[traceID] {
			continue
		}
		seen[traceID] = true
		body, err := fetchOneTrace(ctx, client, baseURL, traceID)
		if err != nil {
			if collector != nil {
				collector.add(&CaptureError{Source: "trace:" + traceID, Err: err})
			}
			continue
		}
		out[loop.Key] = TraceMessages{TraceID: traceID, Body: body}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// extractTraceID pulls metadata.trace_id from a LoopEntity JSON value.
// LoopEntity has top-level Metadata map[string]any; trace_id is stored
// as the canonical agentic.MetadataKeyTraceID (= "trace_id").
//
// Returns "" when the field is absent (older loops pre-beta.43, or
// dispatches that originated outside a NATS-traced context — synthetic
// in-process tests, etc.). The bundler treats absence as silent skip.
func extractTraceID(loopValue json.RawMessage) string {
	if len(loopValue) == 0 {
		return ""
	}
	var entity struct {
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(loopValue, &entity); err != nil {
		return ""
	}
	if entity.Metadata == nil {
		return ""
	}
	raw, ok := entity.Metadata["trace_id"]
	if !ok {
		return ""
	}
	tid, ok := raw.(string)
	if !ok {
		return ""
	}
	return tid
}

func fetchOneTrace(ctx context.Context, client *http.Client, baseURL, traceID string) (json.RawMessage, error) {
	url := strings.TrimRight(baseURL, "/") + "/message-logger/trace/" + traceID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return json.RawMessage(body), nil
}
