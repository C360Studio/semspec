package scenarios

// PlanStallRecoveryScenario tests the state-machine stall recovery endpoints.
//
// All three variants share a common pipeline:
//  1. setup-project       — Write fixture Python Flask project, init git, purge stale KV.
//  2. create-plan         — POST /plans with a /goodbye endpoint description.
//  3. wait-for-approval   — Poll plan until status reaches "approved".
//  4. trigger-execution   — POST /plans/{slug}/execute.
//  5. wait-for-stall      — Poll EXECUTION_STATES KV until at least one requirement
//     has stage "failed" or "error"; confirm plan remains in "implementing".
//  6. verify-stall-state  — Assert plan.Status == "implementing", record counts.
//
// Variant-specific recovery stages follow, then verify-mock-stats.
//
// Variants:
//
//	StallRecoveryRetry    — POST /retry scope=failed, wait for complete/reviewing_rollup.
//	StallRecoveryComplete — POST /complete (force-complete), verify status.
//	StallRecoveryReject   — POST /reject → verify rejected → POST /retry scope=all →
//	                        wait for implementing/ready_for_execution.

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

// StallRecoveryAction selects the recovery path exercised by the scenario.
type StallRecoveryAction int

const (
	// StallRecoveryRetry exercises POST /plans/{slug}/retry with scope=failed.
	StallRecoveryRetry StallRecoveryAction = iota
	// StallRecoveryComplete exercises POST /plans/{slug}/complete (force-complete).
	StallRecoveryComplete
	// StallRecoveryReject exercises POST /plans/{slug}/reject then POST /retry.
	StallRecoveryReject
)

// PlanStallRecoveryScenario tests stall recovery endpoints after a requirement fails.
type PlanStallRecoveryScenario struct {
	name        string
	description string
	action      StallRecoveryAction
	config      *config.Config
	http        *client.HTTPClient
	nats        *client.NATSClient
	fs          *client.FilesystemClient
	mockLLM     *client.MockLLMClient
}

