package planmanager

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semspec/workflow"
)

func (c *Component) newPlanWithStatus(ctx context.Context, plan *workflow.Plan) *PlanWithStatus {
	stage := c.determinePlanStage(plan)
	execution := c.computeExecutionSummary(ctx, plan)
	activeLoops := []ActiveLoopStatus{}
	return &PlanWithStatus{
		Plan:             plan,
		Stage:            stage,
		ActiveLoops:      activeLoops,
		ExecutionSummary: execution,
		PhaseSummary:     buildPlanPhaseSummary(plan, stage, activeLoops, execution),
	}
}

func buildPlanPhaseSummary(plan *workflow.Plan, stage string, activeLoops []ActiveLoopStatus, execution *ExecutionSummary) *PlanPhaseSummary {
	phase, state, title, detail := classifyPlanPhase(plan, stage, execution)
	summary := &PlanPhaseSummary{
		Stage:           stage,
		Phase:           phase,
		State:           state,
		Title:           title,
		Detail:          detail,
		ActiveLoopCount: len(activeLoops),
		Execution:       execution,
		Lessons:         buildPlanLessonSummary(plan, stage),
		QA:              buildPlanQASummary(plan),
		Recovery:        buildPlanRecoverySummary(plan),
		Freshness: PlanFreshnessSummary{
			Source:      "plan-manager",
			GeneratedAt: time.Now().UTC(),
			Stale:       false,
		},
	}
	summary.Wait = buildPlanWaitSummary(plan, stage, summary.Recovery)
	if summary.Wait != nil {
		summary.Phase = "waiting"
		summary.State = "waiting"
	}
	if summary.Recovery != nil && summary.Recovery.Status != string(workflow.PlanDecisionStatusAccepted) {
		summary.Phase = "recovery"
	}
	return summary
}

func classifyPlanPhase(plan *workflow.Plan, stage string, execution *ExecutionSummary) (phase, state, title, detail string) {
	switch stage {
	case "drafting", "exploring", "explored", "ready_for_approval", "reviewed", "needs_changes", "approved",
		"generating_requirements", "requirements_generated", "generating_architecture",
		"architecture_generated", "preparing_stories", "stories_generated",
		"generating_scenarios", "reviewing_scenarios", "scenarios_generated",
		"scenarios_reviewed":
		return "planning", activeOrWaitingState(stage), titleForPlanningStage(stage), detailForPlanningStage(stage)
	case "ready_for_execution":
		return "execution", "waiting", "Ready for execution", "Execution has not started yet."
	case "implementing":
		if execution != nil {
			return "execution", "active", "Execution", fmt.Sprintf("%d of %d requirements complete; %d failed.", execution.Completed, execution.Total, execution.Failed)
		}
		return "execution", "active", "Execution", "Implementation is running."
	case "ready_for_qa":
		return "qa", "waiting", "Ready for QA", "Implementation has converged and is waiting for QA execution."
	case "reviewing_qa":
		return "qa", "active", "Reviewing QA", "QA evidence is being reviewed."
	case "reviewing_rollup":
		return "qa", "active", "Reviewing rollout", "Final plan-level review is in progress."
	case "complete", "complete_with_deferrals":
		return "terminal", "complete", "Complete", "Plan execution reached a terminal complete state."
	case "failed", "rejected":
		return "terminal", "failed", "Failed", "Plan execution reached a terminal failed state."
	case "archived":
		return "terminal", "complete", "Archived", "Plan is archived."
	case "awaiting_review":
		return "waiting", "waiting", "Awaiting review", "A human decision is required before the plan can continue."
	case "changed":
		return "recovery", "waiting", "Plan changed", "Accepted changes are waiting for scoped planning re-entry."
	default:
		if plan != nil && plan.EffectiveStatus().IsInProgress() {
			return "planning", "active", "Planning", "Plan work is in progress."
		}
		return "planning", "active", "Planning", "Plan work has started."
	}
}

func activeOrWaitingState(stage string) string {
	switch stage {
	case "explored", "ready_for_approval", "reviewed", "needs_changes", "approved",
		"requirements_generated", "architecture_generated", "stories_generated",
		"scenarios_generated", "scenarios_reviewed":
		return "waiting"
	default:
		return "active"
	}
}

func titleForPlanningStage(stage string) string {
	switch stage {
	case "exploring":
		return "Exploring"
	case "explored":
		return "Exploration complete"
	case "generating_requirements":
		return "Generating requirements"
	case "requirements_generated":
		return "Requirements generated"
	case "generating_architecture":
		return "Generating architecture"
	case "architecture_generated":
		return "Architecture generated"
	case "preparing_stories":
		return "Preparing Stories"
	case "stories_generated":
		return "Stories generated"
	case "generating_scenarios":
		return "Generating scenarios"
	case "reviewing_scenarios":
		return "Reviewing scenarios"
	case "scenarios_generated":
		return "Scenarios generated"
	case "scenarios_reviewed":
		return "Scenarios reviewed"
	case "ready_for_approval", "reviewed":
		return "Awaiting approval"
	case "needs_changes":
		return "Needs changes"
	default:
		return "Planning"
	}
}

