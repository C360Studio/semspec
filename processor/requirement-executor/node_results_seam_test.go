package requirementexecutor

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
)

// These tests pin #82: the recovery-resume and restructure retry paths must
// actually FIRE the node-results reset mutation. The handler side is already
// tested; without a producer-side pin a refactor that drops the call (or moves
// it past an early return) would silently resurrect stale NodeResults on the
// next rebuild and pass every existing test. The nodeResultsSender seam lets us
// observe the call without a live NATS round-trip.

type capturedNodeResults struct {
	key     string
	results []workflow.NodeResult
}

func installCapturingNodeResultsSender(c *Component) *[]capturedNodeResults {
	calls := &[]capturedNodeResults{}
	c.nodeResultsSender = func(_ context.Context, key string, results []workflow.NodeResult) error {
		*calls = append(*calls, capturedNodeResults{key: key, results: results})
		return nil
	}
	return calls
}

func firedResetFor(calls []capturedNodeResults, key string) bool {
	for _, c := range calls {
		if c.key == key && len(c.results) == 0 {
			return true
		}
	}
	return false
}

func TestResumeFromRecoveryLocked_FiresNodeResultsResetSeam(t *testing.T) {
	c := newTestComponentWithRecoveryDefer(t, 60*time.Second, 1)
	calls := installCapturingNodeResultsSender(c)
	exec := newAwaitingExec("plan-nr", "req-nr")
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	_ = c.deferToAwaitingRecoveryLocked(context.Background(), exec, "reason")
	c.resumeFromRecoveryLocked(context.Background(), exec)
	exec.mu.Unlock()

	if !firedResetFor(*calls, exec.storeKey) {
		t.Errorf("resumeFromRecoveryLocked did not fire the node-results reset seam with empty results for %q; calls=%+v",
			exec.storeKey, *calls)
	}
}

func TestStartRestructureRetryLocked_FiresNodeResultsResetSeam(t *testing.T) {
	c := newTestComponent(t)
	c.sandbox = &stubSandbox{}
	calls := installCapturingNodeResultsSender(c)

	exec := &requirementExecution{
		EntityID:          workflow.EntityPrefix() + ".exec.req.run.nr-restructure",
		Slug:              "plan-nr",
		RequirementID:     "req-restructure",
		storeKey:          workflow.RequirementExecutionKey("plan-nr", "req-restructure"),
		RequirementBranch: "semspec/requirement-req-restructure",
		RetryCount:        0,
		MaxRetries:        3,
		CurrentNodeIdx:    1,
		DAG:               &TaskDAG{},
		SortedNodeIDs:     []string{"node-0", "node-1"},
	}
	c.activeExecs.Set(exec.EntityID, exec) //nolint:errcheck

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-restructure-nr",
		TaskID:       "task-restructure-nr",
		WorkflowSlug: WorkflowSlugRequirementExecution,
		WorkflowStep: stageRequirementReview,
		Outcome:      agentic.OutcomeSuccess,
		Result:       `{"verdict":"rejected","rejection_type":"restructure","feedback":"redo"}`,
	}

	exec.mu.Lock()
	c.handleRequirementReviewerCompleteLocked(context.Background(), event, exec)
	exec.mu.Unlock()

	if !firedResetFor(*calls, exec.storeKey) {
		t.Errorf("startRestructureRetryLocked did not fire the node-results reset seam with empty results for %q; calls=%+v",
			exec.storeKey, *calls)
	}
}
