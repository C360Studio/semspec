// Package workflow provides the Semspec workflow system for managing
// plans and tasks through a structured development process.
package workflow

import (
	"encoding/json"
	"time"
)

// Status represents the current state of a plan in the workflow.
type Status string

const (
	// StatusCreated indicates the plan has been created but not yet drafted.
	StatusCreated Status = "created"
	// StatusDrafted indicates the plan document has been generated.
	StatusDrafted Status = "drafted"
	// StatusReviewed indicates the plan has undergone SOP-aware review.
	StatusReviewed Status = "reviewed"
	// StatusApproved indicates the plan has been approved for execution.
	StatusApproved Status = "approved"
	// StatusRequirementsGenerated indicates requirements have been generated for the plan.
	StatusRequirementsGenerated Status = "requirements_generated"
	// StatusScenariosGenerated indicates scenarios have been generated for all requirements.
	StatusScenariosGenerated Status = "scenarios_generated"
	// StatusScenariosReviewed indicates scenarios have passed review but await human approval.
	// Set when auto_approve=false; the human clicks "Approve & Continue" to advance to ready_for_execution.
	StatusScenariosReviewed Status = "scenarios_reviewed"
	// StatusReadyForExecution indicates the plan has scenarios and is ready for the scenario
	// orchestrator to pick up and decompose into tasks at runtime (reactive execution mode).
	// This status is set by task-generator when reactive_mode=true, bypassing task generation.
	StatusReadyForExecution Status = "ready_for_execution"
	// StatusImplementing indicates task execution is in progress.
	StatusImplementing Status = "implementing"
	// StatusReviewingRollup indicates all scenarios have completed and the plan
	// is undergoing a final synthesis review before being marked complete.
	StatusReviewingRollup Status = "reviewing_rollup"
	// StatusComplete indicates all tasks have been completed successfully.
	StatusComplete Status = "complete"
	// StatusArchived indicates the plan has been archived.
	StatusArchived Status = "archived"
	// StatusRejected indicates the plan was rejected during review or approval.
	StatusRejected Status = "rejected"
	// StatusAwaitingReview indicates execution is done and the plan is waiting for
	// human approval before being marked complete. Gated by auto_approve_review
	// config (default true = skip this state). GitHub-originated plans always gate.
	StatusAwaitingReview Status = "awaiting_review"
	// StatusChanged indicates requirements were deprecated via a change proposal
	// and the plan is awaiting partial requirement regeneration.
	StatusChanged Status = "changed"
	// StatusReadyForQA indicates the plan is waiting for the QA executor (sandbox at
	// level=unit, qa-runner at level=integration or full) to run project tests.
	// Entered at implementing convergence when qa.level ≥ unit; skipped for
	// level=synthesis (which goes straight to reviewing_qa) and level=none.
	StatusReadyForQA Status = "ready_for_qa"
	// StatusReviewingQA indicates qa-reviewer is producing the release-readiness
	// verdict. Entered for all non-"none" levels. Inputs vary by level:
	// synthesis reads plan+impl only; unit/integration/full add test results.
	StatusReviewingQA Status = "reviewing_qa"

	// In-progress statuses — claimed by watchers via plan.mutation.claim before
	// starting work. Plan-manager's single-writer serialization ensures only one
	// claim succeeds per transition. Prevents KV watcher re-triggers on partial saves.

	// StatusDrafting indicates a planner has claimed plan creation.
	StatusDrafting Status = "drafting"
	// StatusReviewingDraft indicates plan-reviewer R1 has claimed the drafted plan.
	StatusReviewingDraft Status = "reviewing_draft"
	// StatusGeneratingRequirements indicates requirement-generator has claimed the approved plan.
	StatusGeneratingRequirements Status = "generating_requirements"
	// StatusGeneratingArchitecture indicates architecture-generator has claimed requirements_generated.
	StatusGeneratingArchitecture Status = "generating_architecture"
	// StatusGeneratingScenarios indicates scenario-generator has claimed architecture_generated.
	StatusGeneratingScenarios Status = "generating_scenarios"
	// StatusReviewingScenarios indicates plan-reviewer R2 has claimed scenarios_generated.
	StatusReviewingScenarios Status = "reviewing_scenarios"
)

const (
	// StatusArchitectureGenerated indicates the architecture phase has completed.
	// This is a terminal for the architecture phase (not in-progress).
	StatusArchitectureGenerated Status = "architecture_generated"
)

// String returns the string representation of the status.
func (s Status) String() string {
	return string(s)
}

// QALevel describes the project-level quality-assurance depth applied at
// plan completion. Each level is a strict superset of the previous and uses
// different execution substrate.
type QALevel string

const (
	// QALevelNone skips qa-reviewer entirely. Escape hatch for doc-only hotfix
	// plans; un-advertised and should not be the default.
	QALevelNone QALevel = "none"
	// QALevelSynthesis runs qa-reviewer with no test execution. Preserves the
	// pre-QA-phase rollup review behavior. Default.
	QALevelSynthesis QALevel = "synthesis"
	// QALevelUnit runs the project's existing test suite in the sandbox at
	// plan-completion time, then qa-reviewer interprets. No qa-runner needed.
	QALevelUnit QALevel = "unit"
	// QALevelIntegration runs .github/workflows/qa.yml via the qa-runner
	// container (act-based). Adds integration-tagged tests with real fixtures.
	QALevelIntegration QALevel = "integration"
	// QALevelFull adds e2e browser flows (Playwright) on top of integration,
	// with screenshot/trace/video artifact collection.
	QALevelFull QALevel = "full"
)

// String returns the string representation of the QA level.
func (l QALevel) String() string {
	return string(l)
}

// IsValid returns true if the level is one of the defined values.
func (l QALevel) IsValid() bool {
	switch l {
	case QALevelNone, QALevelSynthesis, QALevelUnit, QALevelIntegration, QALevelFull:
		return true
	default:
		return false
	}
}

