package observability

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/vocabulary"
)

// allPredicates is the complete set of predicates defined in this package.
// Used to validate registration and format invariants.
var allPredicates = []string{
	Checkpoint,
	Outcome,
	Incident,
	RawResponse,
	Reason,
	Quirk,
	Role,
	Model,
	PromptVersion,
}

func TestPredicatesRegistered(t *testing.T) {
	for _, pred := range allPredicates {
		t.Run(pred, func(t *testing.T) {
			meta := vocabulary.GetPredicateMetadata(pred)
			if meta == nil {
				t.Fatalf("predicate %s not registered", pred)
			}
			if meta.Description == "" {
				t.Errorf("predicate %s missing description", pred)
			}
		})
	}
}

func TestPredicateDataTypes(t *testing.T) {
	tests := []struct {
		predicate    string
		expectedType string
	}{
		// Strings — vast majority of LLM-output telemetry is string-shaped.
		{Checkpoint, "string"},
		{Outcome, "string"},
		{RawResponse, "string"},
		{Reason, "string"},
		{Quirk, "string"},
		{Role, "string"},
		{Model, "string"},
		{PromptVersion, "string"},
		// Relation — incident pointer is an entity_id.
		{Incident, "entity_id"},
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

// TestPredicateFormat enforces the domain.category.property convention.
// Predicates with the wrong shape break NATS wildcard subject patterns and
// graph_query traversal, so this is a hard structural rule, not style.
func TestPredicateFormat(t *testing.T) {
	for _, pred := range allPredicates {
		t.Run(pred, func(t *testing.T) {
			if strings.Count(pred, ".") != 2 {
				t.Errorf("predicate %q does not follow domain.category.property format (expected exactly 2 dots, got %d)",
					pred, strings.Count(pred, "."))
			}
			if !strings.HasPrefix(pred, "llm.parse.") {
				t.Errorf("predicate %q does not have the llm.parse. prefix; observability vocab is scoped to LLM parse incidents", pred)
			}
		})
	}
}

// TestPredicateIRIs spot-checks IRI mappings so registry exports remain
// compatible with downstream RDF consumers.
func TestPredicateIRIs(t *testing.T) {
	tests := []struct {
		predicate   string
		expectedIRI string
	}{
		{Checkpoint, Namespace + "checkpoint"},
		{Outcome, Namespace + "outcome"},
		{Incident, Namespace + "incident"},
		{RawResponse, Namespace + "rawResponse"},
		{Reason, Namespace + "reason"},
		{Quirk, Namespace + "quirk"},
		{Role, Namespace + "role"},
		{Model, Namespace + "model"},
		{PromptVersion, Namespace + "promptVersion"},
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

// TestCheckpointConstants pins the closed enum of checkpoint values. Adding
// a new checkpoint (e.g. CP-3 retry-classification) means updating this
// test deliberately, which forces the contributor to also update the ADR
// and the watch-sidecar detector that aggregates by checkpoint.
func TestCheckpointConstants(t *testing.T) {
	expected := map[string]string{
		"CheckpointResponseParse": CheckpointResponseParse,
		"CheckpointToolCall":      CheckpointToolCall,
	}
	wantValues := map[string]string{
		"CheckpointResponseParse": "response_parse",
		"CheckpointToolCall":      "tool_call",
	}
	for name, got := range expected {
		if want := wantValues[name]; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

// TestOutcomeConstants pins the closed enum of outcome values. Same
// rationale as TestCheckpointConstants — the IncidentRateExceeded detector
// (ADR-035 step 5) and qa-reviewer's incident queries both branch on this
// set; rename or addition needs deliberate coordination.
func TestOutcomeConstants(t *testing.T) {
	expected := map[string]string{
		"OutcomeStrict":         OutcomeStrict,
		"OutcomeToleratedQuirk": OutcomeToleratedQuirk,
		"OutcomeRejected":       OutcomeRejected,
	}
	wantValues := map[string]string{
		"OutcomeStrict":         "strict",
		"OutcomeToleratedQuirk": "tolerated_quirk",
		"OutcomeRejected":       "rejected",
	}
	for name, got := range expected {
		if want := wantValues[name]; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}
