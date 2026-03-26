package planmanager

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// Requirement HTTP request/response types

// CreateRequirementHTTPRequest is the HTTP request body for POST /plans/{slug}/requirements.
type CreateRequirementHTTPRequest struct {
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	DependsOn   []string `json:"depends_on,omitempty"`
}

// UpdateRequirementHTTPRequest is the HTTP request body for PATCH /plans/{slug}/requirements/{reqId}.
type UpdateRequirementHTTPRequest struct {
	Title       *string  `json:"title,omitempty"`
	Description *string  `json:"description,omitempty"`
	DependsOn   []string `json:"depends_on,omitempty"`
}

// extractSlugRequirementAndAction extracts slug, requirementID, and action from paths like:
// /plan-api/plans/{slug}/requirements/{reqId}
// /plan-api/plans/{slug}/requirements/{reqId}/deprecate
func extractSlugRequirementAndAction(path string) (slug, requirementID, action string) {
	idx := strings.Index(path, "/plans/")
	if idx == -1 {
		return "", "", ""
	}

	remainder := path[idx+len("/plans/"):]
	parts := strings.Split(strings.TrimSuffix(remainder, "/"), "/")

	// Need at least 3 parts: slug, "requirements", requirementID
	if len(parts) < 3 {
		return "", "", ""
	}

	if parts[1] != "requirements" {
		return "", "", ""
	}

	slug = parts[0]
	requirementID = parts[2]

	if len(parts) > 3 {
		action = parts[3]
	}

	return slug, requirementID, action
}

