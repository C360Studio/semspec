package planmanager

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// Scenario HTTP request/response types

// CreateScenarioHTTPRequest is the HTTP request body for POST /plans/{slug}/scenarios.
type CreateScenarioHTTPRequest struct {
	RequirementID string   `json:"requirement_id"`
	Given         string   `json:"given"`
	When          string   `json:"when"`
	Then          []string `json:"then"`
}

// UpdateScenarioHTTPRequest is the HTTP request body for PATCH /plans/{slug}/scenarios/{scenarioId}.
type UpdateScenarioHTTPRequest struct {
	Given  *string  `json:"given,omitempty"`
	When   *string  `json:"when,omitempty"`
	Then   []string `json:"then,omitempty"`
	Status *string  `json:"status,omitempty"`
}

// extractSlugScenarioAndAction extracts slug, scenarioID, and action from paths like:
// /plan-api/plans/{slug}/scenarios/{scenarioId}
func extractSlugScenarioAndAction(path string) (slug, scenarioID, action string) {
	idx := strings.Index(path, "/plans/")
	if idx == -1 {
		return "", "", ""
	}

	remainder := path[idx+len("/plans/"):]
	parts := strings.Split(strings.TrimSuffix(remainder, "/"), "/")

	// Need at least 3 parts: slug, "scenarios", scenarioID
	if len(parts) < 3 {
		return "", "", ""
	}

	if parts[1] != "scenarios" {
		return "", "", ""
	}

	slug = parts[0]
	scenarioID = parts[2]

	if len(parts) > 3 {
		action = parts[3]
	}

	return slug, scenarioID, action
}

