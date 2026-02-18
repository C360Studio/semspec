// Package gatherers provides context gathering implementations.
package gatherers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	sourceVocab "github.com/c360studio/semspec/vocabulary/source"
)

const (
	// maxErrorBodySize limits the size of error response bodies to prevent memory issues.
	maxErrorBodySize = 4096
)

// GraphGatherer gathers context from the knowledge graph via GraphQL.
type GraphGatherer struct {
	gatewayURL string
	httpClient *http.Client
}

// NewGraphGatherer creates a new graph gatherer.
func NewGraphGatherer(gatewayURL string) *GraphGatherer {
	return &GraphGatherer{
		gatewayURL: gatewayURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// GraphQLResponse represents a GraphQL response.
type GraphQLResponse struct {
	Data   map[string]any `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// Entity represents a graph entity.
type Entity struct {
	ID      string   `json:"id"`
	Triples []Triple `json:"triples,omitempty"`
}

// Triple is a predicate-object pair.
type Triple struct {
	Predicate string `json:"predicate"`
	Object    any    `json:"object"`
}

// ExecuteQuery executes a raw GraphQL query with optional variables.
func (g *GraphGatherer) ExecuteQuery(ctx context.Context, query string, variables map[string]any) (map[string]any, error) {
	reqBody := map[string]any{"query": query}
	if variables != nil {
		reqBody["variables"] = variables
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", g.gatewayURL+"/graphql", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Limit error body size to prevent memory issues
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
		return nil, fmt.Errorf("graph gateway returned %d: %s", resp.StatusCode, string(body))
	}

	var result GraphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", result.Errors[0].Message)
	}

	return result.Data, nil
}

// QueryEntitiesByPredicate finds entities matching a predicate prefix.
// Uses parameterized queries to prevent GraphQL injection.
func (g *GraphGatherer) QueryEntitiesByPredicate(ctx context.Context, predicatePrefix string) ([]Entity, error) {
	// Sanitize prefix to prevent injection (additional safety layer)
	predicatePrefix = sanitizeGraphQLString(predicatePrefix)

	query := `query($prefix: String!) {
		entities(filter: { predicatePrefix: $prefix }) {
			id
			triples { predicate object }
		}
	}`

	variables := map[string]any{"prefix": predicatePrefix}

	data, err := g.ExecuteQuery(ctx, query, variables)
	if err != nil {
		return nil, err
	}

	return g.parseEntities(data, "entities")
}

// GetEntity retrieves a specific entity by ID.
// Uses parameterized queries to prevent GraphQL injection.
func (g *GraphGatherer) GetEntity(ctx context.Context, entityID string) (*Entity, error) {
	// Sanitize ID to prevent injection
	entityID = sanitizeGraphQLString(entityID)

	query := `query($id: String!) {
		entity(id: $id) {
			id
			triples { predicate object }
		}
	}`

	variables := map[string]any{"id": entityID}

	data, err := g.ExecuteQuery(ctx, query, variables)
	if err != nil {
		return nil, err
	}

	entityRaw, ok := data["entity"]
	if !ok || entityRaw == nil {
		return nil, fmt.Errorf("entity not found: %s", entityID)
	}

	entityMap, ok := entityRaw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid entity format")
	}

	return g.parseEntity(entityMap), nil
}

// HydrateEntity returns a formatted string representation of an entity.
// The depth parameter controls traversal depth for related entities (currently unused,
// reserved for future implementation of recursive entity hydration).
func (g *GraphGatherer) HydrateEntity(ctx context.Context, entityID string, _ int) (string, error) {
	entity, err := g.GetEntity(ctx, entityID)
	if err != nil {
		return "", err
	}

	var sb bytes.Buffer
	sb.WriteString(fmt.Sprintf("Entity: %s\n", entity.ID))
	for _, t := range entity.Triples {
		sb.WriteString(fmt.Sprintf("  %s: %v\n", t.Predicate, t.Object))
	}

	return sb.String(), nil
}

// GetCodebaseSummary returns a high-level summary of the codebase.
func (g *GraphGatherer) GetCodebaseSummary(ctx context.Context) (string, error) {
	categories := []struct {
		name   string
		prefix string
	}{
		{"Functions", "code.function"},
		{"Types", "code.type"},
		{"Interfaces", "code.interface"},
		{"Packages", "code.package"},
	}

	var sb bytes.Buffer
	sb.WriteString("# Codebase Summary\n\n")

	for _, cat := range categories {
		entities, err := g.QueryEntitiesByPredicate(ctx, cat.prefix)
		if err != nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("## %s: %d\n", cat.name, len(entities)))

		// Include up to 5 samples
		for i, e := range entities {
			if i >= 5 {
				sb.WriteString(fmt.Sprintf("  ... and %d more\n", len(entities)-5))
				break
			}
			sb.WriteString(fmt.Sprintf("  - %s\n", e.ID))
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// TraverseRelationships traverses relationships from a starting entity.
// Uses parameterized queries to prevent GraphQL injection.
func (g *GraphGatherer) TraverseRelationships(ctx context.Context, startEntity, predicate string, direction string, depth int) ([]Entity, error) {
	if depth > 3 {
		depth = 3
	}
	if depth < 1 {
		depth = 1
	}

	// Sanitize inputs
	startEntity = sanitizeGraphQLString(startEntity)
	predicate = sanitizeGraphQLString(predicate)

	directionArg := "OUTBOUND"
	if direction == "inbound" {
		directionArg = "INBOUND"
	}

	// Build query with optional predicate filter
	var query string
	variables := map[string]any{
		"start":     startEntity,
		"depth":     depth,
		"direction": directionArg,
	}

	if predicate != "" {
		query = `query($start: String!, $depth: Int!, $direction: TraversalDirection!, $predicate: String!) {
			traverse(start: $start, depth: $depth, direction: $direction, predicate: $predicate) {
				nodes {
					id
					triples { predicate object }
				}
			}
		}`
		variables["predicate"] = predicate
	} else {
		query = `query($start: String!, $depth: Int!, $direction: TraversalDirection!) {
			traverse(start: $start, depth: $depth, direction: $direction) {
				nodes {
					id
					triples { predicate object }
				}
			}
		}`
	}

	data, err := g.ExecuteQuery(ctx, query, variables)
	if err != nil {
		return nil, err
	}

	traverseResult, ok := data["traverse"].(map[string]any)
	if !ok {
		return nil, nil
	}

	nodesRaw, ok := traverseResult["nodes"].([]any)
	if !ok {
		return nil, nil
	}

	entities := make([]Entity, 0, len(nodesRaw))
	for _, n := range nodesRaw {
		nodeMap, ok := n.(map[string]any)
		if !ok {
			continue
		}
		entities = append(entities, *g.parseEntity(nodeMap))
	}

	return entities, nil
}

// parseEntities parses entity data from a GraphQL response.
func (g *GraphGatherer) parseEntities(data map[string]any, key string) ([]Entity, error) {
	entitiesRaw, ok := data[key].([]any)
	if !ok {
		return nil, nil
	}

	entities := make([]Entity, 0, len(entitiesRaw))
	for _, e := range entitiesRaw {
		entityMap, ok := e.(map[string]any)
		if !ok {
			continue
		}
		entities = append(entities, *g.parseEntity(entityMap))
	}

	return entities, nil
}

// parseEntity parses a single entity from a map.
func (g *GraphGatherer) parseEntity(entityMap map[string]any) *Entity {
	entity := &Entity{}

	if id, ok := entityMap["id"].(string); ok {
		entity.ID = id
	}

	if triples, ok := entityMap["triples"].([]any); ok {
		for _, t := range triples {
			tripleMap, ok := t.(map[string]any)
			if !ok {
				continue
			}
			triple := Triple{}
			if pred, ok := tripleMap["predicate"].(string); ok {
				triple.Predicate = pred
			}
			triple.Object = tripleMap["object"]
			entity.Triples = append(entity.Triples, triple)
		}
	}

	return entity
}

// QueryProjectSources finds all source entities belonging to a project.
// Returns entities that have source.project predicate matching the given project ID.
// Uses parameterized queries to prevent GraphQL injection.
func (g *GraphGatherer) QueryProjectSources(ctx context.Context, projectID string) ([]Entity, error) {
	// Sanitize to prevent injection
	projectID = sanitizeGraphQLString(projectID)

	query := fmt.Sprintf(`query($projectID: String!) {
		entities(filter: { predicate: %q, value: $projectID }) {
			id
			triples { predicate object }
		}
	}`, sourceVocab.SourceProject)

	variables := map[string]any{"projectID": projectID}

	data, err := g.ExecuteQuery(ctx, query, variables)
	if err != nil {
		return nil, err
	}

	return g.parseEntities(data, "entities")
}

// sanitizeGraphQLString removes potentially dangerous characters from GraphQL string inputs.
// This provides defense-in-depth alongside parameterized queries.
func sanitizeGraphQLString(s string) string {
	// Remove any control characters and limit problematic sequences
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return s
}
