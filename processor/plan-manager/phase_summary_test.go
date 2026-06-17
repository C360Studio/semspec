package planmanager

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPlanWithStatusIncludesAuthoritativePhaseSummary(t *testing.T) {
	c := setupTestComponent(t)
	plan := &workflow.Plan{
		ID:     workflow.PlanEntityID("demo"),
		Slug:   "demo",
		Title:  "Demo",
		Status: workflow.StatusPreparingStories,
	}

	got := c.newPlanWithStatus(context.Background(), plan)

	require.NotNil(t, got.PhaseSummary)
	assert.Equal(t, "preparing_stories", got.Stage)
	assert.Equal(t, "preparing_stories", got.PhaseSummary.Stage)
	assert.Equal(t, "planning", got.PhaseSummary.Phase)
	assert.Equal(t, "active", got.PhaseSummary.State)
	assert.Equal(t, "Preparing Stories", got.PhaseSummary.Title)
	assert.Equal(t, 0, got.PhaseSummary.ActiveLoopCount)
	assert.Equal(t, "plan-manager", got.PhaseSummary.Freshness.Source)
	assert.False(t, got.PhaseSummary.Freshness.Stale)
	assert.False(t, got.PhaseSummary.Freshness.GeneratedAt.IsZero())
}

func TestBuildPlanPhaseSummaryShowsRecoveryWait(t *testing.T) {
	created := time.Now().UTC()
	plan := &workflow.Plan{
		ID:     workflow.PlanEntityID("demo"),
		Slug:   "demo",
		Title:  "Demo",
		Status: workflow.StatusImplementing,
		PlanDecisions: []workflow.PlanDecision{
			{
				ID:             "decision-1",
				Kind:           workflow.PlanDecisionKindArchitectureRevise,
				Status:         workflow.PlanDecisionStatusProposed,
				ProposedBy:     "recovery-agent",
				Rationale:      "Add missing dependency contract before continuing execution.",
				AffectedReqIDs: []string{"REQ-1"},
				ContractImpact: &workflow.ContractImpact{
					Kind:    workflow.ContractImpactChange,
					Summary: "Add dependency/API contract.",
				},
				CreatedAt: created,
			},
		},
	}
	activeLoops := []ActiveLoopStatus{{LoopID: "loop-1", Role: "recovery-agent", State: "executing"}}
	execution := &ExecutionSummary{Completed: 2, Failed: 1, Pending: 3, Total: 6}

	got := buildPlanPhaseSummary(plan, "implementing", activeLoops, execution)

	assert.Equal(t, "recovery", got.Phase)
	assert.Equal(t, "waiting", got.State)
	assert.Equal(t, 1, got.ActiveLoopCount)
	require.NotNil(t, got.Wait)
	assert.Equal(t, "plan_decision_pending", got.Wait.Reason)
	assert.Equal(t, "decision-1", got.Wait.DecisionID)
	require.NotNil(t, got.Recovery)
	assert.Equal(t, "architecture_revise", got.Recovery.Kind)
	assert.Equal(t, "change", got.Recovery.ContractImpactKind)
	assert.Equal(t, []string{"REQ-1"}, got.Recovery.AffectedRequirementIDs)
	require.NotNil(t, got.Execution)
	assert.Equal(t, 6, got.Execution.Total)
	require.NotNil(t, got.Lessons)
	assert.Equal(t, "future_only", got.Lessons.State)
	assert.Equal(t, "none", got.Lessons.CurrentRunEffect)
}

func TestBuildPlanPhaseSummaryCarriesQAEvidence(t *testing.T) {
	plan := &workflow.Plan{
		ID:      workflow.PlanEntityID("demo"),
		Slug:    "demo",
		Title:   "Demo",
		Status:  workflow.StatusReviewingQA,
		QALevel: workflow.QALevelIntegration,
		QARun: &workflow.QARun{
			RunID:  "qa-1",
			Passed: false,
			Failures: []workflow.QAFailure{
				{Category: workflow.QAFailureCategoryBuildConfig, Message: "composite build failed"},
			},
		},
		QAVerdictSummary: &workflow.QAVerdictSummary{
			Level:   workflow.QALevelIntegration,
			Verdict: workflow.QAVerdictNeedsChanges,
			Summary: "Build configuration is incompatible with the baseline.",
		},
	}

	got := buildPlanPhaseSummary(plan, "reviewing_qa", nil, nil)

	assert.Equal(t, "qa", got.Phase)
	assert.Equal(t, "active", got.State)
	require.NotNil(t, got.QA)
	assert.Equal(t, "integration", got.QA.Level)
	assert.Equal(t, "needs_changes", got.QA.Verdict)
	assert.Equal(t, "qa-1", got.QA.RunID)
	require.NotNil(t, got.QA.Passed)
	assert.False(t, *got.QA.Passed)
	assert.Equal(t, "build_configuration", got.QA.FailureCategory)
	require.NotNil(t, got.Lessons)
	assert.Equal(t, "eligible_for_future_prompts", got.Lessons.FutureRunEffect)
}
