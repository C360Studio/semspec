// Package planmanager mutations file. Holds the request/response DTOs for
// every plan-mutation HTTP endpoint; the count of public structs is the
// API surface, not architectural debt.
//
//revive:disable:max-public-structs
package planmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/c360studio/semspec/pkg/paths"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/message"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

// Mutation subjects — generators use request/reply to return results.
// Plan-manager is the single writer; the KV write IS the event (twofer).
const (
	mutationExplored              = "plan.mutation.explored"
	mutationDrafted               = "plan.mutation.drafted"
	mutationReviewed              = "plan.mutation.reviewed"
	mutationApproved              = "plan.mutation.approved"
	mutationRequirementsGenerated = "plan.mutation.requirements.generated"
	mutationArchitectureGenerated = "plan.mutation.architecture.generated"
	mutationStoriesGenerated      = "plan.mutation.stories.generated"
	mutationStoryStatus           = "plan.mutation.story.status"
	mutationScenariosGenerated    = "plan.mutation.scenarios.generated"
	mutationScenariosReviewed     = "plan.mutation.scenarios.reviewed"
	mutationReadyForExecution     = "plan.mutation.ready_for_execution"
	mutationGenerationFailed      = "plan.mutation.generation.failed"
	mutationClaim                 = "plan.mutation.claim"
	mutationRevision              = "plan.mutation.revision"
	mutationQAStart               = "plan.mutation.qa.start"
	mutationQAVerdict             = "plan.mutation.qa.verdict"
	mutationReviewApprove         = "plan.mutation.review.approve"
	mutationGitHubPlanCreate      = "workflow.trigger.github-plan-create"
	mutationGitHubPRFeedback      = "plan.mutation.github.pr_feedback"
	mutationGitHubPRMetadata      = "plan.mutation.github.pr_metadata"
	mutationPlanDecisionAdd       = "plan.mutation.plan_decision.add"
	mutationPlanDecisionAccept    = "plan.mutation.plan_decision.accept"
)

// Mutation request types — these are the payloads generators send via request/reply.

// RequirementsMutationRequest is sent by the requirement-generator after LLM processing.
type RequirementsMutationRequest struct {
	Slug         string                 `json:"slug"`
	Requirements []workflow.Requirement `json:"requirements"`
	TraceID      string                 `json:"trace_id,omitempty"`
}

// ScenariosMutationRequest is sent by the scenario-generator for a single
// (Requirement, Story) batch.
//
// StoryID identifies the Story this batch belongs to and scopes the merge
// semantics in handleScenariosMutation: when set, only scenarios for the
// matching (RequirementID, StoryID) pair are wiped before appending — so
// parallel per-Story dispatches under the same Requirement preserve each
// other's batches.
//
// StoryID is omitempty for back-compat: pre-Sarah plans and mock fixtures
// dispatch in legacy per-Requirement mode where Bob authors one batch for
// the entire Requirement. The handler falls back to wipe-by-RequirementID
// in that case, matching pre-ADR-043 behavior.
type ScenariosMutationRequest struct {
	Slug          string              `json:"slug"`
	RequirementID string              `json:"requirement_id"`
	StoryID       string              `json:"story_id,omitempty"`
	Scenarios     []workflow.Scenario `json:"scenarios"`
	TraceID       string              `json:"trace_id,omitempty"`
}

// ExploredMutationRequest is sent by the planner component after the
// ADR-040 analyst sub-phase produces an Exploration document. plan-manager
// persists Plan.Exploration to PLAN_STATES, emits Capability triples to
// ENTITY_STATES, and transitions exploring → explored.
type ExploredMutationRequest struct {
	Slug        string               `json:"slug"`
	Exploration workflow.Exploration `json:"exploration"`
	TraceID     string               `json:"trace_id,omitempty"`
}

// DraftedMutationRequest is sent by the planner after focus/synthesis.
type DraftedMutationRequest struct {
	Slug             string          `json:"slug"`
	Title            string          `json:"title,omitempty"`
	Goal             string          `json:"goal"`
	Context          string          `json:"context"`
	Scope            *workflow.Scope `json:"scope,omitempty"`
	SkipArchitecture bool            `json:"skip_architecture,omitempty"`
	TraceID          string          `json:"trace_id,omitempty"`
}

// architectureMutationRequest is sent by architecture-generator after phase completion.
// Architecture is nil when the plan's SkipArchitecture flag is true (pass-through).
type architectureMutationRequest struct {
	Slug         string                         `json:"slug"`
	Architecture *workflow.ArchitectureDocument `json:"architecture,omitempty"`
	TraceID      string                         `json:"trace_id,omitempty"`
}

// storiesMutationRequest is sent by story-preparer after Sarah finishes
// sharding requirements into Stories (ADR-043 Move 3). plan-manager
// persists Plan.Stories and transitions preparing_stories →
// ready_for_execution. Mirror shape of workflow.StoriesGeneratedEvent so the
// over-the-wire bytes are interchangeable between the typed event and the
// mutation request body.
type storiesMutationRequest struct {
	Slug       string           `json:"slug"`
	Stories    []workflow.Story `json:"stories"`
	StoryCount int              `json:"story_count,omitempty"`
	TraceID    string           `json:"trace_id,omitempty"`
}

// ReviewedMutationRequest is sent by the plan-reviewer after reviewing.
type ReviewedMutationRequest struct {
	Slug    string `json:"slug"`
	Verdict string `json:"verdict"` // "approved" or "needs_changes"
	Summary string `json:"summary,omitempty"`
	TraceID string `json:"trace_id,omitempty"`
}

// ApprovedMutationRequest is sent by auto-approve rule or human.
type ApprovedMutationRequest struct {
	Slug    string `json:"slug"`
	TraceID string `json:"trace_id,omitempty"`
}

// GenerationFailedRequest is sent by a generator when all retries are exhausted.
type GenerationFailedRequest struct {
	Slug    string `json:"slug"`
	Phase   string `json:"phase"` // "requirements" or "scenarios"
	Error   string `json:"error"`
	TraceID string `json:"trace_id,omitempty"`
}

// ReadyForExecutionMutationRequest is sent by the plan-reviewer after round 2 approval.
type ReadyForExecutionMutationRequest struct {
	Slug    string `json:"slug"`
	TraceID string `json:"trace_id,omitempty"`
}

// StoryStatusMutationRequest is sent by the requirement-executor to atomically
// transition a Story's lifecycle state. Compare-and-swap is enforced via
// Story.Status.CanTransitionTo: handler reads current Story.Status, checks
// transition validity, persists on success, returns Success=false on
// contention. Used by the executor's per-Story dispatch to reserve a Story
// (target=executing) before dispatching the dev loop — closes the ADR-044
// parallel-executor race window where N requirement-executors covering the
// same M:N Story would otherwise all race-dispatch.
type StoryStatusMutationRequest struct {
	Slug    string               `json:"slug"`
	StoryID string               `json:"story_id"`
	Target  workflow.StoryStatus `json:"target"`
	TraceID string               `json:"trace_id,omitempty"`
}

// ClaimMutationRequest is sent by watchers to atomically claim a plan for processing.
// The target status must be an in-progress status (IsInProgress() == true).
// Plan-manager's single-writer serialization ensures only one claim succeeds.
type ClaimMutationRequest struct {
	Slug   string          `json:"slug"`
	Status workflow.Status `json:"status"`
}

// RevisionMutationRequest is sent by the plan-reviewer when a review returns "needs_changes".
// The handler increments ReviewIteration, stores findings, and either loops the plan back
// to its re-entry point or escalates to StatusRejected at the iteration cap.
type RevisionMutationRequest struct {
	Slug     string          `json:"slug"`
	Round    int             `json:"round"`   // 1 (draft review) or 2 (scenarios review)
	Verdict  string          `json:"verdict"` // "needs_changes"
	Summary  string          `json:"summary"`
	Findings json.RawMessage `json:"findings"` // raw PlanReviewFinding array
	TraceID  string          `json:"trace_id,omitempty"`
}

// MutationResponse is the reply to all mutation requests.
type MutationResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// startMutationHandler subscribes to plan.mutation.* subjects for request/reply.
// Generators send results here; plan-manager is the single writer.
// Called from Start().
func (c *Component) startMutationHandler(ctx context.Context) error {
	if c.natsClient == nil {
		return nil
	}

	subjects := []struct {
		subject string
		handler func(context.Context, []byte) MutationResponse
	}{
		{mutationExplored, c.handleExploredMutation},
		{mutationDrafted, c.handleDraftedMutation},
		{mutationReviewed, c.handleReviewedMutation},
		{mutationApproved, c.handleApprovedMutation},
		{mutationRequirementsGenerated, c.handleRequirementsMutation},
		{mutationArchitectureGenerated, c.handleArchitectureMutation},
		{mutationStoriesGenerated, c.handleStoriesMutation},
		{mutationStoryStatus, c.handleStoryStatusMutation},
		{mutationScenariosGenerated, c.handleScenariosMutation},
		{mutationScenariosReviewed, c.handleScenariosReviewedMutation},
		{mutationReadyForExecution, c.handleReadyForExecutionMutation},
		{mutationGenerationFailed, c.handleGenerationFailedMutation},
		{mutationClaim, c.handleClaimMutation},
		{mutationRevision, c.handleRevisionMutation},
		{mutationQAStart, c.handleQAStartMutation},
		{mutationQAVerdict, c.handleQAVerdictMutation},
		{mutationReviewApprove, c.handleReviewApproveMutation},
		{mutationGitHubPlanCreate, c.handleGitHubPlanCreateMutation},
		{mutationGitHubPRFeedback, c.handleGitHubPRFeedbackMutation},
		{mutationGitHubPRMetadata, c.handleGitHubPRMetadataMutation},
		{mutationPlanDecisionAdd, c.handlePlanDecisionAddMutation},
		{mutationPlanDecisionAccept, c.handlePlanDecisionAcceptMutation},
	}

	for _, s := range subjects {
		h := s.handler // capture for closure
		if _, err := c.natsClient.SubscribeForRequests(ctx, s.subject, func(reqCtx context.Context, data []byte) ([]byte, error) {
			resp := h(reqCtx, data)
			return json.Marshal(resp)
		}); err != nil {
			return fmt.Errorf("subscribe to %s: %w", s.subject, err)
		}
	}

	c.logger.Info("Plan mutation handlers started",
		"count", len(subjects))
	return nil
}

