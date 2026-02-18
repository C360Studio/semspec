package spec

import "github.com/c360studio/semstreams/vocabulary"

// Spec type predicates for OpenSpec documents.
const (
	// SpecType identifies the entity type within OpenSpec.
	// Values: "specification", "requirement", "scenario", "delta-operation"
	SpecType = "spec.meta.type"

	// SpecSpecType distinguishes source-of-truth specs from delta specs.
	// Values: "source-of-truth", "delta"
	SpecSpecType = "spec.meta.spec_type"

	// SpecFilePath is the original file path of the spec document.
	SpecFilePath = "spec.meta.file_path"

	// SpecFileHash is the content hash for staleness detection.
	SpecFileHash = "spec.meta.file_hash"
)

// Requirement predicates for normative requirement blocks.
const (
	// RequirementName is the requirement identifier (from ### Requirement: header).
	RequirementName = "spec.requirement.name"

	// RequirementNormative contains SHALL/MUST normative statements.
	// Array of strings extracted from the requirement body.
	RequirementNormative = "spec.requirement.normative"

	// RequirementDescription is the full requirement text.
	RequirementDescription = "spec.requirement.description"

	// RequirementStatus tracks the requirement lifecycle.
	// Values: "active", "deprecated", "superseded"
	RequirementStatus = "spec.requirement.status"
)

// Scenario predicates for BDD-style scenarios.
const (
	// ScenarioName is the scenario identifier (from #### Scenario: header).
	ScenarioName = "spec.scenario.name"

	// ScenarioGiven is the precondition (GIVEN clause).
	ScenarioGiven = "spec.scenario.given"

	// ScenarioWhen is the action (WHEN clause).
	ScenarioWhen = "spec.scenario.when"

	// ScenarioThen is the expected result (THEN clause).
	ScenarioThen = "spec.scenario.then"
)

// Delta predicates for tracking changes between specs.
const (
	// DeltaOperation is the change type.
	// Values: "added", "modified", "removed"
	DeltaOperation = "spec.delta.operation"

	// DeltaTarget is the requirement name being modified.
	DeltaTarget = "spec.delta.target"

	// DeltaReason explains why the change was made.
	DeltaReason = "spec.delta.reason"
)

// Relationship predicates linking spec entities.
const (
	// Modifies links a delta spec to the source-of-truth it modifies.
	// Domain: delta spec entity, Range: source-of-truth spec entity
	Modifies = "spec.rel.modifies"

	// HasRequirement links a spec to its requirements.
	// Domain: spec entity, Range: requirement entity
	HasRequirement = "spec.rel.has_requirement"

	// HasScenario links a requirement to its scenarios.
	// Domain: requirement entity, Range: scenario entity
	HasScenario = "spec.rel.has_scenario"

	// AppliesTo specifies file patterns this spec applies to.
	// Format: glob patterns like "*.go", "auth/*", "**/*.ts"
	AppliesTo = "spec.rel.applies_to"

	// Targets links a delta operation to the requirement it modifies.
	// Domain: delta operation entity, Range: requirement entity
	Targets = "spec.rel.targets"
)

// Metadata predicates for spec document metadata.
const (
	// SpecTitle is the spec document title.
	SpecTitle = "spec.meta.title"

	// SpecVersion is the spec version identifier.
	SpecVersion = "spec.meta.version"

	// SpecAuthor is who authored this spec.
	SpecAuthor = "spec.meta.author"

	// SpecCreatedAt is when the spec was created (RFC3339).
	SpecCreatedAt = "spec.meta.created_at"

	// SpecUpdatedAt is when the spec was last updated (RFC3339).
	SpecUpdatedAt = "spec.meta.updated_at"
)

func init() {
	// Register spec type predicates
	vocabulary.Register(SpecType,
		vocabulary.WithDescription("Entity type within OpenSpec: specification, requirement, scenario, delta-operation"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"type"))

	vocabulary.Register(SpecSpecType,
		vocabulary.WithDescription("Spec classification: source-of-truth or delta"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"specType"))

	vocabulary.Register(SpecFilePath,
		vocabulary.WithDescription("Original file path of the spec document"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"filePath"))

	vocabulary.Register(SpecFileHash,
		vocabulary.WithDescription("Content hash for staleness detection"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"fileHash"))

	// Register requirement predicates
	vocabulary.Register(RequirementName,
		vocabulary.WithDescription("Requirement identifier from header"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"requirementName"))

	vocabulary.Register(RequirementNormative,
		vocabulary.WithDescription("Array of SHALL/MUST normative statements"),
		vocabulary.WithDataType("array"),
		vocabulary.WithIRI(Namespace+"normative"))

	vocabulary.Register(RequirementDescription,
		vocabulary.WithDescription("Full requirement text"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"requirementDescription"))

	vocabulary.Register(RequirementStatus,
		vocabulary.WithDescription("Requirement lifecycle status: active, deprecated, superseded"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"requirementStatus"))

	// Register scenario predicates
	vocabulary.Register(ScenarioName,
		vocabulary.WithDescription("Scenario identifier from header"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"scenarioName"))

	vocabulary.Register(ScenarioGiven,
		vocabulary.WithDescription("BDD precondition (GIVEN clause)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"given"))

	vocabulary.Register(ScenarioWhen,
		vocabulary.WithDescription("BDD action (WHEN clause)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"when"))

	vocabulary.Register(ScenarioThen,
		vocabulary.WithDescription("BDD expected result (THEN clause)"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"then"))

	// Register delta predicates
	vocabulary.Register(DeltaOperation,
		vocabulary.WithDescription("Change type: added, modified, removed"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"operation"))

	vocabulary.Register(DeltaTarget,
		vocabulary.WithDescription("Requirement name being modified"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"deltaTarget"))

	vocabulary.Register(DeltaReason,
		vocabulary.WithDescription("Explanation for the change"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"deltaReason"))

	// Register relationship predicates
	vocabulary.Register(Modifies,
		vocabulary.WithDescription("Links delta spec to source-of-truth spec it modifies"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(PropModifies))

	vocabulary.Register(HasRequirement,
		vocabulary.WithDescription("Links spec to its requirements"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(PropHasRequirement))

	vocabulary.Register(HasScenario,
		vocabulary.WithDescription("Links requirement to its scenarios"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(PropHasScenario))

	vocabulary.Register(AppliesTo,
		vocabulary.WithDescription("File patterns this spec applies to (glob patterns)"),
		vocabulary.WithDataType("array"),
		vocabulary.WithIRI(Namespace+"appliesTo"))

	vocabulary.Register(Targets,
		vocabulary.WithDescription("Links delta operation to target requirement"),
		vocabulary.WithDataType("entity_id"),
		vocabulary.WithIRI(PropTargets))

	// Register metadata predicates
	vocabulary.Register(SpecTitle,
		vocabulary.WithDescription("Spec document title"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(vocabulary.DcTitle))

	vocabulary.Register(SpecVersion,
		vocabulary.WithDescription("Spec version identifier"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(Namespace+"version"))

	vocabulary.Register(SpecAuthor,
		vocabulary.WithDescription("Spec author"),
		vocabulary.WithDataType("string"),
		vocabulary.WithIRI(DcCreator))

	vocabulary.Register(SpecCreatedAt,
		vocabulary.WithDescription("Spec creation timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(DcCreated))

	vocabulary.Register(SpecUpdatedAt,
		vocabulary.WithDescription("Spec last update timestamp (RFC3339)"),
		vocabulary.WithDataType("datetime"),
		vocabulary.WithIRI(DcModified))
}
