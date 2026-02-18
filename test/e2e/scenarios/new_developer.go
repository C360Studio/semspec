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
)

// NewDeveloperScenario tests the complete new developer experience:
// setup hello-world project → create plan → wait for LLM generation →
// verify plan quality → approve → generate tasks → verify tasks quality →
// capture trajectory data for provider comparison.
type NewDeveloperScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
	nats        *client.NATSClient
}

// NewNewDeveloperScenario creates a new developer experience scenario.
func NewNewDeveloperScenario(cfg *config.Config) *NewDeveloperScenario {
	return &NewDeveloperScenario{
		name:        "new-developer",
		description: "Tests complete new-developer workflow: plan → approve → tasks with LLM trajectory capture",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *NewDeveloperScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *NewDeveloperScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *NewDeveloperScenario) Setup(ctx context.Context) error {
	// Create filesystem client and setup workspace
	s.fs = client.NewFilesystemClient(s.config.WorkspacePath)
	if err := s.fs.SetupWorkspace(); err != nil {
		return fmt.Errorf("setup workspace: %w", err)
	}

	// Create HTTP client
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)

	// Wait for service to be healthy
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

	// Create NATS client for direct JetStream publishing
	natsClient, err := client.NewNATSClient(ctx, s.config.NATSURL)
	if err != nil {
		return fmt.Errorf("create NATS client: %w", err)
	}
	s.nats = natsClient

	return nil
}

