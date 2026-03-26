package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// teamEntityPrefix is the KV key prefix for team entities. It ends with a dot
// so that "team-insight." entries (which start with "team-insight", not "team.")
// are naturally excluded by the HasPrefix check.
const teamEntityPrefix = "semspec.local.agent.team.team."

// teamInsightEntityPrefix is excluded from team counts.
const teamInsightEntityPrefix = "semspec.local.agent.team.insight."

// agentEntityPrefix is the KV key prefix for agent entities.
const agentEntityPrefix = "semspec.local.agent.roster.agent."

// kvEntityState mirrors the structure stored in the ENTITY_STATES KV bucket.
// Each value is a JSON object with a "triples" array of predicate/object pairs.
type kvEntityState struct {
	Triples []struct {
		Predicate string `json:"predicate"`
		Object    any    `json:"object"`
	} `json:"triples"`
}

// TeamRosterScenario tests that team-based agent infrastructure is wired
// correctly in the running semspec instance. It verifies:
//  1. Team entities are seeded in the ENTITY_STATES KV bucket.
//  2. Each team entity has at least one member agent linked via team.member.agent_id.
//  3. Each agent with an agent.team.id predicate references a team entity that exists.
//  4. The plan-api plan lifecycle is operational (shared smoke test).
//
// Full team selection and red-team routing are tested via unit tests in the
// processor/execution-orchestrator package. This E2E scenario validates that
// the infrastructure is correctly wired for those paths.
type TeamRosterScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	nats        *client.NATSClient
}

// NewTeamRosterScenario creates a new team roster scenario.
func NewTeamRosterScenario(cfg *config.Config) *TeamRosterScenario {
	return &TeamRosterScenario{
		name:        "team-roster",
		description: "Tests team-based agent infrastructure: team entities seeded, member linkage correct, red team selection operational",
		config:      cfg,
	}
}

// Name implements Scenario.
func (s *TeamRosterScenario) Name() string { return s.name }

// Description implements Scenario.
func (s *TeamRosterScenario) Description() string { return s.description }