// handlePlanScenarios handles top-level scenario collection endpoints.
func (c *Component) handlePlanScenarios(w http.ResponseWriter, r *http.Request, slug string) {
	switch r.Method {
	case http.MethodGet:
		c.handleListScenarios(w, r, slug)
	case http.MethodPost:
		c.handleCreateScenario(w, r, slug)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleScenarioByID handles scenario-specific endpoints: GET, PATCH, DELETE.
func (c *Component) handleScenarioByID(w http.ResponseWriter, r *http.Request, slug, scenarioID, action string) {
	switch action {
	case "":
		switch r.Method {
		case http.MethodGet:
			c.handleGetScenario(w, r, slug, scenarioID)
		case http.MethodPatch:
			c.handleUpdateScenario(w, r, slug, scenarioID)
		case http.MethodDelete:
			c.handleDeleteScenario(w, r, slug, scenarioID)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		http.Error(w, "Unknown endpoint", http.StatusNotFound)
	}
}

// handleListScenariosByRequirement handles GET /plans/{slug}/requirements/{reqId}/scenarios.
// Reads from the plan cache filtered by requirement ID.
func (c *Component) handleListScenariosByRequirement(w http.ResponseWriter, r *http.Request, slug, requirementID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(slug)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode([]workflow.Scenario{}); err != nil {
			c.logger.Warn("Failed to encode response", "error", err)
		}
		return
	}

	scenarios := plan.ScenariosForRequirement(requirementID)
	if scenarios == nil {
		scenarios = []workflow.Scenario{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(scenarios); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleListScenarios handles GET /plans/{slug}/scenarios.
// Supports optional ?requirement_id= query param to filter by requirement.
// Reads from cache — never hits the graph.
func (c *Component) handleListScenarios(w http.ResponseWriter, r *http.Request, slug string) {
	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(slug)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode([]workflow.Scenario{}); err != nil {
			c.logger.Warn("Failed to encode response", "error", err)
		}
		return
	}

	// Optional filter by requirement_id.
	if reqID := r.URL.Query().Get("requirement_id"); reqID != "" {
		scenarios := plan.ScenariosForRequirement(reqID)
		if scenarios == nil {
			scenarios = []workflow.Scenario{}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(scenarios); err != nil {
			c.logger.Warn("Failed to encode response", "error", err)
		}
		return
	}

	scenarios := plan.Scenarios
	if scenarios == nil {
		scenarios = []workflow.Scenario{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(scenarios); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleGetScenario handles GET /plans/{slug}/scenarios/{scenarioId}.
func (c *Component) handleGetScenario(w http.ResponseWriter, _ *http.Request, slug, scenarioID string) {
	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(slug)
	if !ok {
		http.Error(w, "Scenario not found", http.StatusNotFound)
		return
	}

	sc, _ := plan.FindScenario(scenarioID)
	if sc == nil {
		http.Error(w, "Scenario not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(sc); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleCreateScenario handles POST /plans/{slug}/scenarios.
// Single entity write — no batch save.
func (c *Component) handleCreateScenario(w http.ResponseWriter, r *http.Request, slug string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var body CreateScenarioHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if body.RequirementID == "" {
		http.Error(w, "requirement_id is required", http.StatusBadRequest)
		return
	}
	if body.Given == "" {
		http.Error(w, "given is required", http.StatusBadRequest)
		return
	}
	if body.When == "" {
		http.Error(w, "when is required", http.StatusBadRequest)
		return
	}
	if len(body.Then) == 0 {
		http.Error(w, "then is required and must have at least one item", http.StatusBadRequest)
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
	id := fmt.Sprintf("scenario.%s.%d", slug, len(plan.Scenarios)+1)

	newScenario := workflow.Scenario{
		ID:            id,
		RequirementID: body.RequirementID,
		Given:         body.Given,
		When:          body.When,
		Then:          body.Then,
		Status:        workflow.ScenarioStatusPending,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	plan.Scenarios = append(plan.Scenarios, newScenario)
	if err := ps.save(r.Context(), plan); err != nil {
		c.logger.Error("Failed to save scenario", "slug", slug, "error", err)
		http.Error(w, "Failed to save scenario", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Scenario created via REST API", "slug", slug, "scenario_id", newScenario.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(newScenario); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleUpdateScenario handles PATCH /plans/{slug}/scenarios/{scenarioId}.
// Single entity write.
func (c *Component) handleUpdateScenario(w http.ResponseWriter, r *http.Request, slug, scenarioID string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var body UpdateScenarioHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(slug)
	if !ok {
		http.Error(w, "Scenario not found", http.StatusNotFound)
		return
	}

	_, idx := plan.FindScenario(scenarioID)
	if idx == -1 {
		http.Error(w, "Scenario not found", http.StatusNotFound)
		return
	}

	if body.Given != nil {
		plan.Scenarios[idx].Given = *body.Given
	}
	if body.When != nil {
		plan.Scenarios[idx].When = *body.When
	}
	if body.Then != nil {
		plan.Scenarios[idx].Then = body.Then
	}
	if body.Status != nil {
		newStatus := workflow.ScenarioStatus(*body.Status)
		if !newStatus.IsValid() {
			http.Error(w, "Invalid status value", http.StatusBadRequest)
			return
		}
		if !plan.Scenarios[idx].Status.CanTransitionTo(newStatus) {
			http.Error(w, "Invalid status transition", http.StatusConflict)
			return
		}
		plan.Scenarios[idx].Status = newStatus
	}
	plan.Scenarios[idx].UpdatedAt = time.Now()

	updated := plan.Scenarios[idx]
	if err := ps.save(r.Context(), plan); err != nil {
		c.logger.Error("Failed to save scenario", "scenario_id", scenarioID, "error", err)
		http.Error(w, "Failed to save scenario", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(updated); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleDeleteScenario handles DELETE /plans/{slug}/scenarios/{scenarioId}.
// Single tombstone write.
func (c *Component) handleDeleteScenario(w http.ResponseWriter, r *http.Request, slug, scenarioID string) {
	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(slug)
	if !ok {
		http.Error(w, "Scenario not found", http.StatusNotFound)
		return
	}

	_, idx := plan.FindScenario(scenarioID)
	if idx == -1 {
		http.Error(w, "Scenario not found", http.StatusNotFound)
		return
	}

	plan.Scenarios = append(plan.Scenarios[:idx], plan.Scenarios[idx+1:]...)
	if err := ps.save(r.Context(), plan); err != nil {
		c.logger.Error("Failed to delete scenario", "scenario_id", scenarioID, "error", err)
		http.Error(w, "Failed to delete scenario", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