// handleRequirementsMutation saves requirements inline on the plan and advances plan status.
func (c *Component) handleRequirementsMutation(ctx context.Context, data []byte) MutationResponse {
	var req RequirementsMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}

	if req.Slug == "" || len(req.Requirements) == 0 {
		return MutationResponse{Success: false, Error: "slug and requirements required"}
	}

	// Validators already wrap workflow.ErrInvalidRequirementDAG /
	// workflow.ErrInvalidFileOwnership with %w, so the sentinel text is in
	// err.Error() and the requirement-generator can match on it. Don't
	// double-prefix — that produced "invalid requirement DAG: invalid
	// requirement DAG: ..." in the response and broke the contract.
	if err := workflow.ValidateRequirementDAG(req.Requirements); err != nil {
		return MutationResponse{Success: false, Error: err.Error()}
	}
	if err := workflow.ValidateFileOwnershipPartition(req.Requirements); err != nil {
		return MutationResponse{Success: false, Error: err.Error()}
	}

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	// Ensure all requirements have the correct PlanID.
	planEntityID := workflow.PlanEntityID(req.Slug)
	for i := range req.Requirements {
		if req.Requirements[i].PlanID == "" {
			req.Requirements[i].PlanID = planEntityID
		}
	}

	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusRequirementsGenerated) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → requirements_generated", current)}
	}

	// ADR-040 Move 2: when the plan ran the analyst sub-phase, every
	// capability must own ≥1 requirement and every requirement's
	// CapabilityName must resolve. Validation is permissive on legacy
	// plans (Exploration nil) and mixed-state regressions are surfaced
	// rather than silently accepted.
	if err := workflow.ValidateRequirementCapabilityCoverage(plan.Exploration, req.Requirements); err != nil {
		return MutationResponse{Success: false, Error: err.Error()}
	}

	// Replace requirements inline and advance plan status.
	// The KV write IS the event — watchers (coordinator, SSE) react automatically.
	plan.Requirements = req.Requirements
	plan.Status = workflow.StatusRequirementsGenerated
	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to save requirements via mutation", "slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save plan: %v", err)}
	}

	c.logger.Info("Requirements saved via mutation",
		"slug", req.Slug,
		"count", len(req.Requirements))

	return MutationResponse{Success: true}
}

// handleArchitectureMutation stores the architecture document and advances plan to architecture_generated.
// Architecture is nil when plan.SkipArchitecture is true (pass-through with zero LLM calls).
func (c *Component) handleArchitectureMutation(ctx context.Context, data []byte) MutationResponse {
	var req architectureMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" {
		return MutationResponse{Success: false, Error: "slug required"}
	}

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusArchitectureGenerated) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → architecture_generated", current)}
	}

	// Store the architecture document when provided (nil is valid for skip path).
	if req.Architecture != nil {
		plan.Architecture = req.Architecture
	}
	// A fresh architecture supersedes any revision carry-over.
	plan.PreviousArchitectureJSON = ""
	plan.Status = workflow.StatusArchitectureGenerated

	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to save architecture via mutation", "slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save plan: %v", err)}
	}

	skipped := req.Architecture == nil
	c.logger.Info("Architecture phase complete via mutation",
		"slug", req.Slug,
		"skipped", skipped)

	return MutationResponse{Success: true}
}

// handleStoriesMutation persists Sarah's emitted Stories inline on the plan
// and advances preparing_stories → stories_generated (ADR-043 PR 4c).
// Bob (scenario-generator) watches stories_generated as one of its source
// states and dispatches scenario generation per Requirement — until PR 4d
// rewires for per-Story scenario emission.
//
// Plan-reviewer R3 (mergeStoryFindings) fires on stories_generated via the
// normal review pipeline.
//
// Validates the wire payload (workflow.ValidateStories — the same gate
// story-preparer runs pre-publish) before persistence. Validation
// failures return Success=false so story-preparer treats it as a transient
// error and retries; structural failures that escape Sarah's gate are
// rare and indicate a wire-drift or operator-edited PLAN_STATES.
func (c *Component) handleStoriesMutation(ctx context.Context, data []byte) MutationResponse {
	var req storiesMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" {
		return MutationResponse{Success: false, Error: "slug required"}
	}
	if err := workflow.ValidateStories(req.Stories); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("validate stories: %v", err)}
	}

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusStoriesGenerated) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → stories_generated", current)}
	}

	plan.Stories = req.Stories
	plan.Status = workflow.StatusStoriesGenerated

	// ADR-043 follow-up — auto-derive plan.Scope.Create from the union of
	// Story.FilesOwned. Sarah declares which files each Story will create,
	// but pre-PR-4j the planner had to anticipate those paths in
	// scope.create at plan-draft time. That created a consistency burden
	// the planner reliably failed (smoke 5 / mavlink-hard 2026-06-01 saw
	// plan-reviewer reject 3× because story.files_owned drifted from
	// scope.create). Deriving here eliminates the drift: scope.create
	// always reflects what stories actually intend to create. Files
	// already in scope.include are excluded — they exist already, no
	// creation intent needed.
	plan.Scope = ensureScopeCreateCoversStories(plan.Scope, req.Stories)

	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to save stories via mutation", "slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save plan: %v", err)}
	}

	c.logger.Info("Stories saved via mutation",
		"slug", req.Slug,
		"count", len(req.Stories),
		"scope_create_count", len(plan.Scope.Create))

	return MutationResponse{Success: true}
}

// ensureScopeCreateCoversStories augments scope.Create with every
// Story.FilesOwned path that is not already in scope.Include or scope.Create.
// The returned Scope preserves the original Include / Exclude / DoNotTouch
// fields; only Create may change. Result Create entries are sorted for
// deterministic output.
func ensureScopeCreateCoversStories(scope workflow.Scope, stories []workflow.Story) workflow.Scope {
	have := make(map[string]struct{}, len(scope.Include)+len(scope.Create))
	for _, p := range scope.Include {
		have[p] = struct{}{}
	}
	for _, p := range scope.Create {
		have[p] = struct{}{}
	}
	added := false
	for _, s := range stories {
		for _, p := range s.FilesOwned {
			if p == "" {
				continue
			}
			if _, ok := have[p]; ok {
				continue
			}
			have[p] = struct{}{}
			scope.Create = append(scope.Create, p)
			added = true
		}
	}
	if added {
		sort.Strings(scope.Create)
	}
	return scope
}

// scenarioMatchesMutationScope reports whether an existing scenario should be
// wiped by the incoming mutation. Per-Story dispatch (StoryID non-empty) wipes
// only scenarios matching BOTH (RequirementID, StoryID) so sibling Stories'
// scenarios survive. Legacy per-Requirement dispatch (StoryID empty) wipes
// every scenario under the RequirementID for back-compat with pre-Sarah
// plans and mock fixtures.
func scenarioMatchesMutationScope(existing workflow.Scenario, req ScenariosMutationRequest) bool {
	if existing.RequirementID != req.RequirementID {
		return false
	}
	if req.StoryID == "" {
		return true
	}
	return existing.StoryID == req.StoryID
}

// handleScenariosMutation saves scenarios for a requirement inline on the plan and checks convergence.
func (c *Component) handleScenariosMutation(ctx context.Context, data []byte) MutationResponse {
	var req ScenariosMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}

	if req.Slug == "" || req.RequirementID == "" {
		return MutationResponse{Success: false, Error: "slug and requirement_id required"}
	}

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	// Merge semantic depends on whether the dispatch identified a Story.
	//
	//  - StoryID set (per-Story dispatch, ADR-043 PR 4j): wipe only scenarios
	//    matching BOTH (RequirementID, StoryID), keep everything else. Two
	//    parallel Story dispatches under the same Requirement preserve each
	//    other's batches.
	//  - StoryID empty (legacy per-Requirement dispatch, pre-Sarah / mock):
	//    wipe ALL scenarios for the RequirementID, matching pre-ADR-043
	//    behavior. Sarah's per-Story chain is opt-in via the wire shape; the
	//    fallback keeps mock fixtures and pre-Sarah plans working unchanged.
	//
	// Closes go-reviewer Pass-2 finding C2: pre-fix, parallel per-Story
	// dispatches under the same Requirement silently dropped each other's
	// batches because the merge keyed only on RequirementID. Smoke-6 didn't
	// surface it because every fixture had exactly 1 Story per Requirement.
	if len(req.Scenarios) > 0 {
		var kept []workflow.Scenario
		for _, s := range plan.Scenarios {
			if scenarioMatchesMutationScope(s, req) {
				continue
			}
			kept = append(kept, s)
		}
		plan.Scenarios = append(kept, req.Scenarios...)
	}

	c.logger.Info("Scenarios saved via mutation",
		"slug", req.Slug,
		"requirement_id", req.RequirementID,
		"story_id", req.StoryID,
		"count", len(req.Scenarios))

	// Check convergence: do all requirements have at least one scenario?
	if len(plan.Requirements) == 0 {
		// No requirements to check against — save and return.
		if err := ps.save(ctx, plan); err != nil {
			c.logger.Error("Failed to save scenarios via mutation", "slug", req.Slug, "error", err)
			return MutationResponse{Success: false, Error: fmt.Sprintf("save plan: %v", err)}
		}
		return MutationResponse{Success: true}
	}

	allCovered := true
	for _, r := range plan.Requirements {
		hasScenario := false
		for _, s := range plan.Scenarios {
			if s.RequirementID == r.ID {
				hasScenario = true
				break
			}
		}
		if !hasScenario {
			allCovered = false
			break
		}
	}

	if allCovered {
		current := plan.EffectiveStatus()
		if !current.CanTransitionTo(workflow.StatusScenariosGenerated) {
			return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → scenarios_generated", current)}
		}
		plan.Status = workflow.StatusScenariosGenerated
		c.logger.Info("All requirements have scenarios — advanced to scenarios_generated",
			"slug", req.Slug,
			"requirement_count", len(plan.Requirements))
	}

	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to save scenarios via mutation", "slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save plan: %v", err)}
	}

	return MutationResponse{Success: true}
}

