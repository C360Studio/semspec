package workflow

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/vocabulary"
)

// allPredicates is the complete set of predicates defined in this package.
// Used to validate registration and format invariants.
var allPredicates = []string{
	// Core execution identity
	Type,
	Phase,
	Status,
	Slug,
	Title,
	Description,
	// Execution tracking
	TDDCycle,
	MaxTDDCycles,
	Prompt,
	TraceID,
	ErrorReason,
	// Review-specific
	PlanContent,
	Verdict,
	Summary,
	Findings,
	EscalationReason,
	// Task-execution-specific
	FilesModified,
	ValidationPassed,
	Feedback,
	RejectionType,
	// Scenario-execution-specific
	NodeCount,
	FailureReason,
	// Cascade-specific
	CascadeAffectedRequirements,
	CascadeAffectedScenarios,
	CascadeTasksDirtied,
	// Relationship predicates
	RelPlan,
	RelTask,
	RelScenario,
	RelProject,
	RelLoop,
	RelRequirement,
}

func TestPredicatesRegistered(t *testing.T) {
	// Execution identity predicates
	executionPredicates := []string{
		Type,
		Phase,
		Status,
		Slug,
		Title,
		Description,
	}

	for _, pred := range executionPredicates {
		t.Run(pred, func(t *testing.T) {
			meta := vocabulary.GetPredicateMetadata(pred)
			if meta.Description == "" {
				t.Errorf("predicate %s not registered or missing description", pred)
			}
		})
	}

	// Tracking predicates
	trackingPredicates := []string{
		TDDCycle,
		MaxTDDCycles,
		Prompt,
		TraceID,
		ErrorReason,
	}

	for _, pred := range trackingPredicates {
		t.Run(pred, func(t *testing.T) {
			meta := vocabulary.GetPredicateMetadata(pred)
			if meta.Description == "" {
				t.Errorf("predicate %s not registered or missing description", pred)
			}
		})
	}

	// Review predicates
	reviewPredicates := []string{
		PlanContent,
		Verdict,
		Summary,
		Findings,
		EscalationReason,
	}

	for _, pred := range reviewPredicates {
		t.Run(pred, func(t *testing.T) {
			meta := vocabulary.GetPredicateMetadata(pred)
			if meta.Description == "" {
				t.Errorf("predicate %s not registered or missing description", pred)
			}
		})
	}

	// Task predicates
	taskPredicates := []string{
		FilesModified,
		ValidationPassed,
		Feedback,
		RejectionType,
	}

	for _, pred := range taskPredicates {
		t.Run(pred, func(t *testing.T) {
			meta := vocabulary.GetPredicateMetadata(pred)
			if meta.Description == "" {
				t.Errorf("predicate %s not registered or missing description", pred)
			}
		})
	}

	// Scenario predicates
	scenarioPredicates := []string{
		NodeCount,
		FailureReason,
	}

	for _, pred := range scenarioPredicates {
		t.Run(pred, func(t *testing.T) {
			meta := vocabulary.GetPredicateMetadata(pred)
			if meta.Description == "" {
				t.Errorf("predicate %s not registered or missing description", pred)
			}
		})
	}

	// Cascade predicates
	cascadePredicates := []string{
		CascadeAffectedRequirements,
		CascadeAffectedScenarios,
		CascadeTasksDirtied,
	}

	for _, pred := range cascadePredicates {
		t.Run(pred, func(t *testing.T) {
			meta := vocabulary.GetPredicateMetadata(pred)
			if meta.Description == "" {
				t.Errorf("predicate %s not registered or missing description", pred)
			}
		})
	}

	// Relationship predicates
	relPredicates := []string{
		RelPlan,
		RelTask,
		RelScenario,
		RelProject,
		RelLoop,
		RelRequirement,
	}

	for _, pred := range relPredicates {
		t.Run(pred, func(t *testing.T) {
			meta := vocabulary.GetPredicateMetadata(pred)
			if meta.Description == "" {
				t.Errorf("predicate %s not registered or missing description", pred)
			}
		})
	}
}

func TestPredicateDataTypes(t *testing.T) {
	tests := []struct {
		predicate    string
		expectedType string
	}{
		// String scalars
		{Type, "string"},
		{Phase, "string"},
		{Status, "string"},
		{Slug, "string"},
		{Title, "string"},
		{Description, "string"},
		{Prompt, "string"},
		{TraceID, "string"},
		{ErrorReason, "string"},
		{PlanContent, "string"},
		{Verdict, "string"},
		{Summary, "string"},
		{Findings, "string"},
		{EscalationReason, "string"},
		{FilesModified, "string"},
		{ValidationPassed, "string"},
		{Feedback, "string"},
		{RejectionType, "string"},
		{FailureReason, "string"},
		// Integer counters
		{TDDCycle, "int"},
		{MaxTDDCycles, "int"},
		{NodeCount, "int"},
		{CascadeAffectedRequirements, "int"},
		{CascadeAffectedScenarios, "int"},
		{CascadeTasksDirtied, "int"},
		// Relationship entity IDs
		{RelPlan, "entity_id"},
		{RelTask, "entity_id"},
		{RelScenario, "entity_id"},
		{RelProject, "entity_id"},
		{RelLoop, "entity_id"},
		{RelRequirement, "entity_id"},
	}

	for _, tt := range tests {
		t.Run(tt.predicate, func(t *testing.T) {
			meta := vocabulary.GetPredicateMetadata(tt.predicate)
			if meta == nil {
				t.Fatalf("predicate %s not registered", tt.predicate)
			}
			if meta.DataType != tt.expectedType {
				t.Errorf("predicate %s: expected type %s, got %s", tt.predicate, tt.expectedType, meta.DataType)
			}
		})
	}
}

func TestPredicateFormat(t *testing.T) {
	// Every predicate must follow the domain.category.property format (exactly 2 dots).
	for _, pred := range allPredicates {
		t.Run(pred, func(t *testing.T) {
			if strings.Count(pred, ".") != 2 {
				t.Errorf("predicate %q does not follow domain.category.property format (expected exactly 2 dots, got %d)",
					pred, strings.Count(pred, "."))
			}
		})
	}
}

func TestPredicateIRIs(t *testing.T) {
	// Spot-check that IRI mappings are present for a representative sample.
	tests := []struct {
		predicate   string
		expectedIRI string
	}{
		{Title, vocabulary.DcTitle},
		{RelPlan, Namespace + "plan"},
		{RelTask, Namespace + "task"},
		{RelScenario, Namespace + "scenario"},
		{RelProject, Namespace + "project"},
		{RelLoop, Namespace + "loop"},
		{RelRequirement, Namespace + "requirement"},
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
