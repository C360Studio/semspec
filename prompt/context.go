package prompt

import (
	"slices"

	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/workflow"
)

// AssemblyContext provides all information needed to select and compose
// the right fragments for a specific prompt assembly.
type AssemblyContext struct {
	// Role is the workflow role (developer, planner, reviewer, etc).
	Role Role

	// Provider is the LLM provider for formatting (anthropic, openai, ollama).
	Provider Provider

	// Capability is the resolved model capability.
	Capability model.Capability

	// Domain selects the domain catalog ("software", "research").
	Domain string

	// AvailableTools lists tool names available to this agent.
	AvailableTools []string

	// SupportsTools indicates whether the resolved model supports tool calling.
	SupportsTools bool

	// TaskContext carries task-specific data for developer prompts.
	TaskContext *TaskContext

	// PlanContext carries plan-specific data for planner prompts.
	PlanContext *PlanContext

	// ReviewContext carries review-specific data for reviewer prompts.
	ReviewContext *ReviewContext

	// LessonsLearned carries role-scoped lesson data for prompt injection.
	LessonsLearned *LessonsLearned

	// Standards carries role-filtered project standards for prompt injection.
	Standards *StandardsContext

	// ScenarioReviewContext carries data for scenario-level review prompts.
	ScenarioReviewContext *ScenarioReviewContext

	// QAReviewContext carries data for QA release-readiness review prompts.
	QAReviewContext *QAReviewContext

	// Persona carries optional persona configuration for this role.
	// When non-nil, a CategoryPersona fragment is injected into the prompt.
	Persona *AgentPersona

	// MaxTokens is the model's context window size from the resolved endpoint.
	// Fragment conditions can use this to skip verbose sections for smaller models.
	// Zero means unknown (treat as large model).
	MaxTokens int

	// Vocabulary provides display labels for prompt rendering.
	// When nil, hardcoded defaults are used.
	Vocabulary *Vocabulary

	// HasResponseFormat is true when the dispatch component has attached a
	// JSON-schema ResponseFormat to the outgoing TaskMessage and the
	// resolved endpoint is known to honor it (small-model providers per
	// ADR-034). When true, output-format fragments elide the literal schema
	// prose (intro line, JSON example, "Required: X (string)..." listing)
	// since the model already receives the schema via response_format /
	// submit_work.parameters. CRITICAL semantic guidance and behavioral
	// directives ("Respond ONLY via submit_work") remain.
	//
	// Frontier providers (Anthropic, Gemini OpenAI-compat) leave this
	// false, keeping the existing prose unchanged.
	HasResponseFormat bool

	// RequirementGenerator carries data for the requirement-generator user
	// prompt. Set by the requirement-generator component before assembly;
	// nil for any other role.
	RequirementGenerator *RequirementGeneratorContext

	// PlannerPrompt carries data for the planner user prompt. Set by the
	// planner component before assembly; nil for any other role.
	PlannerPrompt *PlannerPromptContext

	// AnalystPrompt carries data for the ADR-040 analyst sub-phase user prompt.
	// Set by the planner component when dispatching the analyst sub-phase.
	// When non-nil, planner-role user-prompt and output-format fragments
	// route to the analyst variants instead of the planner variants.
	AnalystPrompt *AnalystPromptContext

	// ScenarioGeneratorPrompt carries data for the scenario-generator user
	// prompt. Set by the scenario-generator component before assembly.
	ScenarioGeneratorPrompt *ScenarioGeneratorPromptContext

	// ArchitectPrompt carries data for the architect user prompt. Set by
	// the architecture-generator component before assembly.
	ArchitectPrompt *ArchitectPromptContext

	// StoryPreparerPrompt carries data for the story-preparer user prompt.
	// Set by the story-preparer component before assembly (ADR-043 Move 3).
	StoryPreparerPrompt *StoryPreparerPromptContext

	// PlanReviewerPrompt carries data for the plan-reviewer user prompt.
	// Set by the plan-reviewer component before assembly.
	PlanReviewerPrompt *PlanReviewerPromptContext

	// QAReviewerPrompt carries data for the QA reviewer user prompt. Set
	// by the qa-reviewer component before assembly.
	QAReviewerPrompt *QAReviewerPromptContext

	// LessonDecomposerPrompt carries data for the lesson-decomposer user
	// prompt. Set by the lesson-decomposer component before assembly
	// (ADR-033 Phase 2b); nil for any other role.
	LessonDecomposerPrompt *LessonDecomposerPromptContext

	// Recovery carries data for the recovery-agent (RoleRecoveryAgent) user
	// prompt. Set by the recovery-agent component before assembly. nil for
	// every other role.
	Recovery *RecoveryPromptContext

	// Researcher carries data for the researcher (RoleResearcher) user
	// prompt. Set by the researcher-manager component before dispatching
	// the researcher sub-agent. nil for every other role.
	Researcher *ResearcherPromptContext
}

