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
	mutationPrefix              = "plan.mutation."
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

// handleRequirementsMutation saves requirements through the store and advances plan status.
func (c *Component) handleRequirementsMutation(ctx context.Context, data []byte) MutationResponse {
	var req RequirementsMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}

	if req.Slug == "" || len(req.Requirements) == 0 {
		return MutationResponse{Success: false, Error: "slug and requirements required"}
	}

	c.mu.RLock()
	rs := c.requirements
	ps := c.plans
	c.mu.RUnlock()

	// Save through the requirement store (cache + graph triples).
	if err := rs.saveAll(ctx, req.Requirements, req.Slug); err != nil {
		c.logger.Error("Failed to save requirements from mutation", "slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save requirements: %v", err)}
	}

	// Advance plan status → requirements_generated.
	// The KV write in plan store triggers watchers (coordinator, SSE).
	if plan, ok := ps.get(req.Slug); ok {
		if err := ps.setStatus(ctx, plan.Slug, workflow.StatusRequirementsGenerated); err != nil {
			c.logger.Debug("Failed to advance plan to requirements_generated",
				"slug", req.Slug, "error", err)
		}
	}

	c.logger.Info("Requirements saved via mutation",
		"slug", req.Slug,
		"count", len(req.Requirements))

	return MutationResponse{Success: true}
}

// handleScenariosMutation saves scenarios for a requirement and checks convergence.
func (c *Component) handleScenariosMutation(ctx context.Context, data []byte) MutationResponse {
	var req ScenariosMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}

	if req.Slug == "" || req.RequirementID == "" {
		return MutationResponse{Success: false, Error: "slug and requirement_id required"}
	}

	c.mu.RLock()
	ss := c.scenarios
	rs := c.requirements
	ps := c.plans
	c.mu.RUnlock()

	// Save scenarios through the store.
	if len(req.Scenarios) > 0 {
		if err := ss.saveAll(ctx, req.Scenarios, req.Slug); err != nil {
			c.logger.Error("Failed to save scenarios from mutation",
				"slug", req.Slug, "requirement_id", req.RequirementID, "error", err)
			return MutationResponse{Success: false, Error: fmt.Sprintf("save scenarios: %v", err)}
		}
	}

	c.logger.Info("Scenarios saved via mutation",
		"slug", req.Slug,
		"requirement_id", req.RequirementID,
		"count", len(req.Scenarios))

	// Check convergence: do all requirements have at least one scenario?
	requirements := rs.listByPlan(req.Slug)
	if len(requirements) == 0 {
		return MutationResponse{Success: true} // no requirements to check against
	}

	for _, r := range requirements {
		if len(ss.listByRequirement(r.ID)) == 0 {
			return MutationResponse{Success: true} // not yet converged
		}
	}

	// All requirements covered — advance to scenarios_generated.
	// The KV write triggers watchers.
	if plan, ok := ps.get(req.Slug); ok {
		if err := ps.setStatus(ctx, plan.Slug, workflow.StatusScenariosGenerated); err != nil {
			c.logger.Debug("Failed to advance plan to scenarios_generated",
				"slug", req.Slug, "error", err)
		}
	}

	c.logger.Info("All requirements have scenarios — advanced to scenarios_generated",
		"slug", req.Slug,
		"requirement_count", len(requirements))

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
