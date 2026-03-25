package executionmanager

import (
	"fmt"
	"time"

	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/message"
)

// TaskExecutionEntity converts a taskExecution to graph triples.
// It implements the Graphable interface (EntityID + Triples).
type TaskExecutionEntity struct {
	// Identity
	Slug   string
	TaskID string

	// Execution tracking
	Phase         string
	Iteration     int
	MaxIterations int
	TraceID       string
	ErrorReason   string

	// Developer output
	FilesModified string // JSON array of file paths

	// Validation output
	ValidationPassed bool

	// Reviewer output
	Verdict       string
	RejectionType string
	Feedback      string

	// Worktree state
	WorktreePath   string
	WorktreeBranch string

	// Relationship fields — Objects are 6-part entity IDs, creating graph edges.
	PlanEntityID    string
	TaskEntityID    string
	ProjectEntityID string
	LoopEntityID    string
}

// NewTaskExecutionEntity creates a TaskExecutionEntity from a taskExecution.
// The caller must hold exec.mu before calling this function.
func NewTaskExecutionEntity(exec *taskExecution) *TaskExecutionEntity {
	e := &TaskExecutionEntity{
		Slug:          exec.Slug,
		TaskID:        exec.TaskID,
		Iteration:     exec.Iteration,
		MaxIterations: exec.MaxIterations,
		TraceID:       exec.TraceID,
		Verdict:       exec.Verdict,
		RejectionType: exec.RejectionType,
		Feedback:      exec.Feedback,
	}

	e.WorktreePath = exec.WorktreePath
	e.WorktreeBranch = exec.WorktreeBranch

	if exec.ValidationPassed {
		e.ValidationPassed = exec.ValidationPassed
	}

	if len(exec.FilesModified) > 0 {
		// Store as a simple joined representation; callers can marshal to JSON before setting.
		e.FilesModified = fmt.Sprintf("%v", exec.FilesModified)
	}

	return e
}

// EntityID returns the 6-part canonical graph entity ID.
// Format: {prefix}.exec.task.run.<slug>-<taskID>
// This must match the format used in handleTrigger.
func (e *TaskExecutionEntity) EntityID() string {
	return fmt.Sprintf("%s.exec.task.run.%s-%s", workflow.EntityPrefix(), e.Slug, e.TaskID)
}

// WithPhase sets the current lifecycle phase and returns the entity for chaining.
func (e *TaskExecutionEntity) WithPhase(phase string) *TaskExecutionEntity {
	e.Phase = phase
	return e
}

// WithFilesModifiedJSON sets the files modified as a JSON array string.
func (e *TaskExecutionEntity) WithFilesModifiedJSON(filesJSON string) *TaskExecutionEntity {
	e.FilesModified = filesJSON
	return e
}

// WithPlanEntityID sets the relationship to the associated plan entity.
func (e *TaskExecutionEntity) WithPlanEntityID(id string) *TaskExecutionEntity {
	e.PlanEntityID = id
	return e
}

// WithTaskEntityID sets the relationship to the associated task entity.
func (e *TaskExecutionEntity) WithTaskEntityID(id string) *TaskExecutionEntity {
	e.TaskEntityID = id
	return e
}

// WithProjectEntityID sets the relationship to the associated project entity.
func (e *TaskExecutionEntity) WithProjectEntityID(id string) *TaskExecutionEntity {
	e.ProjectEntityID = id
	return e
}

// WithLoopEntityID sets the relationship to the associated agentic loop entity.
func (e *TaskExecutionEntity) WithLoopEntityID(id string) *TaskExecutionEntity {
	e.LoopEntityID = id
	return e
}

// WithErrorReason sets the error reason for failed executions.
func (e *TaskExecutionEntity) WithErrorReason(reason string) *TaskExecutionEntity {
	e.ErrorReason = reason
	return e
}

// Triples converts the entity to graph triples using vocabulary constants.
// Property triples use scalar Objects; relationship triples use 6-part entity ID Objects.
func (e *TaskExecutionEntity) Triples() []message.Triple {
	id := e.EntityID()
	now := time.Now()

	triples := []message.Triple{
		{Subject: id, Predicate: wf.Type, Object: "task-execution", Source: componentName, Timestamp: now, Confidence: 1.0},
		{Subject: id, Predicate: wf.Slug, Object: e.Slug, Source: componentName, Timestamp: now, Confidence: 1.0},
		{Subject: id, Predicate: wf.Iteration, Object: e.Iteration, Source: componentName, Timestamp: now, Confidence: 1.0},
		{Subject: id, Predicate: wf.MaxIterations, Object: e.MaxIterations, Source: componentName, Timestamp: now, Confidence: 1.0},
	}

	// Optional scalar predicates — only emit when non-empty or non-zero.
	if e.Phase != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.Phase, Object: e.Phase, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.TraceID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.TraceID, Object: e.TraceID, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.FilesModified != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.FilesModified, Object: e.FilesModified, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.ValidationPassed {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.ValidationPassed, Object: "true", Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.Verdict != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.Verdict, Object: e.Verdict, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.RejectionType != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.RejectionType, Object: e.RejectionType, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.Feedback != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.Feedback, Object: e.Feedback, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.ErrorReason != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.ErrorReason, Object: e.ErrorReason, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.WorktreePath != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.WorktreePath, Object: e.WorktreePath, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.WorktreeBranch != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.WorktreeBranch, Object: e.WorktreeBranch, Source: componentName, Timestamp: now, Confidence: 1.0})
	}

	// Relationship predicates — Object is a 6-part entity ID (graph edge).
	if e.PlanEntityID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.RelPlan, Object: e.PlanEntityID, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.TaskEntityID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.RelTask, Object: e.TaskEntityID, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.ProjectEntityID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.RelProject, Object: e.ProjectEntityID, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.LoopEntityID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.RelLoop, Object: e.LoopEntityID, Source: componentName, Timestamp: now, Confidence: 1.0})
	}

	return triples
}
