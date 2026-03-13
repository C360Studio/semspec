package revieworchestrator

import (
	"fmt"
	"time"

	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semstreams/message"
)

// ReviewExecutionEntity converts a reviewExecution to graph triples.
// It implements the Graphable interface (EntityID + Triples).
type ReviewExecutionEntity struct {
	// Identity
	ReviewType string
	Slug       string

	// Execution tracking
	Phase         string
	Iteration     int
	MaxIterations int
	Prompt        string
	TraceID       string
	ErrorReason   string

	// Content fields
	PlanContent string
	Verdict     string
	Summary     string
	Findings    string

	// Relationship fields — Objects are 6-part entity IDs, creating graph edges.
	PlanEntityID    string
	ProjectEntityID string
	LoopEntityID    string
}

// NewReviewExecutionEntity creates a ReviewExecutionEntity from a reviewExecution.
// The caller must hold exec.mu before calling this function.
func NewReviewExecutionEntity(exec *reviewExecution) *ReviewExecutionEntity {
	e := &ReviewExecutionEntity{
		ReviewType:    exec.ReviewType,
		Slug:          exec.Slug,
		Iteration:     exec.Iteration,
		MaxIterations: exec.MaxIterations,
		Prompt:        exec.Prompt,
		TraceID:       exec.TraceID,
		Verdict:       exec.Verdict,
		Summary:       exec.Summary,
	}

	if len(exec.PlanContent) > 0 {
		e.PlanContent = string(exec.PlanContent)
	}
	if len(exec.Findings) > 0 {
		e.Findings = string(exec.Findings)
	}

	return e
}

// EntityID returns the 6-part canonical graph entity ID.
// Format: local.semspec.workflow.<reviewType>.execution.<slug>
// This must match the format used in handleTrigger.
func (e *ReviewExecutionEntity) EntityID() string {
	return fmt.Sprintf("local.semspec.workflow.%s.execution.%s", e.ReviewType, e.Slug)
}

// WithPhase sets the current lifecycle phase and returns the entity for chaining.
func (e *ReviewExecutionEntity) WithPhase(phase string) *ReviewExecutionEntity {
	e.Phase = phase
	return e
}

// WithPlanEntityID sets the relationship to the associated plan entity.
func (e *ReviewExecutionEntity) WithPlanEntityID(id string) *ReviewExecutionEntity {
	e.PlanEntityID = id
	return e
}

// WithProjectEntityID sets the relationship to the associated project entity.
func (e *ReviewExecutionEntity) WithProjectEntityID(id string) *ReviewExecutionEntity {
	e.ProjectEntityID = id
	return e
}

// WithLoopEntityID sets the relationship to the associated agentic loop entity.
func (e *ReviewExecutionEntity) WithLoopEntityID(id string) *ReviewExecutionEntity {
	e.LoopEntityID = id
	return e
}

// WithErrorReason sets the error reason for failed executions.
func (e *ReviewExecutionEntity) WithErrorReason(reason string) *ReviewExecutionEntity {
	e.ErrorReason = reason
	return e
}

// Triples converts the entity to graph triples using vocabulary constants.
// Property triples use scalar Objects; relationship triples use 6-part entity ID Objects.
func (e *ReviewExecutionEntity) Triples() []message.Triple {
	id := e.EntityID()
	now := time.Now()

	triples := []message.Triple{
		{Subject: id, Predicate: wf.Type, Object: e.ReviewType, Source: componentName, Timestamp: now, Confidence: 1.0},
		{Subject: id, Predicate: wf.Slug, Object: e.Slug, Source: componentName, Timestamp: now, Confidence: 1.0},
		{Subject: id, Predicate: wf.Iteration, Object: e.Iteration, Source: componentName, Timestamp: now, Confidence: 1.0},
		{Subject: id, Predicate: wf.MaxIterations, Object: e.MaxIterations, Source: componentName, Timestamp: now, Confidence: 1.0},
	}

	// Optional scalar predicates — only emit when non-empty.
	if e.Phase != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.Phase, Object: e.Phase, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.Prompt != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.Prompt, Object: e.Prompt, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.TraceID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.TraceID, Object: e.TraceID, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.PlanContent != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.PlanContent, Object: e.PlanContent, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.Verdict != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.Verdict, Object: e.Verdict, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.Summary != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.Summary, Object: e.Summary, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.Findings != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.Findings, Object: e.Findings, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.ErrorReason != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.ErrorReason, Object: e.ErrorReason, Source: componentName, Timestamp: now, Confidence: 1.0})
	}

	// Relationship predicates — Object is a 6-part entity ID (graph edge).
	if e.PlanEntityID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.RelPlan, Object: e.PlanEntityID, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.ProjectEntityID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.RelProject, Object: e.ProjectEntityID, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.LoopEntityID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.RelLoop, Object: e.LoopEntityID, Source: componentName, Timestamp: now, Confidence: 1.0})
	}

	return triples
}