// TaskContext carries data for developer task prompts.
type TaskContext struct {
	// Context is pre-built context with SOPs, entities, and documents.
	Context *workflow.ContextPayload

	// PlanTitle is the parent plan title.
	PlanTitle string

	// PlanGoal is the parent plan goal.
	PlanGoal string

	// Feedback is reviewer feedback for retry prompts.
	Feedback string

	// Iteration is the current TDD cycle number (1-based).
	Iteration int

	// MaxIterations is the maximum dev→validate→review cycles for this task.
	MaxIterations int

	// ErrorTrends carries resolved error categories with occurrence counts
	// for role-scoped lesson trend injection.
	ErrorTrends []ErrorTrend

	// IsRetry indicates this dispatch follows a previous failed attempt.
	// When true, the workspace may contain files from the previous attempt.
	IsRetry bool

	// Checklist carries the project-specific quality gate checks from
	// .semspec/checklist.json. When non-empty, prompt fragments inject the
	// actual check names and commands instead of a hardcoded generic list.
	Checklist []workflow.Check

	// TestSurface is the architect's declared test coverage — integration
	// flows to exercise cross-component behavior and e2e flows to exercise
	// actor-driven user-visible outcomes. When non-nil, developer prompts
	// render this as "tests you must write" guidance. Phase 5 threads the
	// struct + prompt; Phase 5.1 wires the value through execution-manager.
	TestSurface *workflow.TestSurface

	// HarnessProfiles are the selected catalog profiles resolved to full
	// developer-facing details (images, ports, readiness, evidence anchors,
	// runner compatibility, and test guidance). Architects select IDs only;
	// developers consume this resolved view when writing integration tests.
	HarnessProfiles []ResolvedHarnessProfileContext

	// WorktreePath is the absolute path to the per-task git worktree the
	// agent's bash will use as cwd. When non-empty, developer prompts
	// render an explicit "Your worktree path: X / DO NOT cd /workspace"
	// banner so the agent's mental model aligns with where its writes
	// actually land. Empty means "fall back to generic 'your working
	// directory is a git worktree' language." Added 2026-05-12 after
	// hybrid @hard take 16 surfaced the cd-/workspace-and-cat-write leak
	// that bypasses the diff gate (see
	// .semspec/investigation-diff-gate-2026-05-12.md).
	WorktreePath string

	// FileScope is the Story-owned file surface for this task
	// (TaskExecution.FileScope, derived from Story.FilesOwned). It includes
	// both existing files and files the task is expected to create.
	FileScope []string

	// Scenarios are the BDD scenarios this task is responsible for
	// satisfying. When non-empty, developer + per-task code-reviewer +
	// validator prompts render each scenario's given/when/then so the
	// dev grounds tests in the contract and the reviewer can verify
	// each scenario_id has a test exercising its specific behavior
	// before approving. Empty for mock fixtures or legacy plans without
	// a per-task scenario binding — prompts fall back to the title-only
	// shape so old fixtures keep working.
	//
	// Closes the disconnect surfaced by paid mavlink-hard 2026-06-03:
	// per-task reviewer was structurally blind to scenarios (only the
	// req-level scenario-reviewer saw them), so Cline approved internally-
	// consistent code that the req-level reviewer then rejected on every
	// scenario. Grounding both dev and per-task reviewer in the contract
	// shifts the contract check earlier (TDD-cycle granular, cheap)
	// instead of req-level (full DAG restart, expensive).
	Scenarios []ScenarioSpec

	// UpstreamResolutions are the architect's resolved external dependencies —
	// concrete build-manifest coordinates plus the API surfaces (symbol,
	// signature, lifecycle) the dev integrates against. When non-empty, the
	// developer + per-task code-reviewer prompts render them so the dev wires
	// the exact pinned coordinate instead of hallucinating one, and the
	// reviewer checks the implementation against the resolved surface. Empty
	// when the architecture declared no external dependencies. Closes the
	// run #6 root cause where Winston resolved coordinates the dev never saw.
	UpstreamResolutions []UpstreamResolutionInfo
}

// ResolvedHarnessProfileContext is a prompt-safe projection of a selected
// harness catalog profile plus the architect's task-specific selection intent.
type ResolvedHarnessProfileContext struct {
	ProfileID          string
	Tier               string
	Orchestration      string
	UsedBy             []string
	Purpose            string
	Covers             []string
	Proves             []string
	RunnerSupport      []string
	Cost               string
	Constraints        []string
	RequiredAssertions []string
	EvidenceAnchors    []string
	Images             []HarnessImageContext
	Ports              []HarnessPortContext
	Env                map[string]string
	Readiness          []string
	TestGuidance       []string
}

// HarnessImageContext is a developer-facing image reference.
type HarnessImageContext struct {
	Name    string
	Purpose string
}

// HarnessPortContext is a developer-facing container endpoint reference.
type HarnessPortContext struct {
	Name          string
	ContainerPort int
	Protocol      string
	Purpose       string
}

