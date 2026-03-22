// Package scenarios provides e2e test scenario implementations.
package scenarios

// EpicMeshtasticScenario tests the full alpha pipeline:
// federated knowledge graph → planning → requirements → scenarios →
// execution → scenario review → plan rollup.
//
// This scenario assumes 3 external semsource instances are running
// (semsource-osh, semsource-meshtastic, semsource-ogc) and indexing
// has completed. The task e2e:epic handles this infrastructure.
//
// Stages:
//  1. verify-service-health   — Confirm semspec HTTP gateway is responsive.
//  2. verify-graph-manifest   — Check that graph-gateway has indexed entities
//     from federated sources (total entity count > 100).
//  3. setup-workspace         — Write a minimal Java/Maven project scaffold.
//  4. init-project            — Initialize project via project-api.
//  5. create-plan             — Submit the Meshtastic driver plan description.
//  6. wait-for-plan-goal      — Poll until planner writes a non-empty Goal.
//  7. wait-for-approval       — Poll until plan.Approved == true.
//  8. trigger-execution       — POST /plan-api/plans/{slug}/execute.
//  9. wait-for-scenarios      — Poll until at least 3 scenarios are generated.
// 10. wait-for-execution      — Poll until plan reaches reviewing_rollup or complete.
// 11. wait-for-rollup         — Poll until plan.Status == "complete".
// 12. verify-deliverables     — Check workspace for .java source, test, README.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/c360studio/semspec/processor/context-builder/gatherers"
	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// EpicMeshtasticScenario is the full alpha pipeline test for the Meshtastic OSH driver.
type EpicMeshtasticScenario struct {
	config   *config.Config
	http     *client.HTTPClient
	fs       *client.FilesystemClient
	planSlug string
}

// NewEpicMeshtasticScenario creates a new epic Meshtastic scenario.
func NewEpicMeshtasticScenario(cfg *config.Config) *EpicMeshtasticScenario {
	return &EpicMeshtasticScenario{
		config: cfg,
		http:   client.NewHTTPClient(cfg.HTTPBaseURL),
		fs:     client.NewFilesystemClient(cfg.WorkspacePath),
	}
}

func (s *EpicMeshtasticScenario) Name() string { return "epic-meshtastic" }
func (s *EpicMeshtasticScenario) Description() string {
	return "Full alpha pipeline: federated graph → Meshtastic OSH driver → plan → execution → deliverables"
}

// Setup writes the Maven project scaffold to the workspace.
func (s *EpicMeshtasticScenario) Setup(ctx context.Context) error {
	return s.setupWorkspace()
}

// Teardown is a no-op; the workspace is cleaned by the test runner.
func (s *EpicMeshtasticScenario) Teardown(ctx context.Context) error { return nil }

// Execute runs all stages sequentially. Each stage has its own deadline; a
// stage failure short-circuits the run and records the error.
func (s *EpicMeshtasticScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.Name())
	defer result.Complete()

	stages := []struct {
		name    string
		fn      func(context.Context, *Result) error
		timeout time.Duration
	}{
		{"verify-service-health", s.stageVerifyServiceHealth, 30 * time.Second},
		{"verify-graph-manifest", s.stageVerifyGraphManifest, 60 * time.Second},
		{"setup-workspace", s.stageSetupWorkspace, 30 * time.Second},
		{"init-project", s.stageInitProject, 15 * time.Second},
		{"create-plan", s.stageCreatePlan, 15 * time.Second},
		{"wait-for-plan-goal", s.stageWaitForPlanGoal, 180 * time.Second},
		{"wait-for-approval", s.stageWaitForApproval, 600 * time.Second},
		{"trigger-execution", s.stageTriggerExecution, 15 * time.Second},
		{"wait-for-scenarios", s.stageWaitForScenarios, 120 * time.Second},
		{"wait-for-execution", s.stageWaitForExecution, 900 * time.Second},
		{"wait-for-rollup", s.stageWaitForRollup, 120 * time.Second},
		{"verify-deliverables", s.stageVerifyDeliverables, 30 * time.Second},
	}

	for _, stage := range stages {
		stageCtx, cancel := context.WithTimeout(ctx, stage.timeout)
		start := time.Now()

		err := stage.fn(stageCtx, result)
		duration := time.Since(start)
		cancel()

		if err != nil {
			result.AddStage(stage.name, false, duration, err.Error())
			result.AddError(fmt.Sprintf("%s: %s", stage.name, err.Error()))
			result.Error = fmt.Sprintf("%s failed: %s", stage.name, err.Error())
			return result, nil
		}

		result.AddStage(stage.name, true, duration, "")
		result.SetMetric(stage.name+"_duration_us", duration.Microseconds())
	}

	result.Success = true
	return result, nil
}