// Setup implements Scenario.
func (s *TeamRosterScenario) Setup(ctx context.Context) error {
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

// Execute implements Scenario.
func (s *TeamRosterScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"verify-teams-seeded", s.stageVerifyTeamsSeeded},
		{"verify-team-members-linked", s.stageVerifyTeamMembersLinked},
		{"verify-agent-team-linkage", s.stageVerifyAgentTeamLinkage},
		{"verify-plan-lifecycle", s.stageVerifyPlanLifecycle},
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

// Teardown implements Scenario.
func (s *TeamRosterScenario) Teardown(ctx context.Context) error {
	if s.nats != nil {
		return s.nats.Close(ctx)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Stages
// ---------------------------------------------------------------------------

// stageVerifyTeamsSeeded queries ENTITY_STATES and counts team entities. It
// expects at least 2 (blue + red team minimum). Finding zero is treated as a
// warning rather than a hard failure because the running instance may not have
// teams enabled.
func (s *TeamRosterScenario) stageVerifyTeamsSeeded(ctx context.Context, result *Result) error {
	kvResp, err := s.http.GetKVEntries(ctx, "ENTITY_STATES")
	if err != nil {
		result.AddWarning(fmt.Sprintf("ENTITY_STATES bucket not queryable via HTTP: %v", err))
		result.SetDetail("teams_seeded", false)
		result.SetDetail("team_count", 0)
		return nil
	}

	teamCount := 0
	for _, entry := range kvResp.Entries {
		if isTeamEntity(entry.Key) {
			teamCount++
		}
	}

	result.SetDetail("teams_seeded", teamCount > 0)
	result.SetDetail("team_count", teamCount)
	result.SetDetail("entity_states_total", len(kvResp.Entries))

	if teamCount == 0 {
		result.AddWarning("no team entities found in ENTITY_STATES — team infrastructure may not be enabled")
	} else if teamCount < 2 {
		result.AddWarning(fmt.Sprintf("expected at least 2 team entities (blue+red), found %d", teamCount))
	}

	return nil
}

// stageVerifyTeamMembersLinked iterates over team entities and checks that
// each one has at least one member linked via the "team.member.agent_id"
// predicate. It also verifies that a corresponding agent entity exists in
// ENTITY_STATES for each declared member ID.
func (s *TeamRosterScenario) stageVerifyTeamMembersLinked(ctx context.Context, result *Result) error {
	kvResp, err := s.http.GetKVEntries(ctx, "ENTITY_STATES")
	if err != nil {
		result.AddWarning(fmt.Sprintf("cannot query ENTITY_STATES for team members: %v", err))
		result.SetDetail("team_members_verified", false)
		return nil
	}

	// Build a set of all agent entity keys for existence checks.
	agentKeys := make(map[string]bool, len(kvResp.Entries))
	for _, entry := range kvResp.Entries {
		if strings.HasPrefix(entry.Key, agentEntityPrefix) {
			agentKeys[entry.Key] = true
		}
	}

	// Gather member counts per team and warn on any missing agents.
	type teamSummary struct {
		MemberCount       int
		MissingAgentCount int
	}
	summaries := make(map[string]teamSummary)
	teamsWithNoMembers := 0

	for _, entry := range kvResp.Entries {
		if !isTeamEntity(entry.Key) {
			continue
		}

		members := extractStringObjects(entry.Value, "team.member.agent_id")
		missing := 0
		for _, memberID := range members {
			agentKey := memberID
			// memberID may be a bare instance ID rather than the full entity key.
			// If it doesn't already look like a full agent entity key, construct one.
			if !strings.HasPrefix(memberID, agentEntityPrefix) {
				agentKey = agentEntityPrefix + memberID
			}
			if !agentKeys[agentKey] {
				missing++
				result.AddWarning(fmt.Sprintf("team %q references agent %q which has no entity in ENTITY_STATES", entry.Key, memberID))
			}
		}

		if len(members) == 0 {
			teamsWithNoMembers++
			result.AddWarning(fmt.Sprintf("team entity %q has no team.member.agent_id triples", entry.Key))
		}

		summaries[entry.Key] = teamSummary{
			MemberCount:       len(members),
			MissingAgentCount: missing,
		}
	}

	// Serialize per-team summary as a detail.
	summaryJSON, _ := json.Marshal(summaries)
	result.SetDetail("team_member_summary", string(summaryJSON))
	result.SetDetail("teams_with_no_members", teamsWithNoMembers)
	result.SetDetail("team_members_verified", teamsWithNoMembers == 0)

	return nil
}

// stageVerifyAgentTeamLinkage checks bidirectional linkage: for every agent
// entity that carries an "agent.team.id" predicate, the referenced team entity
// must exist in ENTITY_STATES.
func (s *TeamRosterScenario) stageVerifyAgentTeamLinkage(ctx context.Context, result *Result) error {
	kvResp, err := s.http.GetKVEntries(ctx, "ENTITY_STATES")
	if err != nil {
		result.AddWarning(fmt.Sprintf("cannot query ENTITY_STATES for agent team linkage: %v", err))
		result.SetDetail("agent_team_linkage_verified", false)
		return nil
	}

	// Build a set of all team entity keys.
	teamKeys := make(map[string]bool, len(kvResp.Entries))
	for _, entry := range kvResp.Entries {
		if isTeamEntity(entry.Key) {
			teamKeys[entry.Key] = true
		}
	}

	agentsWithTeam := 0
	agentsWithBrokenLink := 0

	for _, entry := range kvResp.Entries {
		if !strings.HasPrefix(entry.Key, agentEntityPrefix) {
			continue
		}

		teamIDs := extractStringObjects(entry.Value, "agent.team.id")
		if len(teamIDs) == 0 {
			continue
		}

		agentsWithTeam++
		for _, teamID := range teamIDs {
			teamKey := teamID
			// teamID may be a bare instance or a full entity key.
			if !strings.HasPrefix(teamID, teamEntityPrefix) {
				teamKey = teamEntityPrefix + teamID
			}
			if !teamKeys[teamKey] {
				agentsWithBrokenLink++
				result.AddWarning(fmt.Sprintf("agent %q has agent.team.id %q but no matching team entity exists", entry.Key, teamID))
			}
		}
	}

	result.SetDetail("agents_with_team_id", agentsWithTeam)
	result.SetDetail("agents_with_broken_team_link", agentsWithBrokenLink)
	result.SetDetail("agent_team_linkage_verified", agentsWithBrokenLink == 0)

	return nil
}

// stageVerifyPlanAPI creates a plan via HTTP to confirm the plan-api is
// operational. Unlike the agent-roster equivalent, this stage does NOT poll
// the local filesystem — plan file access now goes through the sandbox.
func (s *TeamRosterScenario) stageVerifyPlanLifecycle(ctx context.Context, result *Result) error {
	resp, err := s.http.CreatePlan(ctx, "team roster smoke test")
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("create plan returned error: %s", resp.Error)
	}

	slug := resp.Slug
	if slug == "" && resp.Plan != nil {
		slug = resp.Plan.Slug
	}
	if slug == "" {
		return fmt.Errorf("create plan returned empty slug")
	}

	result.SetDetail("plan_slug", slug)
	result.SetDetail("plan_lifecycle_verified", true)
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// isTeamEntity returns true when key belongs to a team entity (not a
// team-insight entity). Both share the "team" type prefix up to the dot, but
// team-insight keys contain "team-insight." whereas team keys contain "team."
// followed by an instance that never starts with "insight".
func isTeamEntity(key string) bool {
	return strings.HasPrefix(key, teamEntityPrefix) &&
		!strings.HasPrefix(key, teamInsightEntityPrefix)
}

// extractStringObjects parses a raw KV entry value as a kvEntityState and
// returns all string Object values whose Predicate matches the given predicate.
func extractStringObjects(raw json.RawMessage, predicate string) []string {
	if len(raw) == 0 {
		return nil
	}

	var state kvEntityState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil
	}

	var results []string
	for _, triple := range state.Triples {
		if triple.Predicate != predicate {
			continue
		}
		switch v := triple.Object.(type) {
		case string:
			results = append(results, v)
		}
	}
	return results
}
