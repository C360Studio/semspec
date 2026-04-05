package planmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/pkg/paths"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semspec/workflow/prompts"
	"github.com/nats-io/nats.go/jetstream"
)

// Mutation subjects — generators use request/reply to return results.
// Plan-manager is the single writer; the KV write IS the event (twofer).
const (
	mutationPrefix                = "plan.mutation."
	mutationDrafted               = "plan.mutation.drafted"
	mutationReviewed              = "plan.mutation.reviewed"
	mutationApproved              = "plan.mutation.approved"
	mutationRequirementsGenerated = "plan.mutation.requirements.generated"
	mutationArchitectureGenerated = "plan.mutation.architecture.generated"
	mutationScenariosGenerated    = "plan.mutation.scenarios.generated"
	mutationScenariosReviewed     = "plan.mutation.scenarios.reviewed"
	mutationReadyForExecution     = "plan.mutation.ready_for_execution"
	mutationGenerationFailed      = "plan.mutation.generation.failed"
	mutationClaim                 = "plan.mutation.claim"
	mutationRevision              = "plan.mutation.revision"
	mutationRollupComplete        = "plan.mutation.rollup.complete"
	mutationReviewApprove         = "plan.mutation.review.approve"
	mutationGitHubPlanCreate      = "workflow.trigger.github-plan-create"
	mutationGitHubPRFeedback      = "plan.mutation.github.pr_feedback"
)

// Mutation request types — these are the payloads generators send via request/reply.

// RequirementsMutationRequest is sent by the requirement-generator after LLM processing.
type RequirementsMutationRequest struct {
	Slug         string                 `json:"slug"`
	Requirements []workflow.Requirement `json:"requirements"`
	TraceID      string                 `json:"trace_id,omitempty"`
}

// ScenariosMutationRequest is sent by the scenario-generator for a single requirement.
type ScenariosMutationRequest struct {
	Slug          string              `json:"slug"`
	RequirementID string              `json:"requirement_id"`
	Scenarios     []workflow.Scenario `json:"scenarios"`
	TraceID       string              `json:"trace_id,omitempty"`
}

// DraftedMutationRequest is sent by the planner after focus/synthesis.
type DraftedMutationRequest struct {
	Slug             string          `json:"slug"`
	Title            string          `json:"title,omitempty"`
	Goal             string          `json:"goal"`
	Context          string          `json:"context"`
	Scope            *workflow.Scope `json:"scope,omitempty"`
	SkipArchitecture bool            `json:"skip_architecture,omitempty"`
	TraceID          string          `json:"trace_id,omitempty"`
}

// architectureMutationRequest is sent by architecture-generator after phase completion.
// Architecture is nil when the plan's SkipArchitecture flag is true (pass-through).
type architectureMutationRequest struct {
	Slug         string                         `json:"slug"`
	Architecture *workflow.ArchitectureDocument `json:"architecture,omitempty"`
	TraceID      string                         `json:"trace_id,omitempty"`
}

// ReviewedMutationRequest is sent by the plan-reviewer after reviewing.
type ReviewedMutationRequest struct {
	Slug    string `json:"slug"`
	Verdict string `json:"verdict"` // "approved" or "needs_changes"
	Summary string `json:"summary,omitempty"`
	TraceID string `json:"trace_id,omitempty"`
}

// ApprovedMutationRequest is sent by auto-approve rule or human.
type ApprovedMutationRequest struct {
	Slug    string `json:"slug"`
	TraceID string `json:"trace_id,omitempty"`
}

// GenerationFailedRequest is sent by a generator when all retries are exhausted.
type GenerationFailedRequest struct {
	Slug    string `json:"slug"`
	Phase   string `json:"phase"` // "requirements" or "scenarios"
	Error   string `json:"error"`
	TraceID string `json:"trace_id,omitempty"`
}

// ReadyForExecutionMutationRequest is sent by the plan-reviewer after round 2 approval.
type ReadyForExecutionMutationRequest struct {
	Slug    string `json:"slug"`
	TraceID string `json:"trace_id,omitempty"`
}

// ClaimMutationRequest is sent by watchers to atomically claim a plan for processing.
// The target status must be an in-progress status (IsInProgress() == true).
// Plan-manager's single-writer serialization ensures only one claim succeeds.
type ClaimMutationRequest struct {
	Slug   string          `json:"slug"`
	Status workflow.Status `json:"status"`
}