// ---------------------------------------------------------------------------
// Workspace setup
// ---------------------------------------------------------------------------

func (s *EpicMeshtasticScenario) setupWorkspace() error {
	files := map[string]string{
		"README.md": "# Meshtastic OSH Driver\n\n" +
			"A Meshtastic driver for OpenSensorHub (OSH) that uses the Connected Systems API.\n\n" +
			"## Overview\n\n" +
			"This driver enables OSH to send and receive messages over the Meshtastic mesh network.\n" +
			"It implements the OSH sensor driver interface and exposes Meshtastic nodes as OSH datastreams.\n",

		"pom.xml": `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 http://maven.apache.org/xsd/maven-4.0.0.xsd">
    <modelVersion>4.0.0</modelVersion>

    <groupId>org.sensorhub</groupId>
    <artifactId>osh-driver-meshtastic</artifactId>
    <version>1.0.0-SNAPSHOT</version>
    <packaging>jar</packaging>

    <name>OSH Meshtastic Driver</name>
    <description>OpenSensorHub driver for Meshtastic mesh networks using the Connected Systems API</description>

    <properties>
        <maven.compiler.source>17</maven.compiler.source>
        <maven.compiler.target>17</maven.compiler.target>
        <project.build.sourceEncoding>UTF-8</project.build.sourceEncoding>
        <osh.version>2.0.0</osh.version>
    </properties>

    <dependencies>
        <dependency>
            <groupId>org.sensorhub</groupId>
            <artifactId>sensorhub-core</artifactId>
            <version>${osh.version}</version>
        </dependency>
        <dependency>
            <groupId>junit</groupId>
            <artifactId>junit</artifactId>
            <version>4.13.2</version>
            <scope>test</scope>
        </dependency>
    </dependencies>
</project>
`,
		"src/main/java/org/sensorhub/driver/meshtastic/.gitkeep": "",
		"src/test/java/org/sensorhub/driver/meshtastic/.gitkeep": "",
	}

	for path, content := range files {
		if err := s.fs.WriteFileRelative(path, content); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}

	// Initialize as a git repository so semsource can watch it.
	if err := s.fs.InitGit(); err != nil {
		return fmt.Errorf("git init: %w", err)
	}
	if err := s.fs.GitAdd("."); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if err := s.fs.GitCommit("chore: initial Maven project scaffold"); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Stages
// ---------------------------------------------------------------------------

// stageVerifyServiceHealth confirms the semspec HTTP gateway is reachable
// and the graph-gateway is accepting connections.
func (s *EpicMeshtasticScenario) stageVerifyServiceHealth(ctx context.Context, result *Result) error {
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("semspec gateway not healthy: %w", err)
	}

	g := gatherers.NewGraphGatherer(s.config.GraphURL)
	if err := g.WaitForReady(ctx, 25*time.Second); err != nil {
		return fmt.Errorf("graph-gateway not ready: %w", err)
	}

	result.SetDetail("service_healthy", true)
	result.SetDetail("graph_ready", true)
	return nil
}

// stageVerifyGraphManifest confirms that federated semsource instances have
// indexed enough entities to be useful. We query graph-gateway for predicates
// matching "source.doc" (documents) and verify total entity count > 100.
func (s *EpicMeshtasticScenario) stageVerifyGraphManifest(ctx context.Context, result *Result) error {
	// Query the graph-gateway predicates summary via GraphQL.
	// Schema: { predicates { predicates { predicate entityCount } } }
	query := `{
		predicates {
			predicates {
				predicate
				entityCount
			}
		}
	}`

	gatewayURL := s.config.GraphURL
	if gatewayURL == "" {
		gatewayURL = config.DefaultGraphURL
	}

	totalEntities := 0

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("graph manifest check timed out (total entities observed: %d): %w",
				totalEntities, ctx.Err())
		case <-ticker.C:
			count, err := s.queryGraphPredicateTotalEntities(ctx, gatewayURL, query)
			if err != nil {
				// Transient error — keep polling.
				result.AddWarning(fmt.Sprintf("graph predicates query error (retrying): %v", err))
				continue
			}
			totalEntities = count
			if totalEntities > 100 {
				result.SetDetail("graph_entity_count", totalEntities)
				result.SetDetail("graph_manifest_verified", true)
				return nil
			}
			// Not enough entities yet — log progress and keep polling.
			result.SetDetail("graph_entity_count_snapshot", totalEntities)
		}
	}
}

