package health

// bundle_host.go isolates the host/environment metadata types from the
// core Bundle/event-shape types in bundle.go. Splitting keeps each file
// under the package's max-public-structs threshold (revive.toml = 10)
// and matches the natural seam: bundle.go holds the wire contract,
// this file holds the snapshotted environment around it.

// HostInfo captures the OS / arch / build-info bits a reader needs to
// compare two bundles from different hosts. SemstreamsVersion is best-
// effort from runtime/debug.ReadBuildInfo and may be empty.
type HostInfo struct {
	OS                string          `json:"os"`                 // runtime.GOOS
	Arch              string          `json:"arch"`               // runtime.GOARCH
	SemspecVersion    string          `json:"semspec_version"`    // from runtime/debug.ReadBuildInfo
	SemstreamsVersion string          `json:"semstreams_version"` // best-effort from build info; "" if unreadable
	Ollama            *OllamaHostInfo `json:"ollama,omitempty"`   // present only if Ollama is the LLM provider
}

// OllamaHostInfo records static-ish info about the Ollama daemon.
// Running models are captured separately on Bundle.Ollama.Running so
// the static and runtime views don't overlap.
type OllamaHostInfo struct {
	Version string `json:"version,omitempty"`
}

// ConfigSnapshot is a redaction-aware view of the running config. We
// don't ship the entire config JSON — that risks leaking endpoint URLs
// or auth headers — only the fields that matter for diagnosis.
type ConfigSnapshot struct {
	ActiveCapabilities map[string]string `json:"active_capabilities"` // capability name → resolved model name
	RedactedEndpoints  []string          `json:"redacted_endpoints"`  // endpoint names whose URLs were redacted
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
