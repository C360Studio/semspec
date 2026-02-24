package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/c360studio/semspec/source"
	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
	sourceVocab "github.com/c360studio/semspec/vocabulary/source"
)

// HelloWorldVariant configures expected behavior for scenario variants.
// The zero value represents the happy path (no rejections expected).
type HelloWorldVariant struct {
	ExpectPlanRevisions int // 0 = plan approved first try
	ExpectTaskRevisions int // 0 = tasks approved first try
}

// HelloWorldOption configures a HelloWorldScenario variant.
type HelloWorldOption func(*HelloWorldScenario)

// WithPlanRejections creates a variant that expects N plan review rejections
// before approval. The mock fixture set must provide numbered reviewer fixtures.
func WithPlanRejections(n int) HelloWorldOption {
	return func(s *HelloWorldScenario) {
		s.variant.ExpectPlanRevisions = n
	}
}

// WithTaskRejections creates a variant that expects N task review rejections
// before approval. The mock fixture set must provide numbered task-reviewer fixtures.
func WithTaskRejections(n int) HelloWorldOption {
	return func(s *HelloWorldScenario) {
		s.variant.ExpectTaskRevisions = n
	}
}

// HelloWorldScenario tests the greenfield experience:
// setup Python+JS hello-world → ingest SOP → create plan for /goodbye endpoint →
// verify plan semantics → approve → generate tasks → verify task semantics →
// capture trajectory data for provider comparison.
type HelloWorldScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
	nats        *client.NATSClient
	mockLLM     *client.MockLLMClient
	variant     HelloWorldVariant
}

// NewHelloWorldScenario creates a greenfield hello-world scenario.
// Options modify the variant configuration for rejection/retry testing.
func NewHelloWorldScenario(cfg *config.Config, opts ...HelloWorldOption) *HelloWorldScenario {
	s := &HelloWorldScenario{
		name:        "hello-world",
		description: "Greenfield Python+JS: add /goodbye endpoint with semantic validation",
		config:      cfg,
	}

	for _, opt := range opts {
		opt(s)
	}

	// Derive name from variant configuration
	if s.variant.ExpectPlanRevisions > 0 && s.variant.ExpectTaskRevisions > 0 {
		s.name = "hello-world-double-rejection"
		s.description += " (plan + task rejection)"
	} else if s.variant.ExpectPlanRevisions > 0 {
		s.name = "hello-world-plan-rejection"
		s.description += " (plan rejection)"
	} else if s.variant.ExpectTaskRevisions > 0 {
		s.name = "hello-world-task-rejection"
		s.description += " (task rejection)"
	}

	return s
}

// timeout returns fast if FastTimeouts is enabled, otherwise normal.
// Both values are in seconds and converted to time.Duration.
func (s *HelloWorldScenario) timeout(normalSec, fastSec int) time.Duration {
	if s.config.FastTimeouts {
		return time.Duration(fastSec) * time.Second
	}
	return time.Duration(normalSec) * time.Second
}

// Name returns the scenario name.
func (s *HelloWorldScenario) Name() string { return s.name }

// Description returns the scenario description.
func (s *HelloWorldScenario) Description() string { return s.description }

// Setup prepares the scenario environment.
func (s *HelloWorldScenario) Setup(ctx context.Context) error {
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

	// Initialize mock LLM client for stats verification (only when mock URL is configured)
	if s.config.MockLLMURL != "" {
		s.mockLLM = client.NewMockLLMClient(s.config.MockLLMURL)
	}

	return nil
}