// PlanContext carries data for planner prompts.
type PlanContext struct {
	// Title is the plan title.
	Title string

	// Goal is the plan goal (for revision prompts).
	Goal string

	// Context is the plan context (for revision prompts).
	Context string

	// Scope is the plan scope (for finalization prompts).
	Scope []string

	// Slug is the plan slug (for finalization prompts).
	Slug string

	// ReviewSummary is the reviewer's summary (for revision prompts).
	ReviewSummary string

	// ReviewFindings is the reviewer's findings (for revision prompts).
	ReviewFindings string

	// IsRevision indicates this is a plan revision after rejection.
	IsRevision bool

	// IsFromExploration indicates finalization from an existing exploration.
	IsFromExploration bool

	// FocusArea is the planner's focused analysis area (for parallel planners).
	FocusArea string

	// FocusDescription describes the focus area in detail.
	FocusDescription string

	// Hints are coordinator-provided hints for focused planners.
	Hints []string

	// FocusContext is pre-loaded graph context for focused planners.
	FocusContext *FocusContextInfo
}

// ReviewContext carries data for reviewer prompts.
type ReviewContext struct {
	// PlanSlug is the plan slug being reviewed.
	PlanSlug string

	// PlanContent is the plan JSON being reviewed.
	PlanContent string

	// SOPContext is the SOP context for review.
	SOPContext string
}

// ScenarioReviewContext carries data for scenario-level review prompts.
type ScenarioReviewContext struct {
	// ScenarioGiven is the BDD Given clause (single-scenario legacy path).
	ScenarioGiven string

	// ScenarioWhen is the BDD When clause (single-scenario legacy path).
	ScenarioWhen string

	// ScenarioThen is the BDD Then assertions (single-scenario legacy path).
	ScenarioThen []string

	// Scenarios carries all scenarios for requirement-level review.
	// When set, the reviewer produces per-scenario verdicts.
	Scenarios []ScenarioSpec

	// NodeResults summarises each completed DAG node.
	NodeResults []NodeResultSummary

	// FilesModified is the aggregate list of files changed across all nodes.
	FilesModified []string

	// RetryFeedback carries the reviewer's feedback from a prior rejection.
	// When non-empty, this is a retry — the reviewer should note what was fixed.
	RetryFeedback string

	// PlanTitle, PlanGoal, RequirementTitle frame the Story gate so Murat
	// judges the scenarios in context rather than in isolation. Empty when the
	// plan could not be loaded.
	PlanTitle        string
	PlanGoal         string
	RequirementTitle string

	// ArchitectureContext is the pre-rendered architecture surface (components,
	// resolved upstream dependencies, integrations) so the Story gate can check
	// the implementation against the architect's resolved decisions. Empty when
	// the plan has no architecture.
	ArchitectureContext string
}

// ScenarioSpec identifies a scenario for per-scenario verdict tracking.
type ScenarioSpec struct {
	ID    string   `json:"id"`
	Given string   `json:"given"`
	When  string   `json:"when"`
	Then  []string `json:"then"`
}

// QAReviewContext carries data for QA release-readiness review prompts.
// It is populated by qa-reviewer when dispatching the LLM agent.
type QAReviewContext struct {
	// PlanTitle is the human-readable plan title.
	PlanTitle string

	// PlanGoal is the plan's stated goal.
	PlanGoal string

	// Requirements summarises each requirement's execution status.
	Requirements []RequirementSummary

	// Capabilities summarises every capability and which Stories cover it
	// (ADR-044 M:N evidence rollup). QA-reviewer's release-readiness verdict
	// shifts under ADR-044 from "all requirements complete" to "every
	// capability has evidence from at least one shipped Story" — populated
	// here so the persona can audit coverage explicitly.
	Capabilities []QACapabilityEvidence

	// Stories summarises each Story's execution status + its coverage joins
	// (which Requirements + Capabilities it satisfies). Under ADR-044 the
	// Story is the per-component execution unit; QA-reviewer reads this to
	// answer "did the shipped work actually satisfy the stated capability
	// surface?"
	Stories []QAStorySummary

	// TestSurface is the architect's declared test coverage matrix.
	// Contains integration flows and e2e flows the developer was expected to implement.
	// Nil when the plan has no architecture phase (synthesis-level review).
	TestSurface *workflow.TestSurface

	// QALevel is the project QA policy the executor ran at.
	QALevel workflow.QALevel

	// Passed is true when the QA executor reported no test failures.
	// Always false for synthesis-level review (no tests were run).
	Passed bool

	// Failures lists individual test or CI job failures from the QA executor.
	// Empty for synthesis-level review or when Passed is true.
	Failures []workflow.QAFailure

	// Artifacts lists workspace-relative references to logs, screenshots, traces,
	// and coverage reports produced by the QA run.
	Artifacts []workflow.QAArtifactRef

	// FilesModifiedDiff is the aggregate set of files changed across all requirements.
	// Derived from plan.Requirements[*].FilesModified at review time.
	FilesModifiedDiff []string

	// RunnerError describes an infrastructure failure in the QA executor itself
	// (e.g., sandbox timeout, docker failure). Distinct from test failures.
	// Empty when the executor ran cleanly regardless of test outcomes.
	RunnerError string
}

// HasTool returns true if the named tool is in AvailableTools.
func (ctx *AssemblyContext) HasTool(name string) bool {
	return slices.Contains(ctx.AvailableTools, name)
}
