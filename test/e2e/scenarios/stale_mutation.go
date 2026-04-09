package scenarios

// StaleMutationScenario tests that stale generator mutations do not reject
// healthy plans. This is a Tier 1 scenario (no LLM needed).
//
// Background: with slow LLMs, a timeout-retry or component restart can cause
// a second generator agent to complete after the plan has already advanced.
// The late mutation must be rejected by plan-manager (invalid transition), and
// critically, the plan must NOT be driven to "rejected" by a stale
// generation.failed mutation.
//
// The test drives the plan through states via NATS mutations, then injects
// a stale requirements.generated mutation and verifies the plan survives.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
	"github.com/c360studio/semspec/workflow"
)

// StaleMutationScenario tests stale mutation resilience.
type StaleMutationScenario struct {
	name   string
	config *config.Config
	http   *client.HTTPClient
	nats   *client.NATSClient
}

// NewStaleMutationScenario creates the scenario.
func NewStaleMutationScenario(cfg *config.Config) *StaleMutationScenario {
	return &StaleMutationScenario{
		name:   "stale-mutation",
		config: cfg,
	}
}

// Name returns the scenario name.
func (s *StaleMutationScenario) Name() string { return s.name }

// Description returns the scenario description.
func (s *StaleMutationScenario) Description() string {
	return "Stale generator mutation must not reject healthy plan (Tier 1, no LLM)"
}

// Teardown is a no-op for this Tier 1 scenario.
func (s *StaleMutationScenario) Teardown(_ context.Context) error { return nil }

// Setup prepares HTTP and NATS clients.
func (s *StaleMutationScenario) Setup(ctx context.Context) error {
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

// Execute runs the stale mutation resilience stages.
func (s *StaleMutationScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"create-plan", s.stageCreatePlan},
		{"drive-to-generating-architecture", s.stageDriveToGeneratingArchitecture},
		{"stale-requirements-rejected", s.stageStaleRequirementsRejected},
		{"plan-survives", s.stagePlanSurvives},
	}

	for _, stage := range stages {
		start := time.Now()
		stageCtx, cancel := context.WithTimeout(ctx, s.config.StageTimeout)
		err := stage.fn(stageCtx, result)
		cancel()
		dur := time.Since(start)
		if err != nil {
			result.AddStage(stage.name, false, dur, err.Error())
			result.Error = fmt.Sprintf("stage %s failed: %v", stage.name, err)
			return result, nil
		}
		result.AddStage(stage.name, true, dur, "")
	}

	result.Success = true
	return result, nil
}

// --- helpers ---

// mutationRequest sends a NATS request/reply mutation and returns the response.
func (s *StaleMutationScenario) mutationRequest(ctx context.Context, subject string, payload any) (mutationResp, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return mutationResp{}, fmt.Errorf("marshal: %w", err)
	}
	msg, err := s.nats.Request(ctx, subject, data, 10*time.Second)
	if err != nil {
		return mutationResp{}, fmt.Errorf("request %s: %w", subject, err)
	}
	var resp mutationResp
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return mutationResp{}, fmt.Errorf("unmarshal response: %w", err)
	}
	return resp, nil
}

type mutationResp struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// --- stages ---

func (s *StaleMutationScenario) stageCreatePlan(ctx context.Context, result *Result) error {
	resp, err := s.http.CreatePlan(ctx, "stale mutation resilience test")
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}
	if resp.Slug == "" {
		return fmt.Errorf("empty slug")
	}
	result.SetDetail("slug", resp.Slug)
	return nil
}