// UsesQARunner returns true if this level requires the qa-runner container.
// Synthesis runs in-process; unit runs in the sandbox; integration and full
// require the act-based qa-runner.
func (l QALevel) UsesQARunner() bool {
	return l == QALevelIntegration || l == QALevelFull
}

// UsesSandboxTests returns true if this level runs the project's test suite
// in the sandbox at plan-completion time.
func (l QALevel) UsesSandboxTests() bool {
	return l == QALevelUnit
}

// IsValid returns true if the status is a valid workflow status.
func (s Status) IsValid() bool {
	switch s {
	case StatusCreated, StatusDrafted, StatusReviewed, StatusApproved,
		StatusRequirementsGenerated, StatusArchitectureGenerated,
		StatusScenariosGenerated, StatusScenariosReviewed,
		StatusReadyForExecution,
		StatusImplementing, StatusReviewingRollup, StatusAwaitingReview, StatusComplete, StatusArchived, StatusRejected,
		StatusChanged, StatusReadyForQA, StatusReviewingQA,
		StatusDrafting, StatusReviewingDraft, StatusGeneratingRequirements,
		StatusGeneratingArchitecture, StatusGeneratingScenarios, StatusReviewingScenarios:
		return true
	default:
		return false
	}
}

// IsInProgress returns true if the status is an intermediate in-progress state
// claimed by a watcher before starting work.
func (s Status) IsInProgress() bool {
	switch s {
	case StatusDrafting, StatusReviewingDraft, StatusGeneratingRequirements,
		StatusGeneratingArchitecture, StatusGeneratingScenarios, StatusReviewingScenarios:
		return true
	default:
		return false
	}
}

// CanTransitionTo returns true if the status can transition to the target status.
func (s Status) CanTransitionTo(target Status) bool {
	switch s {
	case StatusCreated:
		return target == StatusDrafting || target == StatusDrafted || target == StatusRejected
	case StatusDrafting:
		return target == StatusDrafted || target == StatusRejected
	case StatusDrafted:
		// drafted → reviewing_draft (plan-reviewer R1 claims)
		// drafted → requirements_generated (new flow: req/scenario gen happens before review)
		// drafted → reviewed (legacy: review directly after drafting)
		// drafted → rejected (rejection at any stage)
		return target == StatusReviewingDraft || target == StatusRequirementsGenerated || target == StatusReviewed || target == StatusRejected
	case StatusReviewingDraft:
		// reviewing_draft → reviewed (R1 approved)
		// reviewing_draft → created (R1 retry — revision loop, ADR-029)
		// reviewing_draft → rejected (escalation)
		return target == StatusReviewed || target == StatusCreated || target == StatusRejected
	case StatusReviewed:
		return target == StatusApproved || target == StatusRejected
	case StatusApproved:
		// approved → generating_requirements (requirement-generator claims)
		// approved → requirements_generated (auto-cascade: generate requirements)
		// approved → scenarios_generated (auto-cascade race: requirements existed, skip to scenarios)
		// approved → ready_for_execution (auto-approve skips req/scenario step)
		// approved → rejected (review loop escalation)
		return target == StatusGeneratingRequirements || target == StatusRequirementsGenerated || target == StatusScenariosGenerated ||
			target == StatusReadyForExecution || target == StatusRejected
	case StatusGeneratingRequirements:
		return target == StatusRequirementsGenerated || target == StatusRejected
	case StatusRequirementsGenerated:
		// requirements_generated → generating_architecture (architecture-generator claims)
		// requirements_generated → architecture_generated (skip path: already done or bypassed)
		// requirements_generated → changed (change proposal deprecated requirements)
		// requirements_generated → rejected (validation failure)
		return target == StatusGeneratingArchitecture || target == StatusArchitectureGenerated ||
			target == StatusChanged || target == StatusRejected
	case StatusGeneratingArchitecture:
		// generating_architecture → architecture_generated (done or skip)
		// generating_architecture → rejected (fatal error)
		return target == StatusArchitectureGenerated || target == StatusRejected
	case StatusArchitectureGenerated:
		// architecture_generated → generating_scenarios (scenario-generator claims)
		// architecture_generated → scenarios_generated (auto-cascade)
		// architecture_generated → changed (change proposal deprecated requirements)
		// architecture_generated → rejected (validation failure)
		return target == StatusGeneratingScenarios || target == StatusScenariosGenerated ||
			target == StatusChanged || target == StatusRejected
	case StatusGeneratingScenarios:
		return target == StatusScenariosGenerated || target == StatusRejected
	case StatusScenariosGenerated:
		// scenarios_generated → reviewing_scenarios (plan-reviewer R2 claims)
		// scenarios_generated → scenarios_reviewed (R2 done, auto_approve=false)
		// scenarios_generated → reviewed (review happens after scenario generation)
		// scenarios_generated → ready_for_execution (reactive mode — task-generator reactive_mode=true, review skipped)
		// scenarios_generated → changed (change proposal deprecated requirements)
		// scenarios_generated → rejected (validation failure)
		return target == StatusReviewingScenarios || target == StatusScenariosReviewed || target == StatusReviewed ||
			target == StatusReadyForExecution || target == StatusChanged || target == StatusRejected
	case StatusReviewingScenarios:
		// reviewing_scenarios → scenarios_reviewed (R2 approved, auto_approve=false)
		// reviewing_scenarios → reviewed (R2 approved, legacy path)
		// reviewing_scenarios → ready_for_execution (R2 approved, auto_approve=true)
		// reviewing_scenarios → approved (R2 retry — revision loop, ADR-029)
		// reviewing_scenarios → created (R2 phase-targeted retry — plan phase failed)
		// reviewing_scenarios → requirements_generated (R2 phase-targeted retry — architecture failed)
		// reviewing_scenarios → architecture_generated (R2 phase-targeted retry — scenarios failed)
		// reviewing_scenarios → rejected (escalation)
		return target == StatusScenariosReviewed || target == StatusReviewed || target == StatusReadyForExecution ||
			target == StatusApproved || target == StatusCreated || target == StatusRequirementsGenerated ||
			target == StatusArchitectureGenerated || target == StatusRejected
	case StatusScenariosReviewed:
		// scenarios_reviewed → ready_for_execution (human clicks "Approve & Continue")
		// scenarios_reviewed → changed (change proposal deprecated requirements)
		return target == StatusReadyForExecution || target == StatusChanged || target == StatusRejected
	case StatusReadyForExecution:
		// ready_for_execution → implementing (scenario orchestrator picks up the plan)
		// ready_for_execution → changed (change proposal deprecated requirements)
		// ready_for_execution → rejected (orchestration failure)
		return target == StatusImplementing || target == StatusChanged || target == StatusRejected
	case StatusImplementing:
		// implementing → reviewing_rollup (legacy alias for reviewing_qa; still emitted today until Phase 2f branch-point move)
		// implementing → reviewing_qa (level=synthesis after Phase 2f)
		// implementing → ready_for_qa (level=unit|integration|full after Phase 2f; executor runs tests before review)
		// implementing → awaiting_review (no QA, auto_approve_review=false or GitHub)
		// implementing → complete (level=none; direct terminal with no review)
		// implementing → changed (change proposal deprecated requirements)
		// implementing → rejected (execution escalation)
		return target == StatusReviewingRollup || target == StatusReviewingQA || target == StatusReadyForQA ||
			target == StatusAwaitingReview || target == StatusComplete ||
			target == StatusChanged || target == StatusRejected
	case StatusReviewingRollup:
		// Legacy state — equivalent to reviewing_qa at level=synthesis. Kept for
		// in-flight plans at upgrade time. New code should emit reviewing_qa.
		// reviewing_rollup → awaiting_review (qa-reviewer approved, auto_approve_review=false or GitHub)
		// reviewing_rollup → complete (qa-reviewer approved, auto_approve_review=true, no GitHub)
		// reviewing_rollup → rejected (qa-reviewer flagged critical issues)
		return target == StatusAwaitingReview ||
			target == StatusComplete || target == StatusRejected
	case StatusReadyForQA:
		// ready_for_qa → reviewing_qa (qa-runner claims the plan)
		// ready_for_qa → rejected (qa-runner cannot execute — missing workflow, infrastructure error)
		return target == StatusReviewingQA || target == StatusRejected
	case StatusReviewingQA:
		// reviewing_qa → complete (QA passed, auto_approve_review=true, no GitHub)
		// reviewing_qa → awaiting_review (QA passed, auto_approve_review=false or GitHub)
		// reviewing_qa → rejected (QA failed — qa-reviewer emits PlanDecisions)
		return target == StatusComplete || target == StatusAwaitingReview || target == StatusRejected
	case StatusAwaitingReview:
		// awaiting_review → complete (human approves: PR merge / UI / HTTP)
		// awaiting_review → ready_for_execution (human requests changes)
		// awaiting_review → rejected (human rejects)
		// awaiting_review → archived (human shelves)
		return target == StatusComplete || target == StatusReadyForExecution ||
			target == StatusRejected || target == StatusArchived
	case StatusComplete:
		// complete → archived (shelve)
		// complete → ready_for_execution (re-execute all requirements)
		// complete → changed (change proposal deprecated requirements)
		return target == StatusArchived || target == StatusReadyForExecution || target == StatusChanged
	case StatusArchived:
		// archived → complete (unarchive)
		// archived → ready_for_execution (unarchive + retry)
		return target == StatusComplete || target == StatusReadyForExecution
	case StatusChanged:
		// changed → generating_requirements (requirement-generator claims for partial regen)
		// changed → rejected (failure)
		return target == StatusGeneratingRequirements || target == StatusRejected
	case StatusRejected:
		// rejected → approved (manual R2 restart — human intervenes)
		// rejected → created (manual R1 restart — human intervenes after escalation, ADR-029)
		// rejected → ready_for_execution (retry failed requirements)
		// rejected → implementing (resume stalled plan — orchestrator already dispatched)
		return target == StatusApproved || target == StatusCreated ||
			target == StatusReadyForExecution || target == StatusImplementing
	default:
		return false
	}
}

