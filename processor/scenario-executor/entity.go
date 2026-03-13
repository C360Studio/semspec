package scenarioexecutor

import (
	"fmt"
	"time"

	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semstreams/message"
)

// ScenarioExecutionEntity converts a scenarioExecution to graph triples.
// It implements the Graphable interface (EntityID + Triples).
type ScenarioExecutionEntity struct {
	// Identity
	Slug       string
	ScenarioID string

	// Execution tracking
	Phase         string
	TraceID       string
	NodeCount     int
	FailureReason string
	ErrorReason   string

	// Relationship fields — Objects are 6-part entity IDs, creating graph edges.
	ScenarioEntityID string
	ProjectEntityID  string
	LoopEntityID     string
}

// NewScenarioExecutionEntity creates a ScenarioExecutionEntity from a scenarioExecution.
// The caller must hold exec.mu before calling this function.
func NewScenarioExecutionEntity(exec *scenarioExecution) *ScenarioExecutionEntity {
	e := &ScenarioExecutionEntity{
		Slug:       exec.Slug,
		ScenarioID: exec.ScenarioID,
		TraceID:    exec.TraceID,
	}

	if exec.DAG != nil {
		e.NodeCount = len(exec.DAG.Nodes)
	}

	return e
}

// EntityID returns the 6-part canonical graph entity ID.
// Format: local.semspec.workflow.scenario-execution.execution.<slug>-<scenarioID>
// This must match the format used in handleTrigger.
func (e *ScenarioExecutionEntity) EntityID() string {
	return fmt.Sprintf("local.semspec.workflow.scenario-execution.execution.%s-%s", e.Slug, e.ScenarioID)
}

// WithPhase sets the current lifecycle phase and returns the entity for chaining.
func (e *ScenarioExecutionEntity) WithPhase(phase string) *ScenarioExecutionEntity {
	e.Phase = phase
	return e
}

// WithNodeCount sets the DAG node count for this scenario execution.
func (e *ScenarioExecutionEntity) WithNodeCount(count int) *ScenarioExecutionEntity {
	e.NodeCount = count
	return e
}

// WithScenarioEntityID sets the relationship to the associated scenario entity.
func (e *ScenarioExecutionEntity) WithScenarioEntityID(id string) *ScenarioExecutionEntity {
	e.ScenarioEntityID = id
	return e
}

// WithProjectEntityID sets the relationship to the associated project entity.
func (e *ScenarioExecutionEntity) WithProjectEntityID(id string) *ScenarioExecutionEntity {
	e.ProjectEntityID = id
	return e
}

// WithLoopEntityID sets the relationship to the associated agentic loop entity.
func (e *ScenarioExecutionEntity) WithLoopEntityID(id string) *ScenarioExecutionEntity {
	e.LoopEntityID = id
	return e
}

// WithFailureReason sets the failure reason for failed scenario executions.
func (e *ScenarioExecutionEntity) WithFailureReason(reason string) *ScenarioExecutionEntity {
	e.FailureReason = reason
	return e
}

// WithErrorReason sets the error reason for error-state executions.
func (e *ScenarioExecutionEntity) WithErrorReason(reason string) *ScenarioExecutionEntity {
	e.ErrorReason = reason
	return e
}

// Triples converts the entity to graph triples using vocabulary constants.
// Property triples use scalar Objects; relationship triples use 6-part entity ID Objects.
func (e *ScenarioExecutionEntity) Triples() []message.Triple {
	id := e.EntityID()
	now := time.Now()

	triples := []message.Triple{
		{Subject: id, Predicate: wf.Type, Object: "scenario-execution", Source: componentName, Timestamp: now, Confidence: 1.0},
		{Subject: id, Predicate: wf.Slug, Object: e.Slug, Source: componentName, Timestamp: now, Confidence: 1.0},
	}

	// Optional scalar predicates — only emit when non-empty or non-zero.
	if e.Phase != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.Phase, Object: e.Phase, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.TraceID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.TraceID, Object: e.TraceID, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.NodeCount > 0 {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.NodeCount, Object: e.NodeCount, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.FailureReason != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.FailureReason, Object: e.FailureReason, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.ErrorReason != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.ErrorReason, Object: e.ErrorReason, Source: componentName, Timestamp: now, Confidence: 1.0})
	}

	// Relationship predicates — Object is a 6-part entity ID (graph edge).
	if e.ScenarioEntityID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.RelScenario, Object: e.ScenarioEntityID, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.ProjectEntityID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.RelProject, Object: e.ProjectEntityID, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.LoopEntityID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.RelLoop, Object: e.LoopEntityID, Source: componentName, Timestamp: now, Confidence: 1.0})
	}

	return triples
}
