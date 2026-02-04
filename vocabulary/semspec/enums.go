package semspec

// ProposalStatus represents the lifecycle status of a proposal.
type ProposalStatus string

const (
	// StatusExploring indicates the proposal is being explored/researched.
	StatusExploring ProposalStatus = "exploring"

	// StatusDrafted indicates the proposal has been drafted.
	StatusDrafted ProposalStatus = "drafted"

	// StatusApproved indicates the proposal has been approved.
	StatusApproved ProposalStatus = "approved"

	// StatusImplementing indicates the proposal is being implemented.
	StatusImplementing ProposalStatus = "implementing"

	// StatusComplete indicates the proposal has been fully implemented.
	StatusComplete ProposalStatus = "complete"

	// StatusRejected indicates the proposal was rejected.
	StatusRejected ProposalStatus = "rejected"

	// StatusAbandoned indicates the proposal was abandoned.
	StatusAbandoned ProposalStatus = "abandoned"
)

// SpecStatus represents the lifecycle status of a specification.
type SpecStatus string

const (
	// SpecStatusDraft indicates the spec is in draft form.
	SpecStatusDraft SpecStatus = "draft"

	// SpecStatusInReview indicates the spec is under review.
	SpecStatusInReview SpecStatus = "in_review"

	// SpecStatusApproved indicates the spec has been approved.
	SpecStatusApproved SpecStatus = "approved"

	// SpecStatusImplemented indicates the spec has been implemented.
	SpecStatusImplemented SpecStatus = "implemented"

	// SpecStatusSuperseded indicates the spec has been superseded by a newer version.
	SpecStatusSuperseded SpecStatus = "superseded"
)

// TaskStatus represents the execution status of a task.
type TaskStatus string

const (
	// TaskStatusPending indicates the task is waiting to be started.
	TaskStatusPending TaskStatus = "pending"

	// TaskStatusInProgress indicates the task is currently being worked on.
	TaskStatusInProgress TaskStatus = "in_progress"

	// TaskStatusComplete indicates the task has been completed.
	TaskStatusComplete TaskStatus = "complete"

	// TaskStatusFailed indicates the task failed.
	TaskStatusFailed TaskStatus = "failed"

	// TaskStatusBlocked indicates the task is blocked by dependencies.
	TaskStatusBlocked TaskStatus = "blocked"

	// TaskStatusCancelled indicates the task was cancelled.
	TaskStatusCancelled TaskStatus = "cancelled"
)

// TaskType represents the type of work a task involves.
type TaskType string

const (
	// TaskTypeImplement indicates implementation work.
	TaskTypeImplement TaskType = "implement"

	// TaskTypeTest indicates test writing or execution.
	TaskTypeTest TaskType = "test"

	// TaskTypeDocument indicates documentation work.
	TaskTypeDocument TaskType = "document"

	// TaskTypeReview indicates review work.
	TaskTypeReview TaskType = "review"

	// TaskTypeRefactor indicates refactoring work.
	TaskTypeRefactor TaskType = "refactor"
)

// LoopStatus represents the execution status of an agent loop.
type LoopStatus string

const (
	// LoopStatusExecuting indicates the loop is currently executing.
	LoopStatusExecuting LoopStatus = "executing"

	// LoopStatusPaused indicates the loop is paused.
	LoopStatusPaused LoopStatus = "paused"

	// LoopStatusAwaitingApproval indicates the loop is waiting for user approval.
	LoopStatusAwaitingApproval LoopStatus = "awaiting_approval"

	// LoopStatusComplete indicates the loop has completed successfully.
	LoopStatusComplete LoopStatus = "complete"

	// LoopStatusFailed indicates the loop failed.
	LoopStatusFailed LoopStatus = "failed"

	// LoopStatusCancelled indicates the loop was cancelled.
	LoopStatusCancelled LoopStatus = "cancelled"
)

