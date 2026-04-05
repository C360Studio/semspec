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

// GraphToolsScenario tests all three graph tool code paths end-to-end with
// a real semsource. Tier 1: no LLM required.
//
// Exercises the same paths agents use:
//   - graph_summary → FormatSummaryForPrompt → watcher.IsAvailable
//   - graph_query   → executeGraphQL (predicates, entitiesByPrefix)
//   - graph_search  → globalSearch GraphQL query
type GraphToolsScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
}

// NewGraphToolsScenario creates a new graph tools scenario.
func NewGraphToolsScenario(cfg *config.Config) *GraphToolsScenario {
	return &GraphToolsScenario{
		name:        "graph-tools",
		description: "Tests all graph tool code paths: graph_summary, graph_query, graph_search with real semsource",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *GraphToolsScenario) Name() string { return s.name }

// Description returns the scenario description.
func (s *GraphToolsScenario) Description() string { return s.description }

// Teardown is a no-op for this read-only scenario.
func (s *GraphToolsScenario) Teardown(_ context.Context) error { return nil }

// Setup waits for the semspec HTTP service to be healthy.
func (s *GraphToolsScenario) Setup(ctx context.Context) error {
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}
	return nil
}

// Execute runs the graph tools verification stages.
func (s *GraphToolsScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"semsource-ready", s.stageSemsourceReady},
		{"graph-summary-content", s.stageGraphSummaryContent},
		{"graph-query-predicates", s.stageGraphQueryPredicates},
		{"graph-query-entity", s.stageGraphQueryEntity},
		{"graph-search", s.stageGraphSearch},
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

// stageSemsourceReady polls /project-manager/graph-summary until 200.
// This exercises the full chain: GlobalSources → FormatSummaryForPrompt →
// IsReady → watcher.IsAvailable → fetchSummaryWithCache.
func (s *GraphToolsScenario) stageSemsourceReady(ctx context.Context, result *Result) error {
	summaryURL := strings.TrimRight(s.config.HTTPBaseURL, "/") + "/project-manager/graph-summary"

	deadline := time.After(90 * time.Second)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastErr error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, summaryURL, nil)
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}

		resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if err != nil {
			lastErr = err
		} else {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && len(body) > 0 {
				result.SetDetail("graph_summary_available", true)
				return nil
			}
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body[:min(200, len(body))]))
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled: last error: %v", lastErr)
		case <-deadline:
			return fmt.Errorf("semsource not ready after 90s — watcher never promoted source. Last error: %v", lastErr)
		case <-ticker.C:
		}
	}
}

// stageGraphSummaryContent verifies the summary has expected structure.
func (s *GraphToolsScenario) stageGraphSummaryContent(ctx context.Context, result *Result) error {
	summaryURL := strings.TrimRight(s.config.HTTPBaseURL, "/") + "/project-manager/graph-summary"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, summaryURL, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	text := string(body)
	checks := map[string]string{
		"Knowledge Graph": "header",
		"entities":        "entity count",
	}
	for needle, label := range checks {
		if !strings.Contains(text, needle) {
			return fmt.Errorf("summary missing %s (%q)", label, needle)
		}
	}

	result.SetDetail("graph_summary_length", len(text))
	result.SetDetail("graph_summary_preview", text[:min(300, len(text))])
	return nil
}

// stageGraphQueryPredicates mirrors graph_query tool: queries predicates.
func (s *GraphToolsScenario) stageGraphQueryPredicates(ctx context.Context, result *Result) error {
	resp, err := s.postGraphQL(ctx, `{ predicates { predicates { predicate entityCount } total } }`)
	if err != nil {
		return fmt.Errorf("predicates query failed: %w", err)
	}

	preds, ok := resp.Data["predicates"].(map[string]any)
	if !ok {
		return fmt.Errorf("unexpected predicates response shape")
	}

	total, _ := preds["total"].(float64)
	if total == 0 {
		return fmt.Errorf("no predicates found — graph-gateway has no indexed data")
	}

	result.SetDetail("predicate_count", int(total))
	return nil
}

// stageGraphQueryEntity mirrors graph_query tool: entity lookup by prefix.
func (s *GraphToolsScenario) stageGraphQueryEntity(ctx context.Context, result *Result) error {
	resp, err := s.postGraphQL(ctx, `{ entitiesByPrefix(prefix: "semspec", limit: 5) { id } }`)
	if err != nil {
		return fmt.Errorf("entitiesByPrefix query failed: %w", err)
	}

	entities, ok := resp.Data["entitiesByPrefix"].([]any)
	if !ok || len(entities) == 0 {
		return fmt.Errorf("no entities found with prefix semspec")
	}

	var ids []string
	for _, e := range entities {
		if ent, ok := e.(map[string]any); ok {
			if id, ok := ent["id"].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return fmt.Errorf("entities returned but no IDs extracted")
	}

	result.SetDetail("entity_count", len(ids))
	result.SetDetail("sample_entity", ids[0])
	return nil
}

// stageGraphSearch mirrors graph_search tool: natural language query.
// globalSearch requires an LLM for answer synthesis, so this stage accepts
// timeouts and empty results gracefully. The critical check is that the
// GraphQL endpoint is reachable and doesn't return a server error.
func (s *GraphToolsScenario) stageGraphSearch(ctx context.Context, result *Result) error {
	resp, err := s.postGraphQL(ctx,
		`query { globalSearch(query: "main function", level: 1, maxCommunities: 5) { answer count } }`)
	if err != nil {
		// globalSearch may timeout when no LLM is configured (answer synthesis
		// requires an LLM backend). This is expected in no-LLM E2E runs.
		if strings.Contains(err.Error(), "deadline exceeded") || strings.Contains(err.Error(), "context canceled") {
			result.SetDetail("graph_search_available", false)
			result.SetDetail("graph_search_skip_reason", "timeout (no LLM for answer synthesis)")
			return nil
		}
		return fmt.Errorf("globalSearch query failed: %w", err)
	}

	search, ok := resp.Data["globalSearch"].(map[string]any)
	if !ok {
		// globalSearch returning null is acceptable for small graphs.
		result.SetDetail("graph_search_available", false)
		return nil
	}

	count, _ := search["count"].(float64)
	answer, _ := search["answer"].(string)

	result.SetDetail("graph_search_available", true)
	result.SetDetail("graph_search_count", int(count))
	if answer != "" {
		result.SetDetail("graph_search_answer_preview", answer[:min(200, len(answer))])
	}
	return nil
}

// postGraphQL sends a GraphQL query to graph-gateway and returns the parsed response.
func (s *GraphToolsScenario) postGraphQL(ctx context.Context, query string) (*graphQLResponse, error) {
	graphqlURL := s.config.GraphURL + "/graphql"
	body, _ := json.Marshal(map[string]string{"query": query})

	req, err := http.NewRequestWithContext(ctx, "POST", graphqlURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
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
