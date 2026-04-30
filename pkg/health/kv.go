package health

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// kvEntriesResponse is the shape returned by GET /message-logger/kv/{bucket}.
// Local to this file; bundle.KVEntry is the public type that survives.
//
// The producer is a semstreams-side handler that emits time.Time values
// using stdlib JSON, i.e. RFC3339. KVEntry.Created decodes against the
// same convention. If a future message-logger release switches to Unix
// nanos, decode silently zeroes the field — pin a contract test there
// (test/e2e/client/http.go owns the symmetric reader).
type kvEntriesResponse struct {
	Bucket  string    `json:"bucket"`
	Entries []KVEntry `json:"entries"`
}

// FetchKVBucket reads every entry in `bucket` via the message-logger
// /kv/{bucket} HTTP route and returns them as KVEntry. A non-existent
// bucket (HTTP 404) returns (nil, nil) — the bundle treats it as an
// empty section, since rendering an error per missing bucket would be
// noisy when adopters disable optional features.
//
// Other non-2xx responses, decode failures, and network errors return
// the error and a nil slice; the caller should record it as a
// CaptureError but continue assembling the bundle.
func FetchKVBucket(ctx context.Context, client *http.Client, baseURL, bucket string) ([]KVEntry, error) {
	if client == nil {
		client = http.DefaultClient
	}
	u := strings.TrimRight(baseURL, "/") + "/message-logger/kv/" + url.PathEscape(bucket)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("kv:%s: build request: %w", bucket, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kv:%s: %w", bucket, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("kv:%s: HTTP %d", bucket, resp.StatusCode)
	}
	var body kvEntriesResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, MaxResponseBytes)).Decode(&body); err != nil {
		return nil, fmt.Errorf("kv:%s: decode: %w", bucket, err)
	}
	return body.Entries, nil
}