// handleGenerationFailedMutation marks the plan as rejected.
func (c *Component) handleGenerationFailedMutation(ctx context.Context, data []byte) MutationResponse {
	var req GenerationFailedRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}

	c.logger.Error("Generation failed via mutation",
		"slug", req.Slug, "phase", req.Phase, "error", req.Error)

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusRejected) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → rejected", current)}
	}

	plan.LastError = req.Error
	now := time.Now()
	plan.LastErrorAt = &now
	plan.Status = workflow.StatusRejected

	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to mark plan rejected", "slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	return MutationResponse{Success: true}
}

// handleExploredMutation persists the analyst sub-phase Exploration to
// PLAN_STATES and transitions exploring → explored (ADR-040 Move 1). The
// writeChildTriples path emits Capability entities + plan-level exploration
// triples as part of the save.
func (c *Component) handleExploredMutation(ctx context.Context, data []byte) MutationResponse {
	var req ExploredMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" {
		return MutationResponse{Success: false, Error: "slug required"}
	}
	if len(req.Exploration.Capabilities) == 0 {
		return MutationResponse{Success: false, Error: "exploration must declare at least one capability"}
	}
	if err := workflow.ValidateCapabilitySet(req.Exploration.Capabilities); err != nil {
		return MutationResponse{Success: false, Error: err.Error()}
	}

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusExplored) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → explored", current)}
	}

	exp := req.Exploration
	plan.Exploration = &exp
	plan.Status = workflow.StatusExplored

	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to save exploration via mutation", "slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("Plan exploration committed",
		"slug", req.Slug,
		"capabilities", len(exp.Capabilities),
		"open_questions", len(exp.OpenQuestions))
	return MutationResponse{Success: true}
}

// handleDraftedMutation updates a plan with goal/context/scope from the planner.
func (c *Component) handleDraftedMutation(ctx context.Context, data []byte) MutationResponse {
	var req DraftedMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" || req.Goal == "" {
		return MutationResponse{Success: false, Error: "slug and goal required"}
	}

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusDrafted) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → drafted", current)}
	}

	if req.Title != "" {
		plan.Title = req.Title
	}
	plan.Goal = req.Goal
	plan.Context = req.Context
	if req.Scope != nil {
		plan.Scope = *req.Scope
	}
	plan.SkipArchitecture = req.SkipArchitecture
	plan.Status = workflow.StatusDrafted

	if err := ps.save(ctx, plan); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("Plan drafted via mutation", "slug", req.Slug, "goal", req.Goal,
		"skip_architecture", req.SkipArchitecture)
	return MutationResponse{Success: true}
}

// handleReviewedMutation updates plan status to reviewed after reviewer verdict.
func (c *Component) handleReviewedMutation(ctx context.Context, data []byte) MutationResponse {
	var req ReviewedMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" {
		return MutationResponse{Success: false, Error: "slug required"}
	}

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusReviewed) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → reviewed", current)}
	}

	plan.ReviewVerdict = req.Verdict
	plan.ReviewSummary = req.Summary
	now := time.Now()
	plan.ReviewedAt = &now
	plan.Status = workflow.StatusReviewed

	if err := ps.save(ctx, plan); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("Plan reviewed via mutation", "slug", req.Slug, "verdict", req.Verdict)
	return MutationResponse{Success: true}
}

// handleApprovedMutation sets plan status to approved (from auto-approve rule or human).
func (c *Component) handleApprovedMutation(ctx context.Context, data []byte) MutationResponse {
	var req ApprovedMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" {
		return MutationResponse{Success: false, Error: "slug required"}
	}

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusApproved) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → approved", current)}
	}

	now := time.Now()
	plan.Approved = true
	plan.ApprovedAt = &now
	plan.Status = workflow.StatusApproved

	if err := ps.save(ctx, plan); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("Plan approved via mutation", "slug", req.Slug)
	return MutationResponse{Success: true}
}

// handleStoryStatusMutation atomically transitions a Story.Status via the
// Story.CanTransitionTo state machine. Returns Success=false on contention
// (another caller already advanced the Story), invalid transition (caller
// supplied a target unreachable from current state), or missing Story ID.
//
// Used by requirement-executor to (a) reserve a Story before dispatching
// the dev loop (target=executing) — the first executor wins, others see
// the rejection and skip — closing the ADR-044 parallel-executor race
// window where N requirement-executors covering the same M:N Story would
// otherwise all race-dispatch the dev loop. Then (b) record terminal
// outcome (target=complete or failed) on dispatch end.
func (c *Component) handleStoryStatusMutation(ctx context.Context, data []byte) MutationResponse {
	var req StoryStatusMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" || req.StoryID == "" || req.Target == "" {
		return MutationResponse{Success: false, Error: "slug, story_id, target required"}
	}
	if !req.Target.IsValid() {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid target status %q", req.Target)}
	}

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	storyPtr, _ := plan.FindStory(req.StoryID)
	if storyPtr == nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("story %q not found in plan", req.StoryID)}
	}

	current := storyPtr.Status
	// Empty/zero-value Status is Sarah's emission shape (omitempty on the
	// wire); treat as Ready for transition purposes since Sarah only emits
	// Stories that passed her readiness gate.
	if current == "" {
		current = workflow.StoryStatusReady
	}
	if !current.CanTransitionTo(req.Target) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → %s", current, req.Target)}
	}

	storyPtr.Status = req.Target
	storyPtr.UpdatedAt = time.Now()

	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to save story status via mutation", "slug", req.Slug, "story_id", req.StoryID, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save plan: %v", err)}
	}

	c.logger.Info("Story status transitioned via mutation",
		"slug", req.Slug, "story_id", req.StoryID,
		"from", current, "to", req.Target)

	// ADR-044: when a Story reaches Complete or Failed, re-fire the
	// scenario orchestrator so non-owner requirements gated by
	// filterByM2NStoryReservations are released. Without this, deferred
	// non-owners would never wake up — execution_events.go re-fires only
	// on requirement-level completion, not Story-level. Only fire while
	// the plan is still implementing; post-convergence re-fires would
	// race with terminal-state transitions.
	if req.Target == workflow.StoryStatusComplete || req.Target == workflow.StoryStatusFailed {
		if plan.EffectiveStatus() == workflow.StatusImplementing {
			if err := c.triggerScenarioOrchestrator(ctx, plan); err != nil {
				c.logger.Warn("Failed to re-fire scenario orchestrator after story status change",
					"slug", req.Slug, "story_id", req.StoryID, "target", req.Target, "error", err)
			}
		}
	}
	return MutationResponse{Success: true}
}

// handleClaimMutation atomically transitions a plan to an in-progress status.
// Used by watchers to claim a plan before starting work. Only one claim succeeds
// per transition — subsequent claims fail because the plan is already at the
// intermediate status, making the transition invalid.
func (c *Component) handleClaimMutation(ctx context.Context, data []byte) MutationResponse {
	var req ClaimMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" || req.Status == "" {
		return MutationResponse{Success: false, Error: "slug and status required"}
	}
	if !req.Status.IsInProgress() {
		return MutationResponse{Success: false, Error: fmt.Sprintf("can only claim in-progress statuses, got %q", req.Status)}
	}

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}
	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(req.Status) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → %s", current, req.Status)}
	}
	plan.Status = req.Status

	if err := ps.save(ctx, plan); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("claim: %v", err)}
	}

	c.logger.Info("Plan claimed via mutation", "slug", req.Slug, "status", req.Status)
	return MutationResponse{Success: true}
}

// handleReadyForExecutionMutation advances plan to ready_for_execution (from round 2 review).
// handleScenariosReviewedMutation sets the plan to scenarios_reviewed.
// Used when auto_approve=false after round-2 review — the plan waits for
// human approval before advancing to ready_for_execution.
func (c *Component) handleScenariosReviewedMutation(ctx context.Context, data []byte) MutationResponse {
	var req struct {
		Slug    string `json:"slug"`
		Summary string `json:"summary,omitempty"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" {
		return MutationResponse{Success: false, Error: "slug required"}
	}

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}
	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusScenariosReviewed) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → scenarios_reviewed", current)}
	}
	plan.Status = workflow.StatusScenariosReviewed

	if err := ps.save(ctx, plan); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("Plan scenarios reviewed via mutation (awaiting human approval)", "slug", req.Slug)
	return MutationResponse{Success: true}
}

func (c *Component) handleReadyForExecutionMutation(ctx context.Context, data []byte) MutationResponse {
	var req struct {
		Slug    string `json:"slug"`
		TraceID string `json:"trace_id,omitempty"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" {
		return MutationResponse{Success: false, Error: "slug required"}
	}

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}
	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusReadyForExecution) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → ready_for_execution", current)}
	}
	plan.Status = workflow.StatusReadyForExecution

	if err := ps.save(ctx, plan); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("Plan ready for execution via mutation", "slug", req.Slug)
	return MutationResponse{Success: true}
}

// escalateRevision transitions the plan to StatusRejected when the review iteration
// cap is reached. Extracted from handleRevisionMutation to keep function length within lint limits.
func (c *Component) escalateRevision(ctx context.Context, ps *planStore, plan *workflow.Plan, req *RevisionMutationRequest, current workflow.Status, maxIterations int) MutationResponse {
	if !current.CanTransitionTo(workflow.StatusRejected) {
		return MutationResponse{Success: false, Error: fmt.Sprintf(
			"invalid transition: %s → rejected", current)}
	}
	plan.ReviewVerdict = "escalated"
	plan.ReviewSummary = fmt.Sprintf("Max revisions exceeded after %d attempts: %s",
		maxIterations, req.Summary)
	plan.LastError = fmt.Sprintf("review revision cap reached (%d/%d): %s",
		plan.ReviewIteration, maxIterations, req.Summary)
	now := time.Now()
	plan.LastErrorAt = &now
	plan.ReviewedAt = &now
	plan.Status = workflow.StatusRejected

	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to save plan (revision escalation)", "slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Warn("Review revision cap reached — plan rejected",
		"slug", req.Slug,
		"round", req.Round,
		"iteration", plan.ReviewIteration,
		"max", maxIterations)

	c.emitRecoveryRequested(ctx, &payloads.RecoveryRequested{
		RecoveryID:          uuid.New().String(),
		Layer:               payloads.RecoveryLayerPhaseLocal,
		Slug:                req.Slug,
		EscalationReason:    plan.LastError,
		LastFailureFeedback: plan.ReviewFormattedFindings,
		TraceID:             req.TraceID,
	})

	return MutationResponse{Success: true}
}

