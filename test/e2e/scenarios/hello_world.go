package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/c360studio/semspec/source"
	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
	sourceVocab "github.com/c360studio/semspec/vocabulary/source"
)

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
}

// NewHelloWorldScenario creates a greenfield hello-world scenario.
func NewHelloWorldScenario(cfg *config.Config) *HelloWorldScenario {
	return &HelloWorldScenario{
		name:        "hello-world",
		description: "Greenfield Python+JS: add /goodbye endpoint with semantic validation",
		config:      cfg,
	}
}

func (s *HelloWorldScenario) Name() string        { return s.name }
func (s *HelloWorldScenario) Description() string  { return s.description }

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

	return nil
}

// Execute runs the hello-world scenario.
func (s *HelloWorldScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name    string
		fn      func(context.Context, *Result) error
		timeout time.Duration
	}{
		{"setup-project", s.stageSetupProject, 30 * time.Second},
		{"check-not-initialized", s.stageCheckNotInitialized, 10 * time.Second},
		{"detect-stack", s.stageDetectStack, 30 * time.Second},
		{"init-project", s.stageInitProject, 30 * time.Second},
		{"verify-initialized", s.stageVerifyInitialized, 10 * time.Second},
		{"ingest-sop", s.stageIngestSOP, 30 * time.Second},
		{"verify-sop-ingested", s.stageVerifySOPIngested, 60 * time.Second},
		{"create-plan", s.stageCreatePlan, 30 * time.Second},
		{"wait-for-plan", s.stageWaitForPlan, 300 * time.Second},
		{"verify-plan-semantics", s.stageVerifyPlanSemantics, 10 * time.Second},
		{"approve-plan", s.stageApprovePlan, 240 * time.Second},
		{"generate-tasks", s.stageGenerateTasks, 30 * time.Second},
		{"wait-for-tasks", s.stageWaitForTasks, 300 * time.Second},
		{"verify-tasks-semantics", s.stageVerifyTasksSemantics, 10 * time.Second},
		{"capture-trajectory", s.stageCaptureTrajectory, 30 * time.Second},
		{"generate-report", s.stageGenerateReport, 10 * time.Second},
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

	if err := s.fs.WriteFileRelative(".semspec/sources/docs/api-testing-sop.md", sopContent); err != nil {
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

// stageApprovePlan approves the plan via the REST API.
// Retries up to maxReviewAttempts if the plan-reviewer returns needs_changes.
// This allows the planner to self-correct hallucinated scope after review feedback.
func (s *HelloWorldScenario) stageApprovePlan(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	for attempt := 1; attempt <= maxReviewAttempts; attempt++ {
		resp, err := s.http.PromotePlan(ctx, slug)
		if err != nil {
			return fmt.Errorf("promote plan (attempt %d): %w", attempt, err)
		}

		if resp.Error != "" {
			return fmt.Errorf("promote returned error: %s", resp.Error)
		}

		result.SetDetail("review_verdict", resp.ReviewVerdict)
		result.SetDetail("review_summary", resp.ReviewSummary)
		result.SetDetail("review_stage", resp.Stage)
		result.SetDetail("review_findings_count", len(resp.ReviewFindings))
		result.SetDetail("review_attempts", attempt)

		if resp.IsApproved() {
			result.SetDetail("approve_response", resp)
			return nil
		}

		// Review returned needs_changes — record findings
		for i, f := range resp.ReviewFindings {
			result.SetDetail(fmt.Sprintf("finding_%d", i), map[string]string{
				"sop_id":   f.SOPID,
				"severity": f.Severity,
				"status":   f.Status,
				"issue":    f.Issue,
			})
		}

		if attempt < maxReviewAttempts {
			result.AddWarning(fmt.Sprintf("plan review attempt %d/%d returned needs_changes: %s — retrying",
				attempt, maxReviewAttempts, resp.ReviewSummary))
			select {
			case <-ctx.Done():
				return fmt.Errorf("context cancelled during review retry: %w", ctx.Err())
			case <-time.After(reviewRetryBackoff * time.Duration(attempt)):
			}
		} else {
			// Final attempt — continue with warning (plan may still be usable)
			result.AddWarning(fmt.Sprintf("plan review failed after %d attempts: %s",
				maxReviewAttempts, resp.ReviewSummary))
			result.SetDetail("approve_response", resp)
		}
	}

	return nil
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

// stageCaptureTrajectory polls the LLM_CALLS KV bucket and retrieves trajectory data.
func (s *HelloWorldScenario) stageCaptureTrajectory(ctx context.Context, result *Result) error {
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
			return nil
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
		return nil
	}

	traceID := parts[0]
	result.SetDetail("trajectory_trace_id", traceID)

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

// stageGenerateReport compiles a summary report with provider and trajectory data.
func (s *HelloWorldScenario) stageGenerateReport(_ context.Context, result *Result) error {
	providerName := os.Getenv(config.ProviderNameEnvVar)
	if providerName == "" {
		providerName = config.DefaultProviderName
	}

	taskCount, _ := result.GetDetail("task_count")
	modelCalls, _ := result.GetDetail("trajectory_model_calls")
	tokensIn, _ := result.GetDetail("trajectory_tokens_in")
	tokensOut, _ := result.GetDetail("trajectory_tokens_out")
	durationMs, _ := result.GetDetail("trajectory_duration_ms")

	result.SetDetail("provider", providerName)
	result.SetDetail("report", map[string]any{
		"provider":      providerName,
		"scenario":      "hello-world",
		"model_calls":   modelCalls,
		"tokens_in":     tokensIn,
		"tokens_out":    tokensOut,
		"duration_ms":   durationMs,
		"plan_created":  true,
		"tasks_created": taskCount,
	})
	return nil
}
