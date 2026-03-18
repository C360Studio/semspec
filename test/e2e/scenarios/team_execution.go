package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// TeamExecutionScenario tests the team-based execution pipeline end-to-end.
//
// It creates a minimal Python project, creates a plan for JWT authentication,
// drives the plan through planning → requirement/scenario creation → execution,
// and then verifies:
//  1. Plan was generated with correct goal/context by the planner LLM.
//  2. Requirements and scenarios are stored correctly via the HTTP API.
//  3. ExecutePlan triggers the execution pipeline (workflow.trigger.task-execution-loop).
//  4. Task execution states appear in ENTITY_STATES or REACTIVE_STATE KV buckets.
//  5. When teams are configured, agent.task.red-team is dispatched (non-fatal check).
//  6. Mock LLM call counts match expected pipeline stages.
//
// Team-specific assertions (stages 4–5) are non-fatal: they emit AddWarning rather
// than returning an error when teams are not enabled in the running config. This
// allows the scenario to serve as a smoke test for the standard execution pipeline
// even when run against configs without teams enabled.
type TeamExecutionScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
	nats        *client.NATSClient
	mockLLM     *client.MockLLMClient
}

// NewTeamExecutionScenario creates a new team execution scenario.
func NewTeamExecutionScenario(cfg *config.Config) *TeamExecutionScenario {
	return &TeamExecutionScenario{
		name:        "team-execution",
		description: "Team pipeline E2E: plan → requirements → scenarios → execute → verify dispatch and KV state",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *TeamExecutionScenario) Name() string { return s.name }

// Description returns the scenario description.
func (s *TeamExecutionScenario) Description() string { return s.description }

// Setup prepares clients and waits for service health.
func (s *TeamExecutionScenario) Setup(ctx context.Context) error {
	s.fs = client.NewFilesystemClient(s.config.WorkspacePath)
	if err := s.fs.SetupWorkspace(); err != nil {
		return fmt.Errorf("setup workspace: %w", err)
	}

	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

	natsClient, err := client.NewNATSClient(ctx, s.config.NATSURL)
	if err != nil {
		return fmt.Errorf("create NATS client: %w", err)
	}
	s.nats = natsClient

	if s.config.MockLLMURL != "" {
		s.mockLLM = client.NewMockLLMClient(s.config.MockLLMURL)
	}

	return nil
}

// Execute runs all scenario stages in sequence.
func (s *TeamExecutionScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := s.buildStages()

	for _, stage := range stages {
		stageStart := time.Now()
		stageCtx, cancel := context.WithTimeout(ctx, stage.timeout)
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

// Teardown cleans up NATS connections.
func (s *TeamExecutionScenario) Teardown(ctx context.Context) error {
	if s.nats != nil {
		return s.nats.Close(ctx)
	}
	return nil
}

// buildStages returns the ordered list of stages for this scenario.
func (s *TeamExecutionScenario) buildStages() []stageDefinition {
	t := func(normalSec, fastSec int) time.Duration {
		if s.config.FastTimeouts {
			return time.Duration(fastSec) * time.Second
		}
		return time.Duration(normalSec) * time.Second
	}

	return []stageDefinition{
		{"setup-project", s.stageSetupProject, t(30, 15)},
		{"create-plan", s.stageCreatePlan, t(30, 15)},
		{"wait-for-plan", s.stageWaitForPlan, t(600, 30)},
		{"verify-plan", s.stageVerifyPlan, t(10, 5)},
		{"approve-plan", s.stageApprovePlan, t(600, 30)},
		{"create-requirements", s.stageCreateRequirements, t(30, 15)},
		{"create-scenarios", s.stageCreateScenarios, t(30, 15)},
		{"execute-plan", s.stageExecutePlan, t(30, 15)},
		{"verify-team-infrastructure", s.stageVerifyTeamInfrastructure, t(30, 15)},
		{"verify-execution-state", s.stageVerifyExecutionState, t(300, 60)},
		{"verify-red-team-dispatch", s.stageVerifyRedTeamDispatch, t(30, 15)},
		{"verify-mock-stats", s.stageVerifyMockStats, t(60, 30)},
	}
}

// ---------------------------------------------------------------------------
// Stage implementations
// ---------------------------------------------------------------------------

// stageSetupProject creates a minimal Python project and purges stale KV state.
func (s *TeamExecutionScenario) stageSetupProject(ctx context.Context, result *Result) error {
	// Purge stale reactive workflow state from any previous run of this scenario.
	// Without this, the notCompleted() condition in reactive rules can see a
	// prior run's completed state and silently skip processing.
	staleKeys := []string{
		"plan-review.",
		"phase-review.",
		"task-review.",
		"task-execution.",
	}
	for _, prefix := range staleKeys {
		if deleted, err := s.nats.PurgeKVByPrefix(ctx, "REACTIVE_STATE", prefix); err != nil {
			return fmt.Errorf("purge %s KV prefix: %w", prefix, err)
		} else if deleted > 0 {
			result.SetDetail(fmt.Sprintf("purged_%s_entries", strings.TrimSuffix(prefix, ".")), deleted)
		}
	}

	appPy := `from flask import Flask, jsonify

app = Flask(__name__)


@app.route("/hello")
def hello():
    return jsonify({"message": "Hello World"})


if __name__ == "__main__":
    app.run(port=5000)
`
	if err := s.fs.WriteFile(filepath.Join(s.config.WorkspacePath, "api", "app.py"), appPy); err != nil {
		return fmt.Errorf("write api/app.py: %w", err)
	}

	requirements := "flask\npyjwt\n"
	if err := s.fs.WriteFile(filepath.Join(s.config.WorkspacePath, "api", "requirements.txt"), requirements); err != nil {
		return fmt.Errorf("write api/requirements.txt: %w", err)
	}

	indexHTML := `<!DOCTYPE html>
<html>
<head><title>Auth Demo</title></head>
<body>
  <h1>Auth Demo</h1>
  <div id="status"></div>
  <script src="app.js"></script>
</body>
</html>
`
	if err := s.fs.WriteFile(filepath.Join(s.config.WorkspacePath, "ui", "index.html"), indexHTML); err != nil {
		return fmt.Errorf("write ui/index.html: %w", err)
	}

	appJS := `async function checkAuth() {
  const status = document.getElementById("status");
  status.textContent = "Not authenticated";
}

checkAuth();
`
	if err := s.fs.WriteFile(filepath.Join(s.config.WorkspacePath, "ui", "app.js"), appJS); err != nil {
		return fmt.Errorf("write ui/app.js: %w", err)
	}

	readme := `# Auth Demo

A minimal Python Flask API demonstrating JWT authentication.
`
	if err := s.fs.WriteFile(filepath.Join(s.config.WorkspacePath, "README.md"), readme); err != nil {
		return fmt.Errorf("write README.md: %w", err)
	}

	if err := s.fs.InitGit(); err != nil {
		return fmt.Errorf("init git: %w", err)
	}
	if err := s.fs.GitAdd("."); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if err := s.fs.GitCommit("Initial commit"); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	result.SetDetail("project_ready", true)
	return nil
}

// stageCreatePlan creates a plan via the REST API.
func (s *TeamExecutionScenario) stageCreatePlan(ctx context.Context, result *Result) error {
	resp, err := s.http.CreatePlan(ctx, "add JWT authentication to the API with a login endpoint and token validation middleware")
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("plan creation returned error: %s", resp.Error)
	}

	if resp.Slug == "" {
		return fmt.Errorf("plan creation returned empty slug")
	}

	result.SetDetail("plan_slug", resp.Slug)
	result.SetDetail("plan_request_id", resp.RequestID)
	result.SetDetail("plan_trace_id", resp.TraceID)
	return nil
}

// stageWaitForPlan polls until the plan has a non-empty Goal from the planner LLM.
func (s *TeamExecutionScenario) stageWaitForPlan(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	plan, err := s.http.WaitForPlanGoal(ctx, slug)
	if err != nil {
		return fmt.Errorf("plan never received goal from LLM: %w", err)
	}

	result.SetDetail("plan_data", plan)
	return nil
}

// stageVerifyPlan checks the plan has a goal that mentions authentication.
func (s *TeamExecutionScenario) stageVerifyPlan(_ context.Context, result *Result) error {
	planRaw, ok := result.GetDetail("plan_data")
	if !ok {
		return fmt.Errorf("plan_data not found in result details")
	}
	plan, ok := planRaw.(*client.Plan)
	if !ok {
		return fmt.Errorf("plan_data has unexpected type")
	}

	if plan.Goal == "" {
		return fmt.Errorf("plan has empty goal")
	}

	if !containsAnyCI(plan.Goal, "auth", "jwt", "login", "token") {
		return fmt.Errorf("plan goal does not mention authentication or JWT: %q", plan.Goal)
	}

	result.SetDetail("plan_goal", plan.Goal)
	result.SetDetail("plan_has_context", plan.Context != "")
	return nil
}

// stageApprovePlan polls until the plan-review-loop approves the plan.
func (s *TeamExecutionScenario) stageApprovePlan(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	reviewTimeout := time.Duration(maxReviewAttempts) * 4 * time.Minute
	backoff := reviewRetryBackoff
	if s.config.FastTimeouts {
		reviewTimeout = time.Duration(maxReviewAttempts) * config.FastReviewStepTimeout
		backoff = config.FastReviewBackoff
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, reviewTimeout)
	defer cancel()

	ticker := time.NewTicker(backoff)
	defer ticker.Stop()

	var lastStage string
	lastIterationSeen := 0
	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("plan approval timed out (last stage: %s, iteration: %d/%d)",
				lastStage, lastIterationSeen, maxReviewAttempts)
		case <-ticker.C:
			plan, err := s.http.GetPlan(timeoutCtx, slug)
			if err != nil {
				continue
			}

			lastStage = plan.Stage
			result.SetDetail("review_stage", plan.Stage)

			if plan.Approved {
				result.SetDetail("plan_approved", true)
				result.SetDetail("review_verdict", plan.ReviewVerdict)
				return nil
			}

			if plan.ReviewIteration > lastIterationSeen {
				lastIterationSeen = plan.ReviewIteration
				if plan.ReviewVerdict == "needs_changes" {
					result.AddWarning(fmt.Sprintf("plan review iteration %d/%d returned needs_changes",
						lastIterationSeen, maxReviewAttempts))
					if lastIterationSeen >= maxReviewAttempts {
						return fmt.Errorf("plan review exhausted %d revision attempts", maxReviewAttempts)
					}
				}
			}
		}
	}
}