// RevisionMutationRequest is sent by the plan-reviewer when a review returns "needs_changes".
// The handler increments ReviewIteration, stores findings, and either loops the plan back
// to its re-entry point or escalates to StatusRejected at the iteration cap.
type RevisionMutationRequest struct {
	Slug     string          `json:"slug"`
	Round    int             `json:"round"`   // 1 (draft review) or 2 (scenarios review)
	Verdict  string          `json:"verdict"` // "needs_changes"
	Summary  string          `json:"summary"`
	Findings json.RawMessage `json:"findings"` // raw PlanReviewFinding array
	TraceID  string          `json:"trace_id,omitempty"`
}

// MutationResponse is the reply to all mutation requests.
type MutationResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// startMutationHandler subscribes to plan.mutation.* subjects for request/reply.
// Generators send results here; plan-manager is the single writer.
// Called from Start().
func (c *Component) startMutationHandler(ctx context.Context) error {
	if c.natsClient == nil {
		return nil
	}

	subjects := []struct {
		subject string
		handler func(context.Context, []byte) MutationResponse
	}{
		{mutationDrafted, c.handleDraftedMutation},
		{mutationReviewed, c.handleReviewedMutation},
		{mutationApproved, c.handleApprovedMutation},
		{mutationRequirementsGenerated, c.handleRequirementsMutation},
		{mutationArchitectureGenerated, c.handleArchitectureMutation},
		{mutationScenariosGenerated, c.handleScenariosMutation},
		{mutationScenariosReviewed, c.handleScenariosReviewedMutation},
		{mutationReadyForExecution, c.handleReadyForExecutionMutation},
		{mutationGenerationFailed, c.handleGenerationFailedMutation},
		{mutationClaim, c.handleClaimMutation},
		{mutationRevision, c.handleRevisionMutation},
		{mutationRollupComplete, c.handleRollupCompleteMutation},
		{mutationReviewApprove, c.handleReviewApproveMutation},
		{mutationGitHubPlanCreate, c.handleGitHubPlanCreateMutation},
		{mutationGitHubPRFeedback, c.handleGitHubPRFeedbackMutation},
	}

	for _, s := range subjects {
		h := s.handler // capture for closure
		if _, err := c.natsClient.SubscribeForRequests(ctx, s.subject, func(reqCtx context.Context, data []byte) ([]byte, error) {
			resp := h(reqCtx, data)
			return json.Marshal(resp)
		}); err != nil {
			return fmt.Errorf("subscribe to %s: %w", s.subject, err)
		}
	}

	c.logger.Info("Plan mutation handlers started",
		"count", len(subjects))
	return nil
}

// handleRequirementsMutation saves requirements inline on the plan and advances plan status.
func (c *Component) handleRequirementsMutation(ctx context.Context, data []byte) MutationResponse {
	var req RequirementsMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}

	if req.Slug == "" || len(req.Requirements) == 0 {
		return MutationResponse{Success: false, Error: "slug and requirements required"}
	}

	if err := workflow.ValidateRequirementDAG(req.Requirements); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid requirement DAG: %v", err)}
	}

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	// Ensure all requirements have the correct PlanID.
	planEntityID := workflow.PlanEntityID(req.Slug)
	for i := range req.Requirements {
		if req.Requirements[i].PlanID == "" {
			req.Requirements[i].PlanID = planEntityID
		}
	}

	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusRequirementsGenerated) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → requirements_generated", current)}
	}

	// Replace requirements inline and advance plan status.
	// The KV write IS the event — watchers (coordinator, SSE) react automatically.
	plan.Requirements = req.Requirements
	plan.Status = workflow.StatusRequirementsGenerated
	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to save requirements via mutation", "slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save plan: %v", err)}
	}

	c.logger.Info("Requirements saved via mutation",
		"slug", req.Slug,
		"count", len(req.Requirements))

	return MutationResponse{Success: true}
}

