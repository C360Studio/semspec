package changeproposalhandler

import (
	"fmt"
	"time"

	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/message"
)

// CascadeEntity records the result of a ChangeProposal cascade.
// It implements the Graphable interface (EntityID + Triples).
type CascadeEntity struct {
	// Identity
	ProposalID string
	Slug       string

	// Cascade metrics
	Phase                     string
	AffectedRequirementsCount int
	AffectedScenariosCount    int
	TraceID                   string
	ErrorReason               string

	// Relationship fields — Objects are 6-part entity IDs, creating graph edges.
	// AffectedRequirementEntityIDs lists entity IDs of requirements affected by the cascade.
	// One RelRequirement triple is emitted per non-empty entry.
	AffectedRequirementEntityIDs []string
}

// NewCascadeEntity creates a CascadeEntity from the cascade request fields.
// affectedRequirements and affectedScenarios come from cascade.Result.
func NewCascadeEntity(proposalID, slug, traceID string, affectedRequirements, affectedScenarios int) *CascadeEntity {
	return &CascadeEntity{
		ProposalID:                proposalID,
		Slug:                      slug,
		TraceID:                   traceID,
		AffectedRequirementsCount: affectedRequirements,
		AffectedScenariosCount:    affectedScenarios,
	}
}

// EntityID returns the 6-part canonical graph entity ID.
// Format: {prefix}.exec.cascade.run.<slug>-<proposalID>
func (e *CascadeEntity) EntityID() string {
	return fmt.Sprintf("%s.exec.cascade.run.%s-%s", workflow.EntityPrefix(), e.Slug, e.ProposalID)
}

// WithPhase sets the current lifecycle phase and returns the entity for chaining.
func (e *CascadeEntity) WithPhase(phase string) *CascadeEntity {
	e.Phase = phase
	return e
}

// WithAffectedRequirementEntityIDs sets the list of affected requirement entity IDs.
// A RelRequirement triple is emitted for each non-empty ID.
func (e *CascadeEntity) WithAffectedRequirementEntityIDs(ids []string) *CascadeEntity {
	e.AffectedRequirementEntityIDs = ids
	return e
}

// WithErrorReason sets the error reason for failed cascade executions.
func (e *CascadeEntity) WithErrorReason(reason string) *CascadeEntity {
	e.ErrorReason = reason
	return e
}

// Triples converts the entity to graph triples using vocabulary constants.
// Property triples use scalar Objects; relationship triples use 6-part entity ID Objects.
func (e *CascadeEntity) Triples() []message.Triple {
	id := e.EntityID()
	now := time.Now()

	triples := []message.Triple{
		{Subject: id, Predicate: wf.Type, Object: "cascade", Source: "change-proposal-handler", Timestamp: now, Confidence: 1.0},
		{Subject: id, Predicate: wf.Slug, Object: e.Slug, Source: "change-proposal-handler", Timestamp: now, Confidence: 1.0},
		{Subject: id, Predicate: wf.CascadeAffectedRequirements, Object: e.AffectedRequirementsCount, Source: "change-proposal-handler", Timestamp: now, Confidence: 1.0},
		{Subject: id, Predicate: wf.CascadeAffectedScenarios, Object: e.AffectedScenariosCount, Source: "change-proposal-handler", Timestamp: now, Confidence: 1.0},
	}

	// Optional scalar predicates — only emit when non-empty.
	if e.Phase != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.Phase, Object: e.Phase, Source: "change-proposal-handler", Timestamp: now, Confidence: 1.0})
	}
	if e.TraceID != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.TraceID, Object: e.TraceID, Source: "change-proposal-handler", Timestamp: now, Confidence: 1.0})
	}
	if e.ErrorReason != "" {
		triples = append(triples, message.Triple{Subject: id, Predicate: wf.ErrorReason, Object: e.ErrorReason, Source: "change-proposal-handler", Timestamp: now, Confidence: 1.0})
	}

	// Relationship predicates — one triple per affected requirement entity ID.
	// Only emit triples for non-empty IDs; callers may populate these incrementally.
	for _, reqID := range e.AffectedRequirementEntityIDs {
		if reqID != "" {
			triples = append(triples, message.Triple{
				Subject:    id,
				Predicate:  wf.RelRequirement,
				Object:     reqID,
				Source:     "change-proposal-handler",
				Timestamp:  now,
				Confidence: 1.0,
			})
		}
	}

	return triples
}
