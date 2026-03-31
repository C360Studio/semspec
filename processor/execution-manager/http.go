package executionmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/c360studio/semspec/agentgraph"
	"github.com/c360studio/semspec/workflow"
)

// RegisterHTTPHandlers registers execution-manager HTTP endpoints under prefix.
// Endpoints provide plan-scoped execution state for the human-in-the-loop UI.
//
// Registered routes:
//
//	GET {prefix}plans/{slug}/stream        — SSE stream of all execution updates
//	GET {prefix}plans/{slug}/tasks         — list active task executions
//	GET {prefix}plans/{slug}/requirements  — list active requirement executions
//	GET {prefix}agents/                    — list all agents with stats
//	GET {prefix}agents/{id}/reviews        — list reviews for an agent
//	GET {prefix}teams                      — list all teams with stats
func (c *Component) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	mux.HandleFunc(prefix+"plans/", c.handlePlanExecutions)
	mux.HandleFunc(prefix+"agents/", c.handleAgents)
	mux.HandleFunc(prefix+"teams", c.handleListTeams)
}

// handlePlanExecutions routes plan-scoped execution requests by subpath.
// Path format: {prefix}plans/{slug}/{subpath}
func (c *Component) handlePlanExecutions(w http.ResponseWriter, r *http.Request) {
	// Extract slug and subpath from URL.
	// The mux registered "plans/" so r.URL.Path starts after the prefix.
	// We need to extract: plans/{slug}/{subpath}
	path := r.URL.Path
	plansIdx := strings.Index(path, "plans/")
	if plansIdx < 0 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	remainder := path[plansIdx+len("plans/"):]

	// Split into slug and subpath.
	parts := strings.SplitN(remainder, "/", 2)
	slug := parts[0]
	if slug == "" {
		http.Error(w, "slug required", http.StatusBadRequest)
		return
	}

	subpath := ""
	if len(parts) > 1 {
		subpath = parts[1]
	}

	switch subpath {
	case "stream":
		c.handleExecutionStream(w, r, slug)
	case "tasks":
		c.handleListTasks(w, r, slug)
	case "requirements":
		c.handleListRequirements(w, r, slug)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// handleAgents routes agent-related requests.
// GET {prefix}agents/             — list all agents
// GET {prefix}agents/{id}/reviews — list reviews for an agent
func (c *Component) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if c.agentHelper == nil {
		http.Error(w, "agent roster not available", http.StatusServiceUnavailable)
		return
	}

	// Parse subpath after "agents/".
	path := r.URL.Path
	agentsIdx := strings.Index(path, "agents/")
	if agentsIdx < 0 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	remainder := strings.TrimSuffix(path[agentsIdx+len("agents/"):], "/")

	// Route: empty remainder = list all agents.
	if remainder == "" {
		c.handleListAgents(w, r)
		return
	}

	// Route: {id}/reviews = list reviews for agent.
	if strings.HasSuffix(remainder, "/reviews") {
		agentID := strings.TrimSuffix(remainder, "/reviews")
		c.handleAgentReviews(w, r, agentID)
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
}

// handleListAgents returns all agent entities with error counts and review stats.
func (c *Component) handleListAgents(w http.ResponseWriter, _ *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var allAgents []AgentResponse
	for _, role := range []string{"tester", "builder", "reviewer", "developer"} {
		agents, err := c.agentHelper.ListAgentsByRole(ctx, role)
		if err != nil {
			continue
		}
		for _, a := range agents {
			aj := AgentResponse{
				ID:          a.ID,
				Name:        a.Name,
				Role:        a.Role,
				Model:       a.Model,
				Status:      string(a.Status),
				ErrorCounts: a.ErrorCounts,
				ReviewStats: a.ReviewStats,
			}
			if a.Persona != nil {
				aj.DisplayName = a.Persona.DisplayName
			}
			allAgents = append(allAgents, aj)
		}
	}
	if allAgents == nil {
		allAgents = []AgentResponse{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(allAgents) //nolint:errcheck
}

// handleAgentReviews returns reviews for a specific agent.
func (c *Component) handleAgentReviews(w http.ResponseWriter, r *http.Request, agentID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	reviews, err := c.agentHelper.ListReviewsByAgent(ctx, agentID)
	if err != nil {
		c.logger.Error("Failed to list reviews", "agent_id", agentID, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if reviews == nil {
		reviews = []agentgraph.Review{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reviews) //nolint:errcheck
}

// handleListTeams returns all team entities with stats and insight counts.
func (c *Component) handleListTeams(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if c.agentHelper == nil {
		http.Error(w, "agent roster not available", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	teams, err := c.agentHelper.ListTeams(ctx)
	if err != nil {
		c.logger.Error("Failed to list teams", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var result []TeamResponse
	for _, t := range teams {
		result = append(result, TeamResponse{
			ID:           t.ID,
			Name:         t.Name,
			Status:       string(t.Status),
			MemberIDs:    t.MemberIDs,
			InsightCount: len(t.SharedKnowledge),
			TeamStats:    t.TeamStats,
			RedTeamStats: t.RedTeamStats,
			ErrorCounts:  t.ErrorCounts,
		})
	}
	if result == nil {
		result = []TeamResponse{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result) //nolint:errcheck
}

// AgentResponse is the JSON response for an agent in the roster listing.
type AgentResponse struct {
	ID          string                         `json:"id"`
	Name        string                         `json:"name"`
	DisplayName string                         `json:"display_name,omitempty"`
	Role        string                         `json:"role"`
	Model       string                         `json:"model"`
	Status      string                         `json:"status"`
	ErrorCounts map[workflow.ErrorCategory]int `json:"error_counts,omitempty"`
	ReviewStats workflow.ReviewStats           `json:"review_stats"`
}

// TeamResponse is the JSON response for a team in the roster listing.
type TeamResponse struct {
	ID           string                         `json:"id"`
	Name         string                         `json:"name"`
	Status       string                         `json:"status"`
	MemberIDs    []string                       `json:"member_ids"`
	InsightCount int                            `json:"insight_count"`
	TeamStats    workflow.ReviewStats           `json:"team_stats"`
	RedTeamStats workflow.ReviewStats           `json:"red_team_stats"`
	ErrorCounts  map[workflow.ErrorCategory]int `json:"error_counts,omitempty"`
}