// collectActiveRequirementIDsForRecovery returns the IDs of every still-
// active requirement on plan, used to populate
// RecoveryRequested.AffectedRequirementIDs on QA-verdict wedges. The
// recovery-agent threads this list into PlanDecision.AffectedReqIDs;
// without it, the auto-accept watcher (plan-decision-handler/
// recovery_autoaccept.go, gated by Config.AutoAcceptRecovery, filter
// Kind=requirement_change + len(AffectedReqIDs)>0) cannot fire, and every
// QA-rejection would require manual operator acceptance to drive a retry
// — defeating the autonomous issue-to-PR shape Phase 1c was built for.
//
// Status == "" is the unset default for requirements drafted before the
// lifecycle field landed and for newly-created requirements where the
// producer relies on the JSON `omitempty` convention.
// RequirementStatus.IsValid() rejects empty string, but in practice
// active requirements emit no status — so the empty branch is the
// "active by default" path, equivalent to RequirementStatusActive.
// Deprecated and superseded entries are excluded: those are no longer
// the source of truth, so retrying them would be wrong work.
//
// Returns nil (not empty slice) when no active requirements exist so the
// payload's `omitempty` JSON tag drops the field cleanly. Logs a warning
// in that case — a plan reaching reviewing_qa with zero active
// requirements shouldn't happen but if it does the auto-accept watcher
// correctly bails (its len>0 filter rejects) and the recovery PlanDecision
// requires manual acceptance.
func (c *Component) collectActiveRequirementIDsForRecovery(plan *workflow.Plan, verdict workflow.QAVerdict) []string {
	var affected []string
	for _, r := range plan.Requirements {
		if r.Status == "" || r.Status == workflow.RequirementStatusActive {
			affected = append(affected, r.ID)
		}
	}
	if len(affected) == 0 {
		c.logger.Warn("QA verdict with zero active requirements — recovery PlanDecision will require manual acceptance",
			"slug", plan.Slug, "verdict", verdict)
	}
	return affected
}

// emitRecoveryRequested is the test-friendly entry point. In production it
// delegates to publishRecoveryRequested; in tests c.recoveryPublisher is set
// to a capturing stub so assertions can verify the wire fires at every
// trigger site. The seam closes the pre-existing coverage gap: before this
// indirection, escalation tests asserted plan-state changes but had no way
// to verify the RecoveryRequested NATS publish actually happened.
func (c *Component) emitRecoveryRequested(ctx context.Context, req *payloads.RecoveryRequested) {
	if c.recoveryPublisher != nil {
		c.recoveryPublisher(ctx, req)
		return
	}
	c.publishRecoveryRequested(ctx, req)
}

// publishRecoveryRequested fires an ADR-037 stage-1 phase-local recovery
// request on recovery.requested.<slug>. Best-effort: a publish failure does
// not roll back the escalation (the plan is already StatusRejected/rejected
// in KV). The recovery-agent component consumes these and, on submit_work,
// emits RecoveryComplete on recovery.complete.<slug> for the watcher to
// reconcile.
func (c *Component) publishRecoveryRequested(ctx context.Context, req *payloads.RecoveryRequested) {
	if c.natsClient == nil {
		return
	}
	if err := req.Validate(); err != nil {
		c.logger.Warn("Recovery request failed local validation; skipping publish",
			"slug", req.Slug, "error", err)
		return
	}
	baseMsg := message.NewBaseMessage(req.Schema(), req, "plan-manager")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Warn("Failed to marshal RecoveryRequested", "slug", req.Slug, "error", err)
		return
	}
	subject := payloads.RecoveryRequestedSubjectPrefix + req.Slug
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		c.logger.Warn("Failed to publish RecoveryRequested",
			"slug", req.Slug, "subject", subject, "error", err)
		return
	}
	c.logger.Info("Recovery requested (phase-local)",
		"slug", req.Slug,
		"recovery_id", req.RecoveryID,
		"reason", req.EscalationReason)
}

// formatReviewFindings attempts to format raw findings JSON into human-readable text.
// Falls back to the summary string if findings can't be parsed.
func formatReviewFindings(findingsJSON json.RawMessage, summary, verdict string) string {
	var result workflow.PlanReviewResult
	if err := json.Unmarshal(findingsJSON, &result.Findings); err == nil {
		result.Summary = summary
		result.Verdict = verdict
		return result.FormatFindings()
	}
	return summary
}

// handleRevisionMutation processes a review rejection and either retries or escalates.
// Round 1 (draft review): loops back to StatusCreated so the planner re-drafts.
// Round 2 (scenarios review): loops back to StatusApproved, clearing Requirements/Scenarios
// so they are re-generated. At the iteration cap, escalates to StatusRejected.
func (c *Component) handleRevisionMutation(ctx context.Context, data []byte) MutationResponse {
	var req RevisionMutationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" || req.Round < 1 || req.Round > 2 {
		return MutationResponse{Success: false, Error: "slug required and round must be 1 or 2"}
	}
	if req.Verdict != "needs_changes" {
		return MutationResponse{Success: false, Error: "revision handler only accepts verdict=needs_changes"}
	}

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	// Guard: plan must be in the reviewing state for the given round.
	current := plan.EffectiveStatus()
	expectedStatus := workflow.StatusReviewingDraft
	if req.Round == 2 {
		expectedStatus = workflow.StatusReviewingScenarios
	}
	if current != expectedStatus {
		return MutationResponse{Success: false, Error: fmt.Sprintf(
			"revision round %d requires status %s, got %s", req.Round, expectedStatus, current)}
	}

	// Store review data and increment iteration.
	plan.ReviewIteration++
	plan.ReviewFindings = req.Findings
	plan.ReviewSummary = req.Summary
	plan.ReviewVerdict = req.Verdict
	plan.ReviewFormattedFindings = formatReviewFindings(req.Findings, req.Summary, req.Verdict)

	maxIterations := c.config.MaxReviewIterations
	if maxIterations <= 0 {
		maxIterations = 1 // safety: at least one attempt before escalation
	}

	if plan.ReviewIteration >= maxIterations {
		return c.escalateRevision(ctx, ps, plan, &req, current, maxIterations)
	}

	// Under limit: loop back to re-entry point.
	// Phase-aware routing: use finding phases to determine the minimal re-entry point.
	var targetStatus workflow.Status
	switch req.Round {
	case 1:
		targetStatus = workflow.StatusCreated
	case 2:
		targetStatus = c.determineR2ReentryPoint(plan, req.Findings)
	}

	if !current.CanTransitionTo(targetStatus) {
		return MutationResponse{Success: false, Error: fmt.Sprintf(
			"invalid transition: %s → %s", current, targetStatus)}
	}

	plan.Status = targetStatus

	// Rollback past the approval gate must reset Approved so the gate is
	// re-enforced on the next pass. Without this, plan.Approved remains true
	// from the original promotion; any client (UI auto-promote helper,
	// downstream checks) guarding on !plan.approved short-circuits and the
	// plan stalls at "reviewed". StatusCreated is the only round-2 reentry
	// point that crosses back through the gate; round-1 always lands here.
	if targetStatus == workflow.StatusCreated {
		plan.Approved = false
		plan.ApprovedAt = nil
	}

	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to save plan (revision retry)", "slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("Plan revision loop — retrying",
		"slug", req.Slug,
		"round", req.Round,
		"iteration", plan.ReviewIteration,
		"max", maxIterations,
		"target_status", targetStatus)

	return MutationResponse{Success: true}
}

// qaStartRequest is the payload qa-reviewer sends when it claims a plan for
// review. The optional QARun is populated for non-synthesis levels where the
// executor already produced results; plan-manager attaches it to the plan at
// the same time as the status transition so downstream UI sees both atomically.
type qaStartRequest struct {
	Slug   string          `json:"slug"`
	PlanID string          `json:"plan_id,omitempty"`
	QARun  *workflow.QARun `json:"qa_run,omitempty"`
}

// handleQAStartMutation transitions a plan from ready_for_qa to reviewing_qa
// and attaches the executor result (when present). qa-reviewer calls this
// before dispatching the LLM so UI consumers see "in review" while the LLM
// runs. Mirrors the shape plan-reviewer uses for its own state transitions.
func (c *Component) handleQAStartMutation(ctx context.Context, data []byte) MutationResponse {
	var req qaStartRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" {
		return MutationResponse{Success: false, Error: "slug required"}
	}

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	current := plan.EffectiveStatus()
	if current != workflow.StatusReadyForQA {
		return MutationResponse{Success: false, Error: fmt.Sprintf("plan must be in ready_for_qa, got %s", current)}
	}
	if !current.CanTransitionTo(workflow.StatusReviewingQA) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → reviewing_qa", current)}
	}

	if req.QARun != nil {
		plan.QARun = req.QARun
	}
	plan.Status = workflow.StatusReviewingQA

	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to save plan after QA start", "slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	// Cold-path safety net: guarantee the QA worktree (staged at convergence)
	// exists before qa-reviewer dispatches Murat, so the release-gate loop
	// inspects the assembled implementation rather than the repo root.
	c.ensureQAWorktree(ctx, plan)

	level := plan.EffectiveQALevel()
	c.logger.Info("QA review started",
		"slug", req.Slug, "level", level, "has_qa_run", req.QARun != nil)

	return MutationResponse{Success: true}
}