// handleArchitectureMutation stores the architecture document and advances plan to architecture_generated.
// Architecture is nil when plan.SkipArchitecture is true (pass-through with zero LLM calls).
func (c *Component) handleArchitectureMutation(ctx context.Context, data []byte) MutationResponse {
	var req architectureMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" {
		return MutationResponse{Success: false, Error: "slug required"}
	}

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusArchitectureGenerated) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → architecture_generated", current)}
	}

	// Store the architecture document when provided (nil is valid for skip path).
	if req.Architecture != nil {
		plan.Architecture = req.Architecture
	}
	plan.Status = workflow.StatusArchitectureGenerated

	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to save architecture via mutation", "slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save plan: %v", err)}
	}

	skipped := req.Architecture == nil
	c.logger.Info("Architecture phase complete via mutation",
		"slug", req.Slug,
		"skipped", skipped)

	return MutationResponse{Success: true}
}

// handleScenariosMutation saves scenarios for a requirement inline on the plan and checks convergence.
func (c *Component) handleScenariosMutation(ctx context.Context, data []byte) MutationResponse {
	var req ScenariosMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}

	if req.Slug == "" || req.RequirementID == "" {
		return MutationResponse{Success: false, Error: "slug and requirement_id required"}
	}

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	// Merge: replace scenarios for this requirement, keep others.
	if len(req.Scenarios) > 0 {
		var kept []workflow.Scenario
		for _, s := range plan.Scenarios {
			if s.RequirementID != req.RequirementID {
				kept = append(kept, s)
			}
		}
		plan.Scenarios = append(kept, req.Scenarios...)
	}

	c.logger.Info("Scenarios saved via mutation",
		"slug", req.Slug,
		"requirement_id", req.RequirementID,
		"count", len(req.Scenarios))

	// Check convergence: do all requirements have at least one scenario?
	if len(plan.Requirements) == 0 {
		// No requirements to check against — save and return.
		if err := ps.save(ctx, plan); err != nil {
			c.logger.Error("Failed to save scenarios via mutation", "slug", req.Slug, "error", err)
			return MutationResponse{Success: false, Error: fmt.Sprintf("save plan: %v", err)}
		}
		return MutationResponse{Success: true}
	}

	allCovered := true
	for _, r := range plan.Requirements {
		hasScenario := false
		for _, s := range plan.Scenarios {
			if s.RequirementID == r.ID {
				hasScenario = true
				break
			}
		}
		if !hasScenario {
			allCovered = false
			break
		}
	}

	if allCovered {
		current := plan.EffectiveStatus()
		if !current.CanTransitionTo(workflow.StatusScenariosGenerated) {
			return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → scenarios_generated", current)}
		}
		plan.Status = workflow.StatusScenariosGenerated
		c.logger.Info("All requirements have scenarios — advanced to scenarios_generated",
			"slug", req.Slug,
			"requirement_count", len(plan.Requirements))
	}

	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to save scenarios via mutation", "slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save plan: %v", err)}
	}

	return MutationResponse{Success: true}
}

// handleGenerationFailedMutation marks the plan as rejected.
func (c *Component) handleGenerationFailedMutation(ctx context.Context, data []byte) MutationResponse {
	var req GenerationFailedRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}

	c.logger.Error("Generation failed via mutation",
		"slug", req.Slug, "phase", req.Phase, "error", req.Error)

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusRejected) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → rejected", current)}
	}

	plan.LastError = req.Error
	now := time.Now()
	plan.LastErrorAt = &now
	plan.Status = workflow.StatusRejected

	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to mark plan rejected", "slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	return MutationResponse{Success: true}
}

// handleDraftedMutation updates a plan with goal/context/scope from the planner.
func (c *Component) handleDraftedMutation(ctx context.Context, data []byte) MutationResponse {
	var req DraftedMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" || req.Goal == "" {
		return MutationResponse{Success: false, Error: "slug and goal required"}
	}

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusDrafted) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → drafted", current)}
	}

	if req.Title != "" {
		plan.Title = req.Title
	}
	plan.Goal = req.Goal
	plan.Context = req.Context
	if req.Scope != nil {
		plan.Scope = *req.Scope
	}
	plan.SkipArchitecture = req.SkipArchitecture
	plan.Status = workflow.StatusDrafted

	if err := ps.save(ctx, plan); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("Plan drafted via mutation", "slug", req.Slug, "goal", req.Goal,
		"skip_architecture", req.SkipArchitecture)
	return MutationResponse{Success: true}
}

