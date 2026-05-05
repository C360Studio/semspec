package health

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// FetchJetStream pulls NATS JetStream's monitoring snapshot from
// monitorURL (e.g. http://localhost:8222). Hits the
// /jsz?streams=true&consumers=true&accounts=true endpoint and returns
// the body opaque (json.RawMessage) so future NATS releases that add
// fields land in our bundle without code changes.
//
// Returns a snapshot regardless of HTTP status — a non-200 response
// (e.g. 404 from a NATS instance with monitoring disabled) is
// preserved as evidence that monitoring was misconfigured rather
// than silently dropping the section. Caller checks
// snap.Status == 200 before relying on snap.JSZ.
//
// Returns nil + error only on transport-level failures (network down,
// timeout, body too large, malformed URL); the bundler turns those
// into CaptureError entries and leaves Bundle.JetStream nil.
//
// CapturedAt is intentionally not on the snapshot — bundle-wide
// timestamping lives on BundleMeta.CapturedAt.
func FetchJetStream(ctx context.Context, client *http.Client, monitorURL string) (*JetStreamSnapshot, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if monitorURL == "" {
		return nil, fmt.Errorf("jetstream: empty monitor URL")
	}
	url := strings.TrimRight(monitorURL, "/") + "/jsz?streams=true&consumers=true&accounts=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("jetstream: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jetstream: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("jetstream: read body: %w", err)
	}
	// Non-200 still gets returned as a snapshot so the bundle records
	// the misconfiguration. Body is preserved for diagnosis (often the
	// 404 page or an "expvar disabled" message points operators at the
	// fix faster than just a status code would).
	return &JetStreamSnapshot{
		URL:    url,
		Status: resp.StatusCode,
		JSZ:    json.RawMessage(body),
	}, nil
}
