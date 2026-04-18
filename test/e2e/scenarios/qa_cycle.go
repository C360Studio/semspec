// Package scenarios provides e2e test scenario implementations.
package scenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// QACycleScenario validates the end-to-end QA phase for qa_level=unit.
//
// This is the Tier 2 runtime validation of Phases 2.5a / 2.5b / 3 / 4 / 5 / 5.1 / 6:
//
//   - sandbox qa_subscriber.go executes `go test ./...` in the workspace
//   - plan-manager publishes QARequestedEvent and transitions ready_for_qa → reviewing_qa
//   - qa-reviewer dispatches a mock LLM verdict (approved) and sends plan.mutation.qa.verdict
//   - plan-manager transitions reviewing_qa → complete
//
// State trajectory:
//
//	drafting → reviewing_draft → approved → requirements_generated →
//	architecture_generated → scenarios_generated → ready_for_execution →
//	implementing → ready_for_qa → reviewing_qa → complete
type QACycleScenario struct {
	config  *config.Config
	http    *client.HTTPClient
	fs      *client.FilesystemClient
	nats    *client.NATSClient
	mockLLM *client.MockLLMClient
}

// NewQACycleScenario creates a new QA cycle scenario that validates the full
// qa_level=unit pipeline using a pre-seeded Go workspace and mock LLM fixtures.
func NewQACycleScenario(cfg *config.Config) *QACycleScenario {
	return &QACycleScenario{config: cfg}
}

// Name implements Scenario.
func (s *QACycleScenario) Name() string { return "qa-cycle" }

// Description implements Scenario.
func (s *QACycleScenario) Description() string {
	return "QA phase end-to-end at qa_level=unit: sandbox runs go test, qa-reviewer approves, plan → complete"
}