// NewPlanStallRecoveryScenario creates a stall recovery scenario for the given action.
func NewPlanStallRecoveryScenario(cfg *config.Config, action StallRecoveryAction) *PlanStallRecoveryScenario {
	var name, description string
	switch action {
	case StallRecoveryRetry:
		name = "plan-stall-retry"
		description = "Execution stall: failed requirement → POST /retry scope=failed → re-execution succeeds"
	case StallRecoveryComplete:
		name = "plan-stall-complete"
		description = "Execution stall: failed requirement → POST /complete force-completes the plan"
	case StallRecoveryReject:
		name = "plan-stall-reject"
		description = "Execution stall: failed requirement → POST /reject → POST /retry scope=all → re-execution"
	}
	return &PlanStallRecoveryScenario{
		name:        name,
		description: description,
		action:      action,
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *PlanStallRecoveryScenario) Name() string { return s.name }

// Description returns the scenario description.
func (s *PlanStallRecoveryScenario) Description() string { return s.description }

// Setup prepares the scenario environment.
func (s *PlanStallRecoveryScenario) Setup(ctx context.Context) error {
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

// Teardown cleans up the scenario environment.
func (s *PlanStallRecoveryScenario) Teardown(ctx context.Context) error {
	if s.nats != nil {
		return s.nats.Close(ctx)
	}
	return nil
}

// Execute runs all stages for the selected recovery variant.
func (s *PlanStallRecoveryScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	t := s.timeout

	shared := []stageDefinition{
		{"setup-project", s.stageSetupProject, t(30, 15)},
		{"create-plan", s.stageCreatePlan, t(15, 10)},
		{"wait-for-approval", s.stageWaitForApproval, t(300, 120)},
		{"trigger-execution", s.stageTriggerExecution, t(15, 10)},
		{"wait-for-stall", s.stageWaitForStall, t(300, 120)},
		{"verify-stall-state", s.stageVerifyStallState, t(15, 10)},
	}

	var recovery []stageDefinition
	switch s.action {
	case StallRecoveryRetry:
		recovery = []stageDefinition{
			{"retry-failed", s.stageRetryFailed, t(15, 10)},
			{"wait-for-complete", s.stageWaitForComplete, t(300, 120)},
			{"verify-completion", s.stageVerifyCompletion, t(15, 10)},
		}
	case StallRecoveryComplete:
		recovery = []stageDefinition{
			{"force-complete", s.stageForceComplete, t(15, 10)},
			{"verify-force-completed", s.stageVerifyForceCompleted, t(30, 15)},
		}
	case StallRecoveryReject:
		recovery = []stageDefinition{
			{"reject-plan", s.stageRejectPlan, t(15, 10)},
			{"verify-rejected", s.stageVerifyRejected, t(15, 10)},
			{"retry-from-rejected", s.stageRetryFromRejected, t(15, 10)},
			{"wait-for-reimplementation", s.stageWaitForReimplementation, t(60, 30)},
		}
	}

	tail := []stageDefinition{
		{"verify-mock-stats", s.stageVerifyMockStats, t(10, 5)},
	}

	stages := append(append(shared, recovery...), tail...)

	for _, stage := range stages {
		stageCtx, cancel := context.WithTimeout(ctx, stage.timeout)
		start := time.Now()
		err := stage.fn(stageCtx, result)
		duration := time.Since(start)
		cancel()

		result.SetMetric(fmt.Sprintf("%s_duration_us", stage.name), duration.Microseconds())
		if err != nil {
			result.AddStage(stage.name, false, duration, err.Error())
			result.AddError(fmt.Sprintf("%s: %v", stage.name, err))
			result.Error = fmt.Sprintf("%s failed: %v", stage.name, err)
			return result, nil
		}
		result.AddStage(stage.name, true, duration, "")
	}

	result.Success = true
	return result, nil
}

// timeout returns normalSec as a Duration, or fastSec when FastTimeouts is set.
func (s *PlanStallRecoveryScenario) timeout(normalSec, fastSec int) time.Duration {
	if s.config.FastTimeouts {
		return time.Duration(fastSec) * time.Second
	}
	return time.Duration(normalSec) * time.Second
}

// ---------------------------------------------------------------------------
// Shared stages
// ---------------------------------------------------------------------------

func (s *PlanStallRecoveryScenario) stageSetupProject(ctx context.Context, result *Result) error {
	// Purge stale KV entries so previous test runs don't pollute this one.
	for _, bucket := range []string{"PLAN_STATES", "EXECUTION_STATES"} {
		if deleted, err := s.nats.PurgeKVByPrefix(ctx, bucket, ""); err != nil {
			return fmt.Errorf("purge %s: %w", bucket, err)
		} else if deleted > 0 {
			result.SetDetail("purged_"+strings.ToLower(bucket), deleted)
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
	requirements := "flask\npytest\n"

	makefile := "test:\n\tcd api && python3 -m pytest . -q 2>/dev/null || true\n\nbuild:\n\t@echo 'no build step'\n\nlint:\n\t@echo 'no lint step'\n"
	files := map[string]string{
		filepath.Join(s.config.WorkspacePath, "api", "app.py"):           appPy,
		filepath.Join(s.config.WorkspacePath, "api", "requirements.txt"): requirements,
		filepath.Join(s.config.WorkspacePath, "README.md"):               "# Hello World\nA simple Python API project.\n",
		filepath.Join(s.config.WorkspacePath, "Makefile"):                makefile,
	}
	for path, content := range files {
		if err := s.fs.WriteFile(path, content); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
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

func (s *PlanStallRecoveryScenario) stageCreatePlan(ctx context.Context, result *Result) error {
	resp, err := s.http.CreatePlan(ctx, "add a /goodbye endpoint that returns a goodbye message")
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
	result.SetDetail("plan_trace_id", resp.TraceID)
	return nil
}

func (s *PlanStallRecoveryScenario) stageWaitForApproval(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("plan approval timed out: %w", ctx.Err())
		case <-ticker.C:
			plan, err := s.http.GetPlan(ctx, slug)
			if err != nil {
				continue
			}
			if plan.Approved {
				result.SetDetail("plan_approved", true)
				result.SetDetail("plan_stage", plan.Stage)
				return nil
			}
			switch plan.Stage {
			case "escalated", "error", "rejected":
				return fmt.Errorf("plan reached terminal state before approval: %s", plan.Stage)
			}
		}
	}
}

func (s *PlanStallRecoveryScenario) stageTriggerExecution(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	resp, err := s.http.ExecutePlan(ctx, slug)
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

// stageWaitForStall polls EXECUTION_STATES until at least one requirement entry
// has stage "failed" or "error". It also confirms the plan itself is still in
// "implementing" — not prematurely rejected or completed.
func (s *PlanStallRecoveryScenario) stageWaitForStall(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")
	prefix := "req." + slug + "."

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("no failed requirement found in EXECUTION_STATES after timeout: %w", ctx.Err())
		case <-ticker.C:
			kvResp, err := s.http.GetKVEntries(ctx, "EXECUTION_STATES")
			if err != nil {
				continue
			}

			failed := 0
			for _, entry := range kvResp.Entries {
				if !strings.HasPrefix(entry.Key, prefix) {
					continue
				}
				stage := kvEntryStage(entry.Value)
				if stage == "failed" || stage == "error" {
					failed++
				}
			}
			if failed == 0 {
				continue
			}

			// Confirm the plan is implementing, not already terminated.
			plan, err := s.http.GetPlan(ctx, slug)
			if err != nil {
				continue
			}
			if plan.Status != "implementing" {
				return fmt.Errorf("expected plan status=implementing during stall, got %s", plan.Status)
			}

			result.SetDetail("stall_failed_count", failed)
			result.SetDetail("stall_plan_status", plan.Status)
			return nil
		}
	}
}

func (s *PlanStallRecoveryScenario) stageVerifyStallState(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	plan, err := s.http.GetPlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}
	if plan.Status != "implementing" {
		return fmt.Errorf("expected plan status=implementing during stall, got %s", plan.Status)
	}

	kvResp, err := s.http.GetKVEntries(ctx, "EXECUTION_STATES")
	if err != nil {
		return fmt.Errorf("get EXECUTION_STATES: %w", err)
	}

	prefix := "req." + slug + "."
	var completed, failed int
	for _, entry := range kvResp.Entries {
		if !strings.HasPrefix(entry.Key, prefix) {
			continue
		}
		switch kvEntryStage(entry.Value) {
		case "complete", "approved":
			completed++
		case "failed", "error":
			failed++
		}
	}

	result.SetDetail("stall_completed_reqs", completed)
	result.SetDetail("stall_failed_reqs", failed)
	result.SetDetail("stall_verified", true)
	return nil
}

// ---------------------------------------------------------------------------
// StallRecoveryRetry stages
// ---------------------------------------------------------------------------

func (s *PlanStallRecoveryScenario) stageRetryFailed(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	retryResp, err := s.http.RetryPlan(ctx, slug, "failed")
	if err != nil {
		return fmt.Errorf("retry plan: %w", err)
	}
	if !retryResp.Success {
		return fmt.Errorf("retry returned success=false")
	}
	if retryResp.ResetCount < 1 {
		return fmt.Errorf("expected reset_count >= 1, got %d", retryResp.ResetCount)
	}

	result.SetDetail("retry_reset_count", retryResp.ResetCount)
	result.SetDetail("retry_scope", retryResp.Scope)
	return nil
}

func (s *PlanStallRecoveryScenario) stageWaitForComplete(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	var lastStatus string
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("plan did not reach complete after retry (last status: %s): %w", lastStatus, ctx.Err())
		case <-ticker.C:
			plan, err := s.http.GetPlan(ctx, slug)
			if err != nil {
				continue
			}
			lastStatus = plan.Status
			switch plan.Status {
			case "complete", "reviewing_rollup":
				result.SetDetail("post_retry_status", plan.Status)
				return nil
			case "rejected", "error":
				return fmt.Errorf("plan reached terminal failure after retry: %s", plan.Status)
			}
		}
	}
}

