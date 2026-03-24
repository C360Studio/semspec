package planapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/c360studio/semspec/workflow"
)

// ---------------------------------------------------------------------------
// Retrospective view (reactive execution mode)
// ---------------------------------------------------------------------------

// RetrospectiveScenario represents a scenario in the retrospective view.
type RetrospectiveScenario struct {
	// ScenarioID is the scenario entity ID.
	ScenarioID string `json:"scenario_id"`
	// ScenarioTitle is a human-readable title derived from the Given/When/Then fields.
	ScenarioTitle string `json:"scenario_title"`
}

// RetrospectivePhase represents one requirement with its nested scenario groups.
type RetrospectivePhase struct {
	// RequirementID is the requirement entity ID.
	RequirementID string `json:"requirement_id"`
	// RequirementTitle is the requirement title.
	RequirementTitle string `json:"requirement_title"`
	// Scenarios lists the scenarios that belong to this requirement.
	Scenarios []RetrospectiveScenario `json:"scenarios"`
}

// RetrospectiveResponse is the full response body for GET /plans/{slug}/phases/retrospective.
type RetrospectiveResponse struct {
	// PlanSlug is the slug of the plan.
	PlanSlug string `json:"plan_slug"`
	// Phases groups completed work by Requirement → Scenario → Task.
	Phases []RetrospectivePhase `json:"phases"`
}

// handlePhasesRetrospective handles GET /plans/{slug}/phases/retrospective.
//
// Returns a retrospective grouping of requirements and scenarios for the plan:
//
//  1. Load requirements for the plan
//  2. Load scenarios; group them by parent requirement
//  3. Return the nested structure: Requirement → Scenario
func (c *Component) handlePhasesRetrospective(w http.ResponseWriter, r *http.Request, slug string) {
	ctx := r.Context()
	c.mu.RLock()
	kvStore := c.kvStore
	c.mu.RUnlock()

	// Verify the plan exists.
	if !workflow.PlanExists(ctx, kvStore, slug) {
		http.Error(w, "Plan not found", http.StatusNotFound)
		return
	}

	// Load requirements.
	requirements, err := workflow.LoadRequirements(ctx, kvStore, slug)
	if err != nil {
		c.logger.Error("Failed to load requirements for retrospective", "slug", slug, "error", err)
		http.Error(w, "Failed to load requirements", http.StatusInternalServerError)
		return
	}

	// Load scenarios and build a lookup: requirementID → []Scenario.
	scenarios, err := workflow.LoadScenarios(ctx, kvStore, slug)
	if err != nil {
		c.logger.Error("Failed to load scenarios for retrospective", "slug", slug, "error", err)
		http.Error(w, "Failed to load scenarios", http.StatusInternalServerError)
		return
	}

	scenariosByReq := make(map[string][]workflow.Scenario, len(requirements))
	for _, s := range scenarios {
		scenariosByReq[s.RequirementID] = append(scenariosByReq[s.RequirementID], s)
	}

	// Build the response by iterating requirements in order.
	phases := make([]RetrospectivePhase, 0, len(requirements))
	for _, req := range requirements {
		reqScenarios := scenariosByReq[req.ID]
		scenarioGroups := make([]RetrospectiveScenario, 0, len(reqScenarios))
		for _, scen := range reqScenarios {
			title := strings.Join(scen.Then, "; ")
			if title == "" {
				title = fmt.Sprintf("Given %s, when %s", scen.Given, scen.When)
			}
			scenarioGroups = append(scenarioGroups, RetrospectiveScenario{
				ScenarioID:    scen.ID,
				ScenarioTitle: title,
			})
		}
		phases = append(phases, RetrospectivePhase{
			RequirementID:    req.ID,
			RequirementTitle: req.Title,
			Scenarios:        scenarioGroups,
		})
	}

	resp := RetrospectiveResponse{
		PlanSlug: slug,
		Phases:   phases,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		c.logger.Warn("Failed to encode retrospective response", "error", err)
	}
}