// handleQAVerdictMutation transitions a plan out of the review state (today
// still reviewing_rollup; Phase 2e moves it to reviewing_qa).
// approved      → StatusComplete (or StatusAwaitingReview when gated).
// needs_changes → StatusRejected with summary as LastError.
// rejected      → StatusRejected with summary as LastError (escalation variant;
// in Phase 6 qa-reviewer will distinguish this from needs_changes by whether
// PlanDecisions can salvage the plan, but the transition is the same).
func (c *Component) handleQAVerdictMutation(ctx context.Context, data []byte) MutationResponse {
	var req workflow.QAVerdictEvent
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" {
		return MutationResponse{Success: false, Error: "slug required"}
	}
	switch req.Verdict {
	case workflow.QAVerdictApproved, workflow.QAVerdictNeedsChanges, workflow.QAVerdictRejected:
	default:
		return MutationResponse{Success: false, Error: fmt.Sprintf("verdict must be approved|needs_changes|rejected, got %q", req.Verdict)}
	}

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	current := plan.EffectiveStatus()
	// Accept both the legacy reviewing_rollup and the new reviewing_qa so this
	// mutation works during the rollup→qa transition. Phase 2e flips the
	// branch point; until then, plans still arrive at reviewing_rollup.
	if current != workflow.StatusReviewingRollup && current != workflow.StatusReviewingQA {
		return MutationResponse{Success: false, Error: fmt.Sprintf("plan must be in reviewing_rollup or reviewing_qa, got %s", current)}
	}

	// Persist the prose verdict before the status transition so it survives
	// both the happy-path save below AND the assemble-fail save inside the
	// approved branch. Without this placement, a plan-level merge conflict
	// on QA-approved leaves operators with LastError but no reviewer
	// narrative to explain WHY the verdict was approved in the first place
	// — exactly the context needed to triage a "QA approved but stuck"
	// state.
	plan.QAVerdictSummary = &workflow.QAVerdictSummary{
		Verdict:    req.Verdict,
		Level:      req.Level,
		Summary:    req.Summary,
		Dimensions: req.Dimensions,
		RecordedAt: time.Now().UTC(),
	}

	switch req.Verdict {
	case workflow.QAVerdictApproved:
		target := workflow.StatusComplete
		if c.shouldGateReview(plan) {
			target = workflow.StatusAwaitingReview
		}
		if !current.CanTransitionTo(target) {
			return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → %s", current, target)}
		}
		// B1: assemble every completed requirement branch onto a plan branch
		// before marking the plan complete. A merge conflict here leaves the
		// plan in its current state with a LastError the UI can surface — the
		// work is done, but humans need to resolve the conflicts before the
		// plan can honestly be called "complete."
		//
		// Write-ahead note: the assembled branch is created on disk BEFORE
		// ps.save persists PlanBranch/PlanMergeCommit to KV. If save fails,
		// the git state "knows" about the assembled branch but the plan
		// record doesn't. This is acceptable because:
		//   (1) the sandbox endpoint uses `checkout -B` for idempotency, so
		//       the next QA-approved retry will reassemble cleanly; and
		//   (2) the mutation returns an error on save failure, so the caller
		//       also won't advance plan.Status = complete.
		// If an operator cherry-picks a conflict fix onto the assembled
		// branch between the failed save and the retry, (1) would destroy
		// their work. Phase 5's reconciliation UX will need to either
		// prevent retries when the assembled branch has diverged from the
		// recorded merge commit, or expose an explicit "reassemble" action.
		if err := c.assembleRequirementBranches(ctx, plan); err != nil {
			plan.LastError = fmt.Sprintf("plan-level merge failed: %v", err)
			now := time.Now()
			plan.LastErrorAt = &now
			if saveErr := ps.save(ctx, plan); saveErr != nil {
				c.logger.Error("Failed to persist LastError after plan merge failure",
					"slug", req.Slug, "error", saveErr)
			}
			c.logger.Error("QA verdict approved but plan-level merge failed — plan stays in current state",
				"slug", req.Slug, "current_status", current, "error", err)
			return MutationResponse{Success: false, Error: plan.LastError}
		}
		plan.Status = target
		c.logger.Info("QA verdict approved",
			"slug", req.Slug, "level", req.Level, "target", target,
			"plan_branch", plan.AssembledBranch, "plan_merge_commit", plan.AssembledMergeCommit)

	case workflow.QAVerdictNeedsChanges, workflow.QAVerdictRejected:
		if !current.CanTransitionTo(workflow.StatusRejected) {
			return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → rejected", current)}
		}
		plan.LastError = req.Summary
		now := time.Now()
		plan.LastErrorAt = &now
		plan.Status = workflow.StatusRejected

		// Persist PlanDecisions emitted by qa-reviewer so the UI and retry
		// flow can surface which proposals accompany this rejection.
		if len(req.PlanDecisions) > 0 {
			plan.PlanDecisions = append(plan.PlanDecisions, req.PlanDecisions...)
			c.logger.Info("QA verdict — persisted change proposals",
				"slug", req.Slug, "count", len(req.PlanDecisions))
		}

		c.logger.Warn("QA verdict — plan rejected",
			"slug", req.Slug, "verdict", req.Verdict, "level", req.Level,
			"plan_decision_ids", req.PlanDecisionIDs, "summary", req.Summary)
	}

	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to save plan after QA verdict", "slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	// The plan has left the QA gate (approved → complete/awaiting_review, or
	// rejected → recovery). Drop the per-plan QA worktree; a recovery re-run
	// re-stages a fresh one at the next convergence. Best-effort.
	c.deleteQAWorktree(ctx, req.Slug)

	c.fireQAVerdictRecovery(ctx, plan, req)

	return MutationResponse{Success: true}
}

// fireQAVerdictRecovery emits phase-local RecoveryRequested for non-approved QA
// verdicts. Mirrors the revision-cap-exhaustion path at
// handleRevisionEscalationMutation — every other wedge-shaped state transition
// published RecoveryRequested except this one until 2026-05-28 (verified on the
// gemini mavlink-decode run where qa-reviewer correctly diagnosed flaky
// time.Sleep timing but the plan terminated at rejected with no retry).
//
// EscalationReason carries the verdict kind so recovery-agent's prompt can
// distinguish "the tests failed and we should try again" from "the plan is
// structurally unsalvageable." LastFailureFeedback carries the qa-reviewer's
// actionable summary so the recovery agent sees the concrete diagnosis (e.g.,
// "replace time.Sleep with active polling"). No-op for approved verdicts.
func (c *Component) fireQAVerdictRecovery(ctx context.Context, plan *workflow.Plan, req workflow.QAVerdictEvent) {
	if req.Verdict != workflow.QAVerdictNeedsChanges && req.Verdict != workflow.QAVerdictRejected {
		return
	}
	c.emitRecoveryRequested(ctx, &payloads.RecoveryRequested{
		RecoveryID:             uuid.New().String(),
		Layer:                  payloads.RecoveryLayerPhaseLocal,
		Slug:                   req.Slug,
		EscalationReason:       fmt.Sprintf("QA verdict %s at level %s", req.Verdict, req.Level),
		LastFailureFeedback:    req.Summary,
		TraceID:                req.TraceID,
		AffectedRequirementIDs: c.collectActiveRequirementIDsForRecovery(plan, req.Verdict),
	})
}

