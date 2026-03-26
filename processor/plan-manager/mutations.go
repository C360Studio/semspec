package planmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// Mutation subjects — generators use request/reply to return results.
// Plan-manager is the single writer; the KV write IS the event (twofer).
const (
	mutationPrefix                = "plan.mutation."
	mutationDrafted               = "plan.mutation.drafted"
	mutationReviewed              = "plan.mutation.reviewed"
	mutationApproved              = "plan.mutation.approved"
	mutationRequirementsGenerated = "plan.mutation.requirements.generated"
	mutationScenariosGenerated    = "plan.mutation.scenarios.generated"
	mutationGenerationFailed      = "plan.mutation.generation.failed"
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
	Slug          string             `json:"slug"`
	RequirementID string             `json:"requirement_id"`
	Scenarios     []workflow.Scenario `json:"scenarios"`
	TraceID       string             `json:"trace_id,omitempty"`
}

// DraftedMutationRequest is sent by the planner after focus/synthesis.
type DraftedMutationRequest struct {
	Slug    string          `json:"slug"`
	Goal    string          `json:"goal"`
	Context string          `json:"context"`
	Scope   *workflow.Scope `json:"scope,omitempty"`
	TraceID string          `json:"trace_id,omitempty"`
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
		{mutationScenariosGenerated, c.handleScenariosMutation},
		{mutationGenerationFailed, c.handleGenerationFailedMutation},
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
		"subjects", []string{mutationRequirementsGenerated, mutationScenariosGenerated, mutationGenerationFailed})
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

	if plan, ok := ps.get(req.Slug); ok {
		plan.LastError = req.Error
		now := time.Now()
		plan.LastErrorAt = &now
		if err := ps.setStatus(ctx, plan.Slug, workflow.StatusRejected); err != nil {
			c.logger.Error("Failed to mark plan rejected", "slug", req.Slug, "error", err)
		}
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

	plan.Goal = req.Goal
	plan.Context = req.Context
	if req.Scope != nil {
		plan.Scope = *req.Scope
	}
	plan.Status = workflow.StatusDrafted

	if err := ps.save(ctx, plan); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("Plan drafted via mutation", "slug", req.Slug, "goal", req.Goal)
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

	plan.ReviewVerdict = req.Verdict
	plan.ReviewSummary = req.Summary
	now := time.Now()
	plan.ReviewedAt = &now

	if err := ps.setStatus(ctx, plan.Slug, workflow.StatusReviewed); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("status transition: %v", err)}
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

	if err := ps.setStatus(ctx, req.Slug, workflow.StatusApproved); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("status transition: %v", err)}
	}

	c.logger.Info("Plan approved via mutation", "slug", req.Slug)
	return MutationResponse{Success: true}
}