// stageCreateRequirements creates two requirements for the plan via the HTTP API.
func (s *TeamExecutionScenario) stageCreateRequirements(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	req1, err := s.http.CreateRequirement(ctx, slug, &client.CreateRequirementRequest{
		Title:       "JWT login endpoint",
		Description: "POST /auth/login accepts username and password credentials and returns a signed JWT token on success, or 401 on invalid credentials.",
	})
	if err != nil {
		return fmt.Errorf("create requirement 1: %w", err)
	}

	req2, err := s.http.CreateRequirement(ctx, slug, &client.CreateRequirementRequest{
		Title:       "Protected endpoint middleware",
		Description: "A decorator validates the JWT Authorization header on protected routes, returning 401 when the token is missing or invalid.",
	})
	if err != nil {
		return fmt.Errorf("create requirement 2: %w", err)
	}

	result.SetDetail("requirement_1_id", req1.ID)
	result.SetDetail("requirement_2_id", req2.ID)
	result.SetDetail("requirements_created", 2)
	return nil
}

// stageCreateScenarios creates BDD scenarios linked to the first requirement.
func (s *TeamExecutionScenario) stageCreateScenarios(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")
	reqID, _ := result.GetDetailString("requirement_1_id")

	sc1, err := s.http.CreateScenario(ctx, slug, &client.CreateScenarioRequest{
		RequirementID: reqID,
		Given:         "valid username and password credentials are provided",
		When:          "POST /auth/login is called",
		Then:          []string{"a 200 response is returned", "the response body contains a JWT token"},
	})
	if err != nil {
		return fmt.Errorf("create scenario 1: %w", err)
	}

	sc2, err := s.http.CreateScenario(ctx, slug, &client.CreateScenarioRequest{
		RequirementID: reqID,
		Given:         "an invalid or expired JWT token is provided in the Authorization header",
		When:          "a protected endpoint is accessed",
		Then:          []string{"a 401 Unauthorized response is returned"},
	})
	if err != nil {
		return fmt.Errorf("create scenario 2: %w", err)
	}

	result.SetDetail("scenario_1_id", sc1.ID)
	result.SetDetail("scenario_2_id", sc2.ID)
	result.SetDetail("scenarios_created", 2)
	return nil
}

