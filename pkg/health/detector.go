package health

// Severity classifies a Diagnosis. Detectors choose the level that
// matches operator response: critical = stop the deploy; warning =
// look before next run; info = pattern noted for trending.
type Severity string

// Severity levels emitted by detectors.
const (
	// SeverityInfo records a pattern worth trending without immediate action.
	SeverityInfo Severity = "info"
	// SeverityWarning means the operator should look at this before the next run.
	SeverityWarning Severity = "warning"
	// SeverityCritical means stop the deploy / abandon the run.
	SeverityCritical Severity = "critical"
	// SeverityUndetermined means the detector ran but couldn't reach a
	// confident verdict — typically because the input bundle was missing
	// a field the detector needed (malformed RawData, empty metric history,
	// etc.). Surfaces "this check is inconclusive" in the bundle's audit
	// trail rather than collapsing it into "found nothing." Detectors that
	// emit this MUST set Remediation to point at the missing input.
	SeverityUndetermined Severity = "undetermined"
)

// IsValid reports whether s is one of the known severity values.
// Bundle writers should validate every Diagnosis.Severity at write
// time so a typo can't slip into a published bundle.
func (s Severity) IsValid() bool {
	switch s {
	case SeverityInfo, SeverityWarning, SeverityCritical, SeverityUndetermined:
		return true
	}
	return false
}

// Diagnosis is one detector's verdict on a Bundle. A detector may emit
// zero, one, or many — one Bundle can have N agent.responses each
// matching a separate empty-stop instance, for example.
type Diagnosis struct {
	Shape       string        `json:"shape"` // detector name, e.g. "ThinkingSpiral"
	Severity    Severity      `json:"severity"`
	Evidence    []EvidenceRef `json:"evidence"`             // pointers into bundle data so a reader can verify
	Remediation string        `json:"remediation"`          // short imperative — what to do about this
	MemoryRef   string        `json:"memory_ref,omitempty"` // e.g. "feedback_e2e_active_monitoring.md"
}

// EvidenceRef is the indirection that lets a Diagnosis cite source
// data without inlining the data itself. The bundle reader can walk
// the ref back to its origin (a message-logger sequence number, a
// metric sample, a log excerpt).
type EvidenceRef struct {
	Kind  EvidenceKind `json:"kind"`
	ID    string       `json:"id,omitempty"`    // sequence, loop_id, metric name — kind-dependent
	Field string       `json:"field,omitempty"` // dotted path within the referenced object
	Value any          `json:"value,omitempty"` // the concrete value the detector saw
}

// EvidenceKind enumerates the ref types a detector can emit. Adding a
// kind is additive within bundle v1; bundle readers must skip unknown
// kinds gracefully.
type EvidenceKind string

const (
	// EvidenceAgentResponse cites a message-logger entry on agent.response.
	// EvidenceRef.ID holds the message sequence number.
	EvidenceAgentResponse EvidenceKind = "agent_response"
	// EvidenceAgentRequest cites a message-logger entry on agent.request.
	// EvidenceRef.ID holds the message sequence number.
	EvidenceAgentRequest EvidenceKind = "agent_request"
	// EvidenceMetricSample cites a parsed metric reading.
	// EvidenceRef.ID holds the metric name.
	EvidenceMetricSample EvidenceKind = "metric_sample"
	// EvidenceLoopEntry cites an AGENT_LOOPS KV entry.
	// EvidenceRef.ID holds the loop's task_id.
	EvidenceLoopEntry EvidenceKind = "loop_entry"
	// EvidenceLogLine cites a verbatim log excerpt.
	// EvidenceRef.ID may be empty for text-only refs; when set, follows
	// "<timestamp>:<excerpt>" form.
	EvidenceLogLine EvidenceKind = "log_line"
	// EvidencePlanState cites a PLAN_STATES KV entry.
	// EvidenceRef.ID holds the plan slug.
	EvidencePlanState EvidenceKind = "plan_state"
)

// IsValid reports whether k is one of the known evidence-kind values.
// Bundle writers should validate every EvidenceRef.Kind at write time
// so a typo can't slip past adopter tooling that switches on the kind.
func (k EvidenceKind) IsValid() bool {
	switch k {
	case EvidenceAgentResponse, EvidenceAgentRequest, EvidenceMetricSample,
		EvidenceLoopEntry, EvidenceLogLine, EvidencePlanState:
		return true
	}
	return false
}

// Detector inspects a Bundle and returns its findings. Pure: no I/O,
// no system clock, no network. Determinism is required for the
// table-driven tests under pkg/health/testdata/.
type Detector interface {
	// Name uniquely identifies the detector. Used as Diagnosis.Shape.
	Name() string

	// Run inspects the bundle and emits diagnoses. Returns nil (NOT
	// an empty slice) when nothing matches — the bundle reader treats
	// nil as "detector ran cleanly, found nothing" and an empty slice
	// as the same. Either is fine; nil avoids an allocation on the
	// happy path.
	Run(*Bundle) []Diagnosis
}

// RunAll runs every supplied detector against the bundle and appends
// their diagnoses to bundle.Diagnoses. Order of detectors is
// preserved; order of diagnoses within a detector is preserved.
//
// This is a convenience over a manual loop. Detector implementations
// must not mutate the bundle — only this function does.
func RunAll(bundle *Bundle, detectors []Detector) {
	if bundle == nil {
		return
	}
	for _, d := range detectors {
		if d == nil {
			continue
		}
		bundle.Diagnoses = append(bundle.Diagnoses, d.Run(bundle)...)
	}
}
