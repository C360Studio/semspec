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
		semspec.QuestionContent,
		semspec.QuestionTopic,
		semspec.QuestionFromAgent,
		semspec.QuestionStatus,
		semspec.QuestionCreatedAt,
		// ADR-040: capability + exploration predicates
		semspec.PlanExploration,
		semspec.PlanOpenQuestions,
		semspec.RequirementCapability,
		semspec.RequirementExternalSpec,
		semspec.CapabilityName,
		semspec.CapabilityLifecycle,
		semspec.CapabilityDescription,
		semspec.CapabilityPlan,
		semspec.CapabilityDependsOn,
		semspec.CapabilityExternalSpec,
		// ADR-041: scenario tag + harness binding + capability surface predicates
		semspec.CapabilitySurface,
		semspec.ScenarioTag,
		semspec.ScenarioHarnessProfile,
		// ADR-043: component + story + scenario.story + task extensions
		semspec.ComponentImplementationFile,
		semspec.ComponentCapability,
		semspec.StoryTitle,
		semspec.StoryIntent,
		semspec.StoryRequirement,
		semspec.StoryComponent,
		semspec.StoryFilesOwned,
		semspec.StoryDependsOn,
		semspec.PredicateStoryStatus,
		semspec.StoryPreparedBy,
		semspec.StoryPreparedAt,
		semspec.StoryCreatedAt,
		semspec.StoryUpdatedAt,
		semspec.ScenarioStory,
		semspec.TaskStory,
		semspec.TaskDependsOn,
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

func TestCapabilityPredicateValues(t *testing.T) {
	tests := []struct {
		name      string
		predicate string
		expected  string
	}{
		{"CapabilityName", semspec.CapabilityName, "semspec.capability.name"},
		{"CapabilityLifecycle", semspec.CapabilityLifecycle, "semspec.capability.lifecycle"},
		{"CapabilityDescription", semspec.CapabilityDescription, "semspec.capability.description"},
		{"CapabilityPlan", semspec.CapabilityPlan, "semspec.capability.plan"},
		{"CapabilityDependsOn", semspec.CapabilityDependsOn, "semspec.capability.depends_on"},
		{"CapabilityExternalSpec", semspec.CapabilityExternalSpec, "semspec.capability.external_spec"},
		{"CapabilitySurface", semspec.CapabilitySurface, "semspec.capability.surface"},
		{"PlanExploration", semspec.PlanExploration, "semspec.plan.exploration"},
		{"PlanOpenQuestions", semspec.PlanOpenQuestions, "semspec.plan.open_questions"},
		{"RequirementCapability", semspec.RequirementCapability, "semspec.requirement.capability"},
		{"RequirementExternalSpec", semspec.RequirementExternalSpec, "semspec.requirement.external_spec"},
		{"ScenarioTag", semspec.ScenarioTag, "semspec.scenario.tag"},
		{"ScenarioHarnessProfile", semspec.ScenarioHarnessProfile, "semspec.scenario.harness_profile"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.predicate != tc.expected {
				t.Errorf("got %q, want %q", tc.predicate, tc.expected)
			}
		})
	}
}

// TestCapabilityPredicatesAreThreePart guards against the ADR-040 load-bearing
// constraint that all new predicates use exactly three dotted segments
// (domain.category.property) with no embedded slugs or instance IDs.
func TestCapabilityPredicatesAreThreePart(t *testing.T) {
	preds := []string{
		semspec.PlanExploration,
		semspec.PlanOpenQuestions,
		semspec.RequirementCapability,
		semspec.RequirementExternalSpec,
		semspec.CapabilityName,
		semspec.CapabilityLifecycle,
		semspec.CapabilityDescription,
		semspec.CapabilityPlan,
		semspec.CapabilityDependsOn,
		semspec.CapabilityExternalSpec,
		semspec.CapabilitySurface,
		semspec.ScenarioTag,
		semspec.ScenarioHarnessProfile,
	}
	for _, p := range preds {
		t.Run(p, func(t *testing.T) {
			parts := 1
			for _, c := range p {
				if c == '.' {
					parts++
				}
			}
			if parts != 3 {
				t.Errorf("predicate %q has %d dotted segments, want 3 (domain.category.property)", p, parts)
			}
		})
	}
}

func TestComponentStoryPredicateValues(t *testing.T) {
	tests := []struct {
		name      string
		predicate string
		expected  string
	}{
		{"ComponentImplementationFile", semspec.ComponentImplementationFile, "semspec.component.implementation_file"},
		{"ComponentCapability", semspec.ComponentCapability, "semspec.component.capability"},
		{"StoryTitle", semspec.StoryTitle, "semspec.story.title"},
		{"StoryIntent", semspec.StoryIntent, "semspec.story.intent"},
		{"StoryRequirement", semspec.StoryRequirement, "semspec.story.requirement"},
		{"StoryComponent", semspec.StoryComponent, "semspec.story.component"},
		{"StoryFilesOwned", semspec.StoryFilesOwned, "semspec.story.files_owned"},
		{"StoryDependsOn", semspec.StoryDependsOn, "semspec.story.depends_on"},
		{"PredicateStoryStatus", semspec.PredicateStoryStatus, "semspec.story.status"},
		{"StoryPreparedBy", semspec.StoryPreparedBy, "semspec.story.prepared_by"},
		{"StoryPreparedAt", semspec.StoryPreparedAt, "semspec.story.prepared_at"},
		{"StoryCreatedAt", semspec.StoryCreatedAt, "semspec.story.created_at"},
		{"StoryUpdatedAt", semspec.StoryUpdatedAt, "semspec.story.updated_at"},
		{"ScenarioStory", semspec.ScenarioStory, "semspec.scenario.story"},
		{"TaskStory", semspec.TaskStory, "semspec.task.story"},
		{"TaskDependsOn", semspec.TaskDependsOn, "semspec.task.depends_on"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.predicate != tc.expected {
				t.Errorf("got %q, want %q", tc.predicate, tc.expected)
			}
		})
	}
}

// TestADR043PredicatesAreThreePart enforces the same three-segment convention
// for the ADR-043 additions.
func TestADR043PredicatesAreThreePart(t *testing.T) {
	preds := []string{
		semspec.ComponentImplementationFile,
		semspec.ComponentCapability,
		semspec.StoryTitle,
		semspec.StoryIntent,
		semspec.StoryRequirement,
		semspec.StoryComponent,
		semspec.StoryFilesOwned,
		semspec.StoryDependsOn,
		semspec.PredicateStoryStatus,
		semspec.StoryPreparedBy,
		semspec.StoryPreparedAt,
		semspec.StoryCreatedAt,
		semspec.StoryUpdatedAt,
		semspec.ScenarioStory,
		semspec.TaskStory,
		semspec.TaskDependsOn,
	}
	for _, p := range preds {
		t.Run(p, func(t *testing.T) {
			parts := 1
			for _, c := range p {
				if c == '.' {
					parts++
				}
			}
			if parts != 3 {
				t.Errorf("predicate %q has %d dotted segments, want 3 (domain.category.property)", p, parts)
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
