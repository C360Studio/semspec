package scenarios

// PlanStateMachineScenario tests the retry/complete/reject endpoint guard clauses.
//
// These endpoints were added by the state-machine refactor (ADR commits 27cfb5f,
// 7ad07c3, 0ffcf7c). This Tier 1 scenario exercises error handling — invalid
// state, not found, invalid scope — without needing an LLM.

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// PlanStateMachineScenario tests plan retry/complete/reject guard clauses.
type PlanStateMachineScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
}

// NewPlanStateMachineScenario creates a new plan state machine scenario.
func NewPlanStateMachineScenario(cfg *config.Config) *PlanStateMachineScenario {
	return &PlanStateMachineScenario{
		name:        "plan-state-machine",
		description: "Tests plan retry/complete/reject endpoint guard clauses (no LLM)",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *PlanStateMachineScenario) Name() string { return s.name }

// Description returns the scenario description.
func (s *PlanStateMachineScenario) Description() string { return s.description }

// Setup prepares the scenario environment.
func (s *PlanStateMachineScenario) Setup(ctx context.Context) error {
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}
	return nil
}

// Execute runs all guard-clause test stages.
func (s *PlanStateMachineScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		// Create a plan in "created" status for wrong-state tests.
		{"create-plan", s.stageCreatePlan},

		// Wrong-state guards (plan is in "created", not implementing/rejected/etc.)
		{"retry-wrong-state", s.stageRetryWrongState},
		{"complete-wrong-state", s.stageCompleteWrongState},
		{"reject-wrong-state", s.stageRejectWrongState},

		// Not-found guards
		{"retry-not-found", s.stageRetryNotFound},
		{"complete-not-found", s.stageCompleteNotFound},
		{"reject-not-found", s.stageRejectNotFound},

		// Invalid scope
		{"retry-invalid-scope", s.stageRetryInvalidScope},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		stageCtx, cancel := context.WithTimeout(ctx, s.config.StageTimeout)

		err := stage.fn(stageCtx, result)
		cancel()

		duration := time.Since(stageStart)
		if err != nil {
			result.AddStage(stage.name, false, duration, err.Error())
			result.Error = fmt.Sprintf("stage %s failed: %v", stage.name, err)
			return result, nil
		}
		result.AddStage(stage.name, true, duration, "")
	}

	result.Success = true
	return result, nil
}

// Teardown is a no-op for this Tier 1 scenario.
func (s *PlanStateMachineScenario) Teardown(_ context.Context) error {
	return nil
}

// --- Stages ---

func (s *PlanStateMachineScenario) stageCreatePlan(ctx context.Context, result *Result) error {
	resp, err := s.http.CreatePlan(ctx, "state machine guard test plan")
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}
	if resp.Slug == "" {
		return fmt.Errorf("plan creation returned empty slug")
	}
	result.SetDetail("plan_slug", resp.Slug)
	return nil
}

func (s *PlanStateMachineScenario) stageRetryWrongState(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")
	code, body, err := s.http.RetryPlanRaw(ctx, slug, "failed")
	if err != nil {
		return fmt.Errorf("retry request failed: %w", err)
	}
	if code != 409 {
		return fmt.Errorf("expected 409 Conflict for retry on created plan, got %d: %s", code, body)
	}
	return nil
}

func (s *PlanStateMachineScenario) stageCompleteWrongState(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")
	code, body, err := s.http.ForceCompletePlanRaw(ctx, slug)
	if err != nil {
		return fmt.Errorf("complete request failed: %w", err)
	}
	if code != 409 {
		return fmt.Errorf("expected 409 Conflict for complete on created plan, got %d: %s", code, body)
	}
	return nil
}

func (s *PlanStateMachineScenario) stageRejectWrongState(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")
	code, body, err := s.http.RejectPlanRaw(ctx, slug)
	if err != nil {
		return fmt.Errorf("reject request failed: %w", err)
	}
	if code != 409 {
		return fmt.Errorf("expected 409 Conflict for reject on created plan, got %d: %s", code, body)
	}
	return nil
}

func (s *PlanStateMachineScenario) stageRetryNotFound(ctx context.Context, _ *Result) error {
	code, body, err := s.http.RetryPlanRaw(ctx, "nonexistent-plan-slug-xyz", "failed")
	if err != nil {
		return fmt.Errorf("retry request failed: %w", err)
	}
	if code != 404 {
		return fmt.Errorf("expected 404 Not Found for nonexistent plan, got %d: %s", code, body)
	}
	return nil
}

func (s *PlanStateMachineScenario) stageCompleteNotFound(ctx context.Context, _ *Result) error {
	code, body, err := s.http.ForceCompletePlanRaw(ctx, "nonexistent-plan-slug-xyz")
	if err != nil {
		return fmt.Errorf("complete request failed: %w", err)
	}
	if code != 404 {
		return fmt.Errorf("expected 404 Not Found for nonexistent plan, got %d: %s", code, body)
	}
	return nil
}

func (s *PlanStateMachineScenario) stageRejectNotFound(ctx context.Context, _ *Result) error {
	code, body, err := s.http.RejectPlanRaw(ctx, "nonexistent-plan-slug-xyz")
	if err != nil {
		return fmt.Errorf("reject request failed: %w", err)
	}
	if code != 404 {
		return fmt.Errorf("expected 404 Not Found for nonexistent plan, got %d: %s", code, body)
	}
	return nil
}

func (s *PlanStateMachineScenario) stageRetryInvalidScope(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")
	code, body, err := s.http.RetryPlanRaw(ctx, slug, "bogus")
	if err != nil {
		return fmt.Errorf("retry request failed: %w", err)
	}
	if code != 400 {
		return fmt.Errorf("expected 400 Bad Request for invalid scope, got %d: %s", code, body)
	}
	return nil
}