// Execute runs the hello-world scenario.
func (s *HelloWorldScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	t := s.timeout // shorthand

	stages := []struct {
		name    string
		fn      func(context.Context, *Result) error
		timeout time.Duration
	}{
		{"setup-project", s.stageSetupProject, t(30, 15)},
		{"check-not-initialized", s.stageCheckNotInitialized, t(10, 5)},
		{"detect-stack", s.stageDetectStack, t(30, 15)},
		{"init-project", s.stageInitProject, t(30, 15)},
		{"verify-initialized", s.stageVerifyInitialized, t(10, 5)},
		{"ingest-sop", s.stageIngestSOP, t(30, 15)},
		{"verify-sop-ingested", s.stageVerifySOPIngested, t(60, 15)},
		{"create-plan", s.stageCreatePlan, t(30, 15)},
		{"wait-for-plan", s.stageWaitForPlan, t(600, 30)},
		{"verify-plan-semantics", s.stageVerifyPlanSemantics, t(10, 5)},
		{"approve-plan", s.stageApprovePlan, t(600, 30)},
		{"generate-tasks", s.stageGenerateTasks, t(30, 15)},
		{"wait-for-tasks", s.stageWaitForTasks, t(600, 30)},
		{"verify-tasks-semantics", s.stageVerifyTasksSemantics, t(10, 5)},
		{"verify-tasks-pending-approval", s.stageVerifyTasksPendingApproval, t(10, 5)},
		{"approve-tasks-individually", s.stageApproveTasksIndividually, t(30, 15)},
		{"verify-tasks-approved", s.stageVerifyTasksApproved, t(10, 5)},
		{"trigger-validation", s.stageTriggerValidation, t(30, 15)},
		{"wait-for-validation", s.stageWaitForValidation, t(300, 30)},
		{"verify-validation-results", s.stageVerifyValidationResults, t(10, 5)},
		{"capture-trajectory", s.stageCaptureTrajectory, t(30, 15)},
		{"verify-mock-stats", s.stageVerifyMockStats, t(10, 5)},
		{"verify-revision-prompts", s.stageVerifyRevisionPrompts, t(10, 5)},
		{"capture-context", s.stageCaptureContext, t(15, 10)},
		{"capture-artifacts", s.stageCaptureArtifacts, t(10, 5)},
		{"generate-report", s.stageGenerateReport, t(10, 5)},
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

// Teardown cleans up after the scenario.
func (s *HelloWorldScenario) Teardown(ctx context.Context) error {
	if s.nats != nil {
		return s.nats.Close(ctx)
	}
	return nil
}

// stageSetupProject creates a minimal Python+JS hello-world project in the workspace.
func (s *HelloWorldScenario) stageSetupProject(_ context.Context, result *Result) error {
	// Python API
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

	requirements := "flask\n"
	if err := s.fs.WriteFile(filepath.Join(s.config.WorkspacePath, "api", "requirements.txt"), requirements); err != nil {
		return fmt.Errorf("write api/requirements.txt: %w", err)
	}

	// JavaScript UI
	indexHTML := `<!DOCTYPE html>
<html>
<head><title>Hello World App</title></head>
<body>
  <h1>Hello World App</h1>
  <div id="greeting"></div>
  <script src="app.js"></script>
</body>
</html>
`
	if err := s.fs.WriteFile(filepath.Join(s.config.WorkspacePath, "ui", "index.html"), indexHTML); err != nil {
		return fmt.Errorf("write ui/index.html: %w", err)
	}

	appJS := `async function loadGreeting() {
  const response = await fetch("/hello");
  const data = await response.json();
  document.getElementById("greeting").textContent = data.message;
}

loadGreeting();
`
	if err := s.fs.WriteFile(filepath.Join(s.config.WorkspacePath, "ui", "app.js"), appJS); err != nil {
		return fmt.Errorf("write ui/app.js: %w", err)
	}

	readme := `# Hello World

A minimal Python API + JavaScript UI demo.
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

// stageCheckNotInitialized verifies the project is NOT initialized (greenfield).
func (s *HelloWorldScenario) stageCheckNotInitialized(ctx context.Context, result *Result) error {
	status, err := s.http.GetProjectStatus(ctx)
	if err != nil {
		return fmt.Errorf("get project status: %w", err)
	}

	if status.Initialized {
		return fmt.Errorf("expected project NOT to be initialized, but it is")
	}

	result.SetDetail("pre_init_initialized", status.Initialized)
	result.SetDetail("pre_init_has_project_json", status.HasProjectJSON)
	result.SetDetail("pre_init_has_checklist", status.HasChecklist)
	result.SetDetail("pre_init_has_standards", status.HasStandards)
	return nil
}

// stageDetectStack runs filesystem-based stack detection on the workspace.
// Detection scans root-level marker files (go.mod, package.json, etc).
// E2E projects place source in subdirectories (api/, ui/), so detection may
// find only docs and no languages — that's OK; we test the full init flow.
func (s *HelloWorldScenario) stageDetectStack(ctx context.Context, result *Result) error {
	detection, err := s.http.DetectProject(ctx)
	if err != nil {
		return fmt.Errorf("detect project: %w", err)
	}

	// The workspace has api/requirements.txt and ui/app.js — subdirectory detection
	// should find Python from api/requirements.txt at minimum.
	if len(detection.Languages) == 0 {
		return fmt.Errorf("no languages detected (expected Python from api/requirements.txt via subdirectory scanning)")
	}

	// Record what was detected
	var langNames []string
	for _, lang := range detection.Languages {
		langNames = append(langNames, lang.Name)
	}
	result.SetDetail("detected_languages", langNames)
	result.SetDetail("detected_frameworks_count", len(detection.Frameworks))
	result.SetDetail("detected_tooling_count", len(detection.Tooling))
	result.SetDetail("detected_docs_count", len(detection.ExistingDocs))
	result.SetDetail("proposed_checks_count", len(detection.ProposedChecklist))

	// Store detection for use in init stage
	result.SetDetail("detection_result", detection)
	return nil
}

// stageInitProject initializes the project using detection results.
func (s *HelloWorldScenario) stageInitProject(ctx context.Context, result *Result) error {
	detectionRaw, ok := result.GetDetail("detection_result")
	if !ok {
		return fmt.Errorf("detection_result not found in result details")
	}
	detection := detectionRaw.(*client.ProjectDetectionResult)

	// Build language list from detection
	var languages []string
	for _, lang := range detection.Languages {
		languages = append(languages, lang.Name)
	}
	var frameworks []string
	for _, fw := range detection.Frameworks {
		frameworks = append(frameworks, fw.Name)
	}

	initReq := &client.ProjectInitRequest{
		Project: client.ProjectInitInput{
			Name:        "Hello World",
			Description: "A minimal Python API + JavaScript UI demo",
			Languages:   languages,
			Frameworks:  frameworks,
		},
		Checklist: detection.ProposedChecklist,
		Standards: client.StandardsInput{
			Version: "1.0.0",
			Rules:   []any{},
		},
	}

	resp, err := s.http.InitProject(ctx, initReq)
	if err != nil {
		return fmt.Errorf("init project: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("init project returned success=false")
	}

	result.SetDetail("init_success", resp.Success)
	result.SetDetail("init_files_written", resp.FilesWritten)
	return nil
}

// stageVerifyInitialized confirms the project is now fully initialized.
func (s *HelloWorldScenario) stageVerifyInitialized(ctx context.Context, result *Result) error {
	status, err := s.http.GetProjectStatus(ctx)
	if err != nil {
		return fmt.Errorf("get project status: %w", err)
	}

	if !status.Initialized {
		missing := []string{}
		if !status.HasProjectJSON {
			missing = append(missing, "project.json")
		}
		if !status.HasChecklist {
			missing = append(missing, "checklist.json")
		}
		if !status.HasStandards {
			missing = append(missing, "standards.json")
		}
		return fmt.Errorf("project not fully initialized — missing: %s", strings.Join(missing, ", "))
	}

	result.SetDetail("post_init_initialized", status.Initialized)
	result.SetDetail("post_init_has_project_json", status.HasProjectJSON)
	result.SetDetail("post_init_has_checklist", status.HasChecklist)
	result.SetDetail("post_init_has_standards", status.HasStandards)

	// Verify the files exist on disk via filesystem client
	projectJSON := filepath.Join(s.config.WorkspacePath, ".semspec", "project.json")
	if _, err := os.Stat(projectJSON); os.IsNotExist(err) {
		return fmt.Errorf(".semspec/project.json not found on disk")
	}

	checklistJSON := filepath.Join(s.config.WorkspacePath, ".semspec", "checklist.json")
	if _, err := os.Stat(checklistJSON); os.IsNotExist(err) {
		return fmt.Errorf(".semspec/checklist.json not found on disk")
	}

	standardsJSON := filepath.Join(s.config.WorkspacePath, ".semspec", "standards.json")
	if _, err := os.Stat(standardsJSON); os.IsNotExist(err) {
		return fmt.Errorf(".semspec/standards.json not found on disk")
	}

	result.SetDetail("project_files_on_disk", true)
	return nil
}

// stageIngestSOP writes an SOP document and publishes an ingestion request.
// Uses YAML frontmatter so the source-ingester skips LLM analysis (fast + deterministic).
func (s *HelloWorldScenario) stageIngestSOP(ctx context.Context, result *Result) error {
	sopContent := `---
category: sop
scope: all
severity: warning
applies_to:
  - "api/**"
domain:
  - testing
  - api-design
requirements:
  - "All API endpoints must have corresponding tests"
  - "API responses must use JSON format with consistent structure"
  - "New endpoints must be documented in README"
---

# API Development SOP

## Ground Truth

- Existing endpoints are defined in api/app.py
- Test patterns should follow the project's testing framework (pytest for Python)
- Response format is established by the /hello endpoint: JSON with a "message" key

## Rules

1. Every new API endpoint must have at least one test covering the happy path.
2. All API responses must return JSON with a "message" or "data" key.
3. New endpoints must be added to the README documentation.
4. Plan scope must reference actual project files (api/app.py, not invented paths).

## Violations

- Adding an endpoint without a corresponding test file or test task
- Returning plain text or HTML instead of JSON from an API route
- Referencing files that don't exist in the project (e.g., src/routes/api.js when the project uses api/app.py)
`

	if err := s.fs.WriteFileRelative("sources/api-testing-sop.md", sopContent); err != nil {
		return fmt.Errorf("write SOP file: %w", err)
	}

	req := source.IngestRequest{
		Path:      "api-testing-sop.md",
		ProjectID: "default",
		AddedBy:   "e2e-test",
	}
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal ingest request: %w", err)
	}

	if err := s.nats.PublishToStream(ctx, config.SourceIngestSubject, data); err != nil {
		return fmt.Errorf("publish ingest request: %w", err)
	}

	result.SetDetail("sop_file_written", true)
	result.SetDetail("sop_ingest_published", true)
	return nil
}

// stageVerifySOPIngested polls the message-logger for graph.ingest.entity entries
// containing SOP-related content, confirming the source-ingester processed the document.
func (s *HelloWorldScenario) stageVerifySOPIngested(ctx context.Context, result *Result) error {
	ticker := time.NewTicker(kvPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("SOP entity never appeared in graph: %w", ctx.Err())
		case <-ticker.C:
			entries, err := s.http.GetMessageLogEntries(ctx, 50, "graph.ingest.entity")
			if err != nil {
				continue
			}
			if len(entries) == 0 {
				continue
			}

			sopEntities := 0
			for _, entry := range entries {
				raw := string(entry.RawData)
				if strings.Contains(raw, sourceVocab.DocCategory) {
					sopEntities++
				}
			}

			if sopEntities > 0 {
				result.SetDetail("sop_entities_found", sopEntities)
				result.SetDetail("total_graph_entities", len(entries))
				return nil
			}
		}
	}
}

// stageCreatePlan creates a plan via the REST API.
func (s *HelloWorldScenario) stageCreatePlan(ctx context.Context, result *Result) error {
	resp, err := s.http.CreatePlan(ctx, "add a /goodbye endpoint that returns a goodbye message and display it in the UI")
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
	result.SetDetail("plan_response", resp)
	return nil
}

// stageWaitForPlan waits for the plan directory and plan.json to appear on disk
// with a non-empty "goal" field, indicating the planner LLM has finished generating.
func (s *HelloWorldScenario) stageWaitForPlan(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	if err := s.fs.WaitForPlan(ctx, slug); err != nil {
		return fmt.Errorf("plan directory not created: %w", err)
	}

	if err := s.fs.WaitForPlanFile(ctx, slug, "plan.json"); err != nil {
		return fmt.Errorf("plan.json not created: %w", err)
	}

	planPath := s.fs.DefaultProjectPlanPath(slug) + "/plan.json"
	ticker := time.NewTicker(kvPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("plan.json never received goal from LLM: %w", ctx.Err())
		case <-ticker.C:
			var plan map[string]any
			if err := s.fs.ReadJSON(planPath, &plan); err != nil {
				continue
			}
			if goal, ok := plan["goal"].(string); ok && goal != "" {
				result.SetDetail("plan_file_exists", true)
				return nil
			}
		}
	}
}

// stageVerifyPlanSemantics reads plan.json and runs semantic validation checks.
func (s *HelloWorldScenario) stageVerifyPlanSemantics(_ context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")
	planPath := s.fs.DefaultProjectPlanPath(slug) + "/plan.json"

	var plan map[string]any
	if err := s.fs.ReadJSON(planPath, &plan); err != nil {
		return fmt.Errorf("read plan.json: %w", err)
	}

	goal, _ := plan["goal"].(string)
	planJSON, _ := json.Marshal(plan)
	planStr := string(planJSON)

	report := &SemanticReport{}

	// Goal mentions goodbye or endpoint
	report.Add("goal-mentions-goodbye",
		containsAnyCI(goal, "goodbye", "endpoint", "/goodbye"),
		fmt.Sprintf("goal: %s", truncate(goal, 100)))

	// Plan references api/ and ui/ directories (checks goal, context, and scope)
	report.Add("plan-references-api",
		planReferencesDir(plan, "api"),
		"plan should reference api/ directory in goal, context, or scope")
	report.Add("plan-references-ui",
		planReferencesDir(plan, "ui"),
		"plan should reference ui/ directory in goal, context, or scope")

	// Plan references existing codebase files or patterns (warning — reviewer enforces scope)
	if !containsAnyCI(planStr, "app.py", "app.js", "hello") {
		result.AddWarning("plan does not reference existing codebase files (app.py, app.js, hello)")
	}
	result.SetDetail("references_existing_code", containsAnyCI(planStr, "app.py", "app.js", "hello"))

	// Scope hallucination detection: record rate as metric, reviewer enforces correctness.
	// The plan-reviewer has the file tree in context and will flag hallucinated paths.
	knownFiles := []string{
		"api/app.py", "api/requirements.txt",
		"ui/index.html", "ui/app.js",
		"README.md",
	}
	if scope, ok := plan["scope"].(map[string]any); ok {
		hallucinationRate := scopeHallucinationRate(scope, knownFiles)
		result.SetDetail("scope_hallucination_rate", hallucinationRate)
		if hallucinationRate > 0.5 {
			result.AddWarning(fmt.Sprintf("%.0f%% of scope paths are hallucinated — reviewer should catch this", hallucinationRate*100))
		}
	}

	// SOP awareness (best-effort — warn if missing, don't fail)
	sopAware := containsAnyCI(planStr, "sop", "test", "testing", "source.doc")
	if !sopAware {
		result.AddWarning("plan does not appear to reference SOPs — context-builder may not have included them")
	}
	result.SetDetail("plan_references_sops", sopAware)

	// Record all checks
	result.SetDetail("plan_goal", goal)
	for _, check := range report.Checks {
		result.SetDetail("semantic_"+check.Name, check.Passed)
	}
	result.SetDetail("semantic_pass_rate", report.PassRate())

	if report.HasFailures() {
		return fmt.Errorf("plan semantic validation failed (%.0f%% pass rate): %s",
			report.PassRate()*100, report.Error())
	}
	return nil
}

// stageApprovePlan waits for the plan-review-loop workflow to approve the plan.
// The workflow-processor handles the planner → reviewer → revise OODA loop (ADR-005).
// This stage polls GET /plans/{slug} until the plan is approved or the timeout expires.
func (s *HelloWorldScenario) stageApprovePlan(ctx context.Context, result *Result) error {
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
	revisionsSeen := 0
	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("plan approval timed out (last stage: %s, revisions: %d/%d)",
				lastStage, revisionsSeen, maxReviewAttempts)
		case <-ticker.C:
			plan, err := s.http.GetPlan(timeoutCtx, slug)
			if err != nil {
				// Transient errors during polling are expected
				continue
			}

			lastStage = plan.Stage
			result.SetDetail("review_stage", plan.Stage)
			result.SetDetail("review_verdict", plan.ReviewVerdict)
			result.SetDetail("review_summary", plan.ReviewSummary)

			if plan.Approved {
				result.SetDetail("approve_response", plan)
				result.SetDetail("review_revisions", revisionsSeen)
				return nil
			}

			// Track revision cycles
			if plan.Stage == "needs_changes" || plan.ReviewVerdict == "needs_changes" {
				revisionsSeen++
				result.AddWarning(fmt.Sprintf("plan review revision %d/%d returned needs_changes: %s",
					revisionsSeen, maxReviewAttempts, plan.ReviewSummary))
				if revisionsSeen >= maxReviewAttempts {
					return fmt.Errorf("plan review exhausted %d revision attempts: %s",
						maxReviewAttempts, plan.ReviewSummary)
				}
			}
		}
	}
}

// stageGenerateTasks triggers LLM-based task generation via the REST API.
func (s *HelloWorldScenario) stageGenerateTasks(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	resp, err := s.http.GenerateTasks(ctx, slug)
	if err != nil {
		return fmt.Errorf("generate tasks: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("generate tasks returned error: %s", resp.Error)
	}

	result.SetDetail("generate_response", resp)
	result.SetDetail("tasks_request_id", resp.RequestID)
	result.SetDetail("tasks_trace_id", resp.TraceID)
	return nil
}

// stageWaitForTasks waits for tasks.json to be created by the LLM.
func (s *HelloWorldScenario) stageWaitForTasks(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	if err := s.fs.WaitForPlanFile(ctx, slug, "tasks.json"); err != nil {
		return fmt.Errorf("tasks.json not created: %w", err)
	}

	return nil
}

// stageVerifyTasksSemantics reads tasks.json and runs semantic validation checks.
func (s *HelloWorldScenario) stageVerifyTasksSemantics(_ context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")
	tasksPath := s.fs.DefaultProjectPlanPath(slug) + "/tasks.json"

	var tasks []map[string]any
	if err := s.fs.ReadJSON(tasksPath, &tasks); err != nil {
		return fmt.Errorf("read tasks.json: %w", err)
	}

	report := &SemanticReport{}

	// At least 2 tasks (backend + frontend minimum)
	report.Add("minimum-tasks",
		len(tasks) >= 2,
		fmt.Sprintf("got %d tasks, need >= 2", len(tasks)))

	// At least one task references api/ files
	report.Add("tasks-cover-api",
		tasksReferenceDir(tasks, "api"),
		"at least one task should reference api/ directory")

	// At least one task references ui/ files
	report.Add("tasks-cover-ui",
		tasksReferenceDir(tasks, "ui"),
		"at least one task should reference ui/ directory")

	// Tasks mention "goodbye" somewhere
	report.Add("tasks-mention-goodbye",
		tasksHaveKeywordInDescription(tasks, "goodbye", "/goodbye"),
		"at least one task should mention goodbye endpoint")

	// SOP compliance: tasks should include a test task
	hasTestTask := tasksHaveType(tasks, "test") ||
		tasksHaveKeywordInDescription(tasks, "test", "testing", "spec", "pytest", "unittest")
	report.Add("sop-test-compliance",
		hasTestTask,
		"SOP requires tests for endpoints; tasks should include test work")

	// Every task has a description
	allValid := true
	for i, task := range tasks {
		desc, _ := task["description"].(string)
		if desc == "" {
			allValid = false
			report.Add(fmt.Sprintf("task-%d-has-description", i), false, "missing description")
			break
		}
	}
	if allValid {
		report.Add("all-tasks-have-description", true, "")
	}

	// Record all checks
	result.SetDetail("task_count", len(tasks))
	for _, check := range report.Checks {
		result.SetDetail("task_semantic_"+check.Name, check.Passed)
	}
	result.SetDetail("task_semantic_pass_rate", report.PassRate())

	if report.HasFailures() {
		return fmt.Errorf("task semantic validation failed (%.0f%% pass rate): %s",
			report.PassRate()*100, report.Error())
	}
	return nil
}

// stageVerifyTasksPendingApproval verifies all tasks are in pending_approval status.
func (s *HelloWorldScenario) stageVerifyTasksPendingApproval(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	tasks, err := s.http.GetTasks(ctx, slug)
	if err != nil {
		return fmt.Errorf("get tasks: %w", err)
	}

	if len(tasks) == 0 {
		return fmt.Errorf("no tasks found for plan")
	}

	for _, task := range tasks {
		if task.Status != "pending_approval" {
			return fmt.Errorf("task %s has status %q, expected pending_approval", task.ID, task.Status)
		}
	}

	result.SetDetail("tasks_pending_count", len(tasks))
	return nil
}

// stageApproveTasksIndividually approves each task individually.
func (s *HelloWorldScenario) stageApproveTasksIndividually(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	tasks, err := s.http.GetTasks(ctx, slug)
	if err != nil {
		return fmt.Errorf("get tasks: %w", err)
	}

	approvedCount := 0
	for _, task := range tasks {
		approvedTask, err := s.http.ApproveTask(ctx, slug, task.ID, "e2e-test")
		if err != nil {
			return fmt.Errorf("approve task %s: %w", task.ID, err)
		}

		if approvedTask.Status != "approved" {
			return fmt.Errorf("task %s approval returned status %q, expected approved", task.ID, approvedTask.Status)
		}

		approvedCount++
	}

	result.SetDetail("tasks_approved_count", approvedCount)
	return nil
}

// stageVerifyTasksApproved verifies all tasks are approved with approval metadata.
func (s *HelloWorldScenario) stageVerifyTasksApproved(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	tasks, err := s.http.GetTasks(ctx, slug)
	if err != nil {
		return fmt.Errorf("get tasks: %w", err)
	}

	for _, task := range tasks {
		if task.Status != "approved" {
			return fmt.Errorf("task %s has status %q, expected approved", task.ID, task.Status)
		}

		if task.ApprovedBy == "" {
			return fmt.Errorf("task %s missing approved_by field", task.ID)
		}

		if task.ApprovedAt == nil {
			return fmt.Errorf("task %s missing approved_at timestamp", task.ID)
		}
	}

	result.SetDetail("tasks_verified_approved", len(tasks))
	return nil
}

// stageTriggerValidation publishes a ValidationTrigger to the structural-validator
// and sets up a message capture on the result subject.
func (s *HelloWorldScenario) stageTriggerValidation(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	// Subscribe to the result subject BEFORE publishing the trigger.
	resultSubject := fmt.Sprintf("workflow.result.structural-validator.%s", slug)
	capture, err := s.nats.CaptureMessages(resultSubject)
	if err != nil {
		return fmt.Errorf("subscribe to validation result: %w", err)
	}
	result.SetDetail("validation_capture", capture)

	// Build a ValidationTrigger wrapped in a BaseMessage envelope.
	// Empty files_modified triggers full-scan mode (all checks run).
	// We construct the envelope manually to avoid importing the
	// structural-validator package into the E2E test binary.
	baseMsg := map[string]any{
		"id": fmt.Sprintf("e2e-validation-%d", time.Now().UnixNano()),
		"type": map[string]string{
			"domain":   "workflow",
			"category": "structural-validation-trigger",
			"version":  "v1",
		},
		"payload": map[string]any{
			"slug":           slug,
			"files_modified": []string{},
			"workflow_id":    "e2e-test",
		},
		"meta": map[string]any{
			"created_at": time.Now().UnixMilli(),
			"source":     "e2e-test",
		},
	}

	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal validation trigger: %w", err)
	}

	if err := s.nats.PublishToStream(ctx, "workflow.async.structural-validator", data); err != nil {
		return fmt.Errorf("publish validation trigger: %w", err)
	}

	result.SetDetail("validation_triggered", true)
	return nil
}

// stageWaitForValidation waits for the structural-validator to publish a result.
func (s *HelloWorldScenario) stageWaitForValidation(ctx context.Context, result *Result) error {
	captureRaw, ok := result.GetDetail("validation_capture")
	if !ok {
		return fmt.Errorf("validation_capture not found in result details")
	}
	capture := captureRaw.(*client.MessageCapture)
	defer func() { _ = capture.Stop() }()

	if err := capture.WaitForCount(ctx, 1); err != nil {
		return fmt.Errorf("validation result never arrived: %w", err)
	}

	msgs := capture.Messages()
	result.SetDetail("validation_result_raw", string(msgs[0].Data))
	return nil
}

// stageVerifyValidationResults parses and validates the structural validation result.
// For the greenfield hello-world scenario the validator should correctly FAIL:
// pytest runs but finds no test files (exit 5), proving the pipeline works and
// the OODA loop would engage the developer to write tests.
func (s *HelloWorldScenario) stageVerifyValidationResults(_ context.Context, result *Result) error {
	rawData, ok := result.GetDetailString("validation_result_raw")
	if !ok {
		return fmt.Errorf("validation_result_raw not found")
	}

	// Parse the BaseMessage envelope to extract the payload.
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal([]byte(rawData), &envelope); err != nil {
		return fmt.Errorf("unmarshal validation result envelope: %w", err)
	}

	var validationResult struct {
		Slug         string `json:"slug"`
		Passed       bool   `json:"passed"`
		ChecksRun    int    `json:"checks_run"`
		Warning      string `json:"warning,omitempty"`
		CheckResults []struct {
			Name     string `json:"name"`
			Passed   bool   `json:"passed"`
			Required bool   `json:"required"`
			Command  string `json:"command"`
			ExitCode int    `json:"exit_code"`
			Stdout   string `json:"stdout"`
			Stderr   string `json:"stderr"`
		} `json:"check_results"`
	}
	if err := json.Unmarshal(envelope.Payload, &validationResult); err != nil {
		return fmt.Errorf("unmarshal validation result payload: %w", err)
	}

	slug, _ := result.GetDetailString("plan_slug")

	// Assert slug matches what we triggered.
	if validationResult.Slug != slug {
		return fmt.Errorf("validation slug mismatch: got %q, want %q",
			validationResult.Slug, slug)
	}

	// Assert at least one check ran — the checklist should have pytest from init.
	if validationResult.ChecksRun == 0 {
		return fmt.Errorf("no checks ran (checklist empty or not loaded)")
	}

	// Greenfield project has no test files — validation should correctly fail.
	// If it passes, either the checklist is wrong or pytest isn't running properly.
	if validationResult.Passed {
		return fmt.Errorf("expected validation to fail (greenfield project has no tests) but it passed")
	}

	// Verify pytest actually ran — exit code must NOT be -1 (command not found).
	// We expect exit 5 (no tests collected) which proves pytest is installed
	// and the validator correctly detected the missing tests.
	for _, cr := range validationResult.CheckResults {
		if cr.ExitCode == -1 {
			return fmt.Errorf("check %q returned exit -1 (command not found); "+
				"tool must be installed in container", cr.Name)
		}
	}

	// Record details for JSON output.
	result.SetDetail("validation_slug", validationResult.Slug)
	result.SetDetail("validation_passed", validationResult.Passed)
	result.SetDetail("validation_checks_run", validationResult.ChecksRun)
	result.SetDetail("validation_warning", validationResult.Warning)

	for i, cr := range validationResult.CheckResults {
		result.SetDetail(fmt.Sprintf("check_%d_name", i), cr.Name)
		result.SetDetail(fmt.Sprintf("check_%d_passed", i), cr.Passed)
		result.SetDetail(fmt.Sprintf("check_%d_command", i), cr.Command)
		result.SetDetail(fmt.Sprintf("check_%d_exit_code", i), cr.ExitCode)
		result.SetDetail(fmt.Sprintf("check_%d_stdout", i), cr.Stdout)
		result.SetDetail(fmt.Sprintf("check_%d_stderr", i), cr.Stderr)
	}

	return nil
}

// stageCaptureTrajectory retrieves trajectory data using the trace ID from plan creation.
// Falls back to polling the LLM_CALLS KV bucket if no trace ID was captured.
func (s *HelloWorldScenario) stageCaptureTrajectory(ctx context.Context, result *Result) error {
	traceID := s.resolveTraceID(ctx, result)
	if traceID == "" {
		return nil
	}

	result.SetDetail("trajectory_trace_id", traceID)

	if err := s.capturePlanTrajectory(ctx, result, traceID); err != nil {
		return err
	}

	s.captureTasksTrajectory(ctx, result, traceID)
	s.captureWorkflowTrajectory(ctx, result)

	return nil
}

// resolveTraceID gets the trace ID from plan creation or falls back to KV bucket.
func (s *HelloWorldScenario) resolveTraceID(ctx context.Context, result *Result) string {
	traceID, _ := result.GetDetailString("plan_trace_id")
	if traceID != "" {
		return traceID
	}

	ticker := time.NewTicker(kvPollInterval)
	defer ticker.Stop()

	var kvEntries *client.KVEntriesResponse
	var lastErr error

	for kvEntries == nil {
		select {
		case <-ctx.Done():
			if lastErr != nil {
				result.AddWarning(fmt.Sprintf("trajectory capture timed out: %v (last error: %v)", ctx.Err(), lastErr))
			} else {
				result.AddWarning(fmt.Sprintf("trajectory capture timed out: %v", ctx.Err()))
			}
			return ""
		case <-ticker.C:
			entries, err := s.http.GetKVEntries(ctx, "LLM_CALLS")
			if err != nil {
				lastErr = err
				continue
			}
			if len(entries.Entries) > 0 {
				kvEntries = entries
			}
		}
	}

	firstKey := kvEntries.Entries[0].Key
	parts := strings.SplitN(firstKey, ".", 2)
	if len(parts) < 2 {
		result.AddWarning(fmt.Sprintf("LLM_CALLS key %q doesn't contain trace prefix", firstKey))
		return ""
	}

	return parts[0]
}

// capturePlanTrajectory captures the main plan trajectory metrics.
func (s *HelloWorldScenario) capturePlanTrajectory(ctx context.Context, result *Result, traceID string) error {
	trajectory, statusCode, err := s.http.GetTrajectoryByTrace(ctx, traceID, true)
	if err != nil {
		if statusCode == 404 {
			result.AddWarning("trajectory-api returned 404 — component may not be enabled")
			return nil
		}
		return fmt.Errorf("get trajectory by trace: %w", err)
	}

	result.SetDetail("trajectory_model_calls", trajectory.ModelCalls)
	result.SetDetail("trajectory_tokens_in", trajectory.TokensIn)
	result.SetDetail("trajectory_tokens_out", trajectory.TokensOut)
	result.SetDetail("trajectory_duration_ms", trajectory.DurationMs)
	result.SetDetail("trajectory_entries_count", len(trajectory.Entries))

	return nil
}

// captureTasksTrajectory captures task generation trajectory if available.
func (s *HelloWorldScenario) captureTasksTrajectory(ctx context.Context, result *Result, planTraceID string) {
	tasksTraceID, _ := result.GetDetailString("tasks_trace_id")
	if tasksTraceID == "" || tasksTraceID == planTraceID {
		return
	}

	result.SetDetail("tasks_trajectory_trace_id", tasksTraceID)
	tasksTrajectory, tasksStatus, tasksErr := s.http.GetTrajectoryByTrace(ctx, tasksTraceID, true)
	if tasksErr != nil {
		if tasksStatus != 404 {
			result.AddWarning(fmt.Sprintf("tasks trajectory query failed: %v", tasksErr))
		}
		return
	}

	result.SetDetail("tasks_trajectory_model_calls", tasksTrajectory.ModelCalls)
	result.SetDetail("tasks_trajectory_tokens_in", tasksTrajectory.TokensIn)
	result.SetDetail("tasks_trajectory_tokens_out", tasksTrajectory.TokensOut)
	result.SetDetail("tasks_trajectory_duration_ms", tasksTrajectory.DurationMs)
}

// captureWorkflowTrajectory captures workflow-level aggregated metrics.
func (s *HelloWorldScenario) captureWorkflowTrajectory(ctx context.Context, result *Result) {
	slug, _ := result.GetDetailString("plan_slug")
	if slug == "" {
		return
	}

	wt, wtStatus, wtErr := s.http.GetWorkflowTrajectory(ctx, slug)
	if wtErr != nil {
		if wtStatus == 404 {
			result.AddWarning("workflow trajectory returned 404 — plan may not have execution_trace_ids yet")
		} else {
			result.AddWarning(fmt.Sprintf("workflow trajectory query failed: %v", wtErr))
		}
		return
	}

	result.SetDetail("workflow_trajectory_slug", wt.Slug)
	result.SetDetail("workflow_trajectory_status", wt.Status)
	result.SetDetail("workflow_trajectory_trace_count", len(wt.TraceIDs))

	if wt.Totals != nil {
		result.SetDetail("workflow_trajectory_total_tokens", wt.Totals.TotalTokens)
		result.SetDetail("workflow_trajectory_call_count", wt.Totals.CallCount)
		result.SetDetail("workflow_trajectory_duration_ms", wt.Totals.DurationMs)
	}

	if wt.TruncationSummary != nil {
		result.SetDetail("workflow_trajectory_truncation_rate", wt.TruncationSummary.TruncationRate)
	}

	var phaseNames []string
	for name := range wt.Phases {
		phaseNames = append(phaseNames, name)
	}
	result.SetDetail("workflow_trajectory_phases", phaseNames)
}

// stageCaptureContext queries the trajectory-api context-stats endpoint to capture
// context utilization metrics. This proves the context builder is effectively managing
// token budgets — showing utilization rates, truncation frequency, and per-capability
// breakdown across the entire workflow.
func (s *HelloWorldScenario) stageCaptureContext(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")
	if slug == "" {
		result.AddWarning("no plan_slug available for context stats")
		return nil
	}

	stats, statusCode, err := s.http.GetContextStats(ctx, slug)
	if err != nil {
		if statusCode == 404 {
			result.AddWarning("context-stats returned 404 — trajectory-api may lack workflow trace IDs")
			return nil
		}
		result.AddWarning(fmt.Sprintf("context-stats query failed (HTTP %d): %v", statusCode, err))
		return nil
	}

	if stats.Summary != nil {
		result.SetDetail("context_total_calls", stats.Summary.TotalCalls)
		result.SetDetail("context_calls_with_budget", stats.Summary.CallsWithBudget)
		result.SetDetail("context_avg_utilization", stats.Summary.AvgUtilization)
		result.SetDetail("context_truncation_rate", stats.Summary.TruncationRate)
		result.SetDetail("context_total_budget", stats.Summary.TotalBudget)
		result.SetDetail("context_total_used", stats.Summary.TotalUsed)
	}

	if len(stats.ByCapability) > 0 {
		capBreakdown := map[string]any{}
		for cap, capStats := range stats.ByCapability {
			capBreakdown[cap] = map[string]any{
				"call_count":      capStats.CallCount,
				"avg_budget":      capStats.AvgBudget,
				"avg_used":        capStats.AvgUsed,
				"avg_utilization": capStats.AvgUtilization,
				"truncation_rate": capStats.TruncationRate,
				"max_utilization": capStats.MaxUtilization,
			}
		}
		result.SetDetail("context_by_capability", capBreakdown)
	}

	if len(stats.Calls) > 0 {
		callDetails := make([]map[string]any, 0, len(stats.Calls))
		for _, call := range stats.Calls {
			callDetails = append(callDetails, map[string]any{
				"capability":  call.Capability,
				"model":       call.Model,
				"budget":      call.Budget,
				"used":        call.Used,
				"utilization": call.Utilization,
				"truncated":   call.Truncated,
			})
		}
		result.SetDetail("context_calls", callDetails)
	}

	return nil
}

// stageCaptureArtifacts reads the plan and task files generated by the LLM and
// captures them in the result for provider quality comparison.
func (s *HelloWorldScenario) stageCaptureArtifacts(_ context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")
	planDir := filepath.Join(s.config.WorkspacePath, ".semspec", "projects", "default", "plans", slug)

	s.capturePlanArtifact(planDir, result)
	s.captureTasksArtifact(planDir, result)

	return nil
}

// capturePlanArtifact reads and captures plan.json artifact details.
func (s *HelloWorldScenario) capturePlanArtifact(planDir string, result *Result) {
	planPath := filepath.Join(planDir, "plan.json")
	planData, err := os.ReadFile(planPath)
	if err != nil {
		result.AddWarning(fmt.Sprintf("could not read plan.json for artifacts: %v", err))
		return
	}

	var plan map[string]any
	if err := json.Unmarshal(planData, &plan); err != nil {
		result.AddWarning(fmt.Sprintf("could not parse plan.json: %v", err))
		return
	}

	planJSON, _ := json.MarshalIndent(plan, "", "  ")
	result.SetDetail("artifact_plan", string(planJSON))

	goal, _ := plan["goal"].(string)
	ctx, _ := plan["context"].(string)
	result.SetDetail("artifact_plan_goal", goal)
	result.SetDetail("artifact_plan_goal_length", len(goal))
	result.SetDetail("artifact_plan_context_length", len(ctx))

	s.capturePlanScope(plan, result)
	s.capturePlanReview(result)
}

// capturePlanScope extracts scope details from the plan.
func (s *HelloWorldScenario) capturePlanScope(plan map[string]any, result *Result) {
	scope, ok := plan["scope"].(map[string]any)
	if !ok {
		return
	}

	inc, ok := scope["include"].([]any)
	if !ok {
		return
	}

	paths := make([]string, 0, len(inc))
	for _, p := range inc {
		if s, ok := p.(string); ok {
			paths = append(paths, s)
		}
	}

	result.SetDetail("artifact_plan_scope_include_count", len(inc))
	result.SetDetail("artifact_plan_scope_paths", paths)
}

// capturePlanReview captures review metrics if available.
func (s *HelloWorldScenario) capturePlanReview(result *Result) {
	if revisions, ok := result.GetDetail("review_revisions"); ok {
		result.SetDetail("artifact_review_revisions", revisions)
	}
	if verdict, ok := result.GetDetailString("review_verdict"); ok {
		result.SetDetail("artifact_review_verdict", verdict)
	}
}

// captureTasksArtifact reads and captures tasks.json artifact details.
func (s *HelloWorldScenario) captureTasksArtifact(planDir string, result *Result) {
	tasksPath := filepath.Join(planDir, "tasks.json")
	tasksData, err := os.ReadFile(tasksPath)
	if err != nil {
		result.AddWarning(fmt.Sprintf("could not read tasks.json for artifacts: %v", err))
		return
	}

	var tasks []map[string]any
	if err := json.Unmarshal(tasksData, &tasks); err != nil {
		result.AddWarning(fmt.Sprintf("could not parse tasks.json: %v", err))
		return
	}

	tasksJSON, _ := json.MarshalIndent(tasks, "", "  ")
	result.SetDetail("artifact_tasks", string(tasksJSON))
	result.SetDetail("artifact_task_count", len(tasks))

	s.captureTaskMetrics(tasks, result)
}

// captureTaskMetrics analyzes task array for type distribution and dependencies.
func (s *HelloWorldScenario) captureTaskMetrics(tasks []map[string]any, result *Result) {
	typeCounts := map[string]int{}
	var descriptions []string
	totalDescLen := 0
	hasDeps := false

	for _, task := range tasks {
		taskType, _ := task["type"].(string)
		if taskType == "" {
			taskType = "unknown"
		}
		typeCounts[taskType]++

		desc, _ := task["description"].(string)
		descriptions = append(descriptions, desc)
		totalDescLen += len(desc)

		if deps, ok := task["depends_on"].([]any); ok && len(deps) > 0 {
			hasDeps = true
		}
	}

	result.SetDetail("artifact_task_types", typeCounts)
	result.SetDetail("artifact_task_descriptions", descriptions)
	result.SetDetail("artifact_task_has_dependencies", hasDeps)
	if len(tasks) > 0 {
		result.SetDetail("artifact_task_avg_desc_length", totalDescLen/len(tasks))
	}
}

// stageVerifyMockStats queries the mock LLM /stats endpoint and asserts per-model
// call counts match variant expectations. Skipped when mockLLM client is not configured.
//
// The task-review-loop runs asynchronously — the e2e test may reach this stage
// before the task_reviewer step executes. We poll until the expected models appear.
func (s *HelloWorldScenario) stageVerifyMockStats(ctx context.Context, result *Result) error {
	if s.mockLLM == nil {
		return nil
	}

	// Determine which models we need to see in stats.
	// The task-review-loop is async, so task-reviewer may not have been called yet.
	requiredModels := []string{"mock-planner", "mock-reviewer", "mock-task-generator"}
	// Task reviewer is part of the async task-review-loop; always wait for it.
	requiredModels = append(requiredModels, "mock-task-reviewer")

	// Poll until all required models appear in stats.
	var stats *client.MockStats
	ticker := time.NewTicker(config.FastPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Report what we have and continue with partial stats
			if stats != nil {
				result.SetDetail("mock_stats_total_calls", stats.TotalCalls)
				result.SetDetail("mock_stats_by_model", stats.CallsByModel)
				result.AddWarning(fmt.Sprintf("mock stats poll timed out; got models: %v", modelNames(stats.CallsByModel)))
			}
			return fmt.Errorf("mock stats: timed out waiting for all required models %v: %w", requiredModels, ctx.Err())
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

	// Verify plan reviewer call count matches expected revisions.
	if s.variant.ExpectPlanRevisions > 0 {
		reviewerCalls := stats.CallsByModel["mock-reviewer"]
		expectedCalls := int64(s.variant.ExpectPlanRevisions + 1)
		if reviewerCalls < expectedCalls {
			return fmt.Errorf("mock-reviewer calls: got %d, want >= %d (expected %d rejections + 1 approval)",
				reviewerCalls, expectedCalls, s.variant.ExpectPlanRevisions)
		}
		result.SetDetail("mock_reviewer_calls", reviewerCalls)
		result.SetDetail("mock_reviewer_expected", expectedCalls)
	}

	// Verify task reviewer call count matches expected revisions.
	if s.variant.ExpectTaskRevisions > 0 {
		// For rejection variants, poll until the task-reviewer reaches the expected count.
		expectedCalls := int64(s.variant.ExpectTaskRevisions + 1)
		for stats.CallsByModel["mock-task-reviewer"] < expectedCalls {
			select {
			case <-ctx.Done():
				return fmt.Errorf("mock-task-reviewer calls: got %d, want >= %d (timed out): %w",
					stats.CallsByModel["mock-task-reviewer"], expectedCalls, ctx.Err())
			case <-ticker.C:
				var err error
				stats, err = s.mockLLM.GetStats(ctx)
				if err != nil {
					continue
				}
			}
		}
		result.SetDetail("mock_task_reviewer_calls", stats.CallsByModel["mock-task-reviewer"])
		result.SetDetail("mock_task_reviewer_expected", expectedCalls)
	}

	// For happy path, record reviewer calls and warn if unexpected retries occurred.
	if s.variant.ExpectPlanRevisions == 0 {
		if reviewerCalls, ok := stats.CallsByModel["mock-reviewer"]; ok {
			result.SetDetail("mock_reviewer_calls", reviewerCalls)
			if reviewerCalls > 1 {
				result.AddWarning(fmt.Sprintf("mock-reviewer called %d times in happy path (expected 1)", reviewerCalls))
			}
		}
	}
	if s.variant.ExpectTaskRevisions == 0 {
		if taskReviewerCalls, ok := stats.CallsByModel["mock-task-reviewer"]; ok {
			result.SetDetail("mock_task_reviewer_calls", taskReviewerCalls)
			if taskReviewerCalls > 1 {
				result.AddWarning(fmt.Sprintf("mock-task-reviewer called %d times in happy path (expected 1)", taskReviewerCalls))
			}
		}
	}

	// Update final stats snapshot
	result.SetDetail("mock_stats_total_calls", stats.TotalCalls)
	result.SetDetail("mock_stats_by_model", stats.CallsByModel)

	return nil
}

// hasAllModels returns true if all required model names appear in the stats map.
func hasAllModels(callsByModel map[string]int64, required []string) bool {
	for _, m := range required {
		if _, ok := callsByModel[m]; !ok {
			return false
		}
	}
	return true
}

// modelNames returns sorted model names from a stats map for logging.
func modelNames(m map[string]int64) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// stageVerifyRevisionPrompts checks that revision prompts contain actual reviewer
// findings — not literal ${steps.plan_reviewer.output.*} template variables.
// This catches workflow interpolation failures that would silently break the OODA loop.
// Skipped when mockLLM client is not configured or no rejections are expected.
func (s *HelloWorldScenario) stageVerifyRevisionPrompts(ctx context.Context, result *Result) error {
	if s.mockLLM == nil {
		return nil // not a mock scenario
	}

	// Verify plan revision prompts contain reviewer findings
	if s.variant.ExpectPlanRevisions > 0 {
		if err := s.verifyPlanRevisionPrompt(ctx, result); err != nil {
			return fmt.Errorf("plan revision prompt: %w", err)
		}
	}

	// Verify task revision prompts contain reviewer findings
	if s.variant.ExpectTaskRevisions > 0 {
		if err := s.verifyTaskRevisionPrompt(ctx, result); err != nil {
			return fmt.Errorf("task revision prompt: %w", err)
		}
	}

	return nil
}

// verifyPlanRevisionPrompt checks the planner's 2nd call contains reviewer feedback.
func (s *HelloWorldScenario) verifyPlanRevisionPrompt(ctx context.Context, result *Result) error {
	reqs, err := s.mockLLM.GetRequests(ctx, "mock-planner")
	if err != nil {
		return fmt.Errorf("get planner requests: %w", err)
	}

	plannerReqs := reqs.RequestsByModel["mock-planner"]
	if len(plannerReqs) < 2 {
		return fmt.Errorf("expected >= 2 planner calls (initial + revision), got %d", len(plannerReqs))
	}

	// The 2nd call is the revision — check its user message content
	revisionReq := plannerReqs[1]
	var userPrompt string
	for _, msg := range revisionReq.Messages {
		if msg.Role == "user" {
			userPrompt = msg.Content
			break
		}
	}

	if userPrompt == "" {
		return fmt.Errorf("revision call has no user message")
	}

	result.SetDetail("plan_revision_prompt_length", len(userPrompt))

	// CRITICAL: Check that template variables were resolved (not literal ${...} text)
	if strings.Contains(userPrompt, "${steps.") {
		result.SetDetail("plan_revision_prompt_has_unresolved_templates", true)
		return fmt.Errorf("plan revision prompt contains unresolved template variables: "+
			"workflow interpolation is broken — planner receives literal ${steps...} instead of reviewer findings")
	}
	result.SetDetail("plan_revision_prompt_has_unresolved_templates", false)

	// Check that the prompt contains the REVISION REQUEST marker
	if !strings.Contains(userPrompt, "REVISION REQUEST") {
		return fmt.Errorf("plan revision prompt missing 'REVISION REQUEST' marker — "+
			"planner may not be receiving the revision prompt at all")
	}

	// Check that the planner's own previous output is included for reference.
	// Without this, the LLM has to guess what its previous plan looked like.
	hasPreviousPlan := strings.Contains(userPrompt, "Your Previous Plan Output")
	result.SetDetail("plan_revision_has_previous_plan", hasPreviousPlan)
	if !hasPreviousPlan {
		return fmt.Errorf("plan revision prompt missing 'Your Previous Plan Output' section — " +
			"planner cannot see its own previous output to make targeted fixes")
	}

	// Check that actual reviewer content is present (from the mock fixture)
	// The mock-reviewer.1.json fixture has specific content we can check for
	checks := []struct {
		name    string
		needle  string
		purpose string
	}{
		{"has_summary", "missing test files", "reviewer summary should be interpolated into prompt"},
		{"has_findings", "api-testing-sop", "reviewer findings should reference the SOP ID"},
		{"has_scope_detail", "scope.include", "reviewer evidence about scope should be present"},
	}

	for _, check := range checks {
		found := containsAnyCI(userPrompt, check.needle)
		result.SetDetail("plan_revision_"+check.name, found)
		if !found {
			return fmt.Errorf("plan revision prompt missing %q: %s", check.needle, check.purpose)
		}
	}

	return nil
}

// verifyTaskRevisionPrompt checks the task-generator's 2nd call contains reviewer feedback.
func (s *HelloWorldScenario) verifyTaskRevisionPrompt(ctx context.Context, result *Result) error {
	reqs, err := s.mockLLM.GetRequests(ctx, "mock-task-generator")
	if err != nil {
		return fmt.Errorf("get task-generator requests: %w", err)
	}

	taskGenReqs := reqs.RequestsByModel["mock-task-generator"]
	if len(taskGenReqs) < 2 {
		return fmt.Errorf("expected >= 2 task-generator calls (initial + revision), got %d", len(taskGenReqs))
	}

	// The 2nd call is the revision
	revisionReq := taskGenReqs[1]
	var userPrompt string
	for _, msg := range revisionReq.Messages {
		if msg.Role == "user" {
			userPrompt = msg.Content
			break
		}
	}

	if userPrompt == "" {
		return fmt.Errorf("task revision call has no user message")
	}

	result.SetDetail("task_revision_prompt_length", len(userPrompt))

	// CRITICAL: Check no unresolved template variables
	if strings.Contains(userPrompt, "${steps.") {
		result.SetDetail("task_revision_prompt_has_unresolved_templates", true)
		return fmt.Errorf("task revision prompt contains unresolved template variables: "+
			"workflow interpolation is broken")
	}
	result.SetDetail("task_revision_prompt_has_unresolved_templates", false)

	// Check revision marker
	if !strings.Contains(userPrompt, "REVISION REQUEST") {
		return fmt.Errorf("task revision prompt missing 'REVISION REQUEST' marker")
	}

	return nil
}

// stageGenerateReport compiles a summary report with provider and trajectory data.
func (s *HelloWorldScenario) stageGenerateReport(_ context.Context, result *Result) error {
	providerName := os.Getenv(config.ProviderNameEnvVar)
	if providerName == "" {
		providerName = config.DefaultProviderName
	}

	report := s.buildBaseReport(result, providerName)
	s.addWorkflowMetrics(report, result)
	s.addContextMetrics(report, result)
	s.addQualityMetrics(report, result)

	result.SetDetail("provider", providerName)
	result.SetDetail("report", report)
	return nil
}

// buildBaseReport creates the base report structure with trajectory data.
func (s *HelloWorldScenario) buildBaseReport(result *Result, providerName string) map[string]any {
	taskCount, _ := result.GetDetail("task_count")
	modelCalls, _ := result.GetDetail("trajectory_model_calls")
	tokensIn, _ := result.GetDetail("trajectory_tokens_in")
	tokensOut, _ := result.GetDetail("trajectory_tokens_out")
	durationMs, _ := result.GetDetail("trajectory_duration_ms")

	report := map[string]any{
		"provider":      providerName,
		"scenario":      s.name,
		"model_calls":   modelCalls,
		"tokens_in":     tokensIn,
		"tokens_out":    tokensOut,
		"duration_ms":   durationMs,
		"plan_created":  true,
		"tasks_created": taskCount,
	}

	// Add variant expectations for mock stats comparison
	if s.variant.ExpectPlanRevisions > 0 || s.variant.ExpectTaskRevisions > 0 {
		report["variant"] = map[string]any{
			"expect_plan_revisions": s.variant.ExpectPlanRevisions,
			"expect_task_revisions": s.variant.ExpectTaskRevisions,
		}
	}

	// Add mock stats if captured
	if mockStats, ok := result.GetDetail("mock_stats_by_model"); ok {
		report["mock_stats"] = mockStats
	}

	return report
}

// addWorkflowMetrics adds workflow-level trajectory data to the report.
func (s *HelloWorldScenario) addWorkflowMetrics(report map[string]any, result *Result) {
	if totalTokens, ok := result.GetDetail("workflow_trajectory_total_tokens"); ok {
		report["workflow_total_tokens"] = totalTokens
	}
	if callCount, ok := result.GetDetail("workflow_trajectory_call_count"); ok {
		report["workflow_call_count"] = callCount
	}
	if truncRate, ok := result.GetDetail("workflow_trajectory_truncation_rate"); ok {
		report["workflow_truncation_rate"] = truncRate
	}
}

// addContextMetrics adds context utilization metrics to the report.
func (s *HelloWorldScenario) addContextMetrics(report map[string]any, result *Result) {
	contextMetrics := map[string]any{}

	metricMappings := map[string]string{
		"context_total_calls":       "total_calls",
		"context_calls_with_budget": "calls_with_budget",
		"context_avg_utilization":   "avg_utilization_pct",
		"context_truncation_rate":   "truncation_rate_pct",
		"context_total_budget":      "total_budget_tokens",
		"context_total_used":        "total_used_tokens",
		"context_by_capability":     "by_capability",
		"context_calls":             "calls",
	}

	for sourceKey, targetKey := range metricMappings {
		if v, ok := result.GetDetail(sourceKey); ok {
			contextMetrics[targetKey] = v
		}
	}

	if len(contextMetrics) > 0 {
		report["context"] = contextMetrics
	}
}

// addQualityMetrics adds artifact quality metrics to the report.
func (s *HelloWorldScenario) addQualityMetrics(report map[string]any, result *Result) {
	quality := map[string]any{}

	qualityMappings := map[string]string{
		"artifact_plan_goal_length":         "plan_goal_length",
		"artifact_plan_context_length":      "plan_context_length",
		"artifact_plan_scope_include_count": "plan_scope_include_count",
		"artifact_plan_scope_paths":         "plan_scope_paths",
		"artifact_review_revisions":         "review_revisions",
		"artifact_review_verdict":           "review_verdict",
		"artifact_task_count":               "task_count",
		"artifact_task_types":               "task_types",
		"artifact_task_has_dependencies":    "task_has_dependencies",
		"artifact_task_avg_desc_length":     "task_avg_desc_length",
		"artifact_plan_goal":                "plan_goal",
		"artifact_task_descriptions":        "task_descriptions",
	}

	for sourceKey, targetKey := range qualityMappings {
		if v, ok := result.GetDetail(sourceKey); ok {
			quality[targetKey] = v
		}
	}

	if len(quality) > 0 {
		report["quality"] = quality
	}
}