// LoopRole represents the role of an agent in a loop.
type LoopRole string

const (
	// RolePlanner indicates the agent is planning work.
	RolePlanner LoopRole = "planner"

	// RoleImplementer indicates the agent is implementing code.
	RoleImplementer LoopRole = "implementer"

	// RoleReviewer indicates the agent is reviewing work.
	RoleReviewer LoopRole = "reviewer"

	// RoleGeneral indicates the agent has no specific role.
	RoleGeneral LoopRole = "general"
)

// ActivityType represents the type of activity performed.
type ActivityType string

const (
	// ActivityTypeModelCall indicates a call to an AI model.
	ActivityTypeModelCall ActivityType = "model_call"

	// ActivityTypeToolCall indicates a call to a tool.
	ActivityTypeToolCall ActivityType = "tool_call"
)

// ResultOutcome represents the outcome of an execution.
type ResultOutcome string

const (
	// OutcomeSuccess indicates successful completion.
	OutcomeSuccess ResultOutcome = "success"

	// OutcomeFailure indicates failure.
	OutcomeFailure ResultOutcome = "failure"

	// OutcomePartial indicates partial success.
	OutcomePartial ResultOutcome = "partial"
)

// Priority represents the priority level of an item.
type Priority string

const (
	// PriorityCritical indicates critical priority.
	PriorityCritical Priority = "critical"

	// PriorityHigh indicates high priority.
	PriorityHigh Priority = "high"

	// PriorityMedium indicates medium priority.
	PriorityMedium Priority = "medium"

	// PriorityLow indicates low priority.
	PriorityLow Priority = "low"
)

// ConstitutionPriority represents the enforcement priority of a rule.
type ConstitutionPriority string

const (
	// EnforcementMust indicates the rule must be followed.
	EnforcementMust ConstitutionPriority = "must"

	// EnforcementShould indicates the rule should be followed.
	EnforcementShould ConstitutionPriority = "should"

	// EnforcementMay indicates the rule may be followed.
	EnforcementMay ConstitutionPriority = "may"
)

// ConstitutionSection represents sections of a constitution.
type ConstitutionSection string

const (
	// SectionCodeQuality covers code quality rules.
	SectionCodeQuality ConstitutionSection = "code_quality"

	// SectionTesting covers testing requirements.
	SectionTesting ConstitutionSection = "testing"

	// SectionSecurity covers security requirements.
	SectionSecurity ConstitutionSection = "security"

	// SectionArchitecture covers architecture guidelines.
	SectionArchitecture ConstitutionSection = "architecture"
)

// CodeType represents the type of a code artifact.
type CodeType string

const (
	// CodeTypeFile indicates a file artifact.
	CodeTypeFile CodeType = "file"

	// CodeTypePackage indicates a package artifact.
	CodeTypePackage CodeType = "package"

	// CodeTypeFunction indicates a function artifact.
	CodeTypeFunction CodeType = "function"

	// CodeTypeMethod indicates a method artifact.
	CodeTypeMethod CodeType = "method"

	// CodeTypeStruct indicates a struct artifact.
	CodeTypeStruct CodeType = "struct"

	// CodeTypeInterface indicates an interface artifact.
	CodeTypeInterface CodeType = "interface"

	// CodeTypeConst indicates a constant artifact.
	CodeTypeConst CodeType = "const"

	// CodeTypeVar indicates a variable artifact.
	CodeTypeVar CodeType = "var"

	// CodeTypeType indicates a type definition artifact.
	CodeTypeType CodeType = "type"
)

// Visibility represents the visibility level of a code artifact.
type Visibility string

const (
	// VisibilityPublic indicates publicly accessible.
	VisibilityPublic Visibility = "public"

	// VisibilityPrivate indicates privately accessible.
	VisibilityPrivate Visibility = "private"

	// VisibilityInternal indicates package-internal accessibility.
	VisibilityInternal Visibility = "internal"
)
