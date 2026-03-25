package planmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

// coordinatorName is the source name used in graph triples for coordination entities.
// This is intentionally "plan-coordinator" so existing triples remain consistent.
const coordinatorName = "plan-coordinator"

func init() {
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "coordination-execution",
		Version:     "v1",
		Description: "Coordination execution entity payload for graph ingestion",
		Factory:     func() any { return &CoordinationPayload{} },
	}); err != nil {
		panic("failed to register CoordinationPayload: " + err.Error())
	}
}

// CoordinationPayloadType is the message type for coordination entity payloads.
var CoordinationPayloadType = message.Type{Domain: "workflow", Category: "coordination-execution", Version: "v1"}

// CoordinationPayload implements message.Payload and wraps entity triples for graph ingestion.
type CoordinationPayload struct {
	ID         string           `json:"id"`
	TripleData []message.Triple `json:"triples"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// EntityID returns the entity identifier.
func (p *CoordinationPayload) EntityID() string { return p.ID }

// Triples returns the graph triples for this entity.
func (p *CoordinationPayload) Triples() []message.Triple { return p.TripleData }

// Schema returns the message type.
func (p *CoordinationPayload) Schema() message.Type { return CoordinationPayloadType }

// Validate ensures the payload has required fields.
func (p *CoordinationPayload) Validate() error {
	if p.ID == "" {
		return errors.New("entity ID is required")
	}
	if len(p.TripleData) == 0 {
		return errors.New("at least one triple is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *CoordinationPayload) MarshalJSON() ([]byte, error) {
	type Alias CoordinationPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *CoordinationPayload) UnmarshalJSON(data []byte) error {
	type Alias CoordinationPayload
	return json.Unmarshal(data, (*Alias)(p))
}

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
// Format: {prefix}.exec.plan.run.<slug>
// This must match the format used in handleTrigger and the rule entity patterns.
func (e *CoordinationEntity) EntityID() string {
	return fmt.Sprintf("%s.exec.plan.run.%s", workflow.EntityPrefix(), e.Slug)
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
		{Subject: id, Predicate: wf.Type, Object: "coordination", Source: coordinatorName, Timestamp: now, Confidence: 1.0},
		{Subject: id, Predicate: wf.Slug, Object: e.Slug, Source: coordinatorName, Timestamp: now, Confidence: 1.0},
	}

	// Optional scalar predicates — only emit when non-empty or non-zero.
	if e.Phase != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.Phase, Object: e.Phase, Source: coordinatorName, Timestamp: now, Confidence: 1.0})
	}
	if e.TraceID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.TraceID, Object: e.TraceID, Source: coordinatorName, Timestamp: now, Confidence: 1.0})
	}
	if e.PlannerCount > 0 {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.MaxIterations, Object: e.PlannerCount, Source: coordinatorName, Timestamp: now, Confidence: 1.0})
	}
	if e.ErrorReason != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.ErrorReason, Object: e.ErrorReason, Source: coordinatorName, Timestamp: now, Confidence: 1.0})
	}

	// Relationship predicates — Object is a 6-part entity ID (graph edge).
	if e.PlanEntityID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.RelPlan, Object: e.PlanEntityID, Source: coordinatorName, Timestamp: now, Confidence: 1.0})
	}
	if e.ProjectEntityID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.RelProject, Object: e.ProjectEntityID, Source: coordinatorName, Timestamp: now, Confidence: 1.0})
	}
	if e.LoopEntityID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.RelLoop, Object: e.LoopEntityID, Source: coordinatorName, Timestamp: now, Confidence: 1.0})
	}

	return triples
}

// publishEntity publishes the entity's triples to the graph ingest subject.
// Failures are logged as warnings but do not propagate — graph ingest is best-effort
// for workflow state observability.
func (co *coordinator) publishEntity(ctx context.Context, entity interface {
	EntityID() string
	Triples() []message.Triple
}) {
	if co.natsClient == nil {
		return
	}

	payload := &CoordinationPayload{
		ID:         entity.EntityID(),
		TripleData: entity.Triples(),
		UpdatedAt:  time.Now(),
	}

	msg := message.NewBaseMessage(CoordinationPayloadType, payload, coordinatorName)
	data, err := json.Marshal(msg)
	if err != nil {
		co.logger.Warn("Failed to marshal entity for graph ingest",
			"entity_id", entity.EntityID(), "error", err)
		return
	}

	js, err := co.natsClient.JetStream()
	if err != nil {
		co.logger.Warn("Failed to get JetStream for entity publish",
			"entity_id", entity.EntityID(), "error", err)
		return
	}

	if _, err := js.Publish(ctx, graphIngestSubject, data); err != nil {
		co.logger.Warn("Failed to publish entity to graph",
			"entity_id", entity.EntityID(), "error", err)
	}
}