// stageExecutePlan calls POST /workflow-api/plans/{slug}/execute to trigger
// the reactive execution pipeline.
func (s *TeamExecutionScenario) stageExecutePlan(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	resp, err := s.http.ExecutePlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("execute plan: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("execute plan returned error: %s", resp.Error)
	}

	result.SetDetail("execute_batch_id", resp.BatchID)
	result.SetDetail("execute_message", resp.Message)
	return nil
}

// stageVerifyTeamInfrastructure checks ENTITY_STATES for team entities.
// This stage is non-fatal: if the running config does not have teams enabled,
// it records a warning and sets teams_available=false for downstream stages.
func (s *TeamExecutionScenario) stageVerifyTeamInfrastructure(ctx context.Context, result *Result) error {
	kvResp, err := s.http.GetKVEntries(ctx, "ENTITY_STATES")
	if err != nil {
		result.AddWarning(fmt.Sprintf("ENTITY_STATES bucket not queryable: %v", err))
		result.SetDetail("teams_available", false)
		return nil
	}

	teamCount := 0
	for _, entry := range kvResp.Entries {
		// Team entity keys use the pattern seeded by execution-orchestrator on startup
		if strings.HasPrefix(entry.Key, teamEntityPrefix) &&
			!strings.HasPrefix(entry.Key, teamInsightEntityPrefix) {
			teamCount++
		}
	}

	if teamCount == 0 {
		result.AddWarning("no team entities found in ENTITY_STATES — teams.enabled may be false in execution-orchestrator config")
		result.SetDetail("teams_available", false)
	} else {
		result.SetDetail("teams_available", true)
		result.SetDetail("team_entity_count", teamCount)
	}

	return nil
}

