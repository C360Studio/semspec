package spec

import "github.com/c360studio/semstreams/vocabulary"

// Namespace for specification predicates.
const Namespace = "https://semspec.dev/vocabulary/spec#"

// Core specification predicates.
const (
	// PredicateTitle is the specification title.
	PredicateTitle = "semspec.spec.title"

	// PredicateContent is the specification content (markdown).
	PredicateContent = "semspec.spec.content"

	// PredicateStatus is the specification status.
	// Values: draft, reviewed, approved, implemented
	PredicateStatus = "semspec.spec.status"

	// PredicateVersion is the specification version.
	PredicateVersion = "semspec.spec.version"

	// PredicateCreatedAt is the RFC3339 timestamp when the spec was created.
	PredicateCreatedAt = "semspec.spec.created_at"

	// PredicateUpdatedAt is the RFC3339 timestamp when the spec was last updated.
	PredicateUpdatedAt = "semspec.spec.updated_at"
)

// Relationship predicates.
const (
	// PredicateImplements links a spec to the proposal it implements.
	PredicateImplements = "semspec.spec.implements"

	// PredicateAffects links a spec to code entities it affects.
	PredicateAffects = "semspec.spec.affects"

	// PredicateDependsOn links a spec to other specs it depends on.
	PredicateDependsOn = "semspec.spec.depends_on"
)

// Requirement predicates.
const (
	// PredicateRequirement links a spec to a requirement.
	PredicateRequirement = "semspec.spec.requirement"

	// PredicateGiven is the precondition (GIVEN) text.
	PredicateGiven = "semspec.spec.given"

	// PredicateWhen is the action (WHEN) text.
	PredicateWhen = "semspec.spec.when"

	// PredicateThen is the expected outcome (THEN) text.
	PredicateThen = "semspec.spec.then"
)

// Task predicates for tasks derived from specs.
const (
	// PredicateTaskTitle is the task title.
	PredicateTaskTitle = "semspec.task.title"

	// PredicateTaskStatus is the task status.
	// Values: pending, in_progress, done, blocked
	PredicateTaskStatus = "semspec.task.status"

	// PredicateTaskSpec links a task to its parent spec.
	PredicateTaskSpec = "semspec.task.spec"

	// PredicateTaskAssignee is the assigned agent or user.
	PredicateTaskAssignee = "semspec.task.assignee"

	// PredicateTaskOrder is the task order/priority.
	PredicateTaskOrder = "semspec.task.order"
)

func init() {
	// Register core predicates
	vocabulary.Register(PredicateTitle,
		vocabulary.WithDescription("Specification title"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"title"))

	vocabulary.Register(PredicateContent,
		vocabulary.WithDescription("Specification content (markdown)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"content"))

	vocabulary.Register(PredicateStatus,
		vocabulary.WithDescription("Specification status"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"status"))

	vocabulary.Register(PredicateVersion,
		vocabulary.WithDescription("Specification version"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"version"))

	vocabulary.Register(PredicateCreatedAt,
		vocabulary.WithDescription("Creation timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"))

	vocabulary.Register(PredicateUpdatedAt,
		vocabulary.WithDescription("Last update timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"))

	// Register relationship predicates
	vocabulary.Register(PredicateImplements,
		vocabulary.WithDescription("Proposal this spec implements"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"implements"))

	vocabulary.Register(PredicateAffects,
		vocabulary.WithDescription("Code entities this spec affects"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"affects"))

	vocabulary.Register(PredicateDependsOn,
		vocabulary.WithDescription("Specs this spec depends on"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(vocabulary.ProvWasDerivedFrom))

	// Register requirement predicates
	vocabulary.Register(PredicateRequirement,
		vocabulary.WithDescription("Requirement linked to this spec"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"requirement"))

	vocabulary.Register(PredicateGiven,
		vocabulary.WithDescription("Precondition (GIVEN)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"given"))

	vocabulary.Register(PredicateWhen,
		vocabulary.WithDescription("Action (WHEN)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"when"))

	vocabulary.Register(PredicateThen,
		vocabulary.WithDescription("Expected outcome (THEN)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"then"))

	// Register task predicates
	vocabulary.Register(PredicateTaskTitle,
		vocabulary.WithDescription("Task title"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"taskTitle"))

	vocabulary.Register(PredicateTaskStatus,
		vocabulary.WithDescription("Task status"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"taskStatus"))

	vocabulary.Register(PredicateTaskSpec,
		vocabulary.WithDescription("Parent spec for this task"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(Namespace+"taskSpec"))

	vocabulary.Register(PredicateTaskAssignee,
		vocabulary.WithDescription("Assigned agent or user"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(vocabulary.ProvWasAttributedTo))

	vocabulary.Register(PredicateTaskOrder,
		vocabulary.WithDescription("Task order/priority"),
		vocabulary.WithDataType("int"),
		vocabulary.WithIRI(Namespace+"taskOrder"))
}
