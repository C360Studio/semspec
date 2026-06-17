package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
	"github.com/c360studio/semspec/workflow"
)

// ContractObservabilityScenario drives plan-manager's deterministic seams for
// contract authority and UI observability. It is Tier 1: no LLM calls are
// required, and the scenario asserts the HTTP surfaces the UI consumes.
type ContractObservabilityScenario struct {
	name   string
	config *config.Config
	http   *client.HTTPClient
	nats   *client.NATSClient
}

// NewContractObservabilityScenario creates the scenario.
func NewContractObservabilityScenario(cfg *config.Config) *ContractObservabilityScenario {
	return &ContractObservabilityScenario{
		name:   "contract-observability",
		config: cfg,
	}
}

// Name returns the scenario name.
func (s *ContractObservabilityScenario) Name() string { return s.name }

// Description returns the scenario description.
func (s *ContractObservabilityScenario) Description() string {
	return "Contract/recovery/topology/execution observability via plan-manager summaries (Tier 1, no LLM)"
}

// Setup prepares HTTP and NATS clients.
func (s *ContractObservabilityScenario) Setup(ctx context.Context) error {
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

// Teardown closes the NATS client.
func (s *ContractObservabilityScenario) Teardown(ctx context.Context) error {
	if s.nats != nil {
		return s.nats.Close(ctx)
	}
	return nil
}

// Execute runs the deterministic observability stages.
func (s *ContractObservabilityScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"create-plan", s.stageCreatePlan},
		{"drive-to-execution", s.stageDriveToExecution},
		{"assert-execution-observability", s.stageAssertExecutionObservability},
		{"scope-shrinkage-boundary", s.stageRecordScopeShrinkageBoundary},
		{"add-targeted-recovery", s.stageAddTargetedRecovery},
		{"assert-recovery-wait", s.stageAssertRecoveryWait},
		{"force-ready-for-qa", s.stageForceReadyForQA},
		{"start-qa-topology-failure", s.stageStartQATopologyFailure},
		{"reject-from-qa", s.stageRejectFromQA},
		{"assert-topology-recovery", s.stageAssertTopologyRecovery},
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

func (s *ContractObservabilityScenario) mutationRequest(ctx context.Context, subject string, payload any) (mutationResp, error) {
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

func (s *ContractObservabilityScenario) requireMutation(ctx context.Context, name, subject string, payload any) error {
	resp, err := s.mutationRequest(ctx, subject, payload)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if !resp.Success {
		return fmt.Errorf("%s rejected: %s", name, resp.Error)
	}
	return nil
}

func (s *ContractObservabilityScenario) stageCreatePlan(ctx context.Context, result *Result) error {
	title := fmt.Sprintf("contract observability e2e %d", time.Now().UnixNano())
	resp, err := s.http.CreatePlan(ctx, title)
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}
	if resp.Slug == "" {
		return fmt.Errorf("empty slug")
	}
	result.SetDetail("slug", resp.Slug)
	return nil
}

func (s *ContractObservabilityScenario) stageDriveToExecution(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("slug")
	plan, err := s.http.WaitForPlanCreated(ctx, slug)
	if err != nil {
		return fmt.Errorf("plan not created: %w", err)
	}
	result.SetDetail("plan_id", plan.ID)
	if err := s.ensureDraftingClaim(ctx, slug); err != nil {
		return err
	}

	reqID := fmt.Sprintf("requirement.%s.1", slug)
	storyID := fmt.Sprintf("story.%s.contract-observer", slug)
	taskID := fmt.Sprintf("task.%s.1.1.1", slug)
	scenarioID := fmt.Sprintf("scenario.%s.1.1", slug)
	result.SetDetail("requirement_id", reqID)
	result.SetDetail("story_id", storyID)
	result.SetDetail("task_id", taskID)

	now := time.Now().UTC()
	scope := &workflow.Scope{
		Create:     []string{"src/contract_observability.go", "src/contract_observability_test.go"},
		DoNotTouch: []string{"baseline/"},
	}
	requirements := []workflow.Requirement{
		{
			ID:             reqID,
			PlanID:         plan.ID,
			Title:          "Expose contract state truthfully",
			Description:    "The UI can tell planning, execution, recovery, and QA states apart.",
			Status:         workflow.RequirementStatusActive,
			CapabilityName: "execution-observability",
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}
	architecture := &workflow.ArchitectureDocument{
		TechnologyChoices: []workflow.TechChoice{
			{Category: "test-harness", Choice: "plan-manager mutations", Rationale: "Exercise public deterministic state transitions without LLM calls."},
		},
		ComponentBoundaries: []workflow.ComponentDef{
			{
				Name:                "contract-observer",
				Responsibility:      "Expose authoritative contract and recovery state to operators.",
				ImplementationFiles: []string{"src/contract_observability.go", "src/contract_observability_test.go"},
				Capabilities:        []string{"execution-observability"},
			},
		},
		DataFlow: "PLAN_STATES feeds phase summaries and UI detail panes.",
		Decisions: []workflow.ArchDecision{
			{ID: "ADR-E2E-001", Title: "Use authoritative phase summaries", Decision: "Read plan-manager phase summaries.", Rationale: "Avoid stale feed labels."},
		},
	}
	stories := []workflow.Story{
		{
			ID:             storyID,
			ComponentName:  "contract-observer",
			RequirementIDs: []string{reqID},
			CapabilityNames: []string{
				"execution-observability",
			},
			Title:      "Expose contract observability",
			Intent:     "Provide execution detail and recovery state without log inspection.",
			FilesOwned: []string{"src/contract_observability.go", "src/contract_observability_test.go"},
			Tasks: []workflow.Task{
				{ID: taskID, StoryID: storyID, Description: "Assert the plan summary carries execution and recovery state."},
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	scenarios := []workflow.Scenario{
		{
			ID:            scenarioID,
			RequirementID: reqID,
			StoryID:       storyID,
			Title:         "Operator sees current execution state",
			Given:         "A plan has entered execution.",
			When:          "The operator opens the plan detail view.",
			Then:          []string{"The current phase is execution.", "Stories and task evidence are visible."},
			Tags:          []string{workflow.TierUnit},
			CreatedAt:     now,
			UpdatedAt:     now,
		},
	}

	mutations := []struct {
		name    string
		subject string
		payload any
	}{
		{
			"drafted",
			"plan.mutation.drafted",
			struct {
				Slug        string          `json:"slug"`
				Title       string          `json:"title,omitempty"`
				Goal        string          `json:"goal"`
				Context     string          `json:"context"`
				Constraints []string        `json:"constraints,omitempty"`
				Scope       *workflow.Scope `json:"scope,omitempty"`
			}{
				Slug:        slug,
				Title:       "Contract observability",
				Goal:        "Expose authoritative live state in UI and API.",
				Context:     "Regression fixture for plan/execution/recovery/QA observability.",
				Constraints: []string{"Preserve baseline topology.", "Do not present stale feed rows as current execution."},
				Scope:       scope,
			},
		},
		{"reviewed", "plan.mutation.reviewed", map[string]string{"slug": slug, "verdict": "approved", "summary": "contract fixture approved"}},
		{"approved", "plan.mutation.approved", map[string]string{"slug": slug}},
		{"claim generating_requirements", "plan.mutation.claim", map[string]string{"slug": slug, "status": string(workflow.StatusGeneratingRequirements)}},
		{
			"requirements_generated",
			"plan.mutation.requirements.generated",
			struct {
				Slug         string                 `json:"slug"`
				Requirements []workflow.Requirement `json:"requirements"`
			}{Slug: slug, Requirements: requirements},
		},
		{"claim generating_architecture", "plan.mutation.claim", map[string]string{"slug": slug, "status": string(workflow.StatusGeneratingArchitecture)}},
		{
			"architecture_generated",
			"plan.mutation.architecture.generated",
			struct {
				Slug         string                         `json:"slug"`
				Architecture *workflow.ArchitectureDocument `json:"architecture,omitempty"`
			}{Slug: slug, Architecture: architecture},
		},
		{"claim preparing_stories", "plan.mutation.claim", map[string]string{"slug": slug, "status": string(workflow.StatusPreparingStories)}},
		{
			"stories_generated",
			"plan.mutation.stories.generated",
			struct {
				Slug       string           `json:"slug"`
				Stories    []workflow.Story `json:"stories"`
				StoryCount int              `json:"story_count,omitempty"`
			}{Slug: slug, Stories: stories, StoryCount: len(stories)},
		},
		{"claim generating_scenarios", "plan.mutation.claim", map[string]string{"slug": slug, "status": string(workflow.StatusGeneratingScenarios)}},
		{
			"scenarios_generated",
			"plan.mutation.scenarios.generated",
			struct {
				Slug          string              `json:"slug"`
				RequirementID string              `json:"requirement_id"`
				StoryID       string              `json:"story_id,omitempty"`
				Scenarios     []workflow.Scenario `json:"scenarios"`
			}{Slug: slug, RequirementID: reqID, StoryID: storyID, Scenarios: scenarios},
		},
	}

	for _, m := range mutations {
		if err := s.requireMutation(ctx, m.name, m.subject, m.payload); err != nil {
			return err
		}
	}
	if err := s.claimReviewingScenarios(ctx, slug); err != nil {
		return err
	}
	if err := s.markScenariosReviewed(ctx, slug); err != nil {
		return err
	}
	return s.markReadyForExecution(ctx, slug)
}

func (s *ContractObservabilityScenario) ensureDraftingClaim(ctx context.Context, slug string) error {
	plan, err := s.http.GetPlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("get plan before drafting: %w", err)
	}

	status := workflow.Status(plan.Status)
	switch status {
	case workflow.StatusCreated:
		if err := s.claimOrAlreadyAt(ctx, slug, workflow.StatusExploring); err != nil {
			return err
		}
		if err := s.markExplored(ctx, slug); err != nil {
			return err
		}
		return s.claimOrAlreadyAt(ctx, slug, workflow.StatusDrafting)
	case workflow.StatusExploring:
		if err := s.markExplored(ctx, slug); err != nil {
			return err
		}
		return s.claimOrAlreadyAt(ctx, slug, workflow.StatusDrafting)
	case workflow.StatusExplored:
		return s.claimOrAlreadyAt(ctx, slug, workflow.StatusDrafting)
	case workflow.StatusDrafting:
		return nil
	default:
		return fmt.Errorf("plan status = %s, want created/exploring/explored/drafting before drafted mutation", plan.Status)
	}
}

func (s *ContractObservabilityScenario) claimOrAlreadyAt(ctx context.Context, slug string, target workflow.Status) error {
	resp, err := s.mutationRequest(ctx, "plan.mutation.claim", map[string]string{"slug": slug, "status": string(target)})
	if err != nil {
		return fmt.Errorf("claim %s: %w", target, err)
	}
	if resp.Success {
		return nil
	}
	plan, getErr := s.http.GetPlan(ctx, slug)
	if getErr == nil && plan.Status == string(target) {
		return nil
	}
	return fmt.Errorf("claim %s rejected: %s", target, resp.Error)
}

func (s *ContractObservabilityScenario) markExplored(ctx context.Context, slug string) error {
	exploration := workflow.Exploration{
		Capabilities: []workflow.Capability{
			{
				Name:        "execution-observability",
				Lifecycle:   workflow.CapabilityModified,
				Description: "Surface truthful execution, recovery, QA, and stale-state evidence to operators.",
				Surfaces:    []workflow.CapabilitySurface{workflow.SurfaceUI, workflow.SurfaceAPI},
			},
		},
	}
	payload := struct {
		Slug        string               `json:"slug"`
		Exploration workflow.Exploration `json:"exploration"`
	}{Slug: slug, Exploration: exploration}
	resp, err := s.mutationRequest(ctx, "plan.mutation.explored", payload)
	if err != nil {
		return fmt.Errorf("explored: %w", err)
	}
	if resp.Success {
		return nil
	}
	plan, getErr := s.http.GetPlan(ctx, slug)
	if getErr == nil && plan.Status == string(workflow.StatusExplored) {
		return nil
	}
	return fmt.Errorf("explored rejected: %s", resp.Error)
}

func (s *ContractObservabilityScenario) claimReviewingScenarios(ctx context.Context, slug string) error {
	resp, err := s.mutationRequest(ctx, "plan.mutation.claim", map[string]string{"slug": slug, "status": string(workflow.StatusReviewingScenarios)})
	if err != nil {
		return fmt.Errorf("claim reviewing_scenarios: %w", err)
	}
	if resp.Success {
		return nil
	}
	plan, getErr := s.http.GetPlan(ctx, slug)
	if getErr != nil {
		return fmt.Errorf("claim reviewing_scenarios rejected: %s", resp.Error)
	}
	switch workflow.Status(plan.Status) {
	case workflow.StatusReviewingScenarios, workflow.StatusScenariosReviewed, workflow.StatusReadyForExecution, workflow.StatusImplementing:
		return nil
	default:
		return fmt.Errorf("claim reviewing_scenarios rejected: %s (current status %s)", resp.Error, plan.Status)
	}
}

func (s *ContractObservabilityScenario) markScenariosReviewed(ctx context.Context, slug string) error {
	resp, err := s.mutationRequest(ctx, "plan.mutation.scenarios.reviewed", map[string]string{"slug": slug, "summary": "contract fixture scenarios reviewed"})
	if err != nil {
		return fmt.Errorf("scenarios_reviewed: %w", err)
	}
	if resp.Success {
		return nil
	}
	plan, getErr := s.http.GetPlan(ctx, slug)
	if getErr != nil {
		return fmt.Errorf("scenarios_reviewed rejected: %s", resp.Error)
	}
	switch workflow.Status(plan.Status) {
	case workflow.StatusScenariosReviewed, workflow.StatusReadyForExecution, workflow.StatusImplementing:
		return nil
	default:
		return fmt.Errorf("scenarios_reviewed rejected: %s (current status %s)", resp.Error, plan.Status)
	}
}

func (s *ContractObservabilityScenario) markReadyForExecution(ctx context.Context, slug string) error {
	resp, err := s.mutationRequest(ctx, "plan.mutation.ready_for_execution", map[string]string{"slug": slug})
	if err != nil {
		return fmt.Errorf("ready_for_execution: %w", err)
	}
	if resp.Success {
		return nil
	}
	plan, getErr := s.http.GetPlan(ctx, slug)
	if getErr != nil {
		return fmt.Errorf("ready_for_execution rejected: %s", resp.Error)
	}
	switch workflow.Status(plan.Status) {
	case workflow.StatusReadyForExecution, workflow.StatusImplementing:
		return nil
	default:
		return fmt.Errorf("ready_for_execution rejected: %s (current status %s)", resp.Error, plan.Status)
	}
}

func (s *ContractObservabilityScenario) stageAssertExecutionObservability(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("slug")
	plan, err := s.http.GetPlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}
	if plan.PhaseSummary == nil {
		return fmt.Errorf("phase_summary missing")
	}
	if plan.PhaseSummary.Phase != "execution" {
		return fmt.Errorf("phase_summary.phase = %q, want execution (stage=%s status=%s)", plan.PhaseSummary.Phase, plan.Stage, plan.Status)
	}
	if plan.PhaseSummary.Stage != "implementing" && plan.PhaseSummary.Stage != "ready_for_execution" {
		return fmt.Errorf("phase_summary.stage = %q, want implementing or ready_for_execution", plan.PhaseSummary.Stage)
	}
	if len(plan.Stories) != 1 {
		return fmt.Errorf("len(stories) = %d, want 1", len(plan.Stories))
	}
	if len(plan.Stories[0].Tasks) != 1 {
		return fmt.Errorf("len(story.tasks) = %d, want 1", len(plan.Stories[0].Tasks))
	}
	result.SetDetail("execution_stage", plan.PhaseSummary.Stage)
	result.SetDetail("execution_phase_title", plan.PhaseSummary.Title)
	return nil
}

func (s *ContractObservabilityScenario) stageRecordScopeShrinkageBoundary(_ context.Context, result *Result) error {
	result.AddWarning("Scope-shrinkage blocking is covered by plan-manager unit tests because public plan creation seeds the root contract before scoped draft data exists; this E2E covers scope-change provenance through PlanDecision contract impact.")
	result.SetDetail("scope_shrinkage_public_seam", "backend-unit-covered")
	return nil
}

func (s *ContractObservabilityScenario) stageAddTargetedRecovery(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("slug")
	planID, _ := result.GetDetailString("plan_id")
	reqID, _ := result.GetDetailString("requirement_id")
	storyID, _ := result.GetDetailString("story_id")
	now := time.Now().UTC()
	decisionID := fmt.Sprintf("plan-decision.%s.story-reprepare", slug)
	result.SetDetail("targeted_recovery_decision_id", decisionID)

	decision := workflow.PlanDecision{
		ID:                 decisionID,
		PlanID:             planID,
		Kind:               workflow.PlanDecisionKindStoryReprepare,
		Title:              "Reprepare affected Story only",
		Rationale:          "A late execution wedge implicated one Story shape; unrelated completed work should remain intact.",
		Status:             workflow.PlanDecisionStatusProposed,
		ProposedBy:         "recovery-agent",
		AffectedReqIDs:     []string{reqID},
		AffectedStoryIDs:   []string{storyID},
		RejectionReasons:   map[string]string{"whole_phase_reset": "No evidence that unrelated Stories are invalid."},
		ContractImpact:     &workflow.ContractImpact{Kind: workflow.ContractImpactRefine, Summary: "Clarify Story ownership without changing the root scope.", AffectedIDs: []string{storyID}},
		ArtifactReferences: []workflow.ArtifactRef{{Path: ".semspec/debug/contract-observability/recovery.json", Type: "trace", Purpose: "Targeted recovery fixture"}},
		CreatedAt:          now,
	}
	payload := struct {
		Slug     string                `json:"slug"`
		Decision workflow.PlanDecision `json:"decision"`
	}{Slug: slug, Decision: decision}
	return s.requireMutation(ctx, "plan_decision.add", "plan.mutation.plan_decision.add", payload)
}

func (s *ContractObservabilityScenario) stageAssertRecoveryWait(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("slug")
	storyID, _ := result.GetDetailString("story_id")
	plan, err := s.http.GetPlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}
	if plan.PhaseSummary == nil || plan.PhaseSummary.Recovery == nil {
		return fmt.Errorf("recovery summary missing")
	}
	recovery := plan.PhaseSummary.Recovery
	if recovery.Kind != string(workflow.PlanDecisionKindStoryReprepare) {
		return fmt.Errorf("recovery.kind = %q, want story_reprepare", recovery.Kind)
	}
	if recovery.ContractImpactKind != string(workflow.ContractImpactRefine) {
		return fmt.Errorf("recovery.contract_impact_kind = %q, want refine", recovery.ContractImpactKind)
	}
	if !containsString(recovery.AffectedStoryIDs, storyID) {
		return fmt.Errorf("recovery affected_story_ids = %v, want %s", recovery.AffectedStoryIDs, storyID)
	}
	if plan.PhaseSummary.Wait == nil || plan.PhaseSummary.Wait.DecisionID != recovery.DecisionID {
		return fmt.Errorf("wait summary missing recovery decision: %+v", plan.PhaseSummary.Wait)
	}
	return nil
}

func (s *ContractObservabilityScenario) stageForceReadyForQA(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("slug")
	plan, err := s.http.ForceCompletePlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("force complete: %w", err)
	}
	if plan.Status != string(workflow.StatusReadyForQA) {
		return fmt.Errorf("force complete status = %q, want ready_for_qa", plan.Status)
	}
	return nil
}

func (s *ContractObservabilityScenario) stageStartQATopologyFailure(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("slug")
	planID, _ := result.GetDetailString("plan_id")
	runID := fmt.Sprintf("qa-%s-topology", slug)
	result.SetDetail("qa_run_id", runID)
	qaRun := &workflow.QARun{
		RunID:  runID,
		Passed: false,
		Failures: []workflow.QAFailure{
			{
				JobName:    "integration",
				StepName:   "gradle-configuration",
				Category:   workflow.QAFailureCategoryTopology,
				Message:    "standalone build root conflicts with the brownfield composite baseline",
				LogExcerpt: "duplicate root element while substituting osh-addons baseline",
			},
		},
		Artifacts: []workflow.QAArtifactRef{
			{Path: ".semspec/qa-artifacts/" + slug + "/" + runID + "/gradle.log", Type: "log", Purpose: "Topology/build configuration failure"},
		},
		DurationMs:  1200,
		TraceID:     "trace-" + runID,
		CompletedAt: time.Now().UTC(),
	}
	payload := struct {
		Slug   string          `json:"slug"`
		PlanID string          `json:"plan_id,omitempty"`
		QARun  *workflow.QARun `json:"qa_run,omitempty"`
	}{Slug: slug, PlanID: planID, QARun: qaRun}
	return s.requireMutation(ctx, "qa.start", "plan.mutation.qa.start", payload)
}

func (s *ContractObservabilityScenario) stageRejectFromQA(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("slug")
	planID, _ := result.GetDetailString("plan_id")
	reqID, _ := result.GetDetailString("requirement_id")
	now := time.Now().UTC()
	decisionID := fmt.Sprintf("plan-decision.%s.topology", slug)
	result.SetDetail("topology_decision_id", decisionID)
	payload := workflow.QAVerdictEvent{
		Slug:    slug,
		PlanID:  planID,
		Level:   workflow.QALevelIntegration,
		Verdict: workflow.QAVerdictNeedsChanges,
		Summary: "QA failed during build configuration because the implementation introduced topology-incompatible project roots.",
		Dimensions: workflow.QAVerdictDimensions{
			RequirementFulfillment: "blocked by topology mismatch",
			CapabilityEvidence:     "not executable in composite baseline",
			Coverage:               "not reached",
			RegressionSurface:      "build-configuration",
		},
		PlanDecisions: []workflow.PlanDecision{
			{
				ID:             decisionID,
				PlanID:         planID,
				Kind:           workflow.PlanDecisionKindArchitectureRevise,
				Title:          "Repair topology contract before execution",
				Rationale:      "QA failed before tests executed: the build shape conflicts with the accepted brownfield topology.",
				Status:         workflow.PlanDecisionStatusProposed,
				ProposedBy:     "qa-reviewer",
				AffectedReqIDs: []string{reqID},
				ContractImpact: &workflow.ContractImpact{
					Kind:        workflow.ContractImpactChange,
					Summary:     "Add a topology/API contract that prevents standalone project replacement.",
					AffectedIDs: []string{"contract.topology.allowed_roots", reqID},
				},
				ArtifactReferences: []workflow.ArtifactRef{
					{Path: ".semspec/qa-artifacts/" + slug + "/gradle.log", Type: "log", Purpose: "Composite build topology failure"},
				},
				CreatedAt: now,
			},
		},
		PlanDecisionIDs: []string{decisionID},
		TraceID:         "trace-qa-verdict-" + slug,
	}
	return s.requireMutation(ctx, "qa.verdict", "plan.mutation.qa.verdict", payload)
}

func (s *ContractObservabilityScenario) stageAssertTopologyRecovery(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("slug")
	decisionID, _ := result.GetDetailString("topology_decision_id")
	plan, err := s.http.GetPlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}
	if plan.Status != string(workflow.StatusRejected) {
		return fmt.Errorf("plan.status = %q, want rejected", plan.Status)
	}
	if plan.PhaseSummary == nil {
		return fmt.Errorf("phase_summary missing")
	}
	if plan.PhaseSummary.QA == nil {
		return fmt.Errorf("qa summary missing")
	}
	if plan.PhaseSummary.QA.Verdict != string(workflow.QAVerdictNeedsChanges) {
		return fmt.Errorf("qa verdict = %q, want needs_changes", plan.PhaseSummary.QA.Verdict)
	}
	if plan.PhaseSummary.QA.FailureCategory != string(workflow.QAFailureCategoryTopology) {
		return fmt.Errorf("qa failure category = %q, want topology", plan.PhaseSummary.QA.FailureCategory)
	}
	if plan.PhaseSummary.Recovery == nil || plan.PhaseSummary.Recovery.DecisionID != decisionID {
		return fmt.Errorf("recovery summary = %+v, want decision %s", plan.PhaseSummary.Recovery, decisionID)
	}
	if plan.PhaseSummary.Recovery.ContractImpactKind != string(workflow.ContractImpactChange) {
		return fmt.Errorf("recovery contract impact kind = %q, want change", plan.PhaseSummary.Recovery.ContractImpactKind)
	}
	if len(plan.PlanDecisions) < 2 {
		return fmt.Errorf("len(plan_decisions) = %d, want targeted recovery + topology decision", len(plan.PlanDecisions))
	}
	result.SetDetail("topology_failure_category", plan.PhaseSummary.QA.FailureCategory)
	result.SetDetail("topology_recovery_phase", plan.PhaseSummary.Phase)
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
