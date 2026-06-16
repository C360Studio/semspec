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