// handleReviewApproveMutation handles plan.mutation.review.approve — transitions
// a plan from awaiting_review to complete. Used by the UI "Approve" button and
// future GitHub PR merge detection.
func (c *Component) handleReviewApproveMutation(ctx context.Context, data []byte) MutationResponse {
	var req struct {
		Slug     string `json:"slug"`
		Reviewer string `json:"reviewer,omitempty"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" {
		return MutationResponse{Success: false, Error: "slug is required"}
	}

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	current := plan.EffectiveStatus()
	if current != workflow.StatusAwaitingReview {
		return MutationResponse{Success: false, Error: fmt.Sprintf("plan must be in awaiting_review, got %s", current)}
	}

	if !current.CanTransitionTo(workflow.StatusComplete) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid transition: %s → complete", current)}
	}

	plan.Status = workflow.StatusComplete
	c.logger.Info("Review approved — plan complete",
		"slug", req.Slug, "reviewer", req.Reviewer)

	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to save plan after review approval", "slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	return MutationResponse{Success: true}
}

// handleGitHubPlanCreateMutation handles workflow.trigger.github-plan-create.
// Creates a plan with GitHub metadata from a validated issue.
func (c *Component) handleGitHubPlanCreateMutation(ctx context.Context, data []byte) MutationResponse {
	// Unwrap BaseMessage envelope.
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal envelope: %v", err)}
	}

	var req payloads.GitHubPlanCreationRequest
	raw := envelope.Payload
	if len(raw) == 0 {
		raw = data // fall back to flat payload
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if err := req.Validate(); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("validate: %v", err)}
	}

	// Slug must be computed before lockSlug — see slugMutexes godoc.
	slug := fmt.Sprintf("%d-%s", req.IssueNumber, paths.Slugify(req.Title))
	defer c.lockSlug(slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	if ps.exists(slug) {
		c.logger.Info("GitHub plan already exists, skipping", "slug", slug, "issue", req.IssueNumber)
		return MutationResponse{Success: true}
	}

	plan, err := ps.create(ctx, slug, req.Title, c.resolveProjectQALevel(), nil)
	if err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("create plan: %v", err)}
	}

	// Attach GitHub metadata and description.
	plan.GitHub = &workflow.GitHubMetadata{
		IssueNumber: req.IssueNumber,
		IssueURL:    req.IssueURL,
		Repository:  req.Repository,
	}
	if req.Description != "" {
		plan.Context = req.Description
	}
	if req.Scope != "" {
		plan.Scope.Include = strings.Split(req.Scope, ",")
		for i := range plan.Scope.Include {
			plan.Scope.Include[i] = strings.TrimSpace(plan.Scope.Include[i])
		}
	}

	if err := ps.save(ctx, plan); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("save plan with github metadata: %v", err)}
	}

	c.logger.Info("Plan created from GitHub issue",
		"slug", slug, "issue", req.IssueNumber, "repo", req.Repository)

	return MutationResponse{Success: true}
}

// handleGitHubPRFeedbackMutation handles plan.mutation.github.pr_feedback.
// Routes PR review comments to affected requirements via PlanDecisions and
// re-triggers execution.
func (c *Component) handleGitHubPRFeedbackMutation(ctx context.Context, data []byte) MutationResponse {
	// Unwrap BaseMessage envelope.
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal envelope: %v", err)}
	}

	var req payloads.GitHubPRFeedbackRequest
	raw := envelope.Payload
	if len(raw) == 0 {
		raw = data
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if err := req.Validate(); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("validate: %v", err)}
	}

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	if plan.EffectiveStatus() != workflow.StatusAwaitingReview {
		return MutationResponse{Success: false, Error: fmt.Sprintf("plan must be in awaiting_review, got %s", plan.EffectiveStatus())}
	}

	if plan.GitHub == nil {
		return MutationResponse{Success: false, Error: "PR feedback requires a GitHub-originated plan"}
	}

	// Map file-scoped comments to requirements.
	fileMap, err := c.buildFileToReqMap(ctx, req.Slug)
	if err != nil {
		c.logger.Warn("Failed to build file→requirement map, will retry all requirements",
			"slug", req.Slug, "error", err)
	}

	affectedReqIDs := mapCommentsToRequirements(req.Comments, fileMap)

	// If no file-scoped comments or mapping failed, store latest feedback
	// on GitHub metadata (not plan.Context — avoids unbounded growth) and
	// reset all active requirements.
	if len(affectedReqIDs) == 0 {
		if req.Body != "" {
			plan.GitHub.LatestFeedback = req.Body
		}
		for _, r := range plan.Requirements {
			if r.Status == workflow.RequirementStatusActive {
				affectedReqIDs = append(affectedReqIDs, r.ID)
			}
		}
	}

	// Create PlanDecision(s) for audit trail.
	appendPRFeedbackPlanDecisions(plan, &req, affectedReqIDs)

	// Update GitHub metadata.
	plan.GitHub.PRRevision++
	plan.GitHub.LastProcessedReviewID = req.ReviewID

	// Reset affected requirement executions. Fail if none were reset —
	// transitioning to ready_for_execution without resets would re-run
	// with stale state.
	resetCount, resetErr := c.resetRequirementExecutionsByID(ctx, req.Slug, affectedReqIDs)
	if resetErr != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("reset requirement executions: %v", resetErr)}
	}
	if resetCount == 0 && len(affectedReqIDs) > 0 {
		return MutationResponse{Success: false, Error: "no requirement executions were reset — cannot re-execute"}
	}

	// Transition awaiting_review → ready_for_execution.
	durableCtx := context.WithoutCancel(ctx)
	if err := c.setPlanStatusCached(durableCtx, plan, workflow.StatusReadyForExecution); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("transition to ready_for_execution: %v", err)}
	}

	c.logger.Info("PR feedback applied — plan re-queued for execution",
		"slug", req.Slug,
		"pr_number", req.PRNumber,
		"review_id", req.ReviewID,
		"affected_reqs", len(affectedReqIDs),
		"reset_count", resetCount,
		"pr_revision", plan.GitHub.PRRevision)

	return MutationResponse{Success: true}
}

// appendPRFeedbackPlanDecisions records one PlanDecision per affected
// requirement so the PR review feedback round becomes part of the plan's
// audit trail. Extracted from handleGitHubPRFeedbackMutation to keep that
// handler within the per-function statement budget.
func appendPRFeedbackPlanDecisions(plan *workflow.Plan, req *payloads.GitHubPRFeedbackRequest, affectedReqIDs []string) {
	now := time.Now()
	for _, reqID := range affectedReqIDs {
		proposalID := fmt.Sprintf("plan-decision.%s.pr-feedback.%d.%s", req.Slug, req.ReviewID, reqID)
		rationale := fmt.Sprintf("PR review feedback from @%s (review %d)", req.Reviewer, req.ReviewID)
		if req.Body != "" {
			rationale += ": " + req.Body
		}
		plan.PlanDecisions = append(plan.PlanDecisions, workflow.PlanDecision{
			ID:             proposalID,
			PlanID:         workflow.PlanEntityID(req.Slug),
			Title:          fmt.Sprintf("PR feedback round %d", plan.GitHub.PRRevision+1),
			Rationale:      rationale,
			Status:         workflow.PlanDecisionStatusAccepted,
			ProposedBy:     "github-pr-review",
			AffectedReqIDs: []string{reqID},
			CreatedAt:      now,
			DecidedAt:      &now,
		})
	}
}

// buildFileToReqMap builds a file→requirementID reverse index from EXECUTION_STATES.
func (c *Component) buildFileToReqMap(ctx context.Context, slug string) (map[string][]string, error) {
	bucket, err := c.getExecBucket(ctx)
	if err != nil {
		return nil, err
	}

	prefix := "req." + slug + "."
	keys, err := bucket.Keys(ctx, jetstream.MetaOnly())
	if err != nil {
		return nil, fmt.Errorf("list execution keys: %w", err)
	}

	result := make(map[string][]string)
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		entry, getErr := bucket.Get(ctx, key)
		if getErr != nil {
			continue
		}
		var exec struct {
			NodeResults []struct {
				FilesModified []string `json:"files_modified"`
			} `json:"node_results"`
		}
		if jsonErr := json.Unmarshal(entry.Value(), &exec); jsonErr != nil {
			continue
		}
		reqID := strings.TrimPrefix(key, prefix)
		for _, nr := range exec.NodeResults {
			for _, file := range nr.FilesModified {
				result[file] = append(result[file], reqID)
			}
		}
	}
	return result, nil
}

// mapCommentsToRequirements maps PR review comments to requirement IDs using
// the file→requirement reverse index. Returns deduplicated requirement IDs.
func mapCommentsToRequirements(comments []payloads.PRReviewComment, fileMap map[string][]string) []string {
	if len(fileMap) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var result []string
	for _, c := range comments {
		if c.Path == "" {
			continue
		}
		for _, reqID := range fileMap[c.Path] {
			if !seen[reqID] {
				seen[reqID] = true
				result = append(result, reqID)
			}
		}
	}
	return result
}

// handleGitHubPRMetadataMutation handles plan.mutation.github.pr_metadata.
// Updates the plan's GitHub metadata with PR number and URL after PR creation.
func (c *Component) handleGitHubPRMetadataMutation(ctx context.Context, data []byte) MutationResponse {
	var req struct {
		Slug     string `json:"slug"`
		PRNumber int    `json:"pr_number"`
		PRURL    string `json:"pr_url"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" || req.PRNumber == 0 {
		return MutationResponse{Success: false, Error: "slug and pr_number are required"}
	}

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}
	if plan.GitHub == nil {
		return MutationResponse{Success: false, Error: "plan has no GitHub metadata"}
	}

	plan.GitHub.PRNumber = req.PRNumber
	plan.GitHub.PRURL = req.PRURL
	plan.GitHub.PRState = "open"

	if err := ps.save(ctx, plan); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("GitHub PR metadata updated",
		"slug", req.Slug, "pr_number", req.PRNumber)

	return MutationResponse{Success: true}
}

// resetRequirementExecutionsByID resets specific requirement executions by ID,
// regardless of their current stage. Used by PR feedback to reset completed
// requirements for re-execution.
func (c *Component) resetRequirementExecutionsByID(ctx context.Context, slug string, reqIDs []string) (int, error) {
	var resetCount int
	for _, reqID := range reqIDs {
		key := "req." + slug + "." + reqID
		if err := c.sendReqReset(ctx, key); err != nil {
			c.logger.Warn("Failed to reset requirement execution by ID",
				"key", key, "error", err)
			continue
		}
		resetCount++
	}
	return resetCount, nil
}

// applyArchitectureRevise performs the state mutation for an accepted
// architecture_revise PlanDecision (RecoveryActionArchitectureRevise). It is
// the execution-phase counterpart of determineR2ReentryPoint's "architecture"
// case: capture the prior architecture so the architect REVISES rather than
// rewrites, wipe Architecture + Stories + Scenarios, reset every requirement
// execution so the re-run cannot reuse stale Story owner evidence via Tier-1 dedup,
// route the recovery diagnosis into ReviewFormattedFindings (the channel the
// architecture-generator already reads on revision rounds — component.go:301),
// and drive the back-transition implementing → requirements_generated so the
// architect re-fires.
//
// Called inline from both accept paths (mutation + HTTP). It does the status
// mutation IN PLACE rather than via setPlanStatusCached for the same reason
// the story_reprepare block does: the trailing ps.save in the caller is the
// sole persist point, so an inline status set avoids a double save / double
// watcher event. The caller skips applyRecoveryHint for this kind — the
// diagnosis reaches the architect through ReviewFormattedFindings, not through
// per-entity RecoveryHints (which would otherwise leak stale architecture
// context into a future developer prompt).
//
// The EXECUTION_STATES reset is I/O (NATS request/reply via resetRequirement-
// Executions, scope "all"); a reset error is returned so the caller can fail
// the accept rather than half-apply the revision. A back-transition that the
// DAG rejects (plan already moved past implementing while a human-review
// window was open) leaves the plan in place and logs a warning — consistent
// with the story_reprepare block's defensive handling.
func (c *Component) applyArchitectureRevise(ctx context.Context, plan *workflow.Plan, proposal *workflow.PlanDecision) error {
	// Check the back-transition is legal BEFORE any mutation or I/O. An
	// out-of-window accept (plan moved past implementing while the accept
	// landed late) must be a clean no-op — wiping a terminal plan's entities
	// or deleting its executions would corrupt it (closes go-reviewer M2).
	from := plan.EffectiveStatus()
	if !from.CanTransitionTo(workflow.StatusRequirementsGenerated) {
		c.logger.Warn("architecture_revise accepted but plan cannot back-transition to requirements_generated; leaving plan untouched",
			"slug", plan.Slug, "proposal_id", proposal.ID, "current_status", from)
		return nil
	}

	// Reset all requirement executions BEFORE the in-memory wipe so a reset
	// failure aborts the accept with the plan untouched (no half-applied
	// revision). Detached context: the reset is N sequential NATS request/
	// replies and must finish even if the caller's request context is
	// cancelled mid-accept (closes go-reviewer H2/M3). Clearing EXECUTION_STATES
	// stops the re-run from fast-completing via the executor's Tier-1 dedup.
	resetCount, err := c.resetRequirementExecutions(context.WithoutCancel(ctx), plan.Slug, "all", nil)
	if err != nil {
		return fmt.Errorf("reset requirement executions for architecture_revise: %w", err)
	}

	reviseArchitectureState(plan, proposal)

	c.logger.Info("Architecture revise applied — plan re-queued for architecture regeneration",
		"slug", plan.Slug,
		"proposal_id", proposal.ID,
		"reset_count", resetCount,
		"from_status", from,
		"had_previous_architecture", plan.PreviousArchitectureJSON != "")
	return nil
}

