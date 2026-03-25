package executionmanager

import (
	"strings"
	"testing"

	wf "github.com/c360studio/semspec/vocabulary/workflow"
)

func TestTaskExecutionEntity_EntityID(t *testing.T) {
	tests := []struct {
		name   string
		slug   string
		taskID string
		want   string
	}{
		{
			name:   "basic",
			slug:   "my-feature",
			taskID: "task-001",
			want:   "semspec.local.exec.task.run.my-feature-task-001",
		},
		{
			name:   "auth-task",
			slug:   "auth-refresh",
			taskID: "impl-jwt",
			want:   "semspec.local.exec.task.run.auth-refresh-impl-jwt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &TaskExecutionEntity{Slug: tt.slug, TaskID: tt.taskID}
			got := e.EntityID()
			if got != tt.want {
				t.Errorf("EntityID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTaskExecutionEntity_EntityID_6PartFormat(t *testing.T) {
	e := &TaskExecutionEntity{Slug: "test-slug", TaskID: "task-1"}
	parts := strings.Split(e.EntityID(), ".")
	if len(parts) != 6 {
		t.Errorf("EntityID() has %d dot-separated parts, want 6: %q", len(parts), e.EntityID())
	}
}

func TestTaskExecutionEntity_Triples_RequiredPredicates(t *testing.T) {
	e := &TaskExecutionEntity{
		Slug:          "test-slug",
		TaskID:        "task-1",
		Iteration:     0,
		MaxIterations: 3,
	}

	triples := e.Triples()
	predicates := make(map[string]bool)
	for _, tr := range triples {
		predicates[tr.Predicate] = true
	}

	required := []string{wf.Type, wf.Slug, wf.Iteration, wf.MaxIterations}
	for _, pred := range required {
		if !predicates[pred] {
			t.Errorf("Triples() missing required predicate %q", pred)
		}
	}
}

func TestTaskExecutionEntity_Triples_TypeIsTaskExecution(t *testing.T) {
	e := &TaskExecutionEntity{Slug: "s", TaskID: "t"}
	for _, tr := range e.Triples() {
		if tr.Predicate == wf.Type {
			if tr.Object != "task-execution" {
				t.Errorf("wf.Type triple object = %q, want %q", tr.Object, "task-execution")
			}
			return
		}
	}
	t.Error("Triples() missing wf.Type triple")
}

func TestTaskExecutionEntity_Triples_OptionalPredicatesOmittedWhenEmpty(t *testing.T) {
	e := &TaskExecutionEntity{
		Slug:          "test-slug",
		TaskID:        "task-1",
		Iteration:     0,
		MaxIterations: 3,
	}

	triples := e.Triples()
	predicates := make(map[string]bool)
	for _, tr := range triples {
		predicates[tr.Predicate] = true
	}

	optional := []string{
		wf.Phase, wf.TraceID, wf.FilesModified, wf.ValidationPassed,
		wf.Verdict, wf.RejectionType, wf.Feedback, wf.ErrorReason,
		wf.RelPlan, wf.RelTask, wf.RelProject, wf.RelLoop,
	}
	for _, pred := range optional {
		if predicates[pred] {
			t.Errorf("Triples() should not emit predicate %q when field is empty/zero", pred)
		}
	}
}

func TestTaskExecutionEntity_Triples_OptionalPredicatesIncludedWhenSet(t *testing.T) {
	e := &TaskExecutionEntity{
		Slug:             "test-slug",
		TaskID:           "task-1",
		Phase:            "reviewing",
		TraceID:          "trace-abc",
		FilesModified:    `["main.go","config.go"]`,
		ValidationPassed: true,
		Verdict:          "approved",
		RejectionType:    "fixable",
		Feedback:         "Fix the error handling",
		TaskEntityID:     "local.semspec.task.default.task.task-1",
	}

	triples := e.Triples()
	predicates := make(map[string]bool)
	for _, tr := range triples {
		predicates[tr.Predicate] = true
	}

	expected := []string{
		wf.Phase, wf.TraceID, wf.FilesModified, wf.ValidationPassed,
		wf.Verdict, wf.RejectionType, wf.Feedback, wf.RelTask,
	}
	for _, pred := range expected {
		if !predicates[pred] {
			t.Errorf("Triples() missing predicate %q when field is set", pred)
		}
	}
}

func TestTaskExecutionEntity_Triples_RelationshipEntityIDFormat(t *testing.T) {
	planID := "local.semspec.plan.default.plan.my-slug"
	taskID := "local.semspec.task.default.task.task-1"
	projectID := "local.semspec.project.default.project.p"
	loopID := "local.semspec.loop.default.loop.l"

	e := &TaskExecutionEntity{
		Slug:            "my-slug",
		TaskID:          "task-1",
		PlanEntityID:    planID,
		TaskEntityID:    taskID,
		ProjectEntityID: projectID,
		LoopEntityID:    loopID,
	}

	relTriples := make(map[string]string)
	for _, tr := range e.Triples() {
		switch tr.Predicate {
		case wf.RelPlan, wf.RelTask, wf.RelProject, wf.RelLoop:
			relTriples[tr.Predicate] = tr.Object.(string)
		}
	}

	if got := relTriples[wf.RelPlan]; got != planID {
		t.Errorf("RelPlan = %q, want %q", got, planID)
	}
	if got := relTriples[wf.RelTask]; got != taskID {
		t.Errorf("RelTask = %q, want %q", got, taskID)
	}
	if got := relTriples[wf.RelProject]; got != projectID {
		t.Errorf("RelProject = %q, want %q", got, projectID)
	}
	if got := relTriples[wf.RelLoop]; got != loopID {
		t.Errorf("RelLoop = %q, want %q", got, loopID)
	}
}

func TestTaskExecutionEntity_Triples_SubjectMatchesEntityID(t *testing.T) {
	e := &TaskExecutionEntity{
		Slug:          "slug",
		TaskID:        "t1",
		Iteration:     0,
		MaxIterations: 2,
	}

	entityID := e.EntityID()
	for _, tr := range e.Triples() {
		if tr.Subject != entityID {
			t.Errorf("triple Subject = %q, want %q (predicate: %s)", tr.Subject, entityID, tr.Predicate)
		}
	}
}

func TestNewTaskExecutionEntity_FromState(t *testing.T) {
	exec := &taskExecution{
		EntityID:         "semspec.local.exec.task.run.my-slug-task-1",
		Slug:             "my-slug",
		TaskID:           "task-1",
		Iteration:        1,
		MaxIterations:    3,
		TraceID:          "trace-xyz",
		Verdict:          "approved",
		RejectionType:    "",
		Feedback:         "Well done",
		ValidationPassed: true,
		FilesModified:    []string{"main.go"},
	}

	entity := NewTaskExecutionEntity(exec)

	if entity.Slug != exec.Slug {
		t.Errorf("Slug = %q, want %q", entity.Slug, exec.Slug)
	}
	if entity.TaskID != exec.TaskID {
		t.Errorf("TaskID = %q, want %q", entity.TaskID, exec.TaskID)
	}
	if entity.Iteration != exec.Iteration {
		t.Errorf("Iteration = %d, want %d", entity.Iteration, exec.Iteration)
	}
	if entity.MaxIterations != exec.MaxIterations {
		t.Errorf("MaxIterations = %d, want %d", entity.MaxIterations, exec.MaxIterations)
	}
	if entity.TraceID != exec.TraceID {
		t.Errorf("TraceID = %q, want %q", entity.TraceID, exec.TraceID)
	}
	if entity.Verdict != exec.Verdict {
		t.Errorf("Verdict = %q, want %q", entity.Verdict, exec.Verdict)
	}
	if entity.ValidationPassed != exec.ValidationPassed {
		t.Errorf("ValidationPassed = %v, want %v", entity.ValidationPassed, exec.ValidationPassed)
	}

	// EntityID should match what handleTrigger produces.
	expectedID := "semspec.local.exec.task.run.my-slug-task-1"
	if got := entity.EntityID(); got != expectedID {
		t.Errorf("EntityID() = %q, want %q", got, expectedID)
	}
}