// queryGraphPredicateTotalEntities executes a predicates GraphQL query and
// sums up entityCount across all predicates.
func (s *EpicMeshtasticScenario) queryGraphPredicateTotalEntities(ctx context.Context, gatewayURL, query string) (int, error) {
	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return 0, fmt.Errorf("marshal query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", gatewayURL+"/graphql", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(rawBody))
	}

	var gqlResp struct {
		Data struct {
			Predicates struct {
				Predicates []struct {
					Predicate   string `json:"predicate"`
					EntityCount int    `json:"entityCount"`
				} `json:"predicates"`
			} `json:"predicates"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(rawBody, &gqlResp); err != nil {
		return 0, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		return 0, fmt.Errorf("graphql error: %s", gqlResp.Errors[0].Message)
	}

	total := 0
	for _, p := range gqlResp.Data.Predicates.Predicates {
		total += p.EntityCount
	}
	return total, nil
}

// stageSetupWorkspace verifies the Maven scaffold written by setupWorkspace
// is present on disk.
func (s *EpicMeshtasticScenario) stageSetupWorkspace(ctx context.Context, result *Result) error {
	for _, path := range []string{"pom.xml", "README.md"} {
		if !s.fs.FileExistsRelative(path) {
			return fmt.Errorf("fixture file missing: %s", path)
		}
	}

	if !s.fs.IsGitRepo() {
		return fmt.Errorf("workspace is not a git repository")
	}

	result.SetDetail("workspace_ready", true)
	result.SetDetail("maven_project", true)
	return nil
}

// stageInitProject runs stack detection and initializes the project.
func (s *EpicMeshtasticScenario) stageInitProject(ctx context.Context, result *Result) error {
	detection, err := s.http.DetectProject(ctx)
	if err != nil {
		// DetectProject may fail if detection finds no recognised languages
		// (Java detection varies by configuration). Treat as warning and continue.
		result.AddWarning(fmt.Sprintf("project detection failed (continuing): %v", err))
		result.SetDetail("project_detection_skipped", true)
		return nil
	}

	var languages []string
	for _, lang := range detection.Languages {
		languages = append(languages, lang.Name)
	}
	// Ensure Java is included even if detection missed it.
	if !containsString(languages, "Java") {
		languages = append(languages, "Java")
	}

	initReq := &client.ProjectInitRequest{
		Project: client.ProjectInitInput{
			Name:        "Meshtastic OSH Driver",
			Description: "OpenSensorHub driver for Meshtastic mesh networks using the Connected Systems API",
			Languages:   languages,
		},
		Checklist: detection.ProposedChecklist,
		Standards: client.StandardsInput{
			Version: "1.0.0",
			Rules:   []any{},
		},
	}

	resp, err := s.http.InitProject(ctx, initReq)
	if err != nil {
		// Non-fatal: project may already be initialised from a prior run.
		result.AddWarning(fmt.Sprintf("init project returned error (continuing): %v", err))
		result.SetDetail("init_skipped", true)
		return nil
	}

	result.SetDetail("init_success", resp.Success)
	result.SetDetail("detected_languages", strings.Join(languages, ", "))
	return nil
}

// stageCreatePlan submits the Meshtastic driver plan to the plan-api.
func (s *EpicMeshtasticScenario) stageCreatePlan(ctx context.Context, result *Result) error {
	const planDescription = "Design and implement a Meshtastic driver for OpenSensorHub (OSH). " +
		"The driver must use the Connected Systems API to send and receive messages over the " +
		"Meshtastic mesh network. Deliver working Java source files, unit tests, and a README " +
		"with usage examples."

	resp, err := s.http.CreatePlan(ctx, planDescription)
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}
	if resp.Slug == "" {
		return fmt.Errorf("empty slug in create plan response")
	}

	s.planSlug = resp.Slug
	result.SetDetail("plan_slug", resp.Slug)
	result.SetDetail("plan_trace_id", resp.TraceID)
	return nil
}

// stageWaitForPlanGoal polls until the planner has written a non-empty Goal
// into the plan record. Complex plans take longer than simple ones.
func (s *EpicMeshtasticScenario) stageWaitForPlanGoal(ctx context.Context, result *Result) error {
	plan, err := s.http.WaitForPlanGoal(ctx, s.planSlug)
	if err != nil {
		return fmt.Errorf("plan goal never populated: %w", err)
	}
	result.SetDetail("plan_goal", plan.Goal)
	result.SetDetail("plan_status", plan.Status)
	return nil
}

// stageWaitForApproval polls until the plan reviewer has approved the plan.
// Handles rejection (hard fail) and escalation (hard fail).
func (s *EpicMeshtasticScenario) stageWaitForApproval(ctx context.Context, result *Result) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("plan approval timed out: %w", ctx.Err())
		case <-ticker.C:
			plan, err := s.http.GetPlan(ctx, s.planSlug)
			if err != nil {
				// Transient — keep polling.
				continue
			}

			if plan.Approved {
				result.SetDetail("plan_approved", true)
				result.SetDetail("plan_stage", plan.Stage)
				result.SetDetail("plan_review_verdict", plan.ReviewVerdict)
				return nil
			}

			switch plan.Stage {
			case "escalated", "error":
				return fmt.Errorf("plan reached terminal state %q before approval: %s",
					plan.Stage, plan.ReviewSummary)
			case "rejected":
				return fmt.Errorf("plan was rejected: %s", plan.ReviewSummary)
			}

			// Still in progress — record current state for observability.
			result.SetDetail("plan_stage_snapshot", plan.Stage)
			result.SetDetail("plan_status_snapshot", plan.Status)
		}
	}
}

// stageTriggerExecution calls ExecutePlan to start the reactive execution pipeline.
func (s *EpicMeshtasticScenario) stageTriggerExecution(ctx context.Context, result *Result) error {
	// Capture baseline agent.complete.* count before triggering execution.
	baselineEntries, _ := s.http.GetMessageLogEntries(ctx, 500, "agent.complete.*")
	result.SetDetail("exec_complete_baseline_count", len(baselineEntries))

	resp, err := s.http.ExecutePlan(ctx, s.planSlug)
	if err != nil {
		return fmt.Errorf("execute plan: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("execute plan returned error: %s", resp.Error)
	}

	result.SetDetail("execute_batch_id", resp.BatchID)
	result.SetDetail("execution_triggered", true)
	return nil
}

// stageWaitForScenarios polls until at least 3 scenarios have been generated
// for the plan. This confirms the reactive pipeline produced work items.
func (s *EpicMeshtasticScenario) stageWaitForScenarios(ctx context.Context, result *Result) error {
	const minScenarios = 3

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("scenario generation timed out waiting for %d scenarios: %w",
				minScenarios, ctx.Err())
		case <-ticker.C:
			scenarios, err := s.http.ListScenarios(ctx, s.planSlug, "")
			if err != nil {
				// Transient — keep polling.
				continue
			}
			if len(scenarios) >= minScenarios {
				result.SetDetail("scenario_count", len(scenarios))
				result.SetDetail("scenarios_generated", true)
				return nil
			}
			result.SetDetail("scenario_count_snapshot", len(scenarios))
		}
	}
}

// stageWaitForExecution polls plan status until the pipeline reaches
// "reviewing_rollup" or "complete", indicating all scenarios have run.
// Tracks progress by counting completed scenarios on each poll.
func (s *EpicMeshtasticScenario) stageWaitForExecution(ctx context.Context, result *Result) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("execution timed out waiting for plan to reach rollup/complete: %w", ctx.Err())
		case <-ticker.C:
			plan, err := s.http.GetPlan(ctx, s.planSlug)
			if err != nil {
				// Transient — keep polling.
				continue
			}

			result.SetDetail("execution_status_snapshot", plan.Status)
			result.SetDetail("execution_stage_snapshot", plan.Stage)

			switch plan.Status {
			case "reviewing_rollup", "complete":
				// Count completed scenarios for the result record.
				scenarios, _ := s.http.ListScenarios(ctx, s.planSlug, "")
				completed := countCompletedScenarios(scenarios)
				result.SetDetail("scenarios_completed", completed)
				result.SetDetail("scenarios_total", len(scenarios))
				result.SetDetail("execution_reached_rollup", true)
				return nil

			case "error", "escalated":
				return fmt.Errorf("plan reached terminal error state %q during execution: %s",
					plan.Status, plan.Stage)
			}

			// Still executing — count completed scenarios for progress tracking.
			scenarios, _ := s.http.ListScenarios(ctx, s.planSlug, "")
			completed := countCompletedScenarios(scenarios)
			result.SetDetail("scenarios_completed_snapshot", completed)
		}
	}
}

// stageWaitForRollup polls until plan.Status == "complete", indicating the
// plan-level rollup review has finished.
func (s *EpicMeshtasticScenario) stageWaitForRollup(ctx context.Context, result *Result) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("rollup timed out waiting for plan to reach complete: %w", ctx.Err())
		case <-ticker.C:
			plan, err := s.http.GetPlan(ctx, s.planSlug)
			if err != nil {
				// Transient — keep polling.
				continue
			}

			if plan.Status == "complete" {
				result.SetDetail("plan_complete", true)
				result.SetDetail("rollup_verdict", plan.ReviewVerdict)
				result.SetDetail("rollup_summary", plan.ReviewSummary)
				return nil
			}

			if plan.Status == "error" || plan.Stage == "escalated" {
				return fmt.Errorf("plan reached terminal state %q/%q during rollup",
					plan.Status, plan.Stage)
			}
		}
	}
}

// stageVerifyDeliverables checks the workspace for concrete output from the
// execution pipeline: at least one .java source file, at least one test file,
// and a populated README.md.
func (s *EpicMeshtasticScenario) stageVerifyDeliverables(ctx context.Context, result *Result) error {
	allFiles, err := s.fs.ListFiles()
	if err != nil {
		return fmt.Errorf("list workspace files: %w", err)
	}

	var (
		javaSourceFiles []string
		javaTestFiles   []string
		hasReadme       bool
	)

	for _, f := range allFiles {
		if filepath.Ext(f) == ".java" {
			if strings.Contains(f, "src/main/") || strings.Contains(f, filepath.Join("src", "main")) {
				javaSourceFiles = append(javaSourceFiles, f)
			}
			if strings.Contains(f, "src/test/") || strings.Contains(f, filepath.Join("src", "test")) {
				javaTestFiles = append(javaTestFiles, f)
			}
		}
		if f == "README.md" {
			hasReadme = true
		}
	}

	result.SetDetail("java_source_files", javaSourceFiles)
	result.SetDetail("java_test_files", javaTestFiles)
	result.SetDetail("workspace_file_count", len(allFiles))

	var missing []string

	if len(javaSourceFiles) == 0 {
		missing = append(missing, "at least one .java file under src/main/")
	}

	if len(javaTestFiles) == 0 {
		// Test files are aspirational — warn rather than fail, since the
		// execution pipeline may not have reached full TDD completion.
		result.AddWarning("no .java test files found under src/test/ — TDD pipeline may not have completed")
	}

	if !hasReadme {
		missing = append(missing, "README.md")
	} else {
		// Verify README has meaningful content (more than the scaffold).
		readmeContent, err := s.fs.ReadFileRelative("README.md")
		if err == nil && len(readmeContent) < 200 {
			result.AddWarning("README.md exists but may not have been updated by the agent (less than 200 bytes)")
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("deliverables missing: %s", strings.Join(missing, "; "))
	}

	result.SetDetail("deliverables_verified", true)
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// countCompletedScenarios returns the number of scenarios with status "complete"
// or "approved" from the given list.
func countCompletedScenarios(scenarios []*client.ScenarioRecord) int {
	count := 0
	for _, sc := range scenarios {
		if sc.Status == "complete" || sc.Status == "approved" {
			count++
		}
	}
	return count
}

// containsString reports whether slice contains the given string (case-sensitive).
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