// handleReviewedMutation updates plan status to reviewed after reviewer verdict.
func (c *Component) handleReviewedMutation(ctx context.Context, data []byte) MutationResponse {
	var req ReviewedMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" {
		return MutationResponse{Success: false, Error: "slug required"}
	}

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusReviewed) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → reviewed", current)}
	}

	plan.ReviewVerdict = req.Verdict
	plan.ReviewSummary = req.Summary
	now := time.Now()
	plan.ReviewedAt = &now
	plan.Status = workflow.StatusReviewed

	if err := ps.save(ctx, plan); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("Plan reviewed via mutation", "slug", req.Slug, "verdict", req.Verdict)
	return MutationResponse{Success: true}
}

// handleApprovedMutation sets plan status to approved (from auto-approve rule or human).
func (c *Component) handleApprovedMutation(ctx context.Context, data []byte) MutationResponse {
	var req ApprovedMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" {
		return MutationResponse{Success: false, Error: "slug required"}
	}

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusApproved) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → approved", current)}
	}

	now := time.Now()
	plan.Approved = true
	plan.ApprovedAt = &now
	plan.Status = workflow.StatusApproved

	if err := ps.save(ctx, plan); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("Plan approved via mutation", "slug", req.Slug)
	return MutationResponse{Success: true}
}

// handleClaimMutation atomically transitions a plan to an in-progress status.
// Used by watchers to claim a plan before starting work. Only one claim succeeds
// per transition — subsequent claims fail because the plan is already at the
// intermediate status, making the transition invalid.
func (c *Component) handleClaimMutation(ctx context.Context, data []byte) MutationResponse {
	var req ClaimMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" || req.Status == "" {
		return MutationResponse{Success: false, Error: "slug and status required"}
	}
	if !req.Status.IsInProgress() {
		return MutationResponse{Success: false, Error: fmt.Sprintf("can only claim in-progress statuses, got %q", req.Status)}
	}

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}
	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(req.Status) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → %s", current, req.Status)}
	}
	plan.Status = req.Status

	if err := ps.save(ctx, plan); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("claim: %v", err)}
	}

	c.logger.Info("Plan claimed via mutation", "slug", req.Slug, "status", req.Status)
	return MutationResponse{Success: true}
}

// handleReadyForExecutionMutation advances plan to ready_for_execution (from round 2 review).
// handleScenariosReviewedMutation sets the plan to scenarios_reviewed.
// Used when auto_approve=false after round-2 review — the plan waits for
// human approval before advancing to ready_for_execution.
func (c *Component) handleScenariosReviewedMutation(ctx context.Context, data []byte) MutationResponse {
	var req struct {
		Slug    string `json:"slug"`
		Summary string `json:"summary,omitempty"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" {
		return MutationResponse{Success: false, Error: "slug required"}
	}

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}
	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusScenariosReviewed) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → scenarios_reviewed", current)}
	}
	plan.Status = workflow.StatusScenariosReviewed

	if err := ps.save(ctx, plan); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("Plan scenarios reviewed via mutation (awaiting human approval)", "slug", req.Slug)
	return MutationResponse{Success: true}
}

func (c *Component) handleReadyForExecutionMutation(ctx context.Context, data []byte) MutationResponse {
	var req struct {
		Slug    string `json:"slug"`
		TraceID string `json:"trace_id,omitempty"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" {
		return MutationResponse{Success: false, Error: "slug required"}
	}

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}
	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusReadyForExecution) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → ready_for_execution", current)}
	}
	plan.Status = workflow.StatusReadyForExecution

	if err := ps.save(ctx, plan); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("Plan ready for execution via mutation", "slug", req.Slug)
	return MutationResponse{Success: true}
}

