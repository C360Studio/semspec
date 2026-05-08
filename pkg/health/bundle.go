// Package health implements ADR-034's diagnostic-bundle format and
// detector library. The package is consumed by `cmd/semspec watch`
// (capture + live mode) and reusable from any code that wants to read
// or analyse a captured bundle.
//
// v1 design rules (load-bearing):
//
//  1. Bundle.Bundle.Format is "v1". Schema evolves additively within v1;
//     any breaking change ships as v2 with a parallel writer for one
//     cycle. See ADR-034 §1.
//  2. Plans/Loops are stored as json.RawMessage rather than typed Go
//     structs so the bundle stays resilient to upstream schema
//     evolution in semspec/semstreams. Detectors decode the slices they
//     need on demand. Trade: each detector does its own parsing.
//  3. Trajectory bodies are NOT inline in Bundle JSON — they're separate
//     files in the tarball under trajectories/<loop_id>.json, with
//     pointers in TrajectoryRefs. Bundle JSON would balloon past tens
//     of MBs on a busy run otherwise.
//  4. Detectors are pure: Detector.Run takes *Bundle and returns
//     []Diagnosis. No I/O, no network, no filesystem. The capture step
//     is the only side-effecting layer.
package health

import (
	"encoding/json"
	"time"
)

// BundleFormat is the load-bearing version string. Bundles claiming any
// other format must be rejected by readers — schema additions land
// within v1; renames or removals mint v2.
const BundleFormat = "v1"

// Bundle is the top-level diagnostic-bundle structure. The JSON
// representation is the bundle's stable contract; its file layout
// (`bundle.json` + `trajectories/<loop_id>.json` per ref) is the
// tarball contract — a tarball reader extracts both.
//
// An empty Diagnoses array means detectors ran cleanly and matched
// nothing. An absent TrajectoryRefs means none were captured.
type Bundle struct {
	Bundle         BundleMeta               `json:"bundle"`
	Host           HostInfo                 `json:"host"`
	Config         ConfigSnapshot           `json:"config"`
	Plans          []KVEntry                `json:"plans"`    // PLAN_STATES KV entries
	Loops          []KVEntry                `json:"loops"`    // AGENT_LOOPS KV entries
	Messages       []Message                `json:"messages"` // most-recent N message-logger entries
	Metrics        MetricsSnapshot          `json:"metrics"`  // parsed Prometheus exposition
	Ollama         *OllamaState             `json:"ollama,omitempty"`
	Diagnoses      []Diagnosis              `json:"diagnoses"` // detector output
	TrajectoryRefs []TrajectoryRef          `json:"trajectory_refs,omitempty"`
	JetStream      *JetStreamSnapshot       `json:"jetstream,omitempty"`      // NATS :8222/jsz snapshot
	TraceMessages  map[string]TraceMessages `json:"trace_messages,omitempty"` // keyed by loop_id
}

// JetStreamSnapshot is the NATS JetStream monitoring endpoint's response
// (`/jsz?streams=true&consumers=true&accounts=true`) captured as opaque
// JSON. The full schema (streams[] with messages/bytes/consumer_count,
// consumers[] with num_pending/num_ack_pending/delivered_seq, etc.) is
// covered by upstream NATS docs; preserving it raw means a future NATS
// release that adds fields lands in our bundle without code changes,
// and detectors that care about a specific field decode just that one.
//
// The whole field is omitempty: when --nats-monitor is empty (offline
// replay) or the endpoint is unreachable, the field is nil and the
// rest of the bundle is still useful.
type JetStreamSnapshot struct {
	URL    string          `json:"url"`    // monitor endpoint hit (e.g. http://localhost:8222/jsz?...)
	Status int             `json:"status"` // HTTP status — non-200 still recorded for diagnosis
	JSZ    json.RawMessage `json:"jsz"`    // raw response body
}

// TraceMessages is the per-loop dump of /message-logger/trace/{traceID}.
// Captured for every active loop in Bundle.Loops whose
// LoopEntity.Metadata carries a "trace_id" key (semstreams beta.43+).
// Older loops without the field are silently skipped; the bundle records
// the omission via Bundle.Diagnoses if a detector emits one.
//
// Body is preserved opaque (json.RawMessage) for the same forward-
// compat reason as KVEntry.Value and JetStreamSnapshot.JSZ.
type TraceMessages struct {
	TraceID string          `json:"trace_id"`
	Body    json.RawMessage `json:"body"` // /message-logger/trace/{traceID} response
}