// PlanRecord represents an active plan in the workflow.
// PlanRecords live in .semspec/plans/{slug}/ and contain metadata.json and tasks.md.
type PlanRecord struct {
	// Slug is the URL-friendly identifier for the plan
	Slug string `json:"slug"`

	// Title is the human-readable title
	Title string `json:"title"`

	// Description is the original description provided when creating the plan
	Description string `json:"description"`

	// Status is the current workflow state
	Status Status `json:"status"`

	// Author is the user who created the plan
	Author string `json:"author"`

	// CreatedAt is when the plan was created
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the plan was last modified
	UpdatedAt time.Time `json:"updated_at"`

	// Files tracks which files exist for this plan
	Files PlanFiles `json:"files"`

	// RelatedEntities contains graph entity IDs related to this plan
	RelatedEntities []string `json:"related_entities,omitempty"`

	// GitHub contains GitHub issue tracking metadata
	GitHub *GitHubMetadata `json:"github,omitempty"`
}

// GitHubMetadata tracks GitHub integration state for a plan (ADR-031).
type GitHubMetadata struct {
	// IssueNumber is the source GitHub issue number.
	IssueNumber int `json:"issue_number,omitempty"`

	// IssueURL is the web URL for the source issue.
	IssueURL string `json:"issue_url,omitempty"`

	// Repository is the GitHub repository (owner/repo format).
	Repository string `json:"repository,omitempty"`

	// PlanBranch is the plan-level branch name (e.g., semspec/<issue>-<slug>).
	PlanBranch string `json:"plan_branch,omitempty"`

	// PRNumber is the pull request number (set after PR creation).
	PRNumber int `json:"pr_number,omitempty"`

	// PRURL is the web URL for the pull request.
	PRURL string `json:"pr_url,omitempty"`

	// PRRevision tracks the current PR feedback round (0 = initial submission).
	PRRevision int `json:"pr_revision,omitempty"`

	// LastProcessedReviewID deduplicates review processing.
	LastProcessedReviewID int64 `json:"last_processed_review_id,omitempty"`

	// PRState tracks the last known PR state (open, merged, closed).
	PRState string `json:"pr_state,omitempty"`

	// LatestFeedback stores the most recent general PR review feedback body
	// (for reviews with no file-scoped comments). Replaced on each round.
	LatestFeedback string `json:"latest_feedback,omitempty"`

	// LastSynced is when the GitHub sync was last performed.
	LastSynced time.Time `json:"last_synced,omitempty"`
}

