package scenarioexecutor

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/tools/decompose"
	wf "github.com/c360studio/semspec/vocabulary/workflow"
)

func TestScenarioExecutionEntity_EntityID(t *testing.T) {
	tests := []struct {
		name       string
		slug       string
		scenarioID string
		want       string
	}{
		{
			name:       "basic",
			slug:       "my-feature",
			scenarioID: "scenario-001",
			want:       "local.semspec.workflow.scenario-execution.execution.my-feature-scenario-001",
		},
		{
			name:       "auth",
			slug:       "auth-refresh",
			scenarioID: "user-login",
			want:       "local.semspec.workflow.scenario-execution.execution.auth-refresh-user-login",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &ScenarioExecutionEntity{Slug: tt.slug, ScenarioID: tt.scenarioID}
			got := e.EntityID()
			if got != tt.want {
				t.Errorf("EntityID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestScenarioExecutionEntity_EntityID_6PartFormat(t *testing.T) {
	e := &ScenarioExecutionEntity{Slug: "test-slug", ScenarioID: "sc-1"}
	parts := strings.Split(e.EntityID(), ".")
	if len(parts) != 6 {
		t.Errorf("EntityID() has %d dot-separated parts, want 6: %q", len(parts), e.EntityID())
	}
}

func TestScenarioExecutionEntity_Triples_RequiredPredicates(t *testing.T) {
	e := &ScenarioExecutionEntity{
		Slug:       "test-slug",
		ScenarioID: "sc-1",
	}

	triples := e.Triples()
	predicates := make(map[string]bool)
	for _, tr := range triples {
		predicates[tr.Predicate] = true
	}

	required := []string{wf.Type, wf.Slug}
	for _, pred := range required {
		if !predicates[pred] {
			t.Errorf("Triples() missing required predicate %q", pred)
		}
	}
}

func TestScenarioExecutionEntity_Triples_TypeIsScenarioExecution(t *testing.T) {
	e := &ScenarioExecutionEntity{Slug: "s", ScenarioID: "sc"}
	for _, tr := range e.Triples() {
		if tr.Predicate == wf.Type {
			if tr.Object != "scenario-execution" {
				t.Errorf("wf.Type triple object = %q, want %q", tr.Object, "scenario-execution")
			}
			return
		}
	}
	t.Error("Triples() missing wf.Type triple")
}

func TestScenarioExecutionEntity_Triples_OptionalPredicatesOmittedWhenEmpty(t *testing.T) {
	e := &ScenarioExecutionEntity{
		Slug:       "test-slug",
		ScenarioID: "sc-1",
	}

	triples := e.Triples()
	predicates := make(map[string]bool)
	for _, tr := range triples {
		predicates[tr.Predicate] = true
	}

	optional := []string{
		wf.Phase, wf.TraceID, wf.NodeCount, wf.FailureReason, wf.ErrorReason,
		wf.RelScenario, wf.RelProject, wf.RelLoop,
	}
	for _, pred := range optional {
		if predicates[pred] {
			t.Errorf("Triples() should not emit predicate %q when field is empty/zero", pred)
		}
	}
}

func TestScenarioExecutionEntity_Triples_OptionalPredicatesIncludedWhenSet(t *testing.T) {
	e := &ScenarioExecutionEntity{
		Slug:             "test-slug",
		ScenarioID:       "sc-1",
		Phase:            "executing",
		TraceID:          "trace-abc",
		NodeCount:        5,
		FailureReason:    "node failed",
		ScenarioEntityID: "local.semspec.scenario.default.scenario.sc-1",
	}

	triples := e.Triples()
	predicates := make(map[string]bool)
	for _, tr := range triples {
		predicates[tr.Predicate] = true
	}

	expected := []string{
		wf.Phase, wf.TraceID, wf.NodeCount, wf.FailureReason, wf.RelScenario,
	}
	for _, pred := range expected {
		if !predicates[pred] {
			t.Errorf("Triples() missing predicate %q when field is set", pred)
		}
	}
}

func TestScenarioExecutionEntity_Triples_RelationshipEntityIDFormat(t *testing.T) {
	scenarioID := "local.semspec.scenario.default.scenario.sc-1"
	projectID := "local.semspec.project.default.project.p"
	loopID := "local.semspec.loop.default.loop.l"

	e := &ScenarioExecutionEntity{
		Slug:             "my-slug",
		ScenarioID:       "sc-1",
		ScenarioEntityID: scenarioID,
		ProjectEntityID:  projectID,
		LoopEntityID:     loopID,
	}

	relTriples := make(map[string]string)
	for _, tr := range e.Triples() {
		switch tr.Predicate {
		case wf.RelScenario, wf.RelProject, wf.RelLoop:
			relTriples[tr.Predicate] = tr.Object.(string)
		}
	}

	if got := relTriples[wf.RelScenario]; got != scenarioID {
		t.Errorf("RelScenario = %q, want %q", got, scenarioID)
	}
	if got := relTriples[wf.RelProject]; got != projectID {
		t.Errorf("RelProject = %q, want %q", got, projectID)
	}
	if got := relTriples[wf.RelLoop]; got != loopID {
		t.Errorf("RelLoop = %q, want %q", got, loopID)
	}
}

func TestScenarioExecutionEntity_Triples_SubjectMatchesEntityID(t *testing.T) {
	e := &ScenarioExecutionEntity{Slug: "slug", ScenarioID: "sc-1"}

	entityID := e.EntityID()
	for _, tr := range e.Triples() {
		if tr.Subject != entityID {
			t.Errorf("triple Subject = %q, want %q (predicate: %s)", tr.Subject, entityID, tr.Predicate)
		}
	}
}

func TestNewScenarioExecutionEntity_FromState(t *testing.T) {
	dag := &decompose.TaskDAG{
		Nodes: []decompose.TaskNode{
			{ID: "node-1"},
			{ID: "node-2"},
			{ID: "node-3"},
		},
	}

	exec := &scenarioExecution{
		EntityID:   "local.semspec.workflow.scenario-execution.execution.my-slug-sc-1",
		Slug:       "my-slug",
		ScenarioID: "sc-1",
		TraceID:    "trace-xyz",
		DAG:        dag,
	}

	entity := NewScenarioExecutionEntity(exec)

	if entity.Slug != exec.Slug {
		t.Errorf("Slug = %q, want %q", entity.Slug, exec.Slug)
	}
	if entity.ScenarioID != exec.ScenarioID {
		t.Errorf("ScenarioID = %q, want %q", entity.ScenarioID, exec.ScenarioID)
	}
	if entity.TraceID != exec.TraceID {
		t.Errorf("TraceID = %q, want %q", entity.TraceID, exec.TraceID)
	}
	if entity.NodeCount != len(dag.Nodes) {
		t.Errorf("NodeCount = %d, want %d", entity.NodeCount, len(dag.Nodes))
	}

	expectedID := "local.semspec.workflow.scenario-execution.execution.my-slug-sc-1"
	if got := entity.EntityID(); got != expectedID {
		t.Errorf("EntityID() = %q, want %q", got, expectedID)
	}
}

func TestNewScenarioExecutionEntity_NilDAG(t *testing.T) {
	exec := &scenarioExecution{
		Slug:       "my-slug",
		ScenarioID: "sc-1",
		DAG:        nil, // not yet decomposed
	}

	entity := NewScenarioExecutionEntity(exec)
	if entity.NodeCount != 0 {
		t.Errorf("NodeCount should be 0 when DAG is nil, got %d", entity.NodeCount)
	}
}