// handlePlanRequirements handles top-level requirement collection endpoints.
func (c *Component) handlePlanRequirements(w http.ResponseWriter, r *http.Request, slug string) {
	switch r.Method {
	case http.MethodGet:
		c.handleListRequirements(w, r, slug)
	case http.MethodPost:
		c.handleCreateRequirement(w, r, slug)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleRequirementByID handles requirement-specific endpoints: GET, PATCH, DELETE, and actions.
func (c *Component) handleRequirementByID(w http.ResponseWriter, r *http.Request, slug, requirementID, action string) {
	switch action {
	case "":
		switch r.Method {
		case http.MethodGet:
			c.handleGetRequirement(w, r, slug, requirementID)
		case http.MethodPatch:
			c.handleUpdateRequirement(w, r, slug, requirementID)
		case http.MethodDelete:
			c.handleDeleteRequirement(w, r, slug, requirementID)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	case "deprecate":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		c.handleDeprecateRequirement(w, r, slug, requirementID)
	case "scenarios":
		c.handleListScenariosByRequirement(w, r, slug, requirementID)
	default:
		http.Error(w, "Unknown endpoint", http.StatusNotFound)
	}
}

// handleListRequirements handles GET /plans/{slug}/requirements.
// Reads from the plan cache — never hits the graph.
func (c *Component) handleListRequirements(w http.ResponseWriter, _ *http.Request, slug string) {
	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(slug)
	if !ok {
		// Return empty slice for unknown plans rather than 404 — consistent with prior behaviour.
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode([]workflow.Requirement{}); err != nil {
			c.logger.Warn("Failed to encode response", "error", err)
		}
		return
	}

	requirements := plan.Requirements
	if requirements == nil {
		requirements = []workflow.Requirement{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(requirements); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleGetRequirement handles GET /plans/{slug}/requirements/{reqId}.
func (c *Component) handleGetRequirement(w http.ResponseWriter, _ *http.Request, slug, requirementID string) {
	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(slug)
	if !ok {
		http.Error(w, "Requirement not found", http.StatusNotFound)
		return
	}

	req, _ := plan.FindRequirement(requirementID)
	if req == nil {
		http.Error(w, "Requirement not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(req); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleCreateRequirement handles POST /plans/{slug}/requirements.
// Validates DAG with existing requirements + the new one, then saves.
func (c *Component) handleCreateRequirement(w http.ResponseWriter, r *http.Request, slug string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var body CreateRequirementHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if body.Title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(slug)
	if !ok {
		http.Error(w, "Plan not found", http.StatusNotFound)
		return
	}

	now := time.Now()
	id := fmt.Sprintf("requirement.%s.%d", slug, len(plan.Requirements)+1)

	newReq := workflow.Requirement{
		ID:          id,
		PlanID:      workflow.PlanEntityID(slug),
		Title:       body.Title,
		Description: body.Description,
		Status:      workflow.RequirementStatusActive,
		DependsOn:   body.DependsOn,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Validate DAG with existing + new requirement.
	if len(body.DependsOn) > 0 {
		candidate := append(plan.Requirements, newReq)
		if err := workflow.ValidateRequirementDAG(candidate); err != nil {
			writeJSONError(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
	}

	plan.Requirements = append(plan.Requirements, newReq)
	if err := ps.save(r.Context(), plan); err != nil {
		c.logger.Error("Failed to save requirement", "slug", slug, "error", err)
		http.Error(w, "Failed to save requirement", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Requirement created via REST API", "slug", slug, "requirement_id", newReq.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(newReq); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleUpdateRequirement handles PATCH /plans/{slug}/requirements/{reqId}.
// Updates a single requirement. DAG validation only when dependencies change.
func (c *Component) handleUpdateRequirement(w http.ResponseWriter, r *http.Request, slug, requirementID string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var body UpdateRequirementHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(slug)
	if !ok {
		http.Error(w, "Requirement not found", http.StatusNotFound)
		return
	}

	_, idx := plan.FindRequirement(requirementID)
	if idx == -1 {
		http.Error(w, "Requirement not found", http.StatusNotFound)
		return
	}

	if body.Title != nil {
		plan.Requirements[idx].Title = *body.Title
	}
	if body.Description != nil {
		plan.Requirements[idx].Description = *body.Description
	}
	depsChanged := body.DependsOn != nil
	if depsChanged {
		plan.Requirements[idx].DependsOn = body.DependsOn
	}
	plan.Requirements[idx].UpdatedAt = time.Now()

	// Validate DAG only when dependencies changed.
	if depsChanged {
		if err := workflow.ValidateRequirementDAG(plan.Requirements); err != nil {
			writeJSONError(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
	}

	updated := plan.Requirements[idx]
	if err := ps.save(r.Context(), plan); err != nil {
		c.logger.Error("Failed to save requirement", "slug", slug, "error", err)
		http.Error(w, "Failed to save requirement", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(updated); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleDeleteRequirement handles DELETE /plans/{slug}/requirements/{reqId}.
// Cascade: deletes transitive dependents and their scenarios.
func (c *Component) handleDeleteRequirement(w http.ResponseWriter, r *http.Request, slug, requirementID string) {
	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(slug)
	if !ok {
		http.Error(w, "Requirement not found", http.StatusNotFound)
		return
	}

	if _, idx := plan.FindRequirement(requirementID); idx == -1 {
		http.Error(w, "Requirement not found", http.StatusNotFound)
		return
	}

	// Compute blast radius.
	toRemove := requirementBlastRadius(plan.Requirements, requirementID)

	// Remove requirements in blast radius.
	remaining := plan.Requirements[:0]
	for _, req := range plan.Requirements {
		if !toRemove[req.ID] {
			remaining = append(remaining, req)
		}
	}
	plan.Requirements = remaining

	// Cascade: remove scenarios for deleted requirements.
	survivingScenarios := plan.Scenarios[:0]
	for _, s := range plan.Scenarios {
		if !toRemove[s.RequirementID] {
			survivingScenarios = append(survivingScenarios, s)
		}
	}
	plan.Scenarios = survivingScenarios

	if err := ps.save(r.Context(), plan); err != nil {
		c.logger.Error("Failed to save after requirement delete", "slug", slug, "error", err)
		http.Error(w, "Failed to delete requirement", http.StatusInternalServerError)
		return
	}

	// Write tombstones to the graph for each removed requirement and its scenarios.
	c.mu.RLock()
	tw := c.tripleWriter
	c.mu.RUnlock()
	if tw != nil {
		for id := range toRemove {
			entityID := workflow.RequirementEntityID(id)
			_ = tw.WriteTriple(r.Context(), entityID, "semspec:requirement:status", "deleted")
		}
	}

	c.logger.Info("Deleted requirement with cascade",
		"slug", slug,
		"requirement_id", requirementID,
		"removed_count", len(toRemove))

	w.WriteHeader(http.StatusNoContent)
}

// requirementBlastRadius computes the set of requirement IDs that would be
// affected by removing the given root ID. This includes the root itself plus
// any requirements that transitively depend on it.
func requirementBlastRadius(requirements []workflow.Requirement, rootID string) map[string]bool {
	toRemove := map[string]bool{rootID: true}

	// Iterate until no new dependents are found (handles transitive chains).
	changed := true
	for changed {
		changed = false
		for _, req := range requirements {
			if toRemove[req.ID] {
				continue
			}
			for _, dep := range req.DependsOn {
				if toRemove[dep] {
					toRemove[req.ID] = true
					changed = true
					break
				}
			}
		}
	}

	return toRemove
}

// handleDeprecateRequirement handles POST /plans/{slug}/requirements/{reqId}/deprecate.
// Cascade: deprecates transitive dependents and removes their scenarios.
func (c *Component) handleDeprecateRequirement(w http.ResponseWriter, r *http.Request, slug, requirementID string) {
	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(slug)
	if !ok {
		http.Error(w, "Requirement not found", http.StatusNotFound)
		return
	}

	target, _ := plan.FindRequirement(requirementID)
	if target == nil {
		http.Error(w, "Requirement not found", http.StatusNotFound)
		return
	}

	if !target.Status.CanTransitionTo(workflow.RequirementStatusDeprecated) {
		http.Error(w, "Cannot deprecate requirement in current status", http.StatusConflict)
		return
	}

	// Compute blast radius.
	toDeprecate := requirementBlastRadius(plan.Requirements, requirementID)

	// Deprecate each requirement in the blast radius.
	now := time.Now()
	for i := range plan.Requirements {
		if toDeprecate[plan.Requirements[i].ID] && plan.Requirements[i].Status != workflow.RequirementStatusDeprecated {
			plan.Requirements[i].Status = workflow.RequirementStatusDeprecated
			plan.Requirements[i].UpdatedAt = now
		}
	}

	// Cascade: remove scenarios for deprecated requirements.
	surviving := plan.Scenarios[:0]
	for _, s := range plan.Scenarios {
		if !toDeprecate[s.RequirementID] {
			surviving = append(surviving, s)
		}
	}
	plan.Scenarios = surviving

	if err := ps.save(r.Context(), plan); err != nil {
		c.logger.Error("Failed to save after deprecate", "slug", slug, "error", err)
		http.Error(w, "Failed to deprecate requirement", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Deprecated requirement with cascade",
		"slug", slug,
		"requirement_id", requirementID,
		"deprecated_count", len(toDeprecate))

	// Re-read updated target from plan.
	updatedTarget, _ := plan.FindRequirement(requirementID)
	if updatedTarget != nil {
		target = updatedTarget
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(target); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}
