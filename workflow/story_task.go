package workflow

import "time"

// Story is a Sarah-authored unit of dev-ready work anchored to a single
// architectural component (ADR-044). Under M:N coverage, a Story covers
// multiple Requirements and multiple Capabilities via join slices.
//
// Stories partition by execution-unit (ComponentName anchor): one Story per
// cohesive component, carrying ALL requirements and capabilities that map
// into that component. FilesOwned equals the component's
// ImplementationFiles directly — Sarah does not choose files; the component
// selection determines them.
//
// DependsOn is system-derived post-emission by DeriveStoryScheduling (NOT
// Sarah-authored). The derivation runs two passes: (1) semantic edges from
// transitive Requirement prereq closure with all-coverers wait semantics,
// (2) resource edges from file-ownership conflict resolution. Sarah's
// readiness gate ensures structural correctness before derivation runs.
//
// Execution-manager dispatches per-Story (ADR-043 Move 5; PR 4).
// Plan-reviewer R3 round validates structural invariants as a defensive
// backstop.
type Story struct {
	// ID is the stable story identifier.
	// Format: story.<plan-slug>.<componentName> where componentName is
	// kebab-case-clean (matching ComponentDef.Name). Generation rewire
	// tracked in ADR-044 commit 6.
	ID string `json:"id"`

	// ComponentName is the 1:1 execution anchor — the single architectural
	// component this Story implements. Sarah selects ONE component per Story;
	// FilesOwned is derived directly from ComponentDef.ImplementationFiles.
	// Plan-reviewer rule story.unresolved_components rejects values that
	// don't match any declared component.
	ComponentName string `json:"component_name,omitempty"`

	// RequirementIDs is the M:N coverage join — every Requirement whose
	// acceptance scope is covered by this Story. Under the ADR-044 model,
	// Sarah ensures every plan Requirement appears in ≥1 Story's
	// RequirementIDs. Plan-reviewer rule story.requirement_orphan checks
	// that every entry resolves to a known Requirement.ID.
	RequirementIDs []string `json:"requirement_ids,omitempty"`

	// CapabilityNames is the M:N coverage join — every Capability whose
	// acceptance spec is provided by this Story's shipped code. Sarah
	// ensures every plan Capability appears in ≥1 Story's CapabilityNames.
	CapabilityNames []string `json:"capability_names,omitempty"`

	// Title is the human-readable story heading.
	Title string `json:"title"`

	// Intent is a 1-2 sentence description of what implementing this story
	// proves.
	Intent string `json:"intent,omitempty"`

	// FilesOwned is the component's ImplementationFiles (NOT a union —
	// Sarah's component selection determines them exactly).
	// Plan-reviewer rule story.missing_files_owned rejects empty lists;
	// story.docs_only_files_owned rejects lists with no source-code file.
	FilesOwned []string `json:"files_owned,omitempty"`

	// DependsOn are other Story.ID entries that must reach StoryStatusComplete
	// before this Story can dispatch. This slice is system-derived by
	// DeriveStoryScheduling — Sarah does NOT author it under ADR-044.
	// Plan-reviewer rules story.depends_on_orphan and story.depends_on_cycle
	// reject unresolved IDs and DAG cycles.
	DependsOn []string `json:"depends_on,omitempty"`

	// Tasks are Sarah's ordered TDD checklist for this Story. A typical
	// shape is 3-5 tasks: write failing tests, implement to pass,
	// integration smoke, verify scenarios. The execution-manager runs
	// Tasks in topo order (intra-story DependsOn); the dev decomposes
	// further as needed inside the TDD pipeline. Plan-reviewer rule
	// task.missing_within_story rejects empty Tasks; task.depends_on_cycle
	// rejects intra-story DAG cycles.
	Tasks []Task `json:"tasks,omitempty"`

	// Status — see Requirement.Status omitempty rationale (b7r50o9ov 2026-05-08).
	// Plan-time stories carry empty status (so the plan-reviewer doesn't see
	// asymmetry across freshly-generated stories); execution-manager sets a
	// non-empty status on dispatch.
	Status StoryStatus `json:"status,omitempty"`

	// PreparedBy is the persona that signed off readiness — "sarah" when
	// the readiness gate passed.
	PreparedBy string `json:"prepared_by,omitempty"`

	// PreparedAt is the timestamp at which Sarah's readiness gate passed.
	PreparedAt *time.Time `json:"prepared_at,omitempty"`

	// RecoveryHint mirrors Requirement.RecoveryHint — populated when ADR-037
	// stage-1 recovery emits a PlanDecision with action=story_reprepare and
	// plan-decision-handler accepts it. story-preparer (Sarah) prepends this
	// hint onto Sarah's prompt on the next preparation cycle so she sees the
	// recovery-agent's recommendation alongside the original requirement
	// intent. Cleared on story completion.
	RecoveryHint string `json:"recovery_hint,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PrimaryRequirementID returns the first entry in RequirementIDs, or empty
// string if the slice is empty. This is a singleton-slice compatibility
// accessor for code that still assumes a single primary requirement per Story
// (ADR-044 commit 2 transition phase). Callers that need the full M:N set
// must iterate RequirementIDs directly.
//
// TODO ADR-044 commit 3+: all callers should be updated to handle the full
// RequirementIDs slice where the singleton assumption breaks down.
func (s *Story) PrimaryRequirementID() string {
	if len(s.RequirementIDs) > 0 {
		return s.RequirementIDs[0]
	}
	return ""
}

// StoryStatus represents the lifecycle state of a Story.
type StoryStatus string

const (
	// StoryStatusPending indicates Sarah hasn't completed preparation —
	// either she's mid-flight or her readiness gate failed.
	StoryStatusPending StoryStatus = "pending"

	// StoryStatusReady indicates Sarah's readiness gate passed; the story
	// is ready for the execution-manager to dispatch when prereq Stories
	// (DependsOn) complete.
	StoryStatusReady StoryStatus = "ready"

	// StoryStatusExecuting indicates execution-manager has dispatched the
	// dev TDD pipeline for this Story.
	StoryStatusExecuting StoryStatus = "executing"

	// StoryStatusComplete indicates dev + per-Story reviewer approved.
	StoryStatusComplete StoryStatus = "complete"

	// StoryStatusFailed indicates execution exhausted retries and a
	// PlanDecision (kind=execution_exhausted or recovery_failure) is
	// required to advance the plan.
	StoryStatusFailed StoryStatus = "failed"
)

// String returns the string representation of the story status.
func (s StoryStatus) String() string {
	return string(s)
}

// IsValid returns true if the story status is one of the defined values.
func (s StoryStatus) IsValid() bool {
	switch s {
	case StoryStatusPending, StoryStatusReady,
		StoryStatusExecuting, StoryStatusComplete, StoryStatusFailed:
		return true
	default:
		return false
	}
}

// CanTransitionTo returns true if this story status can transition to the
// target. The status DAG:
//
//	pending → ready | failed
//	ready → executing | failed
//	executing → complete | failed
//	complete → ready (re-execute on PlanDecision cascade)
//	failed → pending (re-prepare on story_reprepare recovery action)
func (s StoryStatus) CanTransitionTo(target StoryStatus) bool {
	switch s {
	case StoryStatusPending:
		return target == StoryStatusReady || target == StoryStatusFailed
	case StoryStatusReady:
		return target == StoryStatusExecuting || target == StoryStatusFailed
	case StoryStatusExecuting:
		return target == StoryStatusComplete || target == StoryStatusFailed
	case StoryStatusComplete:
		return target == StoryStatusReady
	case StoryStatusFailed:
		return target == StoryStatusPending
	default:
		return false
	}
}

// Task is a Sarah-authored intra-story checklist item (ADR-043 Move 2).
// Tasks replace the runtime decomposer-LLM call: Sarah authors the DAG at
// plan time, the execution-manager dispatches in topo order, and the
// dev TDD pipeline runs structural-validator + code-reviewer per task.
//
// Task IDs are scoped under their parent Story; intra-story DependsOn
// expresses TDD ordering (e.g. "tests-first task" precedes "implementation
// task"). Cross-story ordering lives on Story.DependsOn, not Task.DependsOn.
type Task struct {
	// ID is the stable task identifier.
	// Format: task.<plan-slug>.<reqseq>.<storyseq>.<taskseq>
	ID string `json:"id"`

	// StoryID is the parent Story. Plan-reviewer rule task.missing_within_story
	// rejects orphan tasks; the execution-manager only dispatches Tasks whose
	// Story is StoryStatusExecuting.
	StoryID string `json:"story_id"`

	// Description is the 1-line intent for what this task accomplishes
	// (e.g. "Write failing test for boot lifecycle"). The dev decomposes
	// further inside the TDD pipeline.
	Description string `json:"description"`

	// DependsOn are intra-story Task.IDs that must reach TaskStatusComplete
	// before this Task can dispatch. Plan-reviewer rule task.depends_on_cycle
	// rejects DAG cycles within a Story.
	DependsOn []string `json:"depends_on,omitempty"`

	// Status — see Story.Status / Requirement.Status omitempty rationale.
	Status TaskStatus `json:"status,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TaskStatus represents the lifecycle state of a Task within a Story.
type TaskStatus string

const (
	// TaskStatusPending indicates the task has not yet been dispatched.
	TaskStatusPending TaskStatus = "pending"

	// TaskStatusDispatched indicates the execution-manager has dispatched
	// this task to a dev TDD pipeline.
	TaskStatusDispatched TaskStatus = "dispatched"

	// TaskStatusComplete indicates the dev + structural-validator + code
	// reviewer all approved.
	TaskStatusComplete TaskStatus = "complete"

	// TaskStatusFailed indicates the task exhausted retries; the parent
	// Story transitions to failed and the recovery cascade fires.
	TaskStatusFailed TaskStatus = "failed"
)

// String returns the string representation of the task status.
func (s TaskStatus) String() string {
	return string(s)
}

// IsValid returns true if the task status is one of the defined values.
func (s TaskStatus) IsValid() bool {
	switch s {
	case TaskStatusPending, TaskStatusDispatched,
		TaskStatusComplete, TaskStatusFailed:
		return true
	default:
		return false
	}
}

// CanTransitionTo returns true if this task status can transition to the
// target. The status DAG:
//
//	pending → dispatched | failed
//	dispatched → complete | failed | pending (retry)
//	complete → pending (re-dispatch on cascade)
//	failed → pending (re-dispatch on cascade)
func (s TaskStatus) CanTransitionTo(target TaskStatus) bool {
	switch s {
	case TaskStatusPending:
		return target == TaskStatusDispatched || target == TaskStatusFailed
	case TaskStatusDispatched:
		return target == TaskStatusComplete ||
			target == TaskStatusFailed ||
			target == TaskStatusPending
	case TaskStatusComplete:
		return target == TaskStatusPending
	case TaskStatusFailed:
		return target == TaskStatusPending
	default:
		return false
	}
}