// KVEntry is one row from a NATS KV bucket. Preserves the metadata
// detectors and bundle readers need to cite specific entries as
// evidence (revision for ordering, key for correlation across loops
// and plans). Value stays opaque so the bundle is resilient to
// upstream schema evolution; detectors decode it on demand.
type KVEntry struct {
	Key      string          `json:"key"`
	Revision uint64          `json:"revision"`
	Created  time.Time       `json:"created"`
	Value    json.RawMessage `json:"value"`
}

// BundleMeta describes the bundle itself, not the run it captured.
type BundleMeta struct {
	Format     string    `json:"format"` // always "v1" today
	CapturedAt time.Time `json:"captured_at"`
	CapturedBy string    `json:"captured_by"` // "semspec-vX.Y.Z" or "semspec-dev"
	Redactions []string  `json:"redactions"`  // ["api_key_env", "auth_headers", ...]
}

// HostInfo describes the machine + adjacent runtimes that produced the
// bundle. Used for cross-host comparison ("adopter A on Linux/CUDA hits
// the wedge that adopter B on macOS/CPU does not").
//
// Time fields throughout the bundle SHOULD be UTC. Capture writes
// time.Now().UTC(); adopter tooling that compares bundles across hosts
// expects this convention.
// HostInfo, OllamaHostInfo, ConfigSnapshot, OllamaState, and
// OllamaRunningModel live in bundle_host.go to keep this file under the
// package's max-public-structs threshold.

// Message is the message-logger envelope as it appeared on the wire.
// RawData is preserved as json.RawMessage so detectors can decode the
// payload type they care about without the bundle layer pre-parsing.
//
// Subject is the NATS routing key (e.g. "agent.response.<id>"); detectors
// typically match on Subject. MessageType is the payload schema (e.g.
// "agentic.response.v1"); detectors decode RawData based on it.
type Message struct {
	Sequence    int64           `json:"sequence"`
	Timestamp   time.Time       `json:"timestamp"`
	Subject     string          `json:"subject"`
	MessageType string          `json:"message_type"`
	TraceID     string          `json:"trace_id,omitempty"`
	SpanID      string          `json:"span_id,omitempty"`
	Summary     string          `json:"summary,omitempty"`
	RawData     json.RawMessage `json:"raw_data"`
}

// MetricsSnapshot holds parsed Prometheus metrics with the bits
// detectors actually use. Capturing the full /metrics text would
// inflate bundles by ~100KB each; this surface is enough for the v1
// detector set + space to grow additively.
type MetricsSnapshot struct {
	// Single-valued gauges
	LoopActiveLoops        int64   `json:"loop_active_loops"`
	LoopContextUtilization float64 `json:"loop_context_utilization"` // most recent reading

	// Per-status request counts (sum across all model labels)
	ModelRequestsTotal    int64 `json:"model_requests_total"`
	ModelRequestsErrors   int64 `json:"model_requests_errors"`
	ModelRequestsTimeouts int64 `json:"model_requests_timeouts"`

	// Failure-shape signal counters (truncations, compactions)
	LengthTruncationsTotal    int64 `json:"length_truncations_total"`
	ToolResultsTruncatedTotal int64 `json:"tool_results_truncated_total"`
	ContextCompactionsTotal   int64 `json:"context_compactions_total"`

	// CapturedAt — when the snapshot was pulled. UTC.
	CapturedAt time.Time `json:"captured_at"`
}

// TrajectoryRef points at a sibling file in the bundle's tarball:
// trajectories/<loop_id>.json holds the full agentic.Trajectory.
type TrajectoryRef struct {
	LoopID   string `json:"loop_id"`
	Filename string `json:"filename"` // relative to bundle root, e.g. "trajectories/abc-123.json"
	Steps    int    `json:"steps"`    // step count, for "is this trajectory worth opening?" triage
	Outcome  string `json:"outcome,omitempty"`
}