// PlanFiles tracks which files exist for a plan.
type PlanFiles struct {
	HasPlan          bool `json:"has_plan"`
	HasTasks         bool `json:"has_tasks"`
	HasRequirements  bool `json:"has_requirements"`
	HasScenarios     bool `json:"has_scenarios"`
	HasPlanDecisions bool `json:"has_plan_decisions"`
}

// Spec represents a specification in .semspec/specs/{name}/.
type Spec struct {
	// Name is the spec identifier
	Name string `json:"name"`

	// Title is the human-readable title
	Title string `json:"title"`

	// Version is the spec version
	Version string `json:"version"`

	// CreatedAt is when the spec was created
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the spec was last modified
	UpdatedAt time.Time `json:"updated_at"`

	// OriginPlan is the plan that created this spec (if any)
	OriginPlan string `json:"origin_plan,omitempty"`
}

// Principle represents a constitution principle.
type Principle struct {
	// Number is the principle number (e.g., 1, 2, 3)
	Number int `json:"number"`

	// Title is the principle title
	Title string `json:"title"`

	// Description is the full principle description
	Description string `json:"description"`

	// Rationale explains why this principle exists
	Rationale string `json:"rationale,omitempty"`
}

// Constitution represents the project constitution from .semspec/constitution.md.
type Constitution struct {
	// Version is the constitution version
	Version string `json:"version"`

	// Ratified is when the constitution was ratified
	Ratified time.Time `json:"ratified"`

	// Principles are the governing principles
	Principles []Principle `json:"principles"`
}

// CheckViolation represents a constitution violation found during /check.
type CheckViolation struct {
	// Principle is the principle that was violated
	Principle Principle `json:"principle"`

	// Message describes the violation
	Message string `json:"message"`

	// Location is where the violation was found (optional)
	Location string `json:"location,omitempty"`
}

// CheckResult represents the result of a constitution check.
type CheckResult struct {
	// Passed indicates if all checks passed
	Passed bool `json:"passed"`

	// Violations contains any violations found
	Violations []CheckViolation `json:"violations,omitempty"`

	// CheckedAt is when the check was performed
	CheckedAt time.Time `json:"checked_at"`
}