// stageVerifyExecutionState polls ENTITY_STATES until at least one task-execution
// entity appears for the plan. Falls back to REACTIVE_STATE if ENTITY_STATES has
// no entries (old reactive engine still active).
func (s *TeamExecutionScenario) stageVerifyExecutionState(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	ticker := time.NewTicker(config.FastPollInterval)
	defer ticker.Stop()

	// The execution-orchestrator writes entity IDs of the form:
	// local.semspec.workflow.task-execution.execution.{slug}-{taskID}
	entityPrefix := "local.semspec.workflow.task-execution.execution." + slug

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for task execution state (slug: %s): %w", slug, ctx.Err())
		case <-ticker.C:
			// Primary: check ENTITY_STATES for execution-orchestrator triples.
			found, phases := s.scanEntityStatesForExecution(ctx, entityPrefix)
			if found > 0 {
				result.SetDetail("execution_state_source", "ENTITY_STATES")
				result.SetDetail("execution_entity_count", found)
				result.SetDetail("execution_phase_distribution", phases)
				return nil
			}

			// Fallback: check REACTIVE_STATE for task-execution.{slug}.* entries
			// (used when old reactive engine is running instead of execution-orchestrator).
			fallbackFound, fallbackPhases := s.scanReactiveStateForExecution(ctx, slug)
			if fallbackFound > 0 {
				result.SetDetail("execution_state_source", "REACTIVE_STATE")
				result.SetDetail("execution_entity_count", fallbackFound)
				result.SetDetail("execution_phase_distribution", fallbackPhases)
				return nil
			}
		}
	}
}

// scanEntityStatesForExecution queries ENTITY_STATES and counts entries whose key
// matches the given entity prefix. Returns (count, phase distribution map).
func (s *TeamExecutionScenario) scanEntityStatesForExecution(ctx context.Context, entityPrefix string) (int, map[string]int) {
	kvResp, err := s.http.GetKVEntries(ctx, "ENTITY_STATES")
	if err != nil {
		return 0, nil
	}

	count := 0
	phases := make(map[string]int)

	for _, entry := range kvResp.Entries {
		if !strings.HasPrefix(entry.Key, entityPrefix) {
			continue
		}

		// Each entry value is a JSON object with a "triples" array.
		var state kvEntityState
		if err := json.Unmarshal(entry.Value, &state); err != nil {
			count++ // count the entry even if we can't parse it
			continue
		}

		count++
		for _, triple := range state.Triples {
			if triple.Predicate == "workflow.phase" {
				if phase, ok := triple.Object.(string); ok {
					phases[phase]++
				}
			}
		}
	}

	return count, phases
}

