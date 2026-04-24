package workflow

import "strings"

// Error-classification constants shared between task state
// (TaskExecution.ErrorClass) and plan-level health
// (Plan.InfraHealth) so retry policy and UI can reason about
// failures without re-parsing error messages.
//
// The classification is load-bearing for Phase 5 retry semantics:
//   - "agent" failures (LLM produced bad code, tests failed, merge conflict
//     between branches) are retriable in the normal TDD flow.
//   - "infrastructure" failures (sandbox wedged, NATS unavailable, disk full)
//     must not be retried until an operator clears the underlying problem;
//     retrying them burns tokens and delays the human signal.
const (
	ErrorClassAgent          = "agent"
	ErrorClassInfrastructure = "infrastructure"

	// infrastructureErrorPrefix is the string prefix on ErrorReason that
	// execution-manager writes when the underlying failure is caused by
	// wedged infrastructure rather than agent work (e.g. the sandbox flag
	// needs_reconciliation was set). ClassifyErrorReason keys off this.
	// Changing the prefix requires touching both the writer and the
	// classifier — deliberate because silent drift between them would put
	// us back in "looks like an agent failure" territory.
	infrastructureErrorPrefix = "INFRASTRUCTURE:"
)

// ClassifyErrorReason returns the error class for a given ErrorReason
// string. Any reason beginning with "INFRASTRUCTURE:" is infrastructure;
// everything else (including empty) is agent. Callers that see empty
// reason are in a transitional state and should treat the class as
// provisional until the reason populates.
func ClassifyErrorReason(reason string) string {
	if strings.HasPrefix(reason, infrastructureErrorPrefix) {
		return ErrorClassInfrastructure
	}
	return ErrorClassAgent
}

// Plan-level infrastructure health states. Watched by the UI and by the
// retry endpoint so neither operates against a sandbox that is known to
// be degraded.
const (
	// InfraHealthHealthy means no infrastructure errors have been observed
	// for this plan's tasks in the current execution window. Default.
	InfraHealthHealthy = "healthy"

	// InfraHealthDegraded means at least one task failed with ErrorClass
	// infrastructure but the plan is still processing others. Advisory
	// signal — retry is still allowed but the UI should warn.
	InfraHealthDegraded = "degraded"

	// InfraHealthCritical means plan-manager has concluded that the
	// sandbox (or another infrastructure dependency) is wedged badly
	// enough that further execution is futile. Retry endpoints refuse
	// with 409 until an operator clears the condition.
	InfraHealthCritical = "critical"
)