func detailForPlanningStage(stage string) string {
	switch stage {
	case "exploring":
		return "Analyst exploration is collecting goals, context, and open questions."
	case "explored":
		return "Exploration is complete and ready for drafting."
	case "generating_requirements":
		return "Decomposing the approved plan into requirements."
	case "requirements_generated":
		return "Requirements are ready for architecture generation."
	case "generating_architecture":
		return "Generating architecture and component boundaries."
	case "architecture_generated":
		return "Architecture is ready for Story preparation."
	case "preparing_stories":
		return "Preparing BMAD Stories, ownership, and task checklists."
	case "stories_generated":
		return "Stories are ready for scenario generation."
	case "generating_scenarios":
		return "Generating acceptance scenarios."
	case "reviewing_scenarios":
		return "Reviewing requirements, architecture, Stories, and scenarios."
	case "scenarios_generated":
		return "Scenarios are ready for review."
	case "scenarios_reviewed":
		return "Reviewed scenarios are waiting for approval."
	case "needs_changes":
		return "Reviewer requested changes before the plan can continue."
	default:
		return "Planning is active."
	}
}

func buildPlanWaitSummary(plan *workflow.Plan, stage string, recovery *PlanRecoverySummary) *PlanWaitSummary {
	if recovery != nil {
		switch recovery.Status {
		case string(workflow.PlanDecisionStatusProposed), string(workflow.PlanDecisionStatusUnderReview):
			return &PlanWaitSummary{
				Reason:         "plan_decision_pending",
				DecisionID:     recovery.DecisionID,
				PolicyReason:   "A PlanDecision is waiting for review or acceptance.",
				RequiredAction: "review_plan_decision",
			}
		}
	}
	switch stage {
	case "ready_for_approval", "reviewed", "scenarios_reviewed", "awaiting_review":
		return &PlanWaitSummary{Reason: "human_approval_required", RequiredAction: "approve_or_reject"}
	case "ready_for_execution":
		return &PlanWaitSummary{Reason: "execution_not_started", RequiredAction: "start_execution"}
	case "ready_for_qa":
		return &PlanWaitSummary{Reason: "qa_not_started", RequiredAction: "run_qa"}
	default:
		return nil
	}
}

func buildPlanRecoverySummary(plan *workflow.Plan) *PlanRecoverySummary {
	decision := latestVisiblePlanDecision(plan)
	if decision == nil {
		return nil
	}
	summary := &PlanRecoverySummary{
		DecisionID:             decision.ID,
		Kind:                   string(decision.Kind),
		Status:                 string(decision.Status),
		ProposedBy:             decision.ProposedBy,
		Summary:                decision.Rationale,
		AffectedRequirementIDs: append([]string(nil), decision.AffectedReqIDs...),
		AffectedStoryIDs:       append([]string(nil), decision.AffectedStoryIDs...),
	}
	if decision.ContractImpact != nil {
		summary.ContractImpactKind = string(decision.ContractImpact.Kind)
		summary.ContractImpactSummary = decision.ContractImpact.Summary
	}
	return summary
}

func latestVisiblePlanDecision(plan *workflow.Plan) *workflow.PlanDecision {
	if plan == nil {
		return nil
	}
	var out *workflow.PlanDecision
	for i := range plan.PlanDecisions {
		decision := &plan.PlanDecisions[i]
		if decision.Status == workflow.PlanDecisionStatusArchived {
			continue
		}
		if out == nil || decision.CreatedAt.After(out.CreatedAt) {
			out = decision
		}
	}
	return out
}

func buildPlanLessonSummary(plan *workflow.Plan, stage string) *PlanLessonSummary {
	if plan == nil {
		return nil
	}
	switch stage {
	case "implementing", "reviewing_rollup", "ready_for_qa", "reviewing_qa", "complete", "complete_with_deferrals", "failed", "rejected":
		return &PlanLessonSummary{
			State:            "future_only",
			CurrentRunEffect: "none",
			FutureRunEffect:  "eligible_for_future_prompts",
			Detail:           "Lessons emitted during this run are recorded for later role prompts; they do not change already-dispatched work.",
		}
	default:
		return nil
	}
}

func buildPlanQASummary(plan *workflow.Plan) *PlanQASummary {
	if plan == nil || (plan.QARun == nil && plan.QAVerdictSummary == nil) {
		return nil
	}
	out := &PlanQASummary{}
	if verdict := plan.QAVerdictSummary; verdict != nil {
		out.Level = string(verdict.Level)
		out.Verdict = string(verdict.Verdict)
		out.Summary = verdict.Summary
	}
	if run := plan.QARun; run != nil {
		out.RunID = run.RunID
		out.Passed = &run.Passed
		if out.Level == "" {
			out.Level = string(plan.EffectiveQALevel())
		}
		for _, failure := range run.Failures {
			if failure.Category != "" {
				out.FailureCategory = string(failure.Category)
				break
			}
		}
	}
	return out
}
