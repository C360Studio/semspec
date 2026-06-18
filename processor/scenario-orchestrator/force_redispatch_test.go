package scenarioorchestrator

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestReqCreateMutationPayloadIncludesForceOnlyWhenRequested(t *testing.T) {
	req := workflow.Requirement{ID: "req.force.1", Title: "Force me"}

	forced := reqCreateMutationPayload("plan", "trace", "plan-branch", "base-branch", req, nil, nil, true)
	if forced["force"] != true {
		t.Fatalf("force payload = %v, want true", forced["force"])
	}

	normal := reqCreateMutationPayload("plan", "trace", "plan-branch", "base-branch", req, nil, nil, false)
	if _, ok := normal["force"]; ok {
		t.Fatalf("normal payload unexpectedly included force: %v", normal)
	}
}

func TestForceRequirementSetIgnoresBlankIDs(t *testing.T) {
	got := forceRequirementSet([]string{"req.force.1", "", "req.force.2"})
	if !got["req.force.1"] || !got["req.force.2"] {
		t.Fatalf("force set = %v, want both non-blank ids", got)
	}
	if got[""] {
		t.Fatalf("force set should ignore blank ids: %v", got)
	}
}

func TestRemoveForceRedispatchCompletionsReopensDAGSweep(t *testing.T) {
	completed := map[string]bool{
		"req.force.1": true,
		"req.keep.1":  true,
	}
	removeForceRedispatchCompletions(completed, forceRequirementSet([]string{"req.force.1"}))

	if completed["req.force.1"] {
		t.Fatalf("forced req still marked complete: %v", completed)
	}
	if !completed["req.keep.1"] {
		t.Fatalf("unforced req should remain complete: %v", completed)
	}
}