// Plan represents a structured development plan.
// Plans start as drafts (Approved=false) and must be approved
// via /approve command before task generation.
type Plan struct {
	// ID is the unique identifier for the plan entity
	ID string `json:"id"`

	// Slug is the URL-friendly identifier (used for file paths)
	Slug string `json:"slug"`

	// Title is the human-readable title
	Title string `json:"title"`

	// ProjectID is the entity ID of the parent project.
	// Format: {prefix}.wf.project.project.{project-slug}
	// Required - defaults to the "default" project if not specified.
	ProjectID string `json:"project_id"`

	// Status is the authoritative workflow state for the plan.
	// When empty, EffectiveStatus() infers status from legacy boolean fields.
	Status Status `json:"status,omitempty"`

	// Approved indicates if this plan is ready for execution.
	// false = draft plan, true = user explicitly approved
	Approved bool `json:"approved"`

	// CreatedAt is when the plan was created
	CreatedAt time.Time `json:"created_at"`

	// ApprovedAt is when the plan was approved for execution
	ApprovedAt *time.Time `json:"approved_at,omitempty"`

	// ReviewVerdict is the plan-reviewer's verdict: "approved", "needs_changes", or empty if not reviewed.
	ReviewVerdict string `json:"review_verdict,omitempty"`

	// ReviewSummary is the plan-reviewer's summary of findings.
	ReviewSummary string `json:"review_summary,omitempty"`

	// ReviewedAt is when the plan review completed.
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`

	// ReviewFindings contains the structured findings from the plan reviewer.
	// Stored as raw JSON to avoid coupling to the reviewer's output schema.
	// Updated on each review iteration and on escalation.
	ReviewFindings json.RawMessage `json:"review_findings,omitempty"`

	// ReviewFormattedFindings is the human-readable text version of findings.
	// Updated on each review iteration and on escalation.
	ReviewFormattedFindings string `json:"review_formatted_findings,omitempty"`

	// ReviewIteration is the number of review iterations completed.
	// Incremented on each revision event, set to max on escalation.
	// NOTE: This is a shared budget across both review rounds (R1 + R2).
	// If R1 uses 2 of 3 iterations, R2 only has 1 remaining before escalation.
	// This is intentional — it bounds total LLM review cost per plan.
	ReviewIteration int `json:"review_iteration,omitempty"`

	// LastError is the most recent error from a workflow step for this plan.
	// Set when user.signal.error fires — annotation only, does NOT change status.
	LastError string `json:"last_error,omitempty"`

	// LastErrorAt is when the last error occurred.
	LastErrorAt *time.Time `json:"last_error_at,omitempty"`

	// Goal describes what we're building or fixing
	Goal string `json:"goal,omitempty"`

	// Context describes the current state and why this matters
	Context string `json:"context,omitempty"`

	// Scope defines file/directory boundaries for this plan
	Scope Scope `json:"scope,omitempty"`

	// ExecutionTraceIDs tracks trace IDs from workflow executions.
	// Used by trajectory-api to aggregate LLM metrics per workflow.
	ExecutionTraceIDs []string `json:"execution_trace_ids,omitempty"`

	// LLMCallHistory tracks LLM request IDs per review iteration,
	// enabling the UI to drill down from any loop iteration to the
	// complete prompt/response via the /calls/ endpoint.
	LLMCallHistory *LLMCallHistory `json:"llm_call_history,omitempty"`

	// SkipArchitecture when true causes architecture-generator to pass through
	// immediately without dispatching an LLM agent. Set by the planner for simple
	// changes (config tweaks, single-file fixes, documentation updates).
	SkipArchitecture bool `json:"skip_architecture,omitempty"`

	// Architecture holds the output from the architecture-generator phase.
	// Nil when SkipArchitecture is true or before the phase completes.
	Architecture *ArchitectureDocument `json:"architecture,omitempty"`

	// Requirements, Scenarios, and PlanDecisions are populated when the plan
	// is written to the PLAN_STATES KV bucket so downstream watchers have
	// everything they need without follow-up queries.
	// Not persisted to graph triples — use SaveRequirements/SaveScenarios/SavePlanDecisions for that.
	Requirements  []Requirement  `json:"requirements,omitempty"`
	Scenarios     []Scenario     `json:"scenarios,omitempty"`
	PlanDecisions []PlanDecision `json:"plan_decisions,omitempty"`

	// GitHub contains GitHub integration metadata for plans originating from
	// GitHub issues (ADR-031). Nil for non-GitHub plans.
	GitHub *GitHubMetadata `json:"github,omitempty"`

	// QALevel is the plan's QA policy, snapshotted from the project default at
	// plan creation so a running plan is immutable under QA policy changes.
	// Empty string is treated as QALevelSynthesis (the behavior-preserving
	// default). Decides the branch point at implementing convergence:
	// none → complete; synthesis → reviewing_qa; unit/integration/full →
	// ready_for_qa + QARequestedEvent published for the appropriate executor.
	QALevel QALevel `json:"qa_level,omitempty"`

	// QARun captures the executor result for this plan's QA phase. Populated by
	// plan-manager when it consumes QACompletedEvent and transitions the plan
	// to reviewing_qa. qa-reviewer reads it from the plan KV instead of
	// subscribing to the one-shot event (which races the KV watcher).
	// Nil for synthesis-level plans (no executor run) and plans that have not
	// yet reached reviewing_qa.
	QARun *QARun `json:"qa_run,omitempty"`

	// AssembledBranch is the git branch onto which plan-manager merged every
	// completed requirement branch at plan-complete time (invariant B1 of
	// docs/audit/task-11-worktree-invariants.md). Empty on plans that
	// completed before B1 landed or when the sandbox is disabled; otherwise
	// points at "semspec/plan-<slug>" and contains the assembled work
	// ready for human review + merge-to-main. Named "Assembled" rather than
	// "Plan" to avoid colliding with Plan.GitHub.PlanBranch, which is an
	// unrelated GitHub-integration origin branch.
	AssembledBranch string `json:"assembled_branch,omitempty"`

	// AssembledMergeCommit is the SHA of the final merge commit on
	// AssembledBranch (the merge of the last requirement branch). Lets the
	// UI link to a single verifiable commit without having to re-walk the
	// branch.
	AssembledMergeCommit string `json:"assembled_merge_commit,omitempty"`
}

// QARun carries the executor result persisted on the plan at reviewing_qa.
// Mirrors the informative fields of QACompletedEvent minus slug/plan_id/level
// which already live on the plan.
type QARun struct {
	RunID       string          `json:"run_id"`
	Passed      bool            `json:"passed"`
	Failures    []QAFailure     `json:"failures,omitempty"`
	Artifacts   []QAArtifactRef `json:"artifacts,omitempty"`
	DurationMs  int64           `json:"duration_ms"`
	RunnerError string          `json:"runner_error,omitempty"`
	TraceID     string          `json:"trace_id,omitempty"`
	CompletedAt time.Time       `json:"completed_at"`
}

// EffectiveQALevel returns the plan's QA level, defaulting to synthesis when
// unset. Centralized so the branch point and verdict handlers agree on the
// empty-value interpretation.
func (p *Plan) EffectiveQALevel() QALevel {
	if p.QALevel == "" {
		return QALevelSynthesis
	}
	return p.QALevel
}

// ArchitectureDocument captures the output of the architecture phase.
// It is attached to the plan when architecture-generator completes.
type ArchitectureDocument struct {
	TechnologyChoices   []TechChoice       `json:"technology_choices"`
	ComponentBoundaries []ComponentDef     `json:"component_boundaries"`
	DataFlow            string             `json:"data_flow"`
	Decisions           []ArchDecision     `json:"decisions"`
	Actors              []ActorDef         `json:"actors"`
	Integrations        []IntegrationPoint `json:"integrations"`
	// TestSurface declares the test coverage the architecture implies. Consumed
	// by the developer role to guide integration/e2e test authoring, and by
	// qa-reviewer (Phase 6) to judge coverage adequacy against what was
	// actually implemented. Optional for backward compat with plans drafted
	// before the field existed.
	TestSurface *TestSurface `json:"test_surface,omitempty"`
}

// TestSurface describes the test coverage implied by an ArchitectureDocument.
// Integration flows derive from Integrations[] (each external boundary deserves
// an integration test). E2E flows derive from Actors[] (each human/system actor
// triggers a user-visible flow worth end-to-end coverage).
type TestSurface struct {
	IntegrationFlows []IntegrationFlow `json:"integration_flows,omitempty"`
	E2EFlows         []E2EFlow         `json:"e2e_flows,omitempty"`
}

// IntegrationFlow describes a cross-component flow that needs integration
// tests (real service fixtures, not unit mocks).
type IntegrationFlow struct {
	Name               string   `json:"name"`
	ComponentsInvolved []string `json:"components_involved"`
	Description        string   `json:"description"`
	ScenarioRefs       []string `json:"scenario_refs,omitempty"`
}

// E2EFlow describes an actor-driven user-visible flow that needs end-to-end
// tests (browser, full stack, real data).
type E2EFlow struct {
	Actor           string   `json:"actor"`
	Steps           []string `json:"steps"`
	SuccessCriteria []string `json:"success_criteria"`
}

// TechChoice records a single technology selection with its rationale.
type TechChoice struct {
	Category  string `json:"category"`  // e.g. "database", "framework", "messaging"
	Choice    string `json:"choice"`    // e.g. "PostgreSQL", "Svelte 5"
	Rationale string `json:"rationale"` // why this choice was made
}

// ComponentDef describes a system component and its responsibilities.
type ComponentDef struct {
	Name           string   `json:"name"`
	Responsibility string   `json:"responsibility"`
	Dependencies   []string `json:"dependencies"`
}

// ArchDecision is a single architecture decision record produced by the architect agent.
type ArchDecision struct {
	ID        string `json:"id"` // e.g. "ARCH-001"
	Title     string `json:"title"`
	Decision  string `json:"decision"`
	Rationale string `json:"rationale"`
}

// ActorDef describes who or what initiates actions in the system.
type ActorDef struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"` // human | system | scheduler | event
	Triggers    []string `json:"triggers"`
	Permissions []string `json:"permissions,omitempty"`
}

