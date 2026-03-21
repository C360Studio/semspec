package gatherers

import (
	"context"
	"time"
)

// GraphQuerier is the interface for graph query operations used by context-builder
// strategies. Both GraphGatherer (single source) and FederatedGraphGatherer
// (multi-source) implement this interface.
type GraphQuerier interface {
	// QueryEntitiesByPredicate returns entities matching a predicate prefix.
	QueryEntitiesByPredicate(ctx context.Context, predicatePrefix string) ([]Entity, error)

	// QueryEntitiesByIDPrefix returns entities matching an ID prefix.
	QueryEntitiesByIDPrefix(ctx context.Context, idPrefix string) ([]Entity, error)

	// GetEntity returns a single entity by ID.
	GetEntity(ctx context.Context, entityID string) (*Entity, error)

	// HydrateEntity returns a formatted string representation of an entity.
	HydrateEntity(ctx context.Context, entityID string, depth int) (string, error)

	// GetCodebaseSummary returns a summary of the codebase from the graph.
	GetCodebaseSummary(ctx context.Context) (string, error)

	// TraverseRelationships traverses entity relationships.
	TraverseRelationships(ctx context.Context, startEntity, predicate, direction string, depth int) ([]Entity, error)

	// Ping checks if the graph is reachable.
	Ping(ctx context.Context) error

	// WaitForReady waits for the graph to be queryable.
	WaitForReady(ctx context.Context, budget time.Duration) error

	// QueryProjectSources returns source entities for a project.
	QueryProjectSources(ctx context.Context, projectID string) ([]Entity, error)

	// GraphSummary returns summaries from all connected semsource instances.
	GraphSummary(ctx context.Context) ([]SourceSummary, error)
}

// SourceSummary represents a knowledge graph overview from a single semsource instance.
type SourceSummary struct {
	Source         string            `json:"source"`
	Phase          string            `json:"phase"`
	TotalEntities  int64             `json:"total_entities"`
	EntityIDFormat string            `json:"entity_id_format"`
	Domains        []DomainSummary   `json:"domains"`
	Predicates     []PredicateSchema `json:"predicates,omitempty"`
}

// DomainSummary summarises entity counts within a single domain.
type DomainSummary struct {
	Domain      string      `json:"domain"`
	EntityCount int64       `json:"entity_count"`
	Types       []TypeCount `json:"types"`
}

// TypeCount is an entity type with its count within a domain.
type TypeCount struct {
	Type  string `json:"type"`
	Count int64  `json:"count"`
}

// PredicateSchema groups predicate descriptors by source type.
type PredicateSchema struct {
	SourceType string                `json:"source_type"`
	Predicates []PredicateDescriptor `json:"predicates"`
}

// PredicateDescriptor describes a single predicate in the knowledge graph.
type PredicateDescriptor struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	DataType    string `json:"data_type"`
	Role        string `json:"role"`
}

// Verify both types implement GraphQuerier at compile time.
var (
	_ GraphQuerier = (*GraphGatherer)(nil)
	_ GraphQuerier = (*FederatedGraphGatherer)(nil)
)
