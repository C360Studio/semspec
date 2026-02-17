package semspec_test

import (
	"testing"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semstreams/vocabulary"
)

func TestPredicatesRegistered(t *testing.T) {
	// Sample of predicates to verify registration
	predicates := []string{
		semspec.PlanTitle,
		semspec.PredicatePlanStatus,
		semspec.PlanAuthor,
		semspec.SpecTitle,
		semspec.PredicateSpecStatus,
		semspec.SpecPlan,
		semspec.TaskTitle,
		semspec.PredicateTaskStatus,
		semspec.TaskPredecessor,
		semspec.PredicateLoopStatus,
		semspec.PredicateLoopRole,
		semspec.LoopTask,
		semspec.PredicateActivityType,
		semspec.ActivityLoop,
		semspec.PredicateResultOutcome,
		semspec.CodePath,
		semspec.CodeContains,
		semspec.ConstitutionRule,
		semspec.DCTitle,
		semspec.SKOSPrefLabel,
		semspec.ProvDerivedFrom,
	}

	for _, predicate := range predicates {
		t.Run(predicate, func(t *testing.T) {
			meta := vocabulary.GetPredicateMetadata(predicate)
			if meta == nil {
				t.Errorf("predicate %q not registered", predicate)
				return
			}
			if meta.Description == "" {
				t.Errorf("predicate %q has no description", predicate)
			}
			if meta.DataType == "" {
				t.Errorf("predicate %q has no data type", predicate)
			}
		})
	}
}

func TestPlanPredicateValues(t *testing.T) {
	tests := []struct {
		name      string
		predicate string
		expected  string
	}{
		{"PlanTitle", semspec.PlanTitle, "semspec.plan.title"},
		{"PlanStatus", semspec.PredicatePlanStatus, "semspec.plan.status"},
		{"PlanAuthor", semspec.PlanAuthor, "semspec.plan.author"},
		{"PlanSlug", semspec.PlanSlug, "semspec.plan.slug"},
		{"PlanCreatedAt", semspec.PlanCreatedAt, "semspec.plan.created_at"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.predicate != tc.expected {
				t.Errorf("got %q, want %q", tc.predicate, tc.expected)
			}
		})
	}
}

func TestSpecPredicateValues(t *testing.T) {
	tests := []struct {
		name      string
		predicate string
		expected  string
	}{
		{"SpecTitle", semspec.SpecTitle, "semspec.spec.title"},
		{"SpecStatus", semspec.PredicateSpecStatus, "semspec.spec.status"},
		{"SpecVersion", semspec.SpecVersion, "semspec.spec.version"},
		{"SpecPlan", semspec.SpecPlan, "semspec.spec.plan"},
		{"SpecAffects", semspec.SpecAffects, "semspec.spec.affects"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.predicate != tc.expected {
				t.Errorf("got %q, want %q", tc.predicate, tc.expected)
			}
		})
	}
}

func TestTaskPredicateValues(t *testing.T) {
	tests := []struct {
		name      string
		predicate string
		expected  string
	}{
		{"TaskTitle", semspec.TaskTitle, "semspec.task.title"},
		{"TaskStatus", semspec.PredicateTaskStatus, "semspec.task.status"},
		{"TaskType", semspec.PredicateTaskType, "semspec.task.type"},
		{"TaskPredecessor", semspec.TaskPredecessor, "semspec.task.predecessor"},
		{"TaskSuccessor", semspec.TaskSuccessor, "semspec.task.successor"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.predicate != tc.expected {
				t.Errorf("got %q, want %q", tc.predicate, tc.expected)
			}
		})
	}
}

func TestLoopPredicateValues(t *testing.T) {
	tests := []struct {
		name      string
		predicate string
		expected  string
	}{
		{"LoopStatus", semspec.PredicateLoopStatus, "agent.loop.status"},
		{"LoopRole", semspec.PredicateLoopRole, "agent.loop.role"},
		{"LoopIterations", semspec.LoopIterations, "agent.loop.iterations"},
		{"LoopTask", semspec.LoopTask, "agent.loop.task"},
		{"LoopStartedAt", semspec.LoopStartedAt, "agent.loop.started_at"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.predicate != tc.expected {
				t.Errorf("got %q, want %q", tc.predicate, tc.expected)
			}
		})
	}
}

func TestCodePredicateValues(t *testing.T) {
	tests := []struct {
		name      string
		predicate string
		expected  string
	}{
		{"CodePath", semspec.CodePath, "code.artifact.path"},
		{"CodeType", semspec.PredicateCodeType, "code.artifact.type"},
		{"CodeContains", semspec.CodeContains, "code.structure.contains"},
		{"CodeBelongsTo", semspec.CodeBelongsTo, "code.structure.belongs"},
		{"CodeImports", semspec.CodeImports, "code.dependency.imports"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.predicate != tc.expected {
				t.Errorf("got %q, want %q", tc.predicate, tc.expected)
			}
		})
	}
}

func TestPredicateIRIMappings(t *testing.T) {
	tests := []struct {
		name      string
		predicate string
		wantIRI   string
	}{
		{
			name:      "PlanAuthor maps to PROV wasAttributedTo",
			predicate: semspec.PlanAuthor,
			wantIRI:   vocabulary.ProvWasAttributedTo,
		},
		{
			name:      "SpecPlan maps to PROV wasDerivedFrom",
			predicate: semspec.SpecPlan,
			wantIRI:   vocabulary.ProvWasDerivedFrom,
		},
		{
			name:      "LoopTask maps to PROV used",
			predicate: semspec.LoopTask,
			wantIRI:   vocabulary.ProvUsed,
		},
		{
			name:      "LoopStartedAt maps to PROV startedAtTime",
			predicate: semspec.LoopStartedAt,
			wantIRI:   vocabulary.ProvStartedAtTime,
		},
		{
			name:      "CodeContains maps to BFO has_part",
			predicate: semspec.CodeContains,
			wantIRI:   "http://purl.obolibrary.org/obo/BFO_0000051",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			meta := vocabulary.GetPredicateMetadata(tc.predicate)
			if meta == nil {
				t.Fatalf("predicate %q not registered", tc.predicate)
			}
			if meta.StandardIRI != tc.wantIRI {
				t.Errorf("got IRI %q, want %q", meta.StandardIRI, tc.wantIRI)
			}
		})
	}
}
