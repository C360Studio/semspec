package health

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// messageEntry is the wire shape returned by /message-logger/entries.
// Local to this file — Bundle.Messages is the public type that carries
// forward. Field names mirror the e2e client's LogEntry, which mirrors
// semstreams' MessageLogEntry.
type messageEntry struct {
	Sequence    int64           `json:"sequence"`
	Timestamp   json.RawMessage `json:"timestamp"`
	Subject     string          `json:"subject"`
	MessageType string          `json:"message_type,omitempty"`
	MessageID   string          `json:"message_id,omitempty"`
	TraceID     string          `json:"trace_id,omitempty"`
	SpanID      string          `json:"span_id,omitempty"`
	Summary     string          `json:"summary,omitempty"`
	RawData     json.RawMessage `json:"raw_data,omitempty"`
}

// FetchMessages pulls the most recent N message-logger entries via
// GET /message-logger/entries?limit=N&subject=PATTERN. The endpoint
// returns entries newest-first; this function preserves that order —
// callers that need chronological order should sort by Sequence
// themselves (the detector library does so per-detector).
//
// subjectPattern is a glob honoured server-side: "*" or "" matches
// any subject, "agent.*" matches by prefix, "*foo*" matches as
// substring. The semstreams MessageLogger applies the limit BEFORE
// the filter, so for niche subjects callers should pull a generous
// limit. Two-subject pulls (e.g. agent.* + tool.*) are the
// orchestrator's responsibility.
//
// Bundle.Messages records Subject + MessageType + RawData so detectors
// can decode payload-shape on demand without bundling the full
// metadata map (which can leak adopter-specific labels).
func FetchMessages(ctx context.Context, client *http.Client, baseURL string, limit int, subjectPattern string) ([]Message, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if limit <= 0 {
		limit = DefaultMessageLimit
	}
	u := strings.TrimRight(baseURL, "/") + "/message-logger/entries"
	q := url.Values{"limit": []string{strconv.Itoa(limit)}}
	if subjectPattern != "" {
		q.Set("subject", subjectPattern)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("messages: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("messages: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("messages: HTTP %d", resp.StatusCode)
	}
	var raw []messageEntry
	if err := json.NewDecoder(io.LimitReader(resp.Body, MaxResponseBytes)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("messages: decode: %w", err)
	}
	out := make([]Message, 0, len(raw))
	for _, e := range raw {
		m := Message{
			Sequence:    e.Sequence,
			Subject:     e.Subject,
			MessageType: e.MessageType,
			TraceID:     e.TraceID,
			SpanID:      e.SpanID,
			Summary:     e.Summary,
			RawData:     e.RawData,
		}
		// Timestamp is RFC3339 over the wire. Decode lazily so a future
		// format change in the producer reports a precise error instead
		// of poisoning the whole batch.
		if len(e.Timestamp) > 0 && string(e.Timestamp) != "null" {
			if err := json.Unmarshal(e.Timestamp, &m.Timestamp); err != nil {
				return nil, fmt.Errorf("messages: timestamp seq=%d: %w", e.Sequence, err)
			}
		}
		out = append(out, m)
	}
	return out, nil
}