// IntegrationPoint describes an external boundary the system touches.
type IntegrationPoint struct {
	Name      string `json:"name"`
	Direction string `json:"direction"`            // inbound | outbound | bidirectional
	Protocol  string `json:"protocol"`             // http | nats | grpc | db | filesystem
	Contract  string `json:"contract,omitempty"`   // schema ref or description
	ErrorMode string `json:"error_mode,omitempty"` // what happens on failure
}

// FindRequirement returns a pointer into p.Requirements and its index by ID.
// Returns nil, -1 when the requirement is not found.
func (p *Plan) FindRequirement(id string) (*Requirement, int) {
	for i := range p.Requirements {
		if p.Requirements[i].ID == id {
			return &p.Requirements[i], i
		}
	}
	return nil, -1
}

// FindScenario returns a pointer into p.Scenarios and its index by ID.
// Returns nil, -1 when the scenario is not found.
func (p *Plan) FindScenario(id string) (*Scenario, int) {
	for i := range p.Scenarios {
		if p.Scenarios[i].ID == id {
			return &p.Scenarios[i], i
		}
	}
	return nil, -1
}

// ScenariosForRequirement returns all scenarios whose RequirementID matches reqID.
func (p *Plan) ScenariosForRequirement(reqID string) []Scenario {
	var out []Scenario
	for _, s := range p.Scenarios {
		if s.RequirementID == reqID {
			out = append(out, s)
		}
	}
	return out
}

// FindPlanDecision returns a pointer into p.PlanDecisions and its index by ID.
// Returns nil, -1 when the proposal is not found.
func (p *Plan) FindPlanDecision(id string) (*PlanDecision, int) {
	for i := range p.PlanDecisions {
		if p.PlanDecisions[i].ID == id {
			return &p.PlanDecisions[i], i
		}
	}
	return nil, -1
}

// LLMCallHistory tracks LLM request IDs per review iteration for both
// plan review and task review loops. This enables the UI to correlate
// each loop iteration with its specific LLM calls for full artifact drill-down.
type LLMCallHistory struct {
	PlanReview []IterationCalls `json:"plan_review,omitempty"`
}

// IterationCalls records the LLM request IDs used during a single review iteration.
type IterationCalls struct {
	Iteration     int      `json:"iteration"`
	LLMRequestIDs []string `json:"llm_request_ids"`
	Verdict       string   `json:"verdict,omitempty"`
}

// EffectiveStatus returns the plan's current status.
// If Status is explicitly set, it is returned directly.
// Otherwise, status is inferred from legacy boolean fields for backward compatibility
// with plan.json files that predate the Status field.
func (p *Plan) EffectiveStatus() Status {
	if p.Status != "" {
		return p.Status
	}
	// Infer from legacy boolean fields
	if p.Approved {
		return StatusApproved
	}
	if p.ReviewVerdict == "needs_changes" {
		return StatusReviewed
	}
	// ReviewVerdict tracks the reviewer's opinion; Approved tracks the user's
	// explicit approval. A plan can be reviewed-as-approved but not yet user-approved.
	if p.ReviewVerdict == "approved" {
		return StatusReviewed
	}
	if p.Goal != "" && p.Context != "" {
		return StatusDrafted
	}
	return StatusCreated
}

// Scope defines the file/directory boundaries for a plan.
type Scope struct {
	// Include lists files/directories in scope for this plan
	Include []string `json:"include,omitempty"`

	// Exclude lists files/directories explicitly out of scope
	Exclude []string `json:"exclude,omitempty"`

	// DoNotTouch lists protected files/directories that must not be modified
	DoNotTouch []string `json:"do_not_touch,omitempty"`
}

// TaskType classifies the kind of work a task represents.
// Used by trigger and execution payloads for pipeline selection.
type TaskType string

// RequirementStatus represents the lifecycle state of a requirement.
type RequirementStatus string

const (
	// RequirementStatusActive indicates the requirement is current and actionable.
	RequirementStatusActive RequirementStatus = "active"

	// RequirementStatusDeprecated indicates the requirement is no longer relevant.
	RequirementStatusDeprecated RequirementStatus = "deprecated"

	// RequirementStatusSuperseded indicates the requirement was replaced by another.
	RequirementStatusSuperseded RequirementStatus = "superseded"
)

// String returns the string representation of the requirement status.
func (s RequirementStatus) String() string {
	return string(s)
}

// IsValid returns true if the requirement status is valid.
func (s RequirementStatus) IsValid() bool {
	switch s {
	case RequirementStatusActive, RequirementStatusDeprecated, RequirementStatusSuperseded:
		return true
	default:
		return false
	}
}