// escalateRevision transitions the plan to StatusRejected when the review iteration
// cap is reached. Extracted from handleRevisionMutation to keep function length within lint limits.
func (c *Component) escalateRevision(ctx context.Context, ps *planStore, plan *workflow.Plan, req *RevisionMutationRequest, current workflow.Status, maxIterations int) MutationResponse {
	if !current.CanTransitionTo(workflow.StatusRejected) {
		return MutationResponse{Success: false, Error: fmt.Sprintf(
			"invalid transition: %s → rejected", current)}
	}
	plan.ReviewVerdict = "escalated"
	plan.ReviewSummary = fmt.Sprintf("Max revisions exceeded after %d attempts: %s",
		maxIterations, req.Summary)
	plan.LastError = fmt.Sprintf("review revision cap reached (%d/%d): %s",
		plan.ReviewIteration, maxIterations, req.Summary)
	now := time.Now()
	plan.LastErrorAt = &now
	plan.ReviewedAt = &now
	plan.Status = workflow.StatusRejected

	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to save plan (revision escalation)", "slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Warn("Review revision cap reached — plan rejected",
		"slug", req.Slug,
		"round", req.Round,
		"iteration", plan.ReviewIteration,
		"max", maxIterations)

	return MutationResponse{Success: true}
}

// formatReviewFindings attempts to format raw findings JSON into human-readable text.
// Falls back to the summary string if findings can't be parsed.
func formatReviewFindings(findingsJSON json.RawMessage, summary, verdict string) string {
	var result prompts.PlanReviewResult
	if err := json.Unmarshal(findingsJSON, &result.Findings); err == nil {
		result.Summary = summary
		result.Verdict = verdict
		return result.FormatFindings()
	}
	return summary
}

// handleRevisionMutation processes a review rejection and either retries or escalates.
// Round 1 (draft review): loops back to StatusCreated so the planner re-drafts.
// Round 2 (scenarios review): loops back to StatusApproved, clearing Requirements/Scenarios
// so they are re-generated. At the iteration cap, escalates to StatusRejected.
func (c *Component) handleRevisionMutation(ctx context.Context, data []byte) MutationResponse {
	var req RevisionMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" || req.Round < 1 || req.Round > 2 {
		return MutationResponse{Success: false, Error: "slug required and round must be 1 or 2"}
	}
	if req.Verdict != "needs_changes" {
		return MutationResponse{Success: false, Error: "revision handler only accepts verdict=needs_changes"}
	}

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	// Guard: plan must be in the reviewing state for the given round.
	current := plan.EffectiveStatus()
	expectedStatus := workflow.StatusReviewingDraft
	if req.Round == 2 {
		expectedStatus = workflow.StatusReviewingScenarios
	}
	if current != expectedStatus {
		return MutationResponse{Success: false, Error: fmt.Sprintf(
			"revision round %d requires status %s, got %s", req.Round, expectedStatus, current)}
	}

	// Store review data and increment iteration.
	plan.ReviewIteration++
	plan.ReviewFindings = req.Findings
	plan.ReviewSummary = req.Summary
	plan.ReviewVerdict = req.Verdict
	plan.ReviewFormattedFindings = formatReviewFindings(req.Findings, req.Summary, req.Verdict)

	maxIterations := c.config.MaxReviewIterations
	if maxIterations <= 0 {
		maxIterations = 1 // safety: at least one attempt before escalation
	}

	if plan.ReviewIteration >= maxIterations {
		return c.escalateRevision(ctx, ps, plan, &req, current, maxIterations)
	}

	// Under limit: loop back to re-entry point.
	// Phase-aware routing: use finding phases to determine the minimal re-entry point.
	var targetStatus workflow.Status
	switch req.Round {
	case 1:
		targetStatus = workflow.StatusCreated
	case 2:
		targetStatus = c.determineR2ReentryPoint(plan, req.Findings)
	}

	if !current.CanTransitionTo(targetStatus) {
		return MutationResponse{Success: false, Error: fmt.Sprintf(
			"invalid transition: %s → %s", current, targetStatus)}
	}

	plan.Status = targetStatus

	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to save plan (revision retry)", "slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("Plan revision loop — retrying",
		"slug", req.Slug,
		"round", req.Round,
		"iteration", plan.ReviewIteration,
		"max", maxIterations,
		"target_status", targetStatus)

	return MutationResponse{Success: true}
}

