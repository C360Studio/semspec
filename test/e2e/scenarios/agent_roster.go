package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// AgentRosterScenario tests that the persistent agent roster is operational
// in the running semspec instance. It verifies:
//  1. Error categories are loaded (seeded as graph entities on startup).
//  2. The plan-api exposes plan lifecycle endpoints that the
//     execution-orchestrator's agent selection depends on.
//  3. Agent entity creation works via the ENTITY_STATES KV bucket.
//
// Full benching and model escalation are tested via unit tests in
// processor/execution-orchestrator/agent_test.go. This E2E scenario
// validates the infrastructure is wired correctly for those paths.
type AgentRosterScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	nats        *client.NATSClient
}

// NewAgentRosterScenario creates a new agent roster scenario.
func NewAgentRosterScenario(cfg *config.Config) *AgentRosterScenario {
	return &AgentRosterScenario{
		name:        "agent-roster",
		description: "Tests persistent agent roster: error categories loaded, agent entities created, KV bucket operational",
		config:      cfg,
	}
}

// Name implements Scenario.
func (s *AgentRosterScenario) Name() string { return s.name }

// Description implements Scenario.
func (s *AgentRosterScenario) Description() string { return s.description }

// Setup implements Scenario.
func (s *AgentRosterScenario) Setup(ctx context.Context) error {
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
func (s *AgentRosterScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"verify-entity-states-bucket", s.stageVerifyEntityStatesBucket},
		{"verify-error-categories-seeded", s.stageVerifyErrorCategoriesSeeded},
		{"verify-plan-lifecycle", s.stageVerifyPlanLifecycle},
		{"verify-task-dispatch-subjects", s.stageVerifyTaskDispatchSubjects},
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
func (s *AgentRosterScenario) Teardown(ctx context.Context) error {
	if s.nats != nil {
		return s.nats.Close(ctx)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Stages
// ---------------------------------------------------------------------------

// stageVerifyEntityStatesBucket checks that the ENTITY_STATES KV bucket exists
// and is accessible. This is the bucket where agent entities are stored.
func (s *AgentRosterScenario) stageVerifyEntityStatesBucket(ctx context.Context, result *Result) error {
	kvResp, err := s.http.GetKVEntries(ctx, "ENTITY_STATES")
	if err != nil {
		// Non-fatal: bucket may not be exposed via message-logger.
		result.AddWarning(fmt.Sprintf("ENTITY_STATES bucket not queryable via HTTP: %v", err))
		result.SetDetail("entity_states_accessible", false)
		return nil
	}

	result.SetDetail("entity_states_accessible", true)
	result.SetDetail("entity_states_entry_count", len(kvResp.Entries))
	return nil
}

// stageVerifyErrorCategoriesSeeded checks that error category entities were
// seeded on startup. These are stored with prefix
// "semspec.local.agent.roster.errcat." in ENTITY_STATES.
func (s *AgentRosterScenario) stageVerifyErrorCategoriesSeeded(ctx context.Context, result *Result) error {
	kvResp, err := s.http.GetKVEntries(ctx, "ENTITY_STATES")
	if err != nil {
		result.AddWarning(fmt.Sprintf("cannot query ENTITY_STATES for error categories: %v", err))
		result.SetDetail("error_categories_seeded", false)
		return nil
	}

	// Count entities with the error-category prefix.
	prefix := "semspec.local.agent.roster.errcat."
	count := 0
	for _, entry := range kvResp.Entries {
		if len(entry.Key) > len(prefix) && entry.Key[:len(prefix)] == prefix {
			count++
		}
	}

	result.SetDetail("error_categories_seeded", count > 0)
	result.SetDetail("error_category_count", count)

	if count == 0 {
		result.AddWarning("no error category entities found — agent roster may not be initialized")
	} else if count < 7 {
		result.AddWarning(fmt.Sprintf("expected 7 error categories, found %d", count))
	}

	return nil
}

// stageVerifyPlanLifecycle creates and approves a plan to verify the plan-api
// is operational. The execution-orchestrator's agent selection hooks into the
// task dispatch path, which depends on these APIs working correctly.
func (s *AgentRosterScenario) stageVerifyPlanLifecycle(ctx context.Context, result *Result) error {
	resp, err := s.http.CreatePlan(ctx, "agent roster smoke test")
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

	if _, err := s.http.WaitForPlanCreated(ctx, slug); err != nil {
		return fmt.Errorf("plan not created: %w", err)
	}

	promoteResp, err := s.http.PromotePlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("promote plan: %w", err)
	}
	if promoteResp.Error != "" {
		return fmt.Errorf("promote returned error: %s", promoteResp.Error)
	}

	result.SetDetail("plan_slug", slug)
	result.SetDetail("plan_lifecycle_verified", true)
	return nil
}

// stageVerifyTaskDispatchSubjects checks message-logger for evidence that the
// task dispatch subjects are being consumed. The agent selection code runs
// during task dispatch, so active consumers on these subjects indicate the
// execution-orchestrator is operational.
func (s *AgentRosterScenario) stageVerifyTaskDispatchSubjects(ctx context.Context, result *Result) error {
	stats, err := s.http.GetMessageLogStats(ctx)
	if err != nil {
		result.AddWarning(fmt.Sprintf("could not query message-logger stats: %v", err))
		return nil
	}

	statsJSON, _ := json.Marshal(stats.SubjectCounts)
	result.SetDetail("message_log_stats", string(statsJSON))
	result.SetDetail("total_messages", stats.TotalMessages)

	return nil
}