func (s *PlanStallRecoveryScenario) stageVerifyCompletion(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	plan, err := s.http.GetPlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}
	switch plan.Status {
	case "complete", "reviewing_rollup":
		result.SetDetail("final_status", plan.Status)
		return nil
	default:
		return fmt.Errorf("expected terminal complete status, got %s", plan.Status)
	}
}

// ---------------------------------------------------------------------------
// StallRecoveryComplete stages
// ---------------------------------------------------------------------------

func (s *PlanStallRecoveryScenario) stageForceComplete(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	plan, err := s.http.ForceCompletePlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("force complete plan: %w", err)
	}

	result.SetDetail("force_complete_status", plan.Status)
	return nil
}

func (s *PlanStallRecoveryScenario) stageVerifyForceCompleted(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	deadline := time.Now().Add(30 * time.Second)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("plan did not reach complete after force-complete: %w", ctx.Err())
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("plan did not reach complete after force-complete within 30s")
			}
			plan, err := s.http.GetPlan(ctx, slug)
			if err != nil {
				continue
			}
			switch plan.Status {
			case "complete", "reviewing_rollup":
				result.SetDetail("force_complete_final_status", plan.Status)
				return nil
			case "rejected", "error":
				return fmt.Errorf("plan reached unexpected terminal state after force-complete: %s", plan.Status)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// StallRecoveryReject stages
// ---------------------------------------------------------------------------

func (s *PlanStallRecoveryScenario) stageRejectPlan(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	plan, err := s.http.RejectPlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("reject plan: %w", err)
	}

	result.SetDetail("reject_response_status", plan.Status)
	return nil
}