// applyPlanDecisionAcceptEffects performs the kind-specific state mutation when
// a PlanDecision is accepted. Shared by the mutation and HTTP accept paths so
// both stay byte-identical. Branches on proposal.Kind:
//
//   - architecture_revise → applyArchitectureRevise (capture prior architecture,
//     wipe Architecture + Stories + Scenarios, reset all requirement executions,
//     drive implementing → requirements_generated). Routes the diagnosis through
//     ReviewFormattedFindings, so the recovery-hint + story_reprepare branches
//     are intentionally skipped.
//   - any other kind → apply the recovery hint when proposed_by=recovery-agent,
//     and for story_reprepare drive stories_generated/implementing →
//     preparing_stories so Sarah re-runs with Story.RecoveryHint set (Train C
//     step 4). The implementing path also resets requirement executions so
//     stale dev loops do not resume against the old story shape.
//
// The status mutations are done IN PLACE (NOT setPlanStatusCached): the caller's
// trailing save is the sole persist point, avoiding a double save / double
// watcher event. An out-of-window plan (already moved past the source status
// while a human-review window was open) is left in place with a warning.
func (c *Component) applyPlanDecisionAcceptEffects(ctx context.Context, plan *workflow.Plan, proposal *workflow.PlanDecision, slug string) error {
	if proposal.Kind == workflow.PlanDecisionKindArchitectureRevise {
		return c.applyArchitectureRevise(ctx, plan, proposal)
	}

	if proposal.ProposedBy == "recovery-agent" && proposal.Rationale != "" {
		applyRecoveryHint(plan, proposal)
	}

	if proposal.Kind == workflow.PlanDecisionKindStoryReprepare {
		if err := c.applyStoryReprepare(ctx, plan, proposal, slug); err != nil {
			return err
		}
	}
	return nil
}

func (c *Component) applyStoryReprepare(ctx context.Context, plan *workflow.Plan, proposal *workflow.PlanDecision, slug string) error {
	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusPreparingStories) {
		c.logger.Warn("Could not drive plan to preparing_stories on story_reprepare accept; plan stays in place",
			"slug", slug, "proposal_id", proposal.ID, "current_status", current)
		return nil
	}

	affectedReqIDs := affectedRequirementIDsForStoryReprepare(plan, proposal)
	if current == workflow.StatusImplementing {
		resetCount, err := c.resetRequirementExecutions(context.WithoutCancel(ctx), slug, "all", nil)
		if err != nil {
			return fmt.Errorf("reset requirement executions for story_reprepare: %w", err)
		}
		c.logger.Info("Story reprepare reset requirement executions",
			"slug", slug,
			"proposal_id", proposal.ID,
			"reset_count", resetCount)
	}

	plan.Scenarios = removeScenariosForStoryReprepare(plan.Scenarios, proposal, affectedReqIDs)
	plan.Status = workflow.StatusPreparingStories
	c.logger.Info("Story reprepare applied — plan re-queued for story preparation",
		"slug", slug,
		"proposal_id", proposal.ID,
		"from_status", current,
		"affected_reqs", len(affectedReqIDs))
	return nil
}

