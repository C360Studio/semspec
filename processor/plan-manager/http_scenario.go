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
func (c *Component) handleListScenariosByRequirement(w http.ResponseWriter, r *http.Request, slug, requirementID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	c.mu.RLock()
	tw := c.tripleWriter
	c.mu.RUnlock()

	scenarios, err := workflow.LoadScenarios(r.Context(), tw, slug)
	if err != nil {
		c.logger.Error("Failed to load scenarios", "slug", slug, "error", err)
		http.Error(w, "Failed to load scenarios", http.StatusInternalServerError)
		return
	}

	filtered := make([]workflow.Scenario, 0)
	for _, s := range scenarios {
		if s.RequirementID == requirementID {
			filtered = append(filtered, s)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(filtered); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleListScenarios handles GET /plans/{slug}/scenarios.
// Supports optional ?requirement_id= query param to filter by requirement.
func (c *Component) handleListScenarios(w http.ResponseWriter, r *http.Request, slug string) {
	c.mu.RLock()
	tw := c.tripleWriter
	c.mu.RUnlock()

	scenarios, err := workflow.LoadScenarios(r.Context(), tw, slug)
	if err != nil {
		c.logger.Error("Failed to load scenarios", "slug", slug, "error", err)
		http.Error(w, "Failed to load scenarios", http.StatusInternalServerError)
		return
	}

	// Optional filter by requirement_id
	if reqID := r.URL.Query().Get("requirement_id"); reqID != "" {
		filtered := scenarios[:0]
		for _, s := range scenarios {
			if s.RequirementID == reqID {
				filtered = append(filtered, s)
			}
		}
		scenarios = filtered
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(scenarios); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleGetScenario handles GET /plans/{slug}/scenarios/{scenarioId}.
func (c *Component) handleGetScenario(w http.ResponseWriter, r *http.Request, slug, scenarioID string) {
	c.mu.RLock()
	tw := c.tripleWriter
	c.mu.RUnlock()

	scenarios, err := workflow.LoadScenarios(r.Context(), tw, slug)
	if err != nil {
		c.logger.Error("Failed to load scenarios", "slug", slug, "error", err)
		http.Error(w, "Failed to load scenarios", http.StatusInternalServerError)
		return
	}

	for _, s := range scenarios {
		if s.ID == scenarioID {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(s); err != nil {
				c.logger.Warn("Failed to encode response", "error", err)
			}
			return
		}
	}

	http.Error(w, "Scenario not found", http.StatusNotFound)
}

// handleCreateScenario handles POST /plans/{slug}/scenarios.
func (c *Component) handleCreateScenario(w http.ResponseWriter, r *http.Request, slug string) {
	c.mu.RLock()
	tw := c.tripleWriter
	c.mu.RUnlock()

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req CreateScenarioHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.RequirementID == "" {
		http.Error(w, "requirement_id is required", http.StatusBadRequest)
		return
	}
	if req.Given == "" {
		http.Error(w, "given is required", http.StatusBadRequest)
		return
	}
	if req.When == "" {
		http.Error(w, "when is required", http.StatusBadRequest)
		return
	}
	if len(req.Then) == 0 {
		http.Error(w, "then is required and must have at least one item", http.StatusBadRequest)
		return
	}

	scenarios, err := workflow.LoadScenarios(r.Context(), tw, slug)
	if err != nil {
		c.logger.Error("Failed to load scenarios", "slug", slug, "error", err)
		http.Error(w, "Failed to load scenarios", http.StatusInternalServerError)
		return
	}

	// Count existing scenarios for this requirement to compute sequence
	reqSeqCount := 0
	for _, s := range scenarios {
		if s.RequirementID == req.RequirementID {
			reqSeqCount++
		}
	}

	now := time.Now()
	id := fmt.Sprintf("scenario.%s.%d", slug, len(scenarios)+1)

	newScenario := workflow.Scenario{
		ID:            id,
		RequirementID: req.RequirementID,
		Given:         req.Given,
		When:          req.When,
		Then:          req.Then,
		Status:        workflow.ScenarioStatusPending,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	scenarios = append(scenarios, newScenario)

	if err := workflow.SaveScenarios(r.Context(), tw, scenarios, slug); err != nil {
		c.logger.Error("Failed to save scenarios", "slug", slug, "error", err)
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
func (c *Component) handleUpdateScenario(w http.ResponseWriter, r *http.Request, slug, scenarioID string) {
	c.mu.RLock()
	tw := c.tripleWriter
	c.mu.RUnlock()

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req UpdateScenarioHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	scenarios, err := workflow.LoadScenarios(r.Context(), tw, slug)
	if err != nil {
		c.logger.Error("Failed to load scenarios", "slug", slug, "error", err)
		http.Error(w, "Failed to load scenarios", http.StatusInternalServerError)
		return
	}

	idx := -1
	for i, s := range scenarios {
		if s.ID == scenarioID {
			idx = i
			break
		}
	}
	if idx == -1 {
		http.Error(w, "Scenario not found", http.StatusNotFound)
		return
	}

	if req.Given != nil {
		scenarios[idx].Given = *req.Given
	}
	if req.When != nil {
		scenarios[idx].When = *req.When
	}
	if req.Then != nil {
		scenarios[idx].Then = req.Then
	}
	if req.Status != nil {
		newStatus := workflow.ScenarioStatus(*req.Status)
		if !newStatus.IsValid() {
			http.Error(w, "Invalid status value", http.StatusBadRequest)
			return
		}
		if !scenarios[idx].Status.CanTransitionTo(newStatus) {
			http.Error(w, "Invalid status transition", http.StatusConflict)
			return
		}
		scenarios[idx].Status = newStatus
	}
	scenarios[idx].UpdatedAt = time.Now()

	if err := workflow.SaveScenarios(r.Context(), tw, scenarios, slug); err != nil {
		c.logger.Error("Failed to save scenarios", "slug", slug, "error", err)
		http.Error(w, "Failed to save scenario", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(scenarios[idx]); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleDeleteScenario handles DELETE /plans/{slug}/scenarios/{scenarioId}.
func (c *Component) handleDeleteScenario(w http.ResponseWriter, r *http.Request, slug, scenarioID string) {
	c.mu.RLock()
	tw := c.tripleWriter
	c.mu.RUnlock()

	scenarios, err := workflow.LoadScenarios(r.Context(), tw, slug)
	if err != nil {
		c.logger.Error("Failed to load scenarios", "slug", slug, "error", err)
		http.Error(w, "Failed to load scenarios", http.StatusInternalServerError)
		return
	}

	idx := -1
	for i, s := range scenarios {
		if s.ID == scenarioID {
			idx = i
			break
		}
	}
	if idx == -1 {
		http.Error(w, "Scenario not found", http.StatusNotFound)
		return
	}

	scenarios = append(scenarios[:idx], scenarios[idx+1:]...)

	if err := workflow.SaveScenarios(r.Context(), tw, scenarios, slug); err != nil {
		c.logger.Error("Failed to save scenarios", "slug", slug, "error", err)
		http.Error(w, "Failed to delete scenario", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
