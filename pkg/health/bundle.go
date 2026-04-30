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
// tarball contract.
type Bundle struct {
	Bundle         BundleMeta        `json:"bundle"`
	Host           HostInfo          `json:"host"`
	Config         ConfigSnapshot    `json:"config"`
	Plans          []json.RawMessage `json:"plans"`    // PLAN_STATES KV entries, verbatim
	Loops          []json.RawMessage `json:"loops"`    // AGENT_LOOPS KV entries, verbatim
	Messages       []Message         `json:"messages"` // most-recent N message-logger entries
	Metrics        MetricsSnapshot   `json:"metrics"`  // parsed Prometheus exposition
	Ollama         *OllamaState      `json:"ollama,omitempty"`
	Diagnoses      []Diagnosis       `json:"diagnoses"` // detector output
	TrajectoryRefs []TrajectoryRef   `json:"trajectory_refs,omitempty"`
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
type HostInfo struct {
	OS                string          `json:"os"`                 // runtime.GOOS
	Arch              string          `json:"arch"`               // runtime.GOARCH
	SemspecVersion    string          `json:"semspec_version"`    // from runtime/debug.ReadBuildInfo
	SemstreamsVersion string          `json:"semstreams_version"` // best-effort from build info; "" if unreadable
	Ollama            *OllamaHostInfo `json:"ollama,omitempty"`   // present only if Ollama is the LLM provider
}

// OllamaHostInfo records the Ollama daemon's reported state. Useful for
// the Ollama-flavoured wedge-zone shapes — empty when the run targets
// Anthropic or Google.
type OllamaHostInfo struct {
	Version      string   `json:"version,omitempty"`
	LoadedModels []string `json:"loaded_models,omitempty"` // from `ollama ps`
}

// ConfigSnapshot is a redaction-aware view of the running config. We
// don't ship the entire config JSON — that risks leaking endpoint URLs
// or auth headers — only the fields that matter for diagnosis.
type ConfigSnapshot struct {
	ActiveCapabilities map[string]string `json:"active_capabilities"` // capability name → resolved model name
	RedactedEndpoints  []string          `json:"redacted_endpoints"`  // endpoint names whose URLs were redacted
}

// Message is the message-logger envelope as it appeared on the wire.
// RawData is preserved as json.RawMessage so detectors can decode the
// payload type they care about without the bundle layer pre-parsing.
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
// detector set + space to grow.
type MetricsSnapshot struct {
	// Single-valued gauges/counters
	LoopActiveLoops               int64     `json:"loop_active_loops"`
	LoopContextUtilization        float64   `json:"loop_context_utilization"`                   // most recent reading
	LoopContextUtilizationHistory []float64 `json:"loop_context_utilization_history,omitempty"` // per-snapshot trend; v2 detector seed

	// Per-status request counts (sum across all model labels)
	ModelRequestsTotal    int64 `json:"model_requests_total"`
	ModelRequestsErrors   int64 `json:"model_requests_errors"`
	ModelRequestsTimeouts int64 `json:"model_requests_timeouts"`

	// Failure-shape signal counters (truncations, compactions)
	LengthTruncationsTotal    int64 `json:"length_truncations_total"`
	ToolResultsTruncatedTotal int64 `json:"tool_results_truncated_total"`
	ContextCompactionsTotal   int64 `json:"context_compactions_total"`

	// CapturedAt sub-segments — when the snapshot was pulled. Used for
	// trend computation in v2 wedge-zone detector.
	CapturedAt time.Time `json:"captured_at"`
}

// OllamaState is captured separately from HostInfo when the run uses
// Ollama; HostInfo.Ollama covers static metadata, this covers the
// running daemon's snapshot during the run.
type OllamaState struct {
	Running   []OllamaRunningModel `json:"running,omitempty"`
	LastError string               `json:"last_error,omitempty"` // from "ollama ps" stderr if any
}

// OllamaRunningModel is one row from `ollama ps`.
type OllamaRunningModel struct {
	Name      string `json:"name"`
	ID        string `json:"id,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
	Until     string `json:"until,omitempty"` // raw "Until" column; opaque time format from Ollama
}

// TrajectoryRef points at a sibling file in the bundle's tarball:
// trajectories/<loop_id>.json holds the full agentic.Trajectory.
type TrajectoryRef struct {
	LoopID   string `json:"loop_id"`
	Filename string `json:"filename"` // relative to bundle root, e.g. "trajectories/abc-123.json"
	Steps    int    `json:"steps"`    // step count, for "is this trajectory worth opening?" triage
	Outcome  string `json:"outcome,omitempty"`
}
