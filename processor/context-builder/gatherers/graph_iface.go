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
}

// Verify both types implement GraphQuerier at compile time.
var (
	_ GraphQuerier = (*GraphGatherer)(nil)
	_ GraphQuerier = (*FederatedGraphGatherer)(nil)
)