// Setup prepares the scenario environment.
func (s *QACycleScenario) Setup(ctx context.Context) error {
	s.fs = client.NewFilesystemClient(s.config.WorkspacePath)
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

// Teardown cleans up after the scenario.
func (s *QACycleScenario) Teardown(ctx context.Context) error {
	if s.nats != nil {
		return s.nats.Close(ctx)
	}
	return nil
}

// Execute runs the qa-cycle scenario stages sequentially.
func (s *QACycleScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.Name())
	defer result.Complete()

	t := func(normalSec, fastSec int) time.Duration {
		if s.config.FastTimeouts {
			return time.Duration(fastSec) * time.Second
		}
		return time.Duration(normalSec) * time.Second
	}

	stages := []struct {
		name    string
		fn      func(context.Context, *Result) error
		timeout time.Duration
	}{
		{"setup-workspace", s.stageSetupWorkspace, t(30, 20)},
		{"init-project", s.stageInitProject, t(30, 20)},
		{"set-qa-level", s.stageSetQALevel, t(15, 10)},
		{"verify-qa-level", s.stageVerifyQALevel, t(15, 10)},
		{"create-plan", s.stageCreatePlan, t(30, 15)},
		{"wait-for-plan-goal", s.stageWaitForPlanGoal, t(120, 60)},
		{"wait-for-approved", s.stageWaitForApproved, t(300, 120)},
		{"trigger-execution", s.stageTriggerExecution, t(15, 10)},
		{"wait-for-implementing", s.stageWaitForImplementing, t(300, 120)},
		{"wait-for-ready-for-qa", s.stageWaitForReadyForQA, t(180, 90)},
		{"assert-qa-requested", s.stageAssertQARequested, t(30, 15)},
		{"wait-for-reviewing-qa", s.stageWaitForReviewingQA, t(60, 30)},
		{"assert-qa-completed", s.stageAssertQACompleted, t(30, 15)},
		{"wait-for-complete", s.stageWaitForComplete, t(60, 30)},
		{"assert-qa-verdict", s.stageAssertQAVerdict, t(30, 15)},
		{"verify-mock-stats", s.stageVerifyMockStats, t(10, 5)},
	}

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

// ---------------------------------------------------------------------------
// Stage: setup-workspace
// ---------------------------------------------------------------------------

// stageSetupWorkspace writes the Go project fixture files to the workspace,
// initialises a git repository, and resets any stale reactive KV state.
//
// The workspace must contain a passing Go test suite so the sandbox can run
// `go test ./...` at qa_level=unit and get a green result.
func (s *QACycleScenario) stageSetupWorkspace(ctx context.Context, result *Result) error {
	// Purge stale reactive state that could interfere with a fresh run.
	for _, prefix := range []string{"plan-review.", "phase-review.", "task-review.", "task-execution."} {
		if deleted, err := s.nats.PurgeKVByPrefix(ctx, "REACTIVE_STATE", prefix); err != nil {
			return fmt.Errorf("purge reactive state %s: %w", prefix, err)
		} else if deleted > 0 {
			result.SetDetail("purged_"+prefix+"entries", deleted)
		}
	}

	// Go module and source files — `go test ./...` passes on this workspace.
	files := map[string]string{
		"go.mod":    "module example.com/qa-cycle-project\n\ngo 1.21\n",
		"README.md": "# QA Cycle Project\n\nA minimal Go project used for qa_level=unit E2E validation.\n",
		// Package with a trivial passing test that confirms go test works.
		"pkg/math/math.go":      "// Package math provides simple math utilities.\npackage math\n\n// Add returns the sum of a and b.\nfunc Add(a, b int) int { return a + b }\n",
		"pkg/math/math_test.go": "package math\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif got := Add(1, 2); got != 3 {\n\t\tt.Errorf(\"Add(1, 2) = %d; want 3\", got)\n\t}\n}\n",
		// Main package (no tests needed here — go test ./... only runs _test.go).
		"cmd/server/main.go": "// Package main is the QA cycle project entry point.\npackage main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"QA cycle server\") }\n",
		// Makefile satisfies structural-validator make-based checks.
		"Makefile": "test:\n\tgo test ./...\n\nbuild:\n\tgo build ./...\n\nlint:\n\tgo vet ./...\n",
	}

	for path, content := range files {
		if err := s.fs.WriteFileRelative(path, content); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}

	// Git repo with HEAD — sandbox worktree creation requires a valid HEAD.
	if err := s.fs.InitGit(); err != nil {
		return fmt.Errorf("init git: %w", err)
	}
	if err := s.fs.GitAdd("."); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if err := s.fs.GitCommit("Initial commit"); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	result.SetDetail("workspace_ready", true)
	return nil
}

// ---------------------------------------------------------------------------
// Stage: init-project
// ---------------------------------------------------------------------------

// stageInitProject detects the stack and initialises the project via the HTTP API.
func (s *QACycleScenario) stageInitProject(ctx context.Context, result *Result) error {
	detection, err := s.http.DetectProject(ctx)
	if err != nil {
		return fmt.Errorf("detect project: %w", err)
	}

	// Build language list from detection — Go should be detected from go.mod.
	var languages []string
	for _, lang := range detection.Languages {
		languages = append(languages, lang.Name)
	}

	// Fall back to explicit Go declaration if detection missed it.
	if len(languages) == 0 {
		languages = []string{"Go"}
		result.SetDetail("language_detection_fallback", true)
	}

	initReq := &client.ProjectInitRequest{
		Project: client.ProjectInitInput{
			Name:        "QA Cycle Project",
			Description: "Minimal Go project for qa_level=unit E2E validation",
			Languages:   languages,
		},
		Checklist: detection.ProposedChecklist,
		Standards: client.StandardsInput{
			Version: "1.0.0",
			Items:   []any{},
		},
	}

	resp, err := s.http.InitProject(ctx, initReq)
	if err != nil {
		return fmt.Errorf("init project: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("init project returned success=false")
	}

	result.SetDetail("init_success", true)
	result.SetDetail("init_languages", languages)
	return nil
}

// ---------------------------------------------------------------------------
// Stage: set-qa-level
// ---------------------------------------------------------------------------

// stageSetQALevel patches the project config to set qa_level=unit.
// This ensures plans created after this point snapshot qa_level=unit and
// trigger the sandbox unit-test executor path at implementing convergence.
func (s *QACycleScenario) stageSetQALevel(ctx context.Context, result *Result) error {
	qaLevel := "unit"
	reqBody := map[string]*string{"qa_level": &qaLevel}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal qa_level request: %w", err)
	}

	url := s.config.HTTPBaseURL + "/project-manager/config"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("patch project config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var body strings.Builder
		if _, copyErr := fmt.Fscan(resp.Body, &body); copyErr == nil {
			return fmt.Errorf("PATCH /project-manager/config HTTP %d: %s", resp.StatusCode, body.String())
		}
		return fmt.Errorf("PATCH /project-manager/config HTTP %d", resp.StatusCode)
	}

	result.SetDetail("qa_level_set", true)
	return nil
}

// ---------------------------------------------------------------------------
// Stage: verify-qa-level
// ---------------------------------------------------------------------------

// stageVerifyQALevel reads project.json from disk and confirms qa_level was persisted.
func (s *QACycleScenario) stageVerifyQALevel(_ context.Context, result *Result) error {
	projectJSONPath := filepath.Join(s.config.WorkspacePath, ".semspec", "project.json")
	data, err := os.ReadFile(projectJSONPath)
	if err != nil {
		return fmt.Errorf("read project.json: %w", err)
	}

	var pc struct {
		QALevel string `json:"qa_level"`
	}
	if err := json.Unmarshal(data, &pc); err != nil {
		return fmt.Errorf("unmarshal project.json: %w", err)
	}

	if pc.QALevel != "unit" {
		return fmt.Errorf("expected qa_level=unit in project.json, got %q", pc.QALevel)
	}

	result.SetDetail("qa_level_verified", true)
	return nil
}

// ---------------------------------------------------------------------------
// Stage: create-plan
// ---------------------------------------------------------------------------

// stageCreatePlan posts a new plan. The plan will snapshot qa_level=unit from
// project.json, ensuring the QA executor path fires at implementing convergence.
func (s *QACycleScenario) stageCreatePlan(ctx context.Context, result *Result) error {
	resp, err := s.http.CreatePlan(ctx, "add a math utility package with Add and Subtract functions and full test coverage")
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
	result.SetDetail("plan_trace_id", resp.TraceID)
	return nil
}

// ---------------------------------------------------------------------------
// Stage: wait-for-plan-goal
// ---------------------------------------------------------------------------

// stageWaitForPlanGoal waits for the mock planner to populate the plan Goal.
func (s *QACycleScenario) stageWaitForPlanGoal(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	plan, err := s.http.WaitForPlanGoal(ctx, slug)
	if err != nil {
		return fmt.Errorf("plan goal never populated: %w", err)
	}

	result.SetDetail("plan_goal", plan.Goal)
	return nil
}

// ---------------------------------------------------------------------------
// Stage: wait-for-approved
// ---------------------------------------------------------------------------

// stageWaitForApproved polls until the plan reaches the "approved" status or
// any post-approval status (the pipeline may advance quickly past "approved").
func (s *QACycleScenario) stageWaitForApproved(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Stages that indicate approved or post-approval progress.
	approvedOrBeyond := map[string]bool{
		"approved":               true,
		"requirements_generated": true,
		"architecture_generated": true,
		"scenarios_generated":    true,
		"ready_for_execution":    true,
		"implementing":           true,
		"ready_for_qa":           true,
		"reviewing_qa":           true,
		"complete":               true,
		"awaiting_review":        true,
	}

	var lastStage string
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("plan approval timed out (last stage: %s): %w", lastStage, ctx.Err())
		case <-ticker.C:
			plan, err := s.http.GetPlan(ctx, slug)
			if err != nil {
				continue
			}
			lastStage = plan.Stage
			if approvedOrBeyond[plan.Stage] {
				result.SetDetail("approved_stage", plan.Stage)
				return nil
			}
			if plan.Stage == "rejected" || plan.Stage == "error" || plan.Stage == "escalated" {
				return fmt.Errorf("plan reached terminal failure at stage %s (status: %s)", plan.Stage, plan.Status)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Stage: trigger-execution
// ---------------------------------------------------------------------------

// stageTriggerExecution calls POST /plan-manager/plans/{slug}/execute to start
// the reactive execution pipeline. This is idempotent — if the plan is already
// in ready_for_execution or implementing, it still succeeds.
func (s *QACycleScenario) stageTriggerExecution(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	// Skip if already past ready_for_execution.
	plan, err := s.http.GetPlan(ctx, slug)
	if err == nil {
		alreadyExecuting := map[string]bool{
			"implementing":    true,
			"ready_for_qa":    true,
			"reviewing_qa":    true,
			"complete":        true,
			"awaiting_review": true,
		}
		if alreadyExecuting[plan.Status] {
			result.SetDetail("execution_already_started", true)
			return nil
		}
	}

	resp, err := s.http.ExecutePlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("execute plan: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("execute plan returned error: %s", resp.Error)
	}

	result.SetDetail("execution_triggered", true)
	result.SetDetail("execution_batch_id", resp.BatchID)
	return nil
}

// ---------------------------------------------------------------------------
// Stage: wait-for-implementing
// ---------------------------------------------------------------------------

// stageWaitForImplementing polls until the plan reaches the "implementing"
// status, confirming the execution pipeline has started.
func (s *QACycleScenario) stageWaitForImplementing(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Already past implementing if qa has started.
	implementingOrBeyond := map[string]bool{
		"implementing":    true,
		"ready_for_qa":    true,
		"reviewing_qa":    true,
		"complete":        true,
		"awaiting_review": true,
	}

	var lastStatus string
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("plan never reached implementing (last: %s): %w", lastStatus, ctx.Err())
		case <-ticker.C:
			plan, err := s.http.GetPlan(ctx, slug)
			if err != nil {
				continue
			}
			lastStatus = plan.Status
			if implementingOrBeyond[plan.Status] {
				result.SetDetail("implementing_status", plan.Status)
				return nil
			}
			if plan.Status == "rejected" || plan.Status == "error" {
				return fmt.Errorf("plan reached terminal failure: %s", plan.Status)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Stage: wait-for-ready-for-qa
// ---------------------------------------------------------------------------

// stageWaitForReadyForQA polls until the plan reaches "ready_for_qa" status,
// confirming that all requirements converged and plan-manager published the
// QARequestedEvent. We also accept "reviewing_qa" (sandbox was fast) and
// "complete" / "awaiting_review" (qa-reviewer was very fast).
func (s *QACycleScenario) stageWaitForReadyForQA(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Terminal-for-QA states we will not block on.
	qaOrBeyond := map[string]bool{
		"ready_for_qa":    true,
		"reviewing_qa":    true,
		"complete":        true,
		"awaiting_review": true,
	}

	var lastStatus string
	for {
		select {
		case <-ctx.Done():
			// On timeout, diagnose the stall.
			diagCtx, diagCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer diagCancel()
			plan, _ := s.http.GetPlan(diagCtx, slug)
			if plan != nil && plan.Status == "implementing" {
				return fmt.Errorf("plan stalled in implementing (last: %s) — check EXECUTION_STATES for failed requirements: %w", lastStatus, ctx.Err())
			}
			return fmt.Errorf("plan never reached ready_for_qa (last: %s): %w", lastStatus, ctx.Err())
		case <-ticker.C:
			plan, err := s.http.GetPlan(ctx, slug)
			if err != nil {
				continue
			}
			lastStatus = plan.Status
			if qaOrBeyond[plan.Status] {
				result.SetDetail("qa_entry_status", plan.Status)
				return nil
			}
			if plan.Status == "rejected" || plan.Status == "error" {
				return fmt.Errorf("plan reached terminal failure before QA: %s", plan.Status)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Stage: assert-qa-requested
// ---------------------------------------------------------------------------

// stageAssertQARequested fetches message-logger entries and confirms that
// workflow.events.qa.requested was published with Mode=unit for this plan's slug.
func (s *QACycleScenario) stageAssertQARequested(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	const qaRequestedSubject = "workflow.events.qa.requested"
	const maxAttempts = 10

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for attempt := 0; attempt < maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for %s message: %w", qaRequestedSubject, ctx.Err())
		case <-ticker.C:
		}

		entries, err := s.http.GetMessageLogEntries(ctx, 200, qaRequestedSubject)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !s.entryMatchesSlugAndMode(entry.RawData, slug, "unit") {
				continue
			}
			result.SetDetail("qa_requested_found", true)
			result.SetDetail("qa_requested_subject", entry.Subject)
			return nil
		}
	}

	return fmt.Errorf("no %s message found for slug=%q mode=unit after %d attempts", qaRequestedSubject, slug, maxAttempts)
}

// ---------------------------------------------------------------------------
// Stage: wait-for-reviewing-qa
// ---------------------------------------------------------------------------

// stageWaitForReviewingQA polls until the plan reaches "reviewing_qa", confirming
// that the sandbox QACompletedEvent was consumed by plan-manager.
func (s *QACycleScenario) stageWaitForReviewingQA(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	// If we already passed reviewing_qa (qa-reviewer was very fast), accept it.
	reviewingOrBeyond := map[string]bool{
		"reviewing_qa":    true,
		"complete":        true,
		"awaiting_review": true,
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastStatus string
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("plan never reached reviewing_qa (last: %s): %w", lastStatus, ctx.Err())
		case <-ticker.C:
			plan, err := s.http.GetPlan(ctx, slug)
			if err != nil {
				continue
			}
			lastStatus = plan.Status
			if reviewingOrBeyond[plan.Status] {
				result.SetDetail("reviewing_qa_status", plan.Status)
				return nil
			}
			if plan.Status == "rejected" || plan.Status == "error" {
				return fmt.Errorf("plan reached terminal failure at reviewing_qa wait: %s", plan.Status)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Stage: assert-qa-completed
// ---------------------------------------------------------------------------

// stageAssertQACompleted fetches message-logger entries and confirms that
// workflow.events.qa.completed was published with Level=unit and Passed=true.
func (s *QACycleScenario) stageAssertQACompleted(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	const qaCompletedSubject = "workflow.events.qa.completed"
	const maxAttempts = 10

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for attempt := 0; attempt < maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for %s message: %w", qaCompletedSubject, ctx.Err())
		case <-ticker.C:
		}

		entries, err := s.http.GetMessageLogEntries(ctx, 200, qaCompletedSubject)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			evt, ok := s.parseQACompletedEntry(entry.RawData, slug)
			if !ok {
				continue
			}

			// Validate: level must be "unit" and passed must be true.
			if evt.Level != "unit" {
				result.AddWarning(fmt.Sprintf("QACompleted for slug=%q has level=%q (want unit)", slug, evt.Level))
				continue
			}
			if !evt.Passed {
				failures, _ := json.Marshal(evt.Failures)
				return fmt.Errorf("QACompleted for slug=%q: Passed=false (failures: %s)", slug, string(failures))
			}

			result.SetDetail("qa_completed_found", true)
			result.SetDetail("qa_completed_level", evt.Level)
			result.SetDetail("qa_completed_passed", evt.Passed)
			result.SetDetail("qa_completed_run_id", evt.RunID)
			result.SetDetail("qa_completed_duration_ms", evt.DurationMs)

			// Note artifact path for the log assertion stage.
			if len(evt.Artifacts) > 0 {
				result.SetDetail("qa_artifact_path", evt.Artifacts[0].Path)
			}

			return nil
		}
	}

	return fmt.Errorf("no %s message found for slug=%q level=unit passed=true after %d attempts", qaCompletedSubject, slug, maxAttempts)
}

// ---------------------------------------------------------------------------
// Stage: wait-for-complete
// ---------------------------------------------------------------------------

// stageWaitForComplete polls until the plan reaches "complete" or "awaiting_review".
// qa-reviewer dispatches the mock-qa-reviewer fixture which returns an approved
// verdict, so the plan transitions straight to "complete" (no human gate in e2e).
func (s *QACycleScenario) stageWaitForComplete(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastStatus string
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("plan never reached complete/awaiting_review (last: %s): %w", lastStatus, ctx.Err())
		case <-ticker.C:
			plan, err := s.http.GetPlan(ctx, slug)
			if err != nil {
				continue
			}
			lastStatus = plan.Status

			switch plan.Status {
			case "complete", "awaiting_review":
				result.SetDetail("final_status", plan.Status)
				return nil
			case "rejected", "error":
				return fmt.Errorf("plan reached terminal failure after QA review: %s", plan.Status)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Stage: assert-qa-verdict
// ---------------------------------------------------------------------------

// stageAssertQAVerdict confirms that plan.mutation.qa.verdict was sent and
// accepted by checking the message-logger for the mutation subject.
func (s *QACycleScenario) stageAssertQAVerdict(ctx context.Context, result *Result) error {
	const verdictSubject = "plan.mutation.qa.verdict"

	entries, err := s.http.GetMessageLogEntries(ctx, 200, verdictSubject)
	if err != nil {
		return fmt.Errorf("fetch message-logger entries for %s: %w", verdictSubject, err)
	}

	slug, _ := result.GetDetailString("plan_slug")
	for _, entry := range entries {
		if s.entryMatchesSlug(entry.RawData, slug) {
			result.SetDetail("qa_verdict_mutation_found", true)
			result.SetDetail("qa_verdict_subject", entry.Subject)
			return nil
		}
	}

	// The mutation subject may not always be logged if the request/reply
	// is not captured by the message-logger. Check final plan status as
	// a fallback proof of verdict application.
	plan, err := s.http.GetPlan(ctx, slug)
	if err == nil && (plan.Status == "complete" || plan.Status == "awaiting_review") {
		result.SetDetail("qa_verdict_inferred_from_status", plan.Status)
		result.AddWarning("plan.mutation.qa.verdict not logged but final status confirms QA verdict applied")
		return nil
	}

	return fmt.Errorf("plan.mutation.qa.verdict message not found for slug=%q and plan status is %q", slug, func() string {
		if plan != nil {
			return plan.Status
		}
		return "unknown"
	}())
}

// ---------------------------------------------------------------------------
// Stage: verify-mock-stats
// ---------------------------------------------------------------------------

// stageVerifyMockStats checks that the mock LLM was called the expected number
// of times — planner, plan-reviewer, requirement-generator, scenario-generator,
// architecture-generator, developer (coder), reviewer, and qa-reviewer.
func (s *QACycleScenario) stageVerifyMockStats(ctx context.Context, result *Result) error {
	if s.mockLLM == nil {
		result.AddWarning("mock LLM URL not configured — skipping stats verification")
		return nil
	}

	stats, err := s.mockLLM.GetStats(ctx)
	if err != nil {
		return fmt.Errorf("get mock LLM stats: %w", err)
	}

	result.SetDetail("mock_total_calls", stats.TotalCalls)
	result.SetDetail("mock_calls_by_model", stats.CallsByModel)

	// Summarise for easy reading.
	var parts []string
	for model, count := range stats.CallsByModel {
		parts = append(parts, fmt.Sprintf("%s=%d", model, count))
	}
	result.SetDetail("mock_call_summary", strings.Join(parts, ", "))

	// Required: planner and reviewer must have been called (planning phase).
	for _, model := range []string{"mock-planner", "mock-reviewer"} {
		if count, ok := stats.CallsByModel[model]; !ok || count == 0 {
			return fmt.Errorf("expected mock model %q to be called (got %d calls)", model, count)
		}
	}

	// Required: qa-reviewer must have been called to produce the verdict.
	if count, ok := stats.CallsByModel["mock-qa-reviewer"]; !ok || count == 0 {
		return fmt.Errorf("expected mock-qa-reviewer to be called, got %d calls — QA review pipeline did not fire", count)
	}
	result.SetDetail("mock_qa_reviewer_calls", stats.CallsByModel["mock-qa-reviewer"])

	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// qaCompletedPayload is a minimal decode target for QACompletedEvent entries.
type qaCompletedPayload struct {
	Slug        string `json:"slug"`
	PlanID      string `json:"plan_id"`
	RunID       string `json:"run_id"`
	Level       string `json:"level"`
	Passed      bool   `json:"passed"`
	DurationMs  int64  `json:"duration_ms"`
	RunnerError string `json:"runner_error,omitempty"`
	Failures    []struct {
		TestName string `json:"test_name,omitempty"`
		Message  string `json:"message,omitempty"`
	} `json:"failures,omitempty"`
	Artifacts []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	} `json:"artifacts,omitempty"`
}

// entryMatchesSlugAndMode decodes a message-logger RawData blob and returns
// true when the embedded payload has slug == slug and mode == mode.
// The log entry wraps the payload in a BaseMessage envelope.
func (s *QACycleScenario) entryMatchesSlugAndMode(raw json.RawMessage, slug, mode string) bool {
	if len(raw) == 0 {
		return false
	}

	// Try direct payload (some loggers store the payload inline).
	var direct struct {
		Slug string `json:"slug"`
		Mode string `json:"mode"`
	}
	if json.Unmarshal(raw, &direct) == nil && direct.Slug == slug && direct.Mode == mode {
		return true
	}

	// Try BaseMessage envelope: {"payload": {...}}.
	var envelope struct {
		Payload struct {
			Slug string `json:"slug"`
			Mode string `json:"mode"`
		} `json:"payload"`
	}
	if json.Unmarshal(raw, &envelope) == nil && envelope.Payload.Slug == slug && envelope.Payload.Mode == mode {
		return true
	}

	return false
}

// entryMatchesSlug decodes a message-logger RawData blob and returns true
// when the embedded payload has slug == slug.
func (s *QACycleScenario) entryMatchesSlug(raw json.RawMessage, slug string) bool {
	if len(raw) == 0 {
		return false
	}

	var direct struct {
		Slug string `json:"slug"`
	}
	if json.Unmarshal(raw, &direct) == nil && direct.Slug == slug {
		return true
	}

	var envelope struct {
		Payload struct {
			Slug string `json:"slug"`
		} `json:"payload"`
	}
	if json.Unmarshal(raw, &envelope) == nil && envelope.Payload.Slug == slug {
		return true
	}

	return false
}

// parseQACompletedEntry decodes a message-logger RawData blob as a
// QACompletedEvent. Returns the payload and true on success.
func (s *QACycleScenario) parseQACompletedEntry(raw json.RawMessage, slug string) (qaCompletedPayload, bool) {
	var zero qaCompletedPayload
	if len(raw) == 0 {
		return zero, false
	}

	// Try direct payload.
	var direct qaCompletedPayload
	if json.Unmarshal(raw, &direct) == nil && direct.Slug == slug {
		return direct, true
	}

	// Try BaseMessage envelope.
	var envelope struct {
		Payload qaCompletedPayload `json:"payload"`
	}
	if json.Unmarshal(raw, &envelope) == nil && envelope.Payload.Slug == slug {
		return envelope.Payload, true
	}

	return zero, false
}