// CanTransitionTo returns true if this requirement status can transition to the target.
func (s RequirementStatus) CanTransitionTo(target RequirementStatus) bool {
	switch s {
	case RequirementStatusActive:
		return target == RequirementStatusDeprecated || target == RequirementStatusSuperseded
	case RequirementStatusSuperseded:
		// Can revert supersession if PlanDecision is rolled back
		return target == RequirementStatusActive
	case RequirementStatusDeprecated:
		return false // Terminal state
	default:
		return false
	}
}

// Requirement represents a plan-level behavioral intent.
type Requirement struct {
	ID          string            `json:"id"`
	PlanID      string            `json:"plan_id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Status      RequirementStatus `json:"status"`
	DependsOn   []string          `json:"depends_on,omitempty"` // IDs of prerequisite requirements
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// ScenarioStatus represents the verification state of a scenario.
type ScenarioStatus string

const (
	// ScenarioStatusPending indicates the scenario has not yet been verified.
	ScenarioStatusPending ScenarioStatus = "pending"

	// ScenarioStatusPassing indicates the scenario is verified and passing.
	ScenarioStatusPassing ScenarioStatus = "passing"

	// ScenarioStatusFailing indicates the scenario is verified and failing.
	ScenarioStatusFailing ScenarioStatus = "failing"

	// ScenarioStatusSkipped indicates the scenario was intentionally skipped.
	ScenarioStatusSkipped ScenarioStatus = "skipped"
)

// String returns the string representation of the scenario status.
func (s ScenarioStatus) String() string {
	return string(s)
}

// IsValid returns true if the scenario status is valid.
func (s ScenarioStatus) IsValid() bool {
	switch s {
	case ScenarioStatusPending, ScenarioStatusPassing, ScenarioStatusFailing, ScenarioStatusSkipped:
		return true
	default:
		return false
	}
}

// CanTransitionTo returns true if this scenario status can transition to the target.
func (s ScenarioStatus) CanTransitionTo(target ScenarioStatus) bool {
	switch s {
	case ScenarioStatusPending:
		return target == ScenarioStatusPassing || target == ScenarioStatusFailing || target == ScenarioStatusSkipped
	case ScenarioStatusPassing:
		return target == ScenarioStatusFailing
	case ScenarioStatusFailing:
		return target == ScenarioStatusPassing
	case ScenarioStatusSkipped:
		return target == ScenarioStatusPending
	default:
		return false
	}
}

// Scenario represents a Given/When/Then behavioral contract derived from a Requirement.
type Scenario struct {
	ID            string         `json:"id"`
	RequirementID string         `json:"requirement_id"`
	Given         string         `json:"given"`
	When          string         `json:"when"`
	Then          []string       `json:"then"`
	Status        ScenarioStatus `json:"status"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

// PlanDecisionStatus represents the lifecycle state of a plan decision.
type PlanDecisionStatus string

const (
	// PlanDecisionStatusProposed indicates the decision has been raised for review.
	PlanDecisionStatusProposed PlanDecisionStatus = "proposed"

	// PlanDecisionStatusUnderReview indicates the decision is being reviewed.
	PlanDecisionStatusUnderReview PlanDecisionStatus = "under_review"

	// PlanDecisionStatusAccepted indicates the decision was accepted.
	// For Kind=requirement_change this triggers cascade; for
	// Kind=execution_exhausted it marks the record as human-acknowledged
	// without prescribing a plan mutation.
	PlanDecisionStatusAccepted PlanDecisionStatus = "accepted"

	// PlanDecisionStatusRejected indicates the decision was rejected.
	PlanDecisionStatusRejected PlanDecisionStatus = "rejected"

	// PlanDecisionStatusArchived indicates the decision has been archived —
	// either terminally resolved or auto-closed by plan-manager when the
	// subject requirement reached a non-failed terminal state.
	PlanDecisionStatusArchived PlanDecisionStatus = "archived"
)

// String returns the string representation of the plan decision status.
func (s PlanDecisionStatus) String() string {
	return string(s)
}

// IsValid returns true if the plan decision status is valid.
func (s PlanDecisionStatus) IsValid() bool {
	switch s {
	case PlanDecisionStatusProposed, PlanDecisionStatusUnderReview,
		PlanDecisionStatusAccepted, PlanDecisionStatusRejected, PlanDecisionStatusArchived:
		return true
	default:
		return false
	}
}

// CanTransitionTo returns true if this plan decision status can transition to the target.
func (s PlanDecisionStatus) CanTransitionTo(target PlanDecisionStatus) bool {
	switch s {
	case PlanDecisionStatusProposed:
		// proposed → under_review (manual review flow)
		// proposed → accepted (auto-accept shortcut, skips review)
		// proposed → archived (auto-close when subject requirement resolves)
		return target == PlanDecisionStatusUnderReview ||
			target == PlanDecisionStatusAccepted ||
			target == PlanDecisionStatusArchived
	case PlanDecisionStatusUnderReview:
		return target == PlanDecisionStatusAccepted ||
			target == PlanDecisionStatusRejected ||
			target == PlanDecisionStatusArchived
	case PlanDecisionStatusAccepted:
		return target == PlanDecisionStatusArchived
	case PlanDecisionStatusRejected:
		return target == PlanDecisionStatusArchived
	case PlanDecisionStatusArchived:
		return false // Terminal state
	default:
		return false
	}
}

// PlanDecisionKind narrows the intent of a PlanDecision so downstream handlers
// (cascade, UI, plan-manager auto-close) dispatch correctly. Same container,
// two distinct semantics:
//
//	requirement_change  — something proposes to mutate the plan's requirements
//	                      (e.g. qa-reviewer needs_changes). Accept runs cascade.
//	execution_exhausted — a requirement exhausted its retry budget and needs a
//	                      human to decide next step. Accept is acknowledgement
//	                      only; the actual remedy is taken via existing retry /
//	                      force-complete / reject endpoints.
type PlanDecisionKind string

const (
	// PlanDecisionKindRequirementChange marks a decision proposing a plan
	// mutation (e.g. qa-reviewer emitted needs_changes). Accept runs cascade.
	PlanDecisionKindRequirementChange PlanDecisionKind = "requirement_change"
	// PlanDecisionKindExecutionExhausted marks a decision recording a
	// requirement exhausting its retry budget. Accept is acknowledgement
	// only; the remedy comes from existing retry/force-complete/reject
	// endpoints, and plan-manager auto-archives the decision when the
	// subject requirement reaches a non-failed terminal state.
	PlanDecisionKindExecutionExhausted PlanDecisionKind = "execution_exhausted"
)

// String returns the string representation of the plan decision kind.
func (k PlanDecisionKind) String() string {
	return string(k)
}

// IsValid reports whether the kind is a known value.
func (k PlanDecisionKind) IsValid() bool {
	switch k {
	case PlanDecisionKindRequirementChange, PlanDecisionKindExecutionExhausted:
		return true
	default:
		return false
	}
}

// ArtifactRef is a reference to a QA artifact (log, screenshot, trace, coverage report)
// attached to a PlanDecision. Helps the human reviewer understand why the change is needed.
type ArtifactRef struct {
	// Path is the workspace-relative path to the artifact.
	Path string `json:"path"`
	// Type is the artifact category: log, screenshot, trace, or coverage-report.
	Type string `json:"type"`
	// Purpose describes what this artifact shows (e.g., "playwright flow X failure").
	Purpose string `json:"purpose,omitempty"`
}

// PlanDecision records any human-gated decision about a plan. Two kinds exist
// today: requirement_change (proposed plan mutation, e.g. qa-reviewer flagged
// needs_changes) and execution_exhausted (a requirement exhausted its retry
// budget and needs a human to choose next step). The container is shared;
// cascade/UI/plan-manager dispatch on Kind.
type PlanDecision struct {
	ID     string `json:"id"`
	PlanID string `json:"plan_id"`
	// Kind narrows the intent. Defaults to requirement_change for back-compat
	// with old records that predate the Kind field.
	Kind             PlanDecisionKind   `json:"kind,omitempty"`
	Title            string             `json:"title"`
	Rationale        string             `json:"rationale"`
	Status           PlanDecisionStatus `json:"status"`
	ProposedBy       string             `json:"proposed_by"`
	AffectedReqIDs   []string           `json:"affected_requirement_ids"`
	RejectionReasons map[string]string  `json:"rejection_reasons,omitempty"`
	// ArtifactReferences links artifacts (logs, screenshots, traces, trajectory
	// steps) to this decision. Populated by qa-reviewer on needs_changes and
	// by requirement-executor on retry exhaustion so the human reviewer can
	// see why the decision was raised.
	ArtifactReferences []ArtifactRef `json:"artifact_references,omitempty"`
	CreatedAt          time.Time     `json:"created_at"`
	ReviewedAt         *time.Time    `json:"reviewed_at,omitempty"`
	DecidedAt          *time.Time    `json:"decided_at,omitempty"`
}

// ContextPayload contains pre-built context for task execution.
// Built by context-builder and inlined by task-dispatcher.
type ContextPayload struct {
	// Documents maps file paths to their content
	Documents map[string]string `json:"documents,omitempty"`

	// Entities are references to graph entities included in context
	Entities []EntityRef `json:"entities,omitempty"`

	// SOPs contains SOP content relevant to the task
	SOPs []string `json:"sops,omitempty"`

	// TokenCount is the total token count for agent awareness
	TokenCount int `json:"token_count"`
}

// EntityRef is a reference to a graph entity in the context.
type EntityRef struct {
	// ID is the entity identifier
	ID string `json:"id"`

	// Type is the entity type (e.g., "sop", "function", "type")
	Type string `json:"type,omitempty"`

	// Content is the hydrated entity content
	Content string `json:"content,omitempty"`
}

// PlannerContext contains graph-derived context for a focused planner.
type PlannerContext struct {
	// Entities are entity IDs relevant to this focus area.
	Entities []string `json:"entities,omitempty"`

	// Files are file paths in scope for this focus area.
	Files []string `json:"files,omitempty"`

	// Summary is a brief context summary from the coordinator.
	Summary string `json:"summary,omitempty"`
}

// PlanSession tracks a multi-planner coordination session.
type PlanSession struct {
	// SessionID uniquely identifies this session.
	SessionID string `json:"session_id"`

	// Slug is the plan slug.
	Slug string `json:"slug"`

	// Title is the plan title.
	Title string `json:"title"`

	// Status tracks session progress: "coordinating", "planning", "synthesizing", "complete", "failed".
	Status string `json:"status"`

	// Planners maps planner IDs to their state.
	Planners map[string]*PlannerState `json:"planners,omitempty"`

	// CreatedAt is when the session started.
	CreatedAt time.Time `json:"created_at"`

	// CompletedAt is when the session finished.
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// PlannerState tracks an individual planner within a session.
type PlannerState struct {
	// ID uniquely identifies this planner.
	ID string `json:"id"`

	// FocusArea is the area this planner is focusing on.
	FocusArea string `json:"focus_area"`

	// Status is the planner's progress: "pending", "running", "completed", "failed".
	Status string `json:"status"`

	// Result contains the planner's output once completed.
	Result *PlannerResult `json:"result,omitempty"`

	// Error contains error details if failed.
	Error string `json:"error,omitempty"`

	// StartedAt is when this planner started.
	StartedAt *time.Time `json:"started_at,omitempty"`

	// CompletedAt is when this planner finished.
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// PlannerResult contains the output from a focused planner.
type PlannerResult struct {
	// PlannerID identifies which planner produced this result.
	PlannerID string `json:"planner_id"`

	// FocusArea is the area this planner focused on.
	FocusArea string `json:"focus_area"`

	// Goal is the goal from this planner's perspective.
	Goal string `json:"goal"`

	// Context is the context from this planner's perspective.
	Context string `json:"context"`

	// Scope is the scope from this planner's perspective.
	Scope Scope `json:"scope"`
}