// scanReactiveStateForExecution queries REACTIVE_STATE and counts task-execution
// entries for the given plan slug. Returns (count, phase distribution map).
func (s *TeamExecutionScenario) scanReactiveStateForExecution(ctx context.Context, slug string) (int, map[string]int) {
	kvResp, err := s.http.GetKVEntries(ctx, client.ReactiveStateBucket)
	if err != nil {
		return 0, nil
	}

	count := 0
	phases := make(map[string]int)
	keyInfix := "task-execution." + slug

	for _, entry := range kvResp.Entries {
		if !strings.Contains(entry.Key, keyInfix) {
			continue
		}

		var state client.WorkflowState
		if err := json.Unmarshal(entry.Value, &state); err != nil {
			count++
			continue
		}

		count++
		if state.Phase != "" {
			phases[state.Phase]++
		}
	}

	return count, phases
}

// stageVerifyRedTeamDispatch checks the message-logger for agent.task.red-team
// messages. This stage is non-fatal: teams may not be enabled, so a missing
// red-team dispatch is recorded as a warning rather than a hard failure.
func (s *TeamExecutionScenario) stageVerifyRedTeamDispatch(ctx context.Context, result *Result) error {
	teamsAvailableRaw, _ := result.GetDetail("teams_available")
	teamsAvailable, _ := teamsAvailableRaw.(bool)

	entries, err := s.http.GetMessageLogEntries(ctx, 200, "agent.task.red-team")
	if err != nil {
		if !teamsAvailable {
			result.AddWarning(fmt.Sprintf("could not query message-logger for red-team dispatch: %v", err))
			return nil
		}
		return fmt.Errorf("query message-logger for red-team dispatch: %w", err)
	}

	if len(entries) == 0 {
		if teamsAvailable {
			result.AddWarning("teams are enabled but no agent.task.red-team messages observed — " +
				"red-team dispatch may require a completed validator stage first")
		} else {
			result.AddWarning("no agent.task.red-team messages observed (teams not enabled)")
		}
		result.SetDetail("red_team_dispatched", false)
		return nil
	}

	result.SetDetail("red_team_dispatched", true)
	result.SetDetail("red_team_message_count", len(entries))
	return nil
}

// stageVerifyMockStats queries the mock LLM /stats endpoint and checks that the
// planner and reviewer models were called. Skipped when mockLLM is not configured.
func (s *TeamExecutionScenario) stageVerifyMockStats(ctx context.Context, result *Result) error {
	if s.mockLLM == nil {
		return nil
	}

	// Wait for planner and reviewer to appear in stats — both are required for
	// plan creation + plan review.
	requiredModels := []string{"mock-planner", "mock-reviewer"}

	ticker := time.NewTicker(config.FastPollInterval)
	defer ticker.Stop()

	var stats *client.MockStats
	for {
		select {
		case <-ctx.Done():
			if stats != nil {
				result.SetDetail("mock_stats_total_calls", stats.TotalCalls)
				result.SetDetail("mock_stats_by_model", stats.CallsByModel)
			}
			return fmt.Errorf("mock stats: timed out waiting for all required models %v: %w",
				requiredModels, ctx.Err())
		case <-ticker.C:
			var err error
			stats, err = s.mockLLM.GetStats(ctx)
			if err != nil {
				continue
			}
			if hasAllModels(stats.CallsByModel, requiredModels) {
				goto statsReady
			}
		}
	}

statsReady:
	result.SetDetail("mock_stats_total_calls", stats.TotalCalls)
	result.SetDetail("mock_stats_by_model", stats.CallsByModel)

	// Assert planner was called at least once (for plan generation).
	plannerCalls := stats.CallsByModel["mock-planner"]
	if plannerCalls < 1 {
		return fmt.Errorf("mock-planner: got %d calls, want >= 1", plannerCalls)
	}
	result.SetDetail("mock_planner_calls", plannerCalls)

	// Assert reviewer was called at least once (for plan review).
	reviewerCalls := stats.CallsByModel["mock-reviewer"]
	if reviewerCalls < 1 {
		return fmt.Errorf("mock-reviewer: got %d calls, want >= 1", reviewerCalls)
	}
	result.SetDetail("mock_reviewer_calls", reviewerCalls)

	// Record coder calls if the execution pipeline ran far enough.
	if coderCalls, ok := stats.CallsByModel["mock-coder"]; ok && coderCalls > 0 {
		result.SetDetail("mock_coder_calls", coderCalls)
	}

	return nil
}
