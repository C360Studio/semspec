package plancoordinator

import (
	"fmt"
	"time"

	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semstreams/message"
)

// CoordinationEntity converts a coordinationExecution to graph triples.
// It implements the Graphable interface (EntityID + Triples).
type CoordinationEntity struct {
	// Identity
	Slug string

	// Execution tracking
	Phase        string
	TraceID      string
	PlannerCount int
	ErrorReason  string

	// Relationship fields — Objects are 6-part entity IDs, creating graph edges.
	PlanEntityID    string
	ProjectEntityID string
	LoopEntityID    string
}

// NewCoordinationEntity creates a CoordinationEntity from a coordinationExecution.
// The caller must hold exec.mu before calling this function.
func NewCoordinationEntity(exec *coordinationExecution) *CoordinationEntity {
	return &CoordinationEntity{
		Slug:         exec.Slug,
		TraceID:      exec.TraceID,
		PlannerCount: exec.ExpectedPlanners,
	}
}

// EntityID returns the 6-part canonical graph entity ID.
// Format: local.semspec.workflow.coordination.execution.<slug>
// This must match the format used in handleTrigger.
func (e *CoordinationEntity) EntityID() string {
	return fmt.Sprintf("local.semspec.workflow.coordination.execution.%s", e.Slug)
}

// WithPhase sets the current lifecycle phase and returns the entity for chaining.
func (e *CoordinationEntity) WithPhase(phase string) *CoordinationEntity {
	e.Phase = phase
	return e
}

// WithPlannerCount sets the number of planners dispatched.
func (e *CoordinationEntity) WithPlannerCount(count int) *CoordinationEntity {
	e.PlannerCount = count
	return e
}

// WithPlanEntityID sets the relationship to the associated plan entity.
func (e *CoordinationEntity) WithPlanEntityID(id string) *CoordinationEntity {
	e.PlanEntityID = id
	return e
}

// WithProjectEntityID sets the relationship to the associated project entity.
func (e *CoordinationEntity) WithProjectEntityID(id string) *CoordinationEntity {
	e.ProjectEntityID = id
	return e
}

// WithLoopEntityID sets the relationship to the associated agentic loop entity.
func (e *CoordinationEntity) WithLoopEntityID(id string) *CoordinationEntity {
	e.LoopEntityID = id
	return e
}

// WithErrorReason sets the error reason for failed coordination executions.
func (e *CoordinationEntity) WithErrorReason(reason string) *CoordinationEntity {
	e.ErrorReason = reason
	return e
}

// Triples converts the entity to graph triples using vocabulary constants.
// Property triples use scalar Objects; relationship triples use 6-part entity ID Objects.
func (e *CoordinationEntity) Triples() []message.Triple {
	id := e.EntityID()
	now := time.Now()

	triples := []message.Triple{
		{Subject: id, Predicate: wf.Type, Object: "coordination", Source: componentName, Timestamp: now, Confidence: 1.0},
		{Subject: id, Predicate: wf.Slug, Object: e.Slug, Source: componentName, Timestamp: now, Confidence: 1.0},
	}

	// Optional scalar predicates — only emit when non-empty or non-zero.
	if e.Phase != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.Phase, Object: e.Phase, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.TraceID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.TraceID, Object: e.TraceID, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.PlannerCount > 0 {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.MaxIterations, Object: e.PlannerCount, Source: componentName, Timestamp: now, Confidence: 1.0})
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