// Execute runs the new developer scenario.
func (s *NewDeveloperScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name    string
		fn      func(context.Context, *Result) error
		timeout time.Duration
	}{
		{"setup-project", s.stageSetupProject, 30 * time.Second},
		{"ingest-sop", s.stageIngestSOP, 30 * time.Second},
		{"verify-sop-ingested", s.stageVerifySOPIngested, 60 * time.Second},
		{"create-plan", s.stageCreatePlan, 30 * time.Second},
		{"wait-for-plan", s.stageWaitForPlan, 300 * time.Second},
		{"verify-plan-quality", s.stageVerifyPlanQuality, 10 * time.Second},
		{"approve-plan", s.stageApprovePlan, 240 * time.Second},
		{"generate-tasks", s.stageGenerateTasks, 30 * time.Second},
		{"wait-for-tasks", s.stageWaitForTasks, 300 * time.Second},
		{"verify-tasks-quality", s.stageVerifyTasksQuality, 10 * time.Second},
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
func (s *NewDeveloperScenario) Teardown(ctx context.Context) error {
	if s.nats != nil {
		return s.nats.Close(ctx)
	}
	return nil
}

// stageSetupProject creates a minimal Go hello-world project in the workspace.
func (s *NewDeveloperScenario) stageSetupProject(ctx context.Context, result *Result) error {
	mainGo := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`
	if err := s.fs.WriteFile(filepath.Join(s.config.WorkspacePath, "main.go"), mainGo); err != nil {
		return fmt.Errorf("write main.go: %w", err)
	}

	goMod := `module hello-world

go 1.22
`
	if err := s.fs.WriteFile(filepath.Join(s.config.WorkspacePath, "go.mod"), goMod); err != nil {
		return fmt.Errorf("write go.mod: %w", err)
	}

	readme := `# Hello World

A minimal Go project for demonstrating semspec workflows.
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

// stageIngestSOP writes an SOP document and publishes an ingestion request.
// Uses YAML frontmatter so the source-ingester skips LLM analysis (fast + deterministic).
func (s *NewDeveloperScenario) stageIngestSOP(ctx context.Context, result *Result) error {
	sopContent := `---
category: sop
scope: all
severity: warning
applies_to:
  - "**/*.go"
domain:
  - error-handling
  - logging
requirements:
  - "All errors must be wrapped with fmt.Errorf context"
  - "Use structured logging with slog"
  - "Functions performing I/O must accept context.Context as first parameter"
---

# Go Error Handling SOP

## Purpose

Ensure consistent error handling and observability across all Go code.

## Rules

1. Always wrap errors with context using fmt.Errorf("operation: %w", err)
2. Use log/slog for structured logging — never fmt.Println for diagnostics
3. Pass context.Context as first parameter for any function doing I/O
4. Return errors to callers — do not log-and-swallow
`

	// Write SOP document to sources directory
	if err := s.fs.WriteFileRelative(".semspec/sources/docs/go-error-handling.md", sopContent); err != nil {
		return fmt.Errorf("write SOP file: %w", err)
	}

	// Publish ingestion request to JetStream
	req := source.IngestRequest{
		Path:      "go-error-handling.md",
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
func (s *NewDeveloperScenario) stageVerifySOPIngested(ctx context.Context, result *Result) error {
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

			// Look for entries containing source.doc predicates (SOP entities)
			sopEntities := 0
			for _, entry := range entries {
				raw := string(entry.RawData)
				if strings.Contains(raw, "source.doc.category") {
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
func (s *NewDeveloperScenario) stageCreatePlan(ctx context.Context, result *Result) error {
	resp, err := s.http.CreatePlan(ctx, "add greeting personalization")
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
func (s *NewDeveloperScenario) stageWaitForPlan(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	if err := s.fs.WaitForPlan(ctx, slug); err != nil {
		return fmt.Errorf("plan directory not created: %w", err)
	}

	if err := s.fs.WaitForPlanFile(ctx, slug, "plan.json"); err != nil {
		return fmt.Errorf("plan.json not created: %w", err)
	}

	// Poll until plan.json has a non-empty "goal" field, meaning the LLM finished.
	// The file appears immediately with a skeleton, but Goal/Context/Scope are
	// populated asynchronously by the planner agent loop.
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

// stageVerifyPlanQuality reads plan.json and verifies it has meaningful content.
func (s *NewDeveloperScenario) stageVerifyPlanQuality(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")
	planPath := s.fs.DefaultProjectPlanPath(slug) + "/plan.json"

	var plan map[string]any
	if err := s.fs.ReadJSON(planPath, &plan); err != nil {
		return fmt.Errorf("read plan.json: %w", err)
	}

	if len(plan) == 0 {
		return fmt.Errorf("plan.json is empty")
	}

	// Verify the LLM populated the required fields
	goal, _ := plan["goal"].(string)
	if goal == "" {
		return fmt.Errorf("plan.json missing 'goal' field (LLM may not have finished)")
	}

	result.SetDetail("plan_id", plan["id"])
	result.SetDetail("plan_goal", goal)
	result.SetDetail("plan_data_present", true)

	// Check if plan context mentions SOPs (best-effort — warn if missing)
	planJSON, _ := json.Marshal(plan)
	planStr := string(planJSON)
	if strings.Contains(planStr, "sop") || strings.Contains(planStr, "SOP") || strings.Contains(planStr, "error-handling") || strings.Contains(planStr, "source.doc") {
		result.SetDetail("plan_references_sops", true)
	} else {
		result.AddWarning("plan context does not appear to reference SOPs — context-builder may not have included them")
		result.SetDetail("plan_references_sops", false)
	}

	return nil
}

// stageApprovePlan approves the plan via the REST API.
// The promote endpoint now triggers a plan review before approving.
// Both 200 (approved) and 422 (needs_changes) are valid pipeline outcomes.
func (s *NewDeveloperScenario) stageApprovePlan(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	resp, err := s.http.PromotePlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("promote plan: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("promote returned error: %s", resp.Error)
	}

	// Record review details regardless of verdict
	result.SetDetail("review_verdict", resp.ReviewVerdict)
	result.SetDetail("review_summary", resp.ReviewSummary)
	result.SetDetail("review_stage", resp.Stage)
	result.SetDetail("review_findings_count", len(resp.ReviewFindings))

	if resp.NeedsChanges() {
		// Plan was reviewed and rejected — this is a valid outcome
		// Record findings for the report and continue (don't fail the stage)
		result.AddWarning(fmt.Sprintf("plan review returned needs_changes: %s", resp.ReviewSummary))
		for i, f := range resp.ReviewFindings {
			result.SetDetail(fmt.Sprintf("finding_%d", i), map[string]string{
				"sop_id":   f.SOPID,
				"severity": f.Severity,
				"status":   f.Status,
				"issue":    f.Issue,
			})
		}
		result.SetDetail("approve_response", resp)
		return nil
	}

	// Plan was approved (possibly with review, possibly without if reviewer not running)
	result.SetDetail("approve_response", resp)
	return nil
}

// stageGenerateTasks triggers LLM-based task generation via the REST API.
func (s *NewDeveloperScenario) stageGenerateTasks(ctx context.Context, result *Result) error {
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
func (s *NewDeveloperScenario) stageWaitForTasks(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	if err := s.fs.WaitForPlanFile(ctx, slug, "tasks.json"); err != nil {
		return fmt.Errorf("tasks.json not created: %w", err)
	}

	return nil
}

// stageVerifyTasksQuality reads tasks.json and verifies it has at least one valid task.
func (s *NewDeveloperScenario) stageVerifyTasksQuality(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")
	tasksPath := s.fs.DefaultProjectPlanPath(slug) + "/tasks.json"

	var tasks []map[string]any
	if err := s.fs.ReadJSON(tasksPath, &tasks); err != nil {
		return fmt.Errorf("read tasks.json: %w", err)
	}

	if len(tasks) == 0 {
		return fmt.Errorf("tasks.json contains no tasks")
	}

	for i, task := range tasks {
		desc, ok := task["description"].(string)
		if !ok || desc == "" {
			return fmt.Errorf("task %d missing non-empty 'description' field", i)
		}
	}

	result.SetDetail("task_count", len(tasks))
	return nil
}

// stageCaptureTrajectory polls the LLM_CALLS KV bucket and retrieves trajectory data.
func (s *NewDeveloperScenario) stageCaptureTrajectory(ctx context.Context, result *Result) error {
	ticker := time.NewTicker(kvPollInterval)
	defer ticker.Stop()

	var kvEntries *client.KVEntriesResponse
	var lastErr error

	// Poll until entries appear or context times out
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

	// Extract trace ID from the first key (format: trace_id.request_id)
	firstKey := kvEntries.Entries[0].Key
	parts := strings.SplitN(firstKey, ".", 2)
	if len(parts) < 2 {
		result.AddWarning(fmt.Sprintf("LLM_CALLS key %q doesn't contain trace prefix", firstKey))
		return nil
	}

	traceID := parts[0]
	result.SetDetail("trajectory_trace_id", traceID)

	// Query trajectory data by trace ID
	trajectory, statusCode, err := s.http.GetTrajectoryByTrace(ctx, traceID, true)
	if err != nil {
		if statusCode == 404 {
			result.AddWarning("trajectory-api returned 404 - component may not be enabled")
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
func (s *NewDeveloperScenario) stageGenerateReport(ctx context.Context, result *Result) error {
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
		"model_calls":   modelCalls,
		"tokens_in":     tokensIn,
		"tokens_out":    tokensOut,
		"duration_ms":   durationMs,
		"plan_created":  true,
		"tasks_created": taskCount,
	})
	return nil
}