// handleRollupCompleteMutation transitions a plan out of reviewing_rollup.
// On verdict "approved": advances to StatusComplete.
// On verdict "needs_attention": transitions to StatusRejected with summary as LastError.
func (c *Component) handleRollupCompleteMutation(ctx context.Context, data []byte) MutationResponse {
	var req struct {
		Slug    string `json:"slug"`
		Verdict string `json:"verdict"` // "approved" or "needs_attention"
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" {
		return MutationResponse{Success: false, Error: "slug required"}
	}
	if req.Verdict != "approved" && req.Verdict != "needs_attention" {
		return MutationResponse{Success: false, Error: fmt.Sprintf("verdict must be 'approved' or 'needs_attention', got %q", req.Verdict)}
	}

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	current := plan.EffectiveStatus()
	if current != workflow.StatusReviewingRollup {
		return MutationResponse{Success: false, Error: fmt.Sprintf("plan must be in reviewing_rollup, got %s", current)}
	}

	switch req.Verdict {
	case "approved":
		target := workflow.StatusComplete
		if c.shouldGateReview(plan) {
			target = workflow.StatusAwaitingReview
		}
		if !current.CanTransitionTo(target) {
			return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → %s", current, target)}
		}
		plan.Status = target
		c.logger.Info("Rollup approved", "slug", req.Slug, "target", target)

	case "needs_attention":
		if !current.CanTransitionTo(workflow.StatusRejected) {
			return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → rejected", current)}
		}
		plan.LastError = req.Summary
		now := time.Now()
		plan.LastErrorAt = &now
		plan.Status = workflow.StatusRejected
		c.logger.Warn("Rollup needs attention — plan rejected",
			"slug", req.Slug, "summary", req.Summary)
	}

	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to save plan after rollup review", "slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	return MutationResponse{Success: true}
}

