package requirementexecutor

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/tools/decompose"
	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/message"
)

// RequirementExecutionEntity converts a requirementExecution to graph triples.
// It implements the Graphable interface (EntityID + Triples).
type RequirementExecutionEntity struct {
	// Identity
	Slug          string
	RequirementID string

	// Execution tracking
	Phase         string
	TraceID       string
	NodeCount     int
	FailureReason string
	ErrorReason   string

	// Relationship fields — Objects are 6-part entity IDs, creating graph edges.
	RequirementEntityID string
	ProjectEntityID     string
	LoopEntityID        string
}

// NewRequirementExecutionEntity creates a RequirementExecutionEntity from a requirementExecution.
// The caller must hold exec.mu before calling this function.
func NewRequirementExecutionEntity(exec *requirementExecution) *RequirementExecutionEntity {
	e := &RequirementExecutionEntity{
		Slug:          exec.Slug,
		RequirementID: exec.RequirementID,
		TraceID:       exec.TraceID,
	}

	if exec.DAG != nil {
		e.NodeCount = len(exec.DAG.Nodes)
	}

	return e
}

// EntityID returns the 6-part canonical graph entity ID.
// Format: local.semspec.workflow.requirement-execution.execution.<slug>-<requirementID>
// Dots in slug or requirementID are replaced with hyphens so the result has
// exactly 6 dot-separated parts.  This must match the format used in handleTrigger.
func (e *RequirementExecutionEntity) EntityID() string {
	instance := strings.ReplaceAll(e.Slug+"-"+e.RequirementID, ".", "-")
	return fmt.Sprintf("local.semspec.workflow.requirement-execution.execution.%s", instance)
}

// WithPhase sets the current lifecycle phase and returns the entity for chaining.
func (e *RequirementExecutionEntity) WithPhase(phase string) *RequirementExecutionEntity {
	e.Phase = phase
	return e
}

// WithNodeCount sets the DAG node count for this requirement execution.
func (e *RequirementExecutionEntity) WithNodeCount(count int) *RequirementExecutionEntity {
	e.NodeCount = count
	return e
}

// WithRequirementEntityID sets the relationship to the associated requirement entity.
func (e *RequirementExecutionEntity) WithRequirementEntityID(id string) *RequirementExecutionEntity {
	e.RequirementEntityID = id
	return e
}

// WithProjectEntityID sets the relationship to the associated project entity.
func (e *RequirementExecutionEntity) WithProjectEntityID(id string) *RequirementExecutionEntity {
	e.ProjectEntityID = id
	return e
}

// WithLoopEntityID sets the relationship to the associated agentic loop entity.
func (e *RequirementExecutionEntity) WithLoopEntityID(id string) *RequirementExecutionEntity {
	e.LoopEntityID = id
	return e
}

// WithFailureReason sets the failure reason for failed requirement executions.
func (e *RequirementExecutionEntity) WithFailureReason(reason string) *RequirementExecutionEntity {
	e.FailureReason = reason
	return e
}

// WithErrorReason sets the error reason for error-state executions.
func (e *RequirementExecutionEntity) WithErrorReason(reason string) *RequirementExecutionEntity {
	e.ErrorReason = reason
	return e
}

// Triples converts the entity to graph triples using vocabulary constants.
// Property triples use scalar Objects; relationship triples use 6-part entity ID Objects.
func (e *RequirementExecutionEntity) Triples() []message.Triple {
	id := e.EntityID()
	now := time.Now()

	triples := []message.Triple{
		{Subject: id, Predicate: wf.Type, Object: "requirement-execution", Source: componentName, Timestamp: now, Confidence: 1.0},
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
	if e.RequirementEntityID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.RelRequirement, Object: e.RequirementEntityID, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.ProjectEntityID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.RelProject, Object: e.ProjectEntityID, Source: componentName, Timestamp: now, Confidence: 1.0})
	}
	if e.LoopEntityID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.RelLoop, Object: e.LoopEntityID, Source: componentName, Timestamp: now, Confidence: 1.0})
	}

	return triples
}

// ---------------------------------------------------------------------------
// DAGNodeEntity
// ---------------------------------------------------------------------------

// DAGNodeEntity converts a decompose.TaskNode to graph triples for the
// graph.ingest.entity subject.  It implements the same interface consumed by
// publishEntity so no separate publish path is required.
type DAGNodeEntity struct {
	// executionID is the "{slug}-{requirementID}" suffix used in entity IDs.
	executionID string
	// node is the underlying DAG node from the decomposer.
	node *decompose.TaskNode
	// execEntityID is the parent requirement-execution entity ID (graph edge target).
	execEntityID string
	// status overrides the default "pending" status when set.
	status string
}

// newDAGNodeEntity creates a DAGNodeEntity for initial publishing (status="pending").
// execEntityID is the requirement-execution entity ID that owns this DAG.
func newDAGNodeEntity(executionID string, node *decompose.TaskNode, execEntityID string) *DAGNodeEntity {
	return &DAGNodeEntity{
		executionID:  executionID,
		node:         node,
		execEntityID: execEntityID,
		status:       "pending",
	}
}

// withStatus returns a shallow copy with the status field overridden.
func (e *DAGNodeEntity) withStatus(status string) *DAGNodeEntity {
	clone := *e
	clone.status = status
	return &clone
}

// EntityID returns the canonical graph entity ID for this DAG node.
// Format: local.semspec.workflow.dag-node.node.{executionID}-{nodeID}
func (e *DAGNodeEntity) EntityID() string {
	return workflow.DAGNodeEntityID(e.executionID, e.node.ID)
}

// Triples returns the graph triples for this DAG node.
func (e *DAGNodeEntity) Triples() []message.Triple {
	id := e.EntityID()
	now := time.Now()

	triples := []message.Triple{
		{Subject: id, Predicate: wf.DAGNodeID, Object: e.node.ID, Source: componentName, Timestamp: now, Confidence: 1.0},
		{Subject: id, Predicate: wf.DAGNodePrompt, Object: e.node.Prompt, Source: componentName, Timestamp: now, Confidence: 1.0},
		{Subject: id, Predicate: wf.DAGNodeRole, Object: e.node.Role, Source: componentName, Timestamp: now, Confidence: 1.0},
		{Subject: id, Predicate: wf.DAGNodeStatus, Object: e.status, Source: componentName, Timestamp: now, Confidence: 1.0},
		{Subject: id, Predicate: wf.RelRequirement, Object: e.execEntityID, Source: componentName, Timestamp: now, Confidence: 1.0},
	}

	// File scope as a JSON array string.
	if len(e.node.FileScope) > 0 {
		scopeJSON, err := json.Marshal(e.node.FileScope)
		if err == nil {
			triples = append(triples, message.Triple{
				Subject: id, Predicate: wf.DAGNodeFileScope, Object: string(scopeJSON),
				Source: componentName, Timestamp: now, Confidence: 1.0,
			})
		}
	}

	// Dependency edges to sibling DAG node entities.
	for _, depID := range e.node.DependsOn {
		depEntityID := workflow.DAGNodeEntityID(e.executionID, depID)
		triples = append(triples, message.Triple{
			Subject: id, Predicate: wf.DAGNodeDependsOn, Object: depEntityID,
			Source: componentName, Timestamp: now, Confidence: 1.0,
		})
	}

	return triples
}
