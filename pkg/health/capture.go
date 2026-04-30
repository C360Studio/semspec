package health

// CaptureConfig parameterises a bundle capture run. All fields are
// optional but at least HTTPBaseURL must be set for any HTTP-backed
// source (metrics, message-logger, KV) to succeed.
//
// Capture is intentionally lenient: a missing source becomes an empty
// section in the bundle plus a non-fatal error in the returned
// CaptureResult. The bundle still writes — half-information beats no
// information for an adopter trying to debug a wedge.
type CaptureConfig struct {
	// HTTPBaseURL is the gateway URL serving /metrics, /message-logger/*,
	// etc. Typically "http://localhost:8080" in dev.
	HTTPBaseURL string

	// MessageLimit caps how many message-logger entries land in the
	// bundle. Older entries are dropped first. Zero = use Default.
	MessageLimit int

	// KVBuckets names the buckets to capture. Empty slice = use the v1
	// default set (PLAN_STATES, AGENT_LOOPS).
	KVBuckets []string

	// CapturedBy stamps Bundle.Bundle.CapturedBy. Typically "semspec-vX.Y.Z";
	// "semspec-dev" if unset.
	CapturedBy string

	// SkipOllama disables the `ollama ps`/`--version` shell-out. Useful
	// in tests and on hosts where Ollama isn't installed (so we avoid
	// the error footprint when LastError would always be "not found").
	SkipOllama bool
}

// Capture-time defaults.
const (
	// DefaultMessageLimit is the message-logger entry cap if unset. Tuned
	// for "enough context to diagnose recent activity" without ballooning
	// bundle size. Adopters with extreme runs can override.
	DefaultMessageLimit = 500

	// MaxResponseBytes caps the size of a single source's HTTP body
	// before parsing. The bundle's whole point is bounded artefact size;
	// a misbehaving endpoint should not be able to make the bundle
	// process OOM by streaming garbage. 64 MiB is generous for any
	// real /metrics or /kv response semspec emits today.
	MaxResponseBytes = 64 << 20
)

// DefaultKVBuckets is the v1 set of KV buckets a bundle captures.
// Additive only within v1 — removing a bucket name from this list is a
// breaking schema change that ships as v2.
var DefaultKVBuckets = []string{"PLAN_STATES", "AGENT_LOOPS"}

// CaptureError is a non-fatal capture issue: one source failed but the
// bundle assembly continued. Aggregated into CaptureResult.Errors so the
// reader can see what's missing without losing the bundle outright.
//
// Err must be non-nil; constructors should use errors.New if no
// underlying cause exists. Error() returns the source name alone if
// somehow Err is nil rather than panicking — defensive but cheap.
type CaptureError struct {
	Source string // "metrics", "kv:PLAN_STATES", "ollama", etc.
	Err    error
}

func (e *CaptureError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Source
	}
	return e.Source + ": " + e.Err.Error()
}

func (e *CaptureError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