// handleReviewApproveMutation handles plan.mutation.review.approve — transitions
// a plan from awaiting_review to complete. Used by the UI "Approve" button and
// future GitHub PR merge detection.
func (c *Component) handleReviewApproveMutation(ctx context.Context, data []byte) MutationResponse {
	var req struct {
		Slug     string `json:"slug"`
		Reviewer string `json:"reviewer,omitempty"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" {
		return MutationResponse{Success: false, Error: "slug is required"}
	}

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	current := plan.EffectiveStatus()
	if current != workflow.StatusAwaitingReview {
		return MutationResponse{Success: false, Error: fmt.Sprintf("plan must be in awaiting_review, got %s", current)}
	}

	if !current.CanTransitionTo(workflow.StatusComplete) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → complete", current)}
	}

	plan.Status = workflow.StatusComplete
	c.logger.Info("Review approved — plan complete",
		"slug", req.Slug, "reviewer", req.Reviewer)

	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to save plan after review approval", "slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	return MutationResponse{Success: true}
}

// handleGitHubPlanCreateMutation handles workflow.trigger.github-plan-create.
// Creates a plan with GitHub metadata from a validated issue.
func (c *Component) handleGitHubPlanCreateMutation(ctx context.Context, data []byte) MutationResponse {
	// Unwrap BaseMessage envelope.
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal envelope: %v", err)}
	}

	var req payloads.GitHubPlanCreationRequest
	raw := envelope.Payload
	if len(raw) == 0 {
		raw = data // fall back to flat payload
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if err := req.Validate(); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("validate: %v", err)}
	}

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	slug := fmt.Sprintf("%d-%s", req.IssueNumber, paths.Slugify(req.Title))
	if ps.exists(slug) {
		c.logger.Info("GitHub plan already exists, skipping", "slug", slug, "issue", req.IssueNumber)
		return MutationResponse{Success: true}
	}

	plan, err := ps.create(ctx, slug, req.Title)
	if err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("create plan: %v", err)}
	}

	// Attach GitHub metadata and description.
	plan.GitHub = &workflow.GitHubMetadata{
		IssueNumber: req.IssueNumber,
		IssueURL:    req.IssueURL,
		Repository:  req.Repository,
	}
	if req.Description != "" {
		plan.Context = req.Description
	}
	if req.Scope != "" {
		plan.Scope.Include = strings.Split(req.Scope, ",")
		for i := range plan.Scope.Include {
			plan.Scope.Include[i] = strings.TrimSpace(plan.Scope.Include[i])
		}
	}

	if err := ps.save(ctx, plan); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("save plan with github metadata: %v", err)}
	}

	c.logger.Info("Plan created from GitHub issue",
		"slug", slug, "issue", req.IssueNumber, "repo", req.Repository)

	return MutationResponse{Success: true}
}

// handleGitHubPRFeedbackMutation handles plan.mutation.github.pr_feedback.
// Routes PR review comments to affected requirements via ChangeProposals and
// re-triggers execution.
func (c *Component) handleGitHubPRFeedbackMutation(ctx context.Context, data []byte) MutationResponse {
	// Unwrap BaseMessage envelope.
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal envelope: %v", err)}
	}

	var req payloads.GitHubPRFeedbackRequest
	raw := envelope.Payload
	if len(raw) == 0 {
		raw = data
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if err := req.Validate(); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("validate: %v", err)}
	}

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	if plan.EffectiveStatus() != workflow.StatusAwaitingReview {
		return MutationResponse{Success: false, Error: fmt.Sprintf("plan must be in awaiting_review, got %s", plan.EffectiveStatus())}
	}

	if plan.GitHub == nil {
		return MutationResponse{Success: false, Error: "PR feedback requires a GitHub-originated plan"}
	}

	// Map file-scoped comments to requirements.
	fileMap, err := c.buildFileToReqMap(ctx, req.Slug)
	if err != nil {
		c.logger.Warn("Failed to build file→requirement map, will retry all requirements",
			"slug", req.Slug, "error", err)
	}

	affectedReqIDs := mapCommentsToRequirements(req.Comments, fileMap)

	// If no file-scoped comments or mapping failed, store latest feedback
	// on GitHub metadata (not plan.Context — avoids unbounded growth) and
	// reset all active requirements.
	if len(affectedReqIDs) == 0 {
		if req.Body != "" {
			plan.GitHub.LatestFeedback = req.Body
		}
		for _, r := range plan.Requirements {
			if r.Status == workflow.RequirementStatusActive {
				affectedReqIDs = append(affectedReqIDs, r.ID)
			}
		}
	}

	// Create ChangeProposal(s) for audit trail.
	now := time.Now()
	for _, reqID := range affectedReqIDs {
		proposalID := fmt.Sprintf("change-proposal.%s.pr-feedback.%d.%s", req.Slug, req.ReviewID, reqID)
		rationale := fmt.Sprintf("PR review feedback from @%s (review %d)", req.Reviewer, req.ReviewID)
		if req.Body != "" {
			rationale += ": " + req.Body
		}
		plan.ChangeProposals = append(plan.ChangeProposals, workflow.ChangeProposal{
			ID:             proposalID,
			PlanID:         workflow.PlanEntityID(req.Slug),
			Title:          fmt.Sprintf("PR feedback round %d", plan.GitHub.PRRevision+1),
			Rationale:      rationale,
			Status:         workflow.ChangeProposalStatusAccepted,
			ProposedBy:     "github-pr-review",
			AffectedReqIDs: []string{reqID},
			CreatedAt:      now,
			DecidedAt:      &now,
		})
	}

	// Update GitHub metadata.
	plan.GitHub.PRRevision++
	plan.GitHub.LastProcessedReviewID = req.ReviewID

	// Reset affected requirement executions. Fail if none were reset —
	// transitioning to ready_for_execution without resets would re-run
	// with stale state.
	resetCount, resetErr := c.resetRequirementExecutionsByID(ctx, req.Slug, affectedReqIDs)
	if resetErr != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("reset requirement executions: %v", resetErr)}
	}
	if resetCount == 0 && len(affectedReqIDs) > 0 {
		return MutationResponse{Success: false, Error: "no requirement executions were reset — cannot re-execute"}
	}

	// Transition awaiting_review → ready_for_execution.
	durableCtx := context.WithoutCancel(ctx)
	if err := c.setPlanStatusCached(durableCtx, plan, workflow.StatusReadyForExecution); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("transition to ready_for_execution: %v", err)}
	}

	c.logger.Info("PR feedback applied — plan re-queued for execution",
		"slug", req.Slug,
		"pr_number", req.PRNumber,
		"review_id", req.ReviewID,
		"affected_reqs", len(affectedReqIDs),
		"reset_count", resetCount,
		"pr_revision", plan.GitHub.PRRevision)

	return MutationResponse{Success: true}
}

// buildFileToReqMap builds a file→requirementID reverse index from EXECUTION_STATES.
func (c *Component) buildFileToReqMap(ctx context.Context, slug string) (map[string][]string, error) {
	bucket, err := c.getExecBucket(ctx)
	if err != nil {
		return nil, err
	}

	prefix := "req." + slug + "."
	keys, err := bucket.Keys(ctx, jetstream.MetaOnly())
	if err != nil {
		return nil, fmt.Errorf("list execution keys: %w", err)
	}

	result := make(map[string][]string)
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		entry, getErr := bucket.Get(ctx, key)
		if getErr != nil {
			continue
		}
		var exec struct {
			NodeResults []struct {
				FilesModified []string `json:"files_modified"`
			} `json:"node_results"`
		}
		if jsonErr := json.Unmarshal(entry.Value(), &exec); jsonErr != nil {
			continue
		}
		reqID := strings.TrimPrefix(key, prefix)
		for _, nr := range exec.NodeResults {
			for _, file := range nr.FilesModified {
				result[file] = append(result[file], reqID)
			}
		}
	}
	return result, nil
}

// mapCommentsToRequirements maps PR review comments to requirement IDs using
// the file→requirement reverse index. Returns deduplicated requirement IDs.
func mapCommentsToRequirements(comments []payloads.PRReviewComment, fileMap map[string][]string) []string {
	if len(fileMap) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var result []string
	for _, c := range comments {
		if c.Path == "" {
			continue
		}
		for _, reqID := range fileMap[c.Path] {
			if !seen[reqID] {
				seen[reqID] = true
				result = append(result, reqID)
			}
		}
	}
	return result
}

// resetRequirementExecutionsByID resets specific requirement executions by ID,
// regardless of their current stage. Used by PR feedback to reset completed
// requirements for re-execution.
func (c *Component) resetRequirementExecutionsByID(ctx context.Context, slug string, reqIDs []string) (int, error) {
	var resetCount int
	for _, reqID := range reqIDs {
		key := "req." + slug + "." + reqID
		if err := c.sendReqReset(ctx, key); err != nil {
			c.logger.Warn("Failed to reset requirement execution by ID",
				"key", key, "error", err)
			continue
		}
		resetCount++
	}
	return resetCount, nil
}

// determineR2ReentryPoint examines review findings to pick the minimal re-entry
// point for Round 2. When findings carry phase markers, the plan can retry only
// the affected phase instead of clearing everything.
//
// Priority (highest first): plan > requirements > architecture > scenarios.
// If ANY error finding targets an earlier phase, re-entry cascades from there.
// Without phase markers, falls back to StatusApproved (clear everything).
func (c *Component) determineR2ReentryPoint(plan *workflow.Plan, findingsJSON json.RawMessage) workflow.Status {
	var findings []prompts.PlanReviewFinding
	if err := json.Unmarshal(findingsJSON, &findings); err != nil {
		// Can't parse findings — fall back to clear everything.
		plan.Requirements = nil
		plan.Scenarios = nil
		plan.Architecture = nil
		return workflow.StatusApproved
	}

	// Collect error-severity phases from findings.
	phaseHit := map[string]bool{}
	hasPhaseMarker := false
	for _, f := range findings {
		if f.Severity != "error" || f.Status != "violation" {
			continue
		}
		if f.Phase != "" {
			phaseHit[f.Phase] = true
			hasPhaseMarker = true
		}
	}

	if !hasPhaseMarker {
		// No phase markers — fall back to clear everything.
		plan.Requirements = nil
		plan.Scenarios = nil
		plan.Architecture = nil
		return workflow.StatusApproved
	}

	// Cascade: earlier phases force re-entry from their start point.
	switch {
	case phaseHit["plan"]:
		// Re-draft from scratch.
		plan.Requirements = nil
		plan.Scenarios = nil
		plan.Architecture = nil
		return workflow.StatusCreated

	case phaseHit["requirements"]:
		// Re-generate requirements (and downstream).
		plan.Requirements = nil
		plan.Scenarios = nil
		plan.Architecture = nil
		return workflow.StatusApproved

	case phaseHit["architecture"]:
		// Re-generate architecture (and downstream scenarios).
		plan.Architecture = nil
		plan.Scenarios = nil
		return workflow.StatusRequirementsGenerated

	case phaseHit["scenarios"]:
		// Re-generate scenarios only, preserve requirements and architecture.
		plan.Scenarios = nil
		return workflow.StatusArchitectureGenerated

	default:
		// Unknown phase values — fall back to clear everything.
		plan.Requirements = nil
		plan.Scenarios = nil
		plan.Architecture = nil
		return workflow.StatusApproved
	}
}