func affectedRequirementIDsForStoryReprepare(plan *workflow.Plan, proposal *workflow.PlanDecision) []string {
	seen := make(map[string]struct{}, len(proposal.AffectedReqIDs))
	add := func(id string) {
		if id == "" {
			return
		}
		seen[id] = struct{}{}
	}
	for _, id := range proposal.AffectedReqIDs {
		add(id)
	}
	if len(proposal.AffectedStoryIDs) > 0 {
		storySet := make(map[string]struct{}, len(proposal.AffectedStoryIDs))
		for _, id := range proposal.AffectedStoryIDs {
			storySet[id] = struct{}{}
		}
		for _, s := range plan.Stories {
			if _, ok := storySet[s.ID]; !ok {
				continue
			}
			for _, id := range s.RequirementIDs {
				add(id)
			}
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func removeScenariosForStoryReprepare(scenarios []workflow.Scenario, proposal *workflow.PlanDecision, affectedReqIDs []string) []workflow.Scenario {
	storySet := make(map[string]struct{}, len(proposal.AffectedStoryIDs))
	for _, id := range proposal.AffectedStoryIDs {
		if id != "" {
			storySet[id] = struct{}{}
		}
	}
	reqSet := make(map[string]struct{}, len(affectedReqIDs))
	for _, id := range affectedReqIDs {
		reqSet[id] = struct{}{}
	}
	out := make([]workflow.Scenario, 0, len(scenarios))
	for _, s := range scenarios {
		if len(storySet) > 0 {
			if _, ok := storySet[s.StoryID]; ok {
				continue
			}
		} else if _, ok := reqSet[s.RequirementID]; ok {
			continue
		}
		out = append(out, s)
	}
	return out
}

// reviseArchitectureState performs the pure in-memory state mutation for an
// accepted architecture_revise PlanDecision, with no I/O so it is unit-testable
// in isolation (the seam):
//
//   - route the recovery diagnosis into ReviewFormattedFindings (the channel
//     the architecture-generator reads on revision rounds);
//   - capture the prior architecture into PreviousArchitectureJSON so the
//     architect REVISES rather than rewrites (mirrors determineR2ReentryPoint's
//     "architecture" case);
//   - wipe Architecture + Stories + Scenarios;
//   - drive the back-transition implementing → requirements_generated.
//
// The wipe is gated behind the same CanTransitionTo check the caller performs,
// so it is also safe standalone: an out-of-window plan is left entirely
// untouched and the function reports transitioned=false (closes go-reviewer M2 —
// the wipe no longer precedes the transition check). Returns whether the
// transition was applied and the status evaluated from. The EXECUTION_STATES
// reset is the caller's responsibility — it is NATS I/O.
func reviseArchitectureState(plan *workflow.Plan, proposal *workflow.PlanDecision) (transitioned bool, from workflow.Status) {
	from = plan.EffectiveStatus()
	if !from.CanTransitionTo(workflow.StatusRequirementsGenerated) {
		return false, from
	}

	if proposal.Rationale != "" {
		plan.ReviewFormattedFindings = proposal.Rationale
	}

	// Clear any stale carry-over first so a failed marshal leaves no leftover.
	plan.PreviousArchitectureJSON = ""
	if plan.Architecture != nil {
		if b, err := json.Marshal(plan.Architecture); err == nil {
			plan.PreviousArchitectureJSON = string(b)
		}
	}
	plan.Architecture = nil
	plan.Stories = nil
	plan.Scenarios = nil

	plan.Status = workflow.StatusRequirementsGenerated
	return true, from
}

// determineR2ReentryPoint examines review findings to pick the minimal re-entry
// point for Round 2. When findings carry phase markers, the plan can retry only
// the affected phase instead of clearing everything.
//
// Priority (highest first): plan > requirements > architecture > stories > scenarios.
// If ANY error finding targets an earlier phase, re-entry cascades from there.
// Without phase markers, falls back to StatusApproved (clear everything).
//
// The "stories" phase case closes go-reviewer Pass-4 finding P4-C3.
// Pre-fix, story_rules.go emitted findings with Phase:"stories" but the
// switch had no matching case, so the cascade fell through to default and
// nuked Requirements + Architecture + Scenarios to re-run req-gen.
// Sarah's defective Story output survived the regen because the plan
// re-traversed the requirements path that didn't fail. With this case
// in place, story-phase findings clear only Stories + Scenarios and
// return to StatusArchitectureGenerated — Sarah's watcher claim point.
func (c *Component) determineR2ReentryPoint(plan *workflow.Plan, findingsJSON json.RawMessage) workflow.Status {
	// Clear any stale revision carry-over up front; only the architecture
	// re-entry branch below re-captures it (just before nil-ing Architecture).
	// Without this, a PreviousArchitectureJSON left over from a failed prior
	// architecture re-run could leak into a plan/requirements re-entry whose
	// regenerated requirements no longer match that architecture.
	plan.PreviousArchitectureJSON = ""

	var findings []workflow.PlanReviewFinding
	if err := json.Unmarshal(findingsJSON, &findings); err != nil {
		// Can't parse findings — fall back to clear everything.
		plan.Requirements = nil
		plan.Architecture = nil
		plan.Stories = nil
		plan.Scenarios = nil
		return workflow.StatusApproved
	}

	// Collect error-severity phases from findings.
	phaseHit := map[string]bool{}
	hasPhaseMarker := false
	for _, f := range findings {
		if f.Severity != "error" || f.Status != "violation" {
			continue
		}
		if f.Phase != "" {
			phaseHit[f.Phase] = true
			hasPhaseMarker = true
		}
	}

	if !hasPhaseMarker {
		// No phase markers — fall back to clear everything (including Stories;
		// pre-fix the Stories slice survived this branch, which on a Sarah-
		// authored plan would leave stale Stories pinned to wiped Requirements).
		plan.Requirements = nil
		plan.Architecture = nil
		plan.Stories = nil
		plan.Scenarios = nil
		return workflow.StatusApproved
	}

	// Cascade: earlier phases force re-entry from their start point.
	switch {
	case phaseHit["plan"]:
		// Re-draft from scratch.
		plan.Requirements = nil
		plan.Architecture = nil
		plan.Stories = nil
		plan.Scenarios = nil
		return workflow.StatusCreated

	case phaseHit["requirements"]:
		// Re-generate requirements (and downstream architecture / stories / scenarios).
		plan.Requirements = nil
		plan.Architecture = nil
		plan.Stories = nil
		plan.Scenarios = nil
		return workflow.StatusApproved

	case phaseHit["architecture"]:
		// Re-generate architecture (and downstream stories + scenarios).
		// Capture the prior architecture so the architect revises it instead
		// of rewriting from scratch (mirrors planner PreviousPlanJSON).
		if plan.Architecture != nil {
			if b, err := json.Marshal(plan.Architecture); err == nil {
				plan.PreviousArchitectureJSON = string(b)
			}
		}
		plan.Architecture = nil
		plan.Stories = nil
		plan.Scenarios = nil
		return workflow.StatusRequirementsGenerated

	case phaseHit["stories"]:
		// Re-run Sarah only; preserve Requirements + Architecture. Closes
		// Pass-4 P4-C3 — pre-fix this case was missing and story findings
		// fell to default, nuking everything.
		plan.Stories = nil
		plan.Scenarios = nil
		return workflow.StatusArchitectureGenerated

	case phaseHit["scenarios"]:
		// Re-generate scenarios only, preserve requirements + architecture +
		// stories. The architect's Sarah-prepared Stories stay; only Bob's
		// scenario emission is re-run.
		plan.Scenarios = nil
		return workflow.StatusArchitectureGenerated

	default:
		// Unknown phase values — fall back to clear everything.
		plan.Requirements = nil
		plan.Architecture = nil
		plan.Stories = nil
		plan.Scenarios = nil
		return workflow.StatusApproved
	}
}

// planDecisionAddRequest carries a single new PlanDecision to append to a
// plan. Sent by upstream components (requirement-executor on retry exhaust,
// future sources) so plan-manager remains the single writer.
type planDecisionAddRequest struct {
	Slug     string                `json:"slug"`
	Decision workflow.PlanDecision `json:"decision"`
}

// handlePlanDecisionAddMutation appends a single PlanDecision to the plan's
// decisions slice. For ExecutionExhausted kind, any existing open
// (proposed/under_review) decisions targeting the same requirement IDs are
// archived first — exhaustion should not accumulate parallel open records
// for the same requirement; the newer one always supersedes.
func (c *Component) handlePlanDecisionAddMutation(ctx context.Context, data []byte) MutationResponse {
	var req planDecisionAddRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" {
		return MutationResponse{Success: false, Error: "slug is required"}
	}
	if req.Decision.ID == "" {
		return MutationResponse{Success: false, Error: "decision.id is required"}
	}
	if req.Decision.PlanID == "" {
		return MutationResponse{Success: false, Error: "decision.plan_id is required"}
	}
	// Back-compat default — zero-valued Kind maps to the qa-reviewer path.
	if req.Decision.Kind == "" {
		req.Decision.Kind = workflow.PlanDecisionKindRequirementChange
	}
	if !req.Decision.Kind.IsValid() {
		return MutationResponse{Success: false, Error: fmt.Sprintf("invalid decision kind: %q", req.Decision.Kind)}
	}
	if req.Decision.Status == "" {
		req.Decision.Status = workflow.PlanDecisionStatusProposed
	}
	if req.Decision.CreatedAt.IsZero() {
		req.Decision.CreatedAt = time.Now()
	}

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()
	if ps == nil {
		return MutationResponse{Success: false, Error: "plan store not ready"}
	}

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}

	// For execution_exhausted, supersede earlier open decisions on the same
	// requirement set so we don't accumulate stale records every retry cycle.
	if req.Decision.Kind == workflow.PlanDecisionKindExecutionExhausted {
		supersededIDs := c.archiveOpenExhaustionDecisionsLocked(plan, req.Decision.AffectedReqIDs)
		if len(supersededIDs) > 0 {
			c.logger.Info("Superseded open exhaustion decisions",
				"slug", req.Slug,
				"superseded", supersededIDs,
				"replacement", req.Decision.ID,
			)
		}
	}

	plan.PlanDecisions = append(plan.PlanDecisions, req.Decision)
	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to save plan after plan_decision add",
			"slug", req.Slug, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("Plan decision added",
		"slug", req.Slug,
		"decision_id", req.Decision.ID,
		"kind", req.Decision.Kind,
		"affected", req.Decision.AffectedReqIDs,
	)
	return MutationResponse{Success: true}
}

// planDecisionAcceptRequest is the NATS-side counterpart of the HTTP
// accept endpoint. Decoupling lets plan-decision-handler's auto-accept
// watcher invoke the same logic without a synthetic HTTP request.
type planDecisionAcceptRequest struct {
	Slug       string `json:"slug"`
	ProposalID string `json:"proposal_id"`
	// AcceptedBy identifies the caller — "user" (HTTP), "auto:recovery"
	// (auto-accept watcher). Surfaces in logs so operators can tell who
	// closed the decision without having to cross-reference timestamps.
	AcceptedBy string `json:"accepted_by,omitempty"`
}

// handlePlanDecisionAcceptMutation accepts a proposed PlanDecision in
// the plan, applies any recovery-agent-shaped side effects (req
// RecoveryHint via applyRecoveryHint), saves, and publishes a cascade
// trigger. Same logical contract as the HTTP handleAcceptPlanDecision
// — both must stay in sync; future apply logic should land here so
// the HTTP path inherits it.
//
// Returns ok=true when the proposal transitions to accepted; returns
// ok=false with an error message when the proposal is missing or in a
// status that can't transition to accepted.
func (c *Component) handlePlanDecisionAcceptMutation(ctx context.Context, data []byte) MutationResponse {
	var req planDecisionAcceptRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return MutationResponse{Success: false, Error: fmt.Sprintf("unmarshal: %v", err)}
	}
	if req.Slug == "" || req.ProposalID == "" {
		return MutationResponse{Success: false, Error: "slug and proposal_id are required"}
	}
	acceptedBy := req.AcceptedBy
	if acceptedBy == "" {
		acceptedBy = "auto"
	}

	defer c.lockSlug(req.Slug)()

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()
	if ps == nil {
		return MutationResponse{Success: false, Error: "plan store not ready"}
	}

	plan, ok := ps.get(req.Slug)
	if !ok {
		return MutationResponse{Success: false, Error: "plan not found"}
	}
	proposal, idx := plan.FindPlanDecision(req.ProposalID)
	if idx == -1 || proposal == nil {
		return MutationResponse{Success: false, Error: "plan_decision not found"}
	}
	if !proposal.Status.CanTransitionTo(workflow.PlanDecisionStatusAccepted) {
		return MutationResponse{Success: false, Error: fmt.Sprintf("cannot accept in status %q", proposal.Status)}
	}

	now := time.Now()
	proposal.Status = workflow.PlanDecisionStatusAccepted
	proposal.DecidedAt = &now

	if err := c.applyPlanDecisionAcceptEffects(ctx, plan, proposal, req.Slug); err != nil {
		return MutationResponse{Success: false, Error: err.Error()}
	}

	if err := ps.save(ctx, plan); err != nil {
		c.logger.Error("Failed to save plan after auto-accepting plan_decision",
			"slug", req.Slug, "proposal_id", req.ProposalID, "error", err)
		return MutationResponse{Success: false, Error: fmt.Sprintf("save: %v", err)}
	}

	c.logger.Info("Plan decision accepted via mutation",
		"slug", req.Slug,
		"proposal_id", req.ProposalID,
		"accepted_by", acceptedBy,
		"proposed_by", proposal.ProposedBy,
		"kind", proposal.Kind,
	)

	// Publish cascade trigger. Detach from request context so the
	// publish completes even if the caller's deadline expires; the
	// proposal status mutation has already landed and we want the
	// cascade to follow regardless.
	if c.natsClient != nil {
		cascadeReq := &payloads.PlanDecisionCascadeRequest{
			ProposalID: req.ProposalID,
			Slug:       req.Slug,
		}
		baseMsg := message.NewBaseMessage(cascadeReq.Schema(), cascadeReq, "plan-manager")
		cascadeData, err := json.Marshal(baseMsg)
		if err != nil {
			c.logger.Error("Failed to marshal cascade request",
				"proposal_id", req.ProposalID, "error", err)
		} else {
			pubCtx, pubCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer pubCancel()
			if err := c.natsClient.PublishToStream(pubCtx, c.config.CascadeTriggerSubject, cascadeData); err != nil {
				c.logger.Error("Failed to publish cascade request after auto-accept",
					"proposal_id", req.ProposalID, "subject", c.config.CascadeTriggerSubject, "error", err)
			} else {
				c.logger.Info("Published cascade request (auto-accept)",
					"slug", req.Slug, "proposal_id", req.ProposalID, "subject", c.config.CascadeTriggerSubject)
			}
		}
	}
	return MutationResponse{Success: true}
}

// archiveOpenExhaustionDecisionsLocked archives any existing proposed /
// under_review ExecutionExhausted decisions on the plan whose AffectedReqIDs
// overlap with the given set. Returns the IDs that were superseded.
//
// Caller must ensure the plan object is mutable (single-writer discipline
// inside the mutation handler satisfies this).
func (c *Component) archiveOpenExhaustionDecisionsLocked(plan *workflow.Plan, reqIDs []string) []string {
	if len(reqIDs) == 0 {
		return nil
	}
	wanted := make(map[string]struct{}, len(reqIDs))
	for _, id := range reqIDs {
		wanted[id] = struct{}{}
	}
	var superseded []string
	now := time.Now()
	for i := range plan.PlanDecisions {
		d := &plan.PlanDecisions[i]
		if d.Kind != workflow.PlanDecisionKindExecutionExhausted {
			continue
		}
		if d.Status != workflow.PlanDecisionStatusProposed && d.Status != workflow.PlanDecisionStatusUnderReview {
			continue
		}
		hit := false
		for _, id := range d.AffectedReqIDs {
			if _, ok := wanted[id]; ok {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		d.Status = workflow.PlanDecisionStatusArchived
		d.DecidedAt = &now
		superseded = append(superseded, d.ID)
	}
	return superseded
}
