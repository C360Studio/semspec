package scenarios

func executionAlreadyStarted(status, stage string) bool {
	switch status {
	case "implementing", "ready_for_qa", "reviewing_qa", "reviewing_rollup", "awaiting_review", "complete":
		return true
	}
	switch stage {
	case "implementing", "ready_for_qa", "reviewing_qa", "reviewing_rollup", "awaiting_review", "complete":
		return true
	default:
		return false
	}
}

// planExecutionTerminal reports whether the plan has reached a terminal state
// that execution (not plan review) produced: failed/rejected via a dev-gate
// escalation + AutoRejectOnExhaustion, an explicit escalation, or a
// deferral-complete. Reactive-mode scenarios use this to recognise that
// execution already ran (so a manual POST /execute would 400) without
// conflating it with a pre-execution plan-review rejection — callers must have
// already confirmed the plan passed approval before treating this as success.
func planExecutionTerminal(status, stage string) bool {
	switch status {
	case "failed", "rejected", "escalated", "error", "complete_with_deferrals":
		return true
	}
	switch stage {
	case "failed", "rejected", "escalated", "error", "complete_with_deferrals":
		return true
	default:
		return false
	}
}
