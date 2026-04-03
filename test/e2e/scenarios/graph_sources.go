package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// GraphSourcesScenario tests the graph source registry pipeline (ADR-032).
// Verifies: semsource indexes Go fixture → graph-gateway serves GraphQL →
// predicates and entities are queryable.
//
// Tier 1: no LLM required.
type GraphSourcesScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
}

// NewGraphSourcesScenario creates a new graph sources scenario.
func NewGraphSourcesScenario(cfg *config.Config) *GraphSourcesScenario {
	return &GraphSourcesScenario{
		name:        "graph-sources",
		description: "Tests graph source registry: semsource readiness, GraphQL predicates, entity indexing",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *GraphSourcesScenario) Name() string { return s.name }

// Description returns the scenario description.
func (s *GraphSourcesScenario) Description() string { return s.description }

// Teardown is a no-op for this read-only scenario.
func (s *GraphSourcesScenario) Teardown(_ context.Context) error { return nil }

// Setup waits for the semspec HTTP service to be healthy.
func (s *GraphSourcesScenario) Setup(ctx context.Context) error {
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}
	return nil
}

// Execute runs the graph sources verification stages.
func (s *GraphSourcesScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"graph-ping", s.stageGraphPing},
		{"wait-for-entities", s.stageWaitForEntities},
		{"verify-predicates", s.stageVerifyPredicates},
		{"verify-entities-by-prefix", s.stageVerifyEntitiesByPrefix},
	}

	for _, stage := range stages {
		start := time.Now()
		stageCtx, cancel := context.WithTimeout(ctx, s.config.StageTimeout)
		err := stage.fn(stageCtx, result)
		cancel()
		duration := time.Since(start)

		if err != nil {
			result.AddStage(stage.name, false, duration, err.Error())
			result.AddError(fmt.Sprintf("[%s] %s", stage.name, err))
			return result, nil
		}
		result.AddStage(stage.name, true, duration, "")
	}

	result.Success = true
	return result, nil
}

// stageGraphPing verifies the graph-gateway GraphQL endpoint is reachable.
func (s *GraphSourcesScenario) stageGraphPing(ctx context.Context, result *Result) error {
	graphqlURL := s.config.GraphURL + "/graphql"

	resp, err := s.postGraphQL(ctx, graphqlURL, `{ entity(id: "nonexistent") { id } }`)
	if err != nil {
		return fmt.Errorf("graph gateway unreachable: %w", err)
	}

	// Any valid GraphQL response (even with errors/nulls) means the gateway is up.
	if resp.Data != nil || resp.Errors != nil {
		result.SetDetail("graph_reachable", true)
		return nil
	}

	return fmt.Errorf("unexpected empty response from graph gateway")
}

// stageWaitForEntities polls until the graph has indexed entities from the Go fixture.
func (s *GraphSourcesScenario) stageWaitForEntities(ctx context.Context, result *Result) error {
	graphqlURL := s.config.GraphURL + "/graphql"
	query := `{ predicates { total } }`

	deadline := time.After(60 * time.Second)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		resp, err := s.postGraphQL(ctx, graphqlURL, query)
		if err == nil && resp.Data != nil {
			if preds, ok := resp.Data["predicates"].(map[string]any); ok {
				if total, ok := preds["total"].(float64); ok && total > 0 {
					result.SetDetail("predicate_count", int(total))
					return nil
				}
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled waiting for entities")
		case <-deadline:
			return fmt.Errorf("timed out waiting for graph to have entities (semsource may not be indexing)")
		case <-ticker.C:
			// retry
		}
	}
}

// stageVerifyPredicates queries the predicates endpoint and verifies expected families exist.
func (s *GraphSourcesScenario) stageVerifyPredicates(ctx context.Context, result *Result) error {
	graphqlURL := s.config.GraphURL + "/graphql"
	query := `{ predicates { predicates { predicate entityCount } total } }`

	resp, err := s.postGraphQL(ctx, graphqlURL, query)
	if err != nil {
		return fmt.Errorf("predicates query failed: %w", err)
	}

	preds, ok := resp.Data["predicates"].(map[string]any)
	if !ok {
		return fmt.Errorf("unexpected predicates response shape")
	}

	total, _ := preds["total"].(float64)
	if total == 0 {
		return fmt.Errorf("no predicates found")
	}

	// Verify we have source.* predicates (from Go fixture code indexing).
	predList, ok := preds["predicates"].([]any)
	if !ok {
		return fmt.Errorf("predicates list not an array")
	}

	families := make(map[string]int)
	for _, p := range predList {
		pred, ok := p.(map[string]any)
		if !ok {
			continue
		}
		name, _ := pred["predicate"].(string)
		count, _ := pred["entity_count"].(float64)
		if name == "" {
			continue
		}
		// Extract family (first dot segment).
		family := name
		if idx := strings.IndexByte(name, '.'); idx > 0 {
			family = name[:idx]
		}
		families[family] += int(count)
	}

	result.SetDetail("predicate_families", families)
	result.SetDetail("total_predicates", int(total))

	// The Go fixture project should produce source.* predicates at minimum.
	if families["source"] == 0 {
		return fmt.Errorf("expected source.* predicates from Go fixture, got families: %v", families)
	}

	return nil
}

// stageVerifyEntitiesByPrefix queries entities by prefix to verify indexing structure.
func (s *GraphSourcesScenario) stageVerifyEntitiesByPrefix(ctx context.Context, result *Result) error {
	graphqlURL := s.config.GraphURL + "/graphql"

	// Query for entities using the namespace from docker/semsource.json.
	// The prefix "semspec" matches both local graph entities (semspec.{platform}.*)
	// and semsource-indexed entities (semspec.semsource.*). The namespace is
	// configurable per project — this test uses the E2E default.
	query := `{ entitiesByPrefix(prefix: "semspec", limit: 5) { id } }`

	resp, err := s.postGraphQL(ctx, graphqlURL, query)
	if err != nil {
		return fmt.Errorf("entitiesByPrefix query failed: %w", err)
	}

	entities, ok := resp.Data["entitiesByPrefix"].([]any)
	if !ok || len(entities) == 0 {
		return fmt.Errorf("no entities found with prefix semspec (namespace from semsource config)")
	}

	// Verify entity IDs follow the expected 6-part dotted notation.
	var entityIDs []string
	for _, e := range entities {
		ent, ok := e.(map[string]any)
		if !ok {
			continue
		}
		id, _ := ent["id"].(string)
		if id != "" {
			entityIDs = append(entityIDs, id)
		}
	}

	if len(entityIDs) == 0 {
		return fmt.Errorf("entities returned but no IDs extracted")
	}

	// Verify at least one entity ID has the expected dotted format.
	parts := strings.Split(entityIDs[0], ".")
	if len(parts) < 4 {
		return fmt.Errorf("entity ID %q doesn't follow dotted notation (got %d parts)", entityIDs[0], len(parts))
	}

	result.SetDetail("entity_count", len(entityIDs))
	result.SetDetail("sample_entity_id", entityIDs[0])

	return nil
}

// graphQLResponse represents a generic GraphQL response.
type graphQLResponse struct {
	Data   map[string]any `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// postGraphQL sends a GraphQL query and returns the parsed response.
func (s *GraphSourcesScenario) postGraphQL(ctx context.Context, url, query string) (*graphQLResponse, error) {
	body, _ := json.Marshal(map[string]string{"query": query})

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var gqlResp graphQLResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &gqlResp, nil
}
