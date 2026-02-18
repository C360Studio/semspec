package spec

import (
	"testing"

	"github.com/c360studio/semstreams/vocabulary"
)

func TestPredicatesRegistered(t *testing.T) {
	// Spec type predicates
	specPredicates := []string{
		SpecType,
		SpecSpecType,
		SpecFilePath,
		SpecFileHash,
	}

	for _, pred := range specPredicates {
		t.Run(pred, func(t *testing.T) {
			meta := vocabulary.GetPredicateMetadata(pred)
			if meta.Description == "" {
				t.Errorf("predicate %s not registered or missing description", pred)
			}
		})
	}

	// Requirement predicates
	reqPredicates := []string{
		RequirementName,
		RequirementNormative,
		RequirementDescription,
		RequirementStatus,
	}

	for _, pred := range reqPredicates {
		t.Run(pred, func(t *testing.T) {
			meta := vocabulary.GetPredicateMetadata(pred)
			if meta.Description == "" {
				t.Errorf("predicate %s not registered or missing description", pred)
			}
		})
	}

	// Scenario predicates
	scenarioPredicates := []string{
		ScenarioName,
		ScenarioGiven,
		ScenarioWhen,
		ScenarioThen,
	}

	for _, pred := range scenarioPredicates {
		t.Run(pred, func(t *testing.T) {
			meta := vocabulary.GetPredicateMetadata(pred)
			if meta.Description == "" {
				t.Errorf("predicate %s not registered or missing description", pred)
			}
		})
	}

	// Delta predicates
	deltaPredicates := []string{
		DeltaOperation,
		DeltaTarget,
		DeltaReason,
	}

	for _, pred := range deltaPredicates {
		t.Run(pred, func(t *testing.T) {
			meta := vocabulary.GetPredicateMetadata(pred)
			if meta.Description == "" {
				t.Errorf("predicate %s not registered or missing description", pred)
			}
		})
	}

	// Relationship predicates
	relPredicates := []string{
		Modifies,
		HasRequirement,
		HasScenario,
		AppliesTo,
		Targets,
	}

	for _, pred := range relPredicates {
		t.Run(pred, func(t *testing.T) {
			meta := vocabulary.GetPredicateMetadata(pred)
			if meta.Description == "" {
				t.Errorf("predicate %s not registered or missing description", pred)
			}
		})
	}

	// Metadata predicates
	metaPredicates := []string{
		SpecTitle,
		SpecVersion,
		SpecAuthor,
		SpecCreatedAt,
		SpecUpdatedAt,
	}

	for _, pred := range metaPredicates {
		t.Run(pred, func(t *testing.T) {
			meta := vocabulary.GetPredicateMetadata(pred)
			if meta.Description == "" {
				t.Errorf("predicate %s not registered or missing description", pred)
			}
		})
	}
}

func TestPredicateIRIMappings(t *testing.T) {
	tests := []struct {
		predicate   string
		expectedIRI string
	}{
		{SpecTitle, vocabulary.DcTitle},
		{SpecAuthor, DcCreator},
		{SpecCreatedAt, DcCreated},
		{SpecUpdatedAt, DcModified},
		{Modifies, PropModifies},
		{HasRequirement, PropHasRequirement},
		{HasScenario, PropHasScenario},
		{Targets, PropTargets},
	}

	for _, tt := range tests {
		t.Run(tt.predicate, func(t *testing.T) {
			meta := vocabulary.GetPredicateMetadata(tt.predicate)
			if meta == nil {
				t.Fatalf("predicate %s not registered", tt.predicate)
			}
			if meta.StandardIRI != tt.expectedIRI {
				t.Errorf("predicate %s: expected IRI %s, got %s", tt.predicate, tt.expectedIRI, meta.StandardIRI)
			}
		})
	}
}

func TestPredicateDataTypes(t *testing.T) {
	tests := []struct {
		predicate    string
		expectedType string
	}{
		{SpecType, "string"},
		{SpecSpecType, "string"},
		{SpecFilePath, "string"},
		{SpecFileHash, "string"},
		{RequirementName, "string"},
		{RequirementNormative, "array"},
		{RequirementDescription, "string"},
		{RequirementStatus, "string"},
		{ScenarioName, "string"},
		{ScenarioGiven, "string"},
		{ScenarioWhen, "string"},
		{ScenarioThen, "string"},
		{DeltaOperation, "string"},
		{DeltaTarget, "string"},
		{DeltaReason, "string"},
		{Modifies, "entity_id"},
		{HasRequirement, "entity_id"},
		{HasScenario, "entity_id"},
		{AppliesTo, "array"},
		{Targets, "entity_id"},
		{SpecTitle, "string"},
		{SpecVersion, "string"},
		{SpecAuthor, "string"},
		{SpecCreatedAt, "datetime"},
		{SpecUpdatedAt, "datetime"},
	}

	for _, tt := range tests {
		t.Run(tt.predicate, func(t *testing.T) {
			meta := vocabulary.GetPredicateMetadata(tt.predicate)
			if meta.DataType != tt.expectedType {
				t.Errorf("predicate %s: expected type %s, got %s", tt.predicate, tt.expectedType, meta.DataType)
			}
		})
	}
}

func TestClassIRIs(t *testing.T) {
	// Verify class IRIs are correctly formed
	tests := []struct {
		name        string
		classIRI    string
		expectedIRI string
	}{
		{"ClassSpec", ClassSpec, Namespace + "Specification"},
		{"ClassSourceOfTruth", ClassSourceOfTruth, Namespace + "SourceOfTruthSpec"},
		{"ClassDelta", ClassDelta, Namespace + "DeltaSpec"},
		{"ClassRequirement", ClassRequirement, Namespace + "Requirement"},
		{"ClassScenario", ClassScenario, Namespace + "Scenario"},
		{"ClassDeltaOperation", ClassDeltaOperation, Namespace + "DeltaOperation"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.classIRI != tt.expectedIRI {
				t.Errorf("%s: expected %s, got %s", tt.name, tt.expectedIRI, tt.classIRI)
			}
		})
	}
}

func TestPropertyIRIs(t *testing.T) {
	// Verify object property IRIs are correctly formed
	tests := []struct {
		name        string
		propIRI     string
		expectedIRI string
	}{
		{"PropHasRequirement", PropHasRequirement, Namespace + "hasRequirement"},
		{"PropHasScenario", PropHasScenario, Namespace + "hasScenario"},
		{"PropModifies", PropModifies, Namespace + "modifies"},
		{"PropTargets", PropTargets, Namespace + "targets"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.propIRI != tt.expectedIRI {
				t.Errorf("%s: expected %s, got %s", tt.name, tt.expectedIRI, tt.propIRI)
			}
		})
	}
}

func TestDataPropertyIRIs(t *testing.T) {
	// Verify data property IRIs are correctly formed
	tests := []struct {
		name        string
		propIRI     string
		expectedIRI string
	}{
		{"PropNormative", PropNormative, Namespace + "normative"},
		{"PropGiven", PropGiven, Namespace + "given"},
		{"PropWhen", PropWhen, Namespace + "when"},
		{"PropThen", PropThen, Namespace + "then"},
		{"PropOperation", PropOperation, Namespace + "operation"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.propIRI != tt.expectedIRI {
				t.Errorf("%s: expected %s, got %s", tt.name, tt.expectedIRI, tt.propIRI)
			}
		})
	}
}
