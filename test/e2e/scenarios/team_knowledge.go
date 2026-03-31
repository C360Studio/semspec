package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// TeamKnowledgeScenario tests the always-on team knowledge infrastructure.
// It verifies that teams are auto-seeded without explicit config, the HTTP
// roster endpoints return correct data, and the knowledge loop primitives
// (error categories, agent entities) are operational.
//
// This is a Tier 1 scenario (no LLM required). The full rejection → retry →
// knowledge injection loop is exercised by hello-world variants with mock LLM.
type TeamKnowledgeScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	nats        *client.NATSClient
}

// NewTeamKnowledgeScenario creates a new team knowledge infrastructure scenario.
func NewTeamKnowledgeScenario(cfg *config.Config) *TeamKnowledgeScenario {
	return &TeamKnowledgeScenario{
		name:        "team-knowledge",
		description: "Always-on team knowledge infrastructure: auto-seeding, HTTP endpoints, error categories",
		config:      cfg,
	}
}

func (s *TeamKnowledgeScenario) Name() string        { return s.name }
func (s *TeamKnowledgeScenario) Description() string  { return s.description }

func (s *TeamKnowledgeScenario) Setup(ctx context.Context) error {
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

	natsClient, err := client.NewNATSClient(ctx, s.config.NATSURL)
	if err != nil {
		return fmt.Errorf("create NATS client: %w", err)
	}
	s.nats = natsClient
	return nil
}

func (s *TeamKnowledgeScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"verify-teams-auto-seeded", s.stageVerifyTeamsAutoSeeded},
		{"verify-agents-endpoint", s.stageVerifyAgentsEndpoint},
		{"verify-teams-endpoint", s.stageVerifyTeamsEndpoint},
		{"verify-error-categories-seeded", s.stageVerifyErrorCategories},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		stageCtx, cancel := context.WithTimeout(ctx, s.config.StageTimeout)
		err := stage.fn(stageCtx, result)
		cancel()
		stageDuration := time.Since(stageStart)
		result.SetMetric(fmt.Sprintf("%s_duration_us", stage.name), stageDuration.Microseconds())
		if err != nil {
			result.AddStage(stage.name, false, stageDuration, err.Error())
			result.AddError(fmt.Sprintf("%s: %v", stage.name, err))
			result.Error = fmt.Sprintf("%s failed: %v", stage.name, err)
			return result, nil
		}
		result.AddStage(stage.name, true, stageDuration, "")
	}

	result.Success = true
	return result, nil
}

func (s *TeamKnowledgeScenario) Teardown(ctx context.Context) error {
	if s.nats != nil {
		return s.nats.Close(ctx)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Stages
// ---------------------------------------------------------------------------

// stageVerifyTeamsAutoSeeded checks that teams exist in ENTITY_STATES even
// without explicit teams config (always-on default roster).
func (s *TeamKnowledgeScenario) stageVerifyTeamsAutoSeeded(ctx context.Context, result *Result) error {
	kvResp, err := s.http.GetKVEntries(ctx, "ENTITY_STATES")
	if err != nil {
		return fmt.Errorf("query ENTITY_STATES: %w", err)
	}

	teamCount := 0
	agentCount := 0
	for _, entry := range kvResp.Entries {
		if isTeamEntity(entry.Key) {
			teamCount++
		}
		if isAgentEntity(entry.Key) {
			agentCount++
		}
	}

	result.SetDetail("team_count", teamCount)
	result.SetDetail("agent_count", agentCount)

	if teamCount < 2 {
		return fmt.Errorf("expected at least 2 teams (always-on default), found %d", teamCount)
	}
	if agentCount < 4 {
		return fmt.Errorf("expected at least 4 agents (2 teams × 2+ roles), found %d", agentCount)
	}

	return nil
}

// stageVerifyAgentsEndpoint tests GET /execution-manager/agents/ returns
// a non-empty JSON array with the expected fields.
func (s *TeamKnowledgeScenario) stageVerifyAgentsEndpoint(ctx context.Context, result *Result) error {
	url := s.config.HTTPBaseURL + "/execution-manager/agents/"
	resp, err := httpGetJSON(ctx, url)
	if err != nil {
		return fmt.Errorf("GET agents: %w", err)
	}

	agents, ok := resp.([]any)
	if !ok {
		return fmt.Errorf("expected JSON array from /agents/, got %T", resp)
	}

	result.SetDetail("agents_endpoint_count", len(agents))

	if len(agents) == 0 {
		return fmt.Errorf("expected non-empty agent list from /agents/")
	}

	// Verify first agent has expected fields.
	first, ok := agents[0].(map[string]any)
	if !ok {
		return fmt.Errorf("expected agent object, got %T", agents[0])
	}
	for _, field := range []string{"id", "name", "role", "model", "status"} {
		if _, exists := first[field]; !exists {
			return fmt.Errorf("agent missing field %q", field)
		}
	}

	return nil
}

// stageVerifyTeamsEndpoint tests GET /execution-manager/teams returns
// a non-empty JSON array with the expected fields.
func (s *TeamKnowledgeScenario) stageVerifyTeamsEndpoint(ctx context.Context, result *Result) error {
	url := s.config.HTTPBaseURL + "/execution-manager/teams"
	resp, err := httpGetJSON(ctx, url)
	if err != nil {
		return fmt.Errorf("GET teams: %w", err)
	}

	teams, ok := resp.([]any)
	if !ok {
		return fmt.Errorf("expected JSON array from /teams, got %T", resp)
	}

	result.SetDetail("teams_endpoint_count", len(teams))

	if len(teams) == 0 {
		return fmt.Errorf("expected non-empty team list from /teams")
	}

	// Verify first team has expected fields.
	first, ok := teams[0].(map[string]any)
	if !ok {
		return fmt.Errorf("expected team object, got %T", teams[0])
	}
	for _, field := range []string{"id", "name", "status", "member_ids", "insight_count"} {
		if _, exists := first[field]; !exists {
			return fmt.Errorf("team missing field %q", field)
		}
	}

	return nil
}

// stageVerifyErrorCategories checks that error categories are seeded in
// ENTITY_STATES (they're the foundation for signal matching and trend injection).
func (s *TeamKnowledgeScenario) stageVerifyErrorCategories(ctx context.Context, result *Result) error {
	kvResp, err := s.http.GetKVEntries(ctx, "ENTITY_STATES")
	if err != nil {
		return fmt.Errorf("query ENTITY_STATES: %w", err)
	}

	const errcatPrefix = "semspec.local.agent.roster.errcat."
	errcatCount := 0
	for _, entry := range kvResp.Entries {
		if len(entry.Key) > len(errcatPrefix) && entry.Key[:len(errcatPrefix)] == errcatPrefix {
			errcatCount++
		}
	}

	result.SetDetail("error_category_count", errcatCount)

	if errcatCount < 7 {
		return fmt.Errorf("expected at least 7 error categories, found %d", errcatCount)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// isAgentEntity returns true for agent entity keys.
func isAgentEntity(key string) bool {
	return len(key) > len(agentEntityPrefix) && key[:len(agentEntityPrefix)] == agentEntityPrefix
}

// httpGetJSON performs a GET request and decodes the JSON response.
func httpGetJSON(ctx context.Context, url string) (any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	var result any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	return result, nil
}
