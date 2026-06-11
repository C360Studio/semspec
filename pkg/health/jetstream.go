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

// ConsumerRedelivery is the redelivery-relevant slice of one JetStream
// consumer's state, extracted from a /jsz consumer_detail. NumRedelivered
// is the count of messages currently in a redelivered state on the consumer
// at snapshot time — a non-zero value means JetStream is re-dispatching
// in-flight work, the symptom of an ack_wait shorter than processing time.
type ConsumerRedelivery struct {
	Stream         string `json:"stream"`
	Consumer       string `json:"consumer"`
	NumRedelivered int    `json:"num_redelivered"`
	NumAckPending  int    `json:"num_ack_pending"`
	NumPending     int    `json:"num_pending"`
}

// jszConsumerEnvelope is the minimal view of
// /jsz?accounts=true&streams=true&consumers=true needed to read per-consumer
// redelivery counts. The full response is far larger; we decode only these
// fields and leave the rest of JetStreamSnapshot.JSZ opaque. Field names and
// nesting verified against a live NATS 2.x capture pinned at
// testdata/fixtures/jsz-real-2026-06-11/jsz.json.
type jszConsumerEnvelope struct {
	AccountDetails []struct {
		StreamDetail []struct {
			Name           string `json:"name"`
			ConsumerDetail []struct {
				StreamName     string `json:"stream_name"`
				Name           string `json:"name"`
				NumRedelivered int    `json:"num_redelivered"`
				NumAckPending  int    `json:"num_ack_pending"`
				NumPending     int    `json:"num_pending"`
			} `json:"consumer_detail"`
		} `json:"stream_detail"`
	} `json:"account_details"`
}

// Redeliveries decodes per-consumer redelivery counts from the snapshot's
// raw /jsz body. Returns nil when the snapshot is absent or was a non-200
// capture (a non-200 body is an error page, not consumer data). Returns an
// error only when a 200 body fails to decode as the expected shape — the
// caller should surface that as an inconclusive check, not "no redelivery".
//
// Each consumer's stream_name is preferred; it falls back to the enclosing
// stream_detail.name (they agree in every capture, but the explicit field is
// authoritative).
func (s *JetStreamSnapshot) Redeliveries() ([]ConsumerRedelivery, error) {
	if s == nil || s.Status != http.StatusOK || len(s.JSZ) == 0 {
		return nil, nil
	}
	var env jszConsumerEnvelope
	if err := json.Unmarshal(s.JSZ, &env); err != nil {
		return nil, fmt.Errorf("jetstream: decode jsz consumers: %w", err)
	}
	var out []ConsumerRedelivery
	for _, acct := range env.AccountDetails {
		for _, sd := range acct.StreamDetail {
			for _, cd := range sd.ConsumerDetail {
				stream := cd.StreamName
				if stream == "" {
					stream = sd.Name
				}
				out = append(out, ConsumerRedelivery{
					Stream:         stream,
					Consumer:       cd.Name,
					NumRedelivered: cd.NumRedelivered,
					NumAckPending:  cd.NumAckPending,
					NumPending:     cd.NumPending,
				})
			}
		}
	}
	return out, nil
}
