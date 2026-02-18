package spec

// Namespace is the base IRI prefix for OpenSpec vocabulary terms.
const Namespace = "https://semspec.dev/ontology/spec/"

// EntityNamespace is the base IRI for spec entity instances.
const EntityNamespace = "https://semspec.dev/entity/spec/"

// Standard ontology IRI constants for mappings.
const (
	// DcCreator is the Dublin Core creator property.
	DcCreator = "http://purl.org/dc/terms/creator"

	// DcCreated is the Dublin Core created property.
	DcCreated = "http://purl.org/dc/terms/created"

	// DcModified is the Dublin Core modified property.
	DcModified = "http://purl.org/dc/terms/modified"
)

// Class IRIs define the types of spec entities.
const (
	// ClassSpec represents an OpenSpec specification document.
	// Extends: bfo:GenericallyDependentContinuant, cco:InformationContentEntity
	ClassSpec = Namespace + "Specification"

	// ClassSourceOfTruth represents a source-of-truth spec (canonical state).
	// Extends: ClassSpec
	ClassSourceOfTruth = Namespace + "SourceOfTruthSpec"

	// ClassDelta represents a delta spec (proposed changes).
	// Extends: ClassSpec
	ClassDelta = Namespace + "DeltaSpec"

	// ClassRequirement represents a normative requirement block.
	// Extends: cco:DirectiveInformationContentEntity
	ClassRequirement = Namespace + "Requirement"

	// ClassScenario represents a BDD scenario (Given-When-Then).
	// Extends: cco:DescriptiveInformationContentEntity
	ClassScenario = Namespace + "Scenario"

	// ClassDeltaOperation represents a single delta operation.
	// Extends: prov:Activity
	ClassDeltaOperation = Namespace + "DeltaOperation"
)

// Object Property IRIs define relationships between spec entities.
const (
	// PropHasRequirement links a spec to its requirements.
	// Domain: ClassSpec, Range: ClassRequirement
	PropHasRequirement = Namespace + "hasRequirement"

	// PropHasScenario links a requirement to its scenarios.
	// Domain: ClassRequirement, Range: ClassScenario
	PropHasScenario = Namespace + "hasScenario"

	// PropModifies links a delta spec to the source-of-truth it modifies.
	// Domain: ClassDelta, Range: ClassSourceOfTruth
	PropModifies = Namespace + "modifies"

	// PropTargets links a delta operation to the requirement it targets.
	// Domain: ClassDeltaOperation, Range: ClassRequirement
	PropTargets = Namespace + "targets"
)

// Data Property IRIs define literal-valued attributes.
const (
	// PropNormative is a normative statement (SHALL/MUST).
	PropNormative = Namespace + "normative"

	// PropGiven is the scenario precondition.
	PropGiven = Namespace + "given"

	// PropWhen is the scenario action.
	PropWhen = Namespace + "when"

	// PropThen is the scenario expected result.
	PropThen = Namespace + "then"

	// PropOperation is the delta operation type (added/modified/removed).
	PropOperation = Namespace + "operation"
)