// stageDriveToGeneratingArchitecture advances the plan through the state machine
// via NATS mutations: created → drafted → reviewed → approved → claim(generating_requirements)
// → requirements_generated → claim(generating_architecture).
func (s *StaleMutationScenario) stageDriveToGeneratingArchitecture(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("slug")

	// Wait for planner to pick up the plan. Since there's no mock LLM, the
	// planner may fail — that's fine. We just need the plan to exist in
	// PLAN_STATES. Poll until the plan appears via HTTP (the create endpoint
	// already persists it).
	if _, err := s.http.WaitForPlanCreated(ctx, slug); err != nil {
		return fmt.Errorf("plan not created: %w", err)
	}

	mutations := []struct {
		name    string
		subject string
		payload any
	}{
		{
			"drafted",
			"plan.mutation.drafted",
			map[string]string{"slug": slug, "title": "test", "goal": "test goal", "context": "test context"},
		},
		{
			"reviewed",
			"plan.mutation.reviewed",
			map[string]string{"slug": slug, "verdict": "approved", "summary": "looks good"},
		},
		{
			"approved",
			"plan.mutation.approved",
			map[string]string{"slug": slug},
		},
		{
			"claim generating_requirements",
			"plan.mutation.claim",
			map[string]string{"slug": slug, "status": string(workflow.StatusGeneratingRequirements)},
		},
		{
			"requirements_generated",
			"plan.mutation.requirements.generated",
			struct {
				Slug         string                 `json:"slug"`
				Requirements []workflow.Requirement `json:"requirements"`
			}{
				Slug: slug,
				Requirements: []workflow.Requirement{
					{
						ID:     fmt.Sprintf("requirement.%s.1", slug),
						PlanID: fmt.Sprintf("plan.%s", slug),
						Title:  "test requirement",
						Status: workflow.RequirementStatusActive,
					},
				},
			},
		},
		{
			"claim generating_architecture",
			"plan.mutation.claim",
			map[string]string{"slug": slug, "status": string(workflow.StatusGeneratingArchitecture)},
		},
	}

	for _, m := range mutations {
		resp, err := s.mutationRequest(ctx, m.subject, m.payload)
		if err != nil {
			return fmt.Errorf("%s: %w", m.name, err)
		}
		if !resp.Success {
			return fmt.Errorf("%s rejected: %s", m.name, resp.Error)
		}
	}

	return nil
}

// stageStaleRequirementsRejected sends a stale requirements.generated mutation
// while the plan is at generating_architecture. The mutation must fail with
// "invalid transition" — NOT reject the plan.
func (s *StaleMutationScenario) stageStaleRequirementsRejected(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("slug")

	stalePayload := struct {
		Slug         string                 `json:"slug"`
		Requirements []workflow.Requirement `json:"requirements"`
	}{
		Slug: slug,
		Requirements: []workflow.Requirement{
			{
				ID:     fmt.Sprintf("requirement.%s.99", slug),
				PlanID: fmt.Sprintf("plan.%s", slug),
				Title:  "stale requirement from late agent",
				Status: workflow.RequirementStatusActive,
			},
		},
	}

	resp, err := s.mutationRequest(ctx, "plan.mutation.requirements.generated", stalePayload)
	if err != nil {
		return fmt.Errorf("stale mutation request: %w", err)
	}

	// Must be rejected (invalid transition), not accepted.
	if resp.Success {
		return fmt.Errorf("stale mutation was accepted — state machine did not guard")
	}
	if resp.Error == "" {
		return fmt.Errorf("expected 'invalid transition' error, got empty error")
	}

	result.SetDetail("stale_mutation_error", resp.Error)
	return nil
}

// stagePlanSurvives verifies the plan is still at generating_architecture,
// NOT rejected. In the old code, the generator would call sendGenerationFailed
// after receiving the "invalid transition" error, which would reject the plan.
func (s *StaleMutationScenario) stagePlanSurvives(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("slug")

	plan, err := s.http.GetPlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}

	// The plan must still be in generating_architecture (or later), not rejected.
	if plan.Status == "rejected" {
		return fmt.Errorf("plan was rejected by stale mutation — race condition regression")
	}

	// Verify we're still at the expected state.
	if plan.Status != string(workflow.StatusGeneratingArchitecture) {
		return fmt.Errorf("expected status generating_architecture, got %s", plan.Status)
	}

	return nil
}