func (s *PlanStallRecoveryScenario) stageVerifyRejected(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	plan, err := s.http.GetPlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}
	if plan.Status != "rejected" {
		return fmt.Errorf("expected status=rejected, got %s", plan.Status)
	}

	result.SetDetail("rejected_verified", true)
	return nil
}

func (s *PlanStallRecoveryScenario) stageRetryFromRejected(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	retryResp, err := s.http.RetryPlan(ctx, slug, "all")
	if err != nil {
		return fmt.Errorf("retry from rejected: %w", err)
	}
	if !retryResp.Success {
		return fmt.Errorf("retry from rejected returned success=false")
	}

	result.SetDetail("retry_from_rejected_reset_count", retryResp.ResetCount)
	return nil
}

func (s *PlanStallRecoveryScenario) stageWaitForReimplementation(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastStatus string
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("plan did not reach implementing/ready_for_execution after retry (last: %s): %w",
				lastStatus, ctx.Err())
		case <-ticker.C:
			plan, err := s.http.GetPlan(ctx, slug)
			if err != nil {
				continue
			}
			lastStatus = plan.Status
			switch plan.Status {
			case "implementing", "ready_for_execution":
				result.SetDetail("reimplementation_status", plan.Status)
				return nil
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Mock stats stage (all variants)
// ---------------------------------------------------------------------------

func (s *PlanStallRecoveryScenario) stageVerifyMockStats(ctx context.Context, result *Result) error {
	if s.mockLLM == nil {
		return nil
	}

	stats, err := s.mockLLM.GetStats(ctx)
	if err != nil {
		result.AddWarning(fmt.Sprintf("could not fetch mock LLM stats: %v", err))
		return nil
	}

	result.SetDetail("mock_total_calls", stats.TotalCalls)
	result.SetDetail("mock_calls_by_model", stats.CallsByModel)

	var summary []string
	for model, count := range stats.CallsByModel {
		summary = append(summary, fmt.Sprintf("%s=%d", model, count))
	}
	result.SetDetail("mock_call_summary", strings.Join(summary, ", "))
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// kvEntryStage extracts the "stage" string field from a raw KV entry value.
// The value is a JSON object; returns "" if the field is absent or unparseable.
func kvEntryStage(raw json.RawMessage) string {
	var obj struct {
		Stage string `json:"stage"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	return obj.Stage
}
