package trajectoryapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/vocabulary/semspec"
)

const (
	// maxGraphErrorBodySize limits the size of error response bodies.
	maxGraphErrorBodySize = 4096
)

// LLMCallQuerier queries LLM call entities from the knowledge graph.
type LLMCallQuerier struct {
	gatewayURL string
	httpClient *http.Client
}

// NewLLMCallQuerier creates a new querier.
func NewLLMCallQuerier(gatewayURL string) *LLMCallQuerier {
	return &LLMCallQuerier{
		gatewayURL: gatewayURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// graphQLResponse represents a GraphQL response.
type graphQLResponse struct {
	Data   map[string]any `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// graphEntity represents a graph entity with triples.
type graphEntity struct {
	ID      string        `json:"id"`
	Triples []graphTriple `json:"triples,omitempty"`
}

// graphTriple is a predicate-object pair.
type graphTriple struct {
	Predicate string `json:"predicate"`
	Object    any    `json:"object"`
}

// QueryByLoopID returns all LLM calls for a specific agent loop.
func (q *LLMCallQuerier) QueryByLoopID(ctx context.Context, loopID string) ([]*llm.CallRecord, error) {
	// Sanitize input for defense-in-depth alongside parameterized queries
	loopID = sanitizeGraphQLString(loopID)

	// Query entities where agent.activity.loop matches the loopID
	query := `query($loopID: String!) {
		entities(filter: { predicate: "agent.activity.loop", value: $loopID }) {
			id
			triples { predicate object }
		}
	}`

	variables := map[string]any{"loopID": loopID}

	entities, err := q.executeQuery(ctx, query, variables)
	if err != nil {
		return nil, fmt.Errorf("query by loop_id: %w", err)
	}

	records := make([]*llm.CallRecord, 0, len(entities))
	for _, entity := range entities {
		// Only include LLM call entities (type=model_call)
		if isLLMCallEntity(entity) {
			records = append(records, entityToCallRecord(entity))
		}
	}

	llm.SortByStartTime(records)
	return records, nil
}

// QueryByTraceID returns all LLM calls for a specific trace.
func (q *LLMCallQuerier) QueryByTraceID(ctx context.Context, traceID string) ([]*llm.CallRecord, error) {
	// Sanitize input for defense-in-depth alongside parameterized queries
	traceID = sanitizeGraphQLString(traceID)

	// Query entities where dc.terms.identifier matches the traceID
	query := `query($traceID: String!) {
		entities(filter: { predicate: "dc.terms.identifier", value: $traceID }) {
			id
			triples { predicate object }
		}
	}`

	variables := map[string]any{"traceID": traceID}

	entities, err := q.executeQuery(ctx, query, variables)
	if err != nil {
		return nil, fmt.Errorf("query by trace_id: %w", err)
	}

	records := make([]*llm.CallRecord, 0, len(entities))
	for _, entity := range entities {
		// Only include LLM call entities (type=model_call)
		if isLLMCallEntity(entity) {
			records = append(records, entityToCallRecord(entity))
		}
	}

	llm.SortByStartTime(records)
	return records, nil
}

// QueryByRequestID returns a single LLM call by its request ID.
func (q *LLMCallQuerier) QueryByRequestID(ctx context.Context, requestID string) (*llm.CallRecord, error) {
	// Sanitize input for defense-in-depth alongside parameterized queries
	requestID = sanitizeGraphQLString(requestID)

	// Query entities where llm.call.request_id matches
	query := `query($requestID: String!) {
		entities(filter: { predicate: "llm.call.request_id", value: $requestID }, limit: 1) {
			id
			triples { predicate object }
		}
	}`

	variables := map[string]any{"requestID": requestID}

	entities, err := q.executeQuery(ctx, query, variables)
	if err != nil {
		return nil, fmt.Errorf("query by request_id: %w", err)
	}

	if len(entities) == 0 {
		return nil, nil
	}

	return entityToCallRecord(entities[0]), nil
}

// executeQuery runs a GraphQL query and returns entities.
func (q *LLMCallQuerier) executeQuery(ctx context.Context, query string, variables map[string]any) ([]graphEntity, error) {
	reqBody := map[string]any{"query": query}
	if variables != nil {
		reqBody["variables"] = variables
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", q.gatewayURL+"/graphql", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := q.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxGraphErrorBodySize))
		return nil, fmt.Errorf("graph gateway returned %d: %s", resp.StatusCode, string(body))
	}

	var result graphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", result.Errors[0].Message)
	}

	// Parse entities from response
	entitiesRaw, ok := result.Data["entities"].([]any)
	if !ok {
		return nil, nil
	}

	entities := make([]graphEntity, 0, len(entitiesRaw))
	for _, e := range entitiesRaw {
		entityMap, ok := e.(map[string]any)
		if !ok {
			continue
		}
		entities = append(entities, parseGraphEntity(entityMap))
	}

	return entities, nil
}

// parseGraphEntity parses a single entity from a map.
func parseGraphEntity(entityMap map[string]any) graphEntity {
	entity := graphEntity{}

	if id, ok := entityMap["id"].(string); ok {
		entity.ID = id
	}

	if triples, ok := entityMap["triples"].([]any); ok {
		for _, t := range triples {
			tripleMap, ok := t.(map[string]any)
			if !ok {
				continue
			}
			triple := graphTriple{}
			if pred, ok := tripleMap["predicate"].(string); ok {
				triple.Predicate = pred
			}
			triple.Object = tripleMap["object"]
			entity.Triples = append(entity.Triples, triple)
		}
	}

	return entity
}

// isLLMCallEntity checks if an entity is an LLM call (type=model_call).
func isLLMCallEntity(entity graphEntity) bool {
	for _, t := range entity.Triples {
		if t.Predicate == semspec.PredicateActivityType {
			if val, ok := t.Object.(string); ok {
				return val == "model_call"
			}
		}
	}
	return false
}

// entityToCallRecord converts graph entity triples to CallRecord.
func entityToCallRecord(entity graphEntity) *llm.CallRecord {
	record := &llm.CallRecord{}

	// Build a map for easier lookup
	predicates := make(map[string]any)
	var fallbacks []string

	for _, t := range entity.Triples {
		// Handle multi-value predicates
		if t.Predicate == semspec.LLMFallback {
			if val, ok := t.Object.(string); ok {
				fallbacks = append(fallbacks, val)
			}
			continue
		}
		predicates[t.Predicate] = t.Object
	}

	// Map predicates to CallRecord fields
	record.LoopID = getString(predicates, semspec.ActivityLoop)
	record.TraceID = getString(predicates, semspec.DCIdentifier)
	record.RequestID = getString(predicates, semspec.LLMRequestID)
	record.Capability = getString(predicates, semspec.LLMCapability)
	record.Model = getString(predicates, semspec.ActivityModel)
	record.Provider = getString(predicates, semspec.LLMProvider)
	record.PromptTokens = getInt(predicates, semspec.ActivityTokensIn)
	record.CompletionTokens = getInt(predicates, semspec.ActivityTokensOut)
	record.TotalTokens = record.PromptTokens + record.CompletionTokens
	record.DurationMs = getInt64(predicates, semspec.ActivityDuration)
	record.FinishReason = getString(predicates, semspec.LLMFinishReason)
	record.Error = getString(predicates, semspec.ActivityError)
	record.ContextBudget = getInt(predicates, semspec.LLMContextBudget)
	record.ContextTruncated = getBool(predicates, semspec.LLMContextTruncated)
	record.Retries = getInt(predicates, semspec.LLMRetries)
	record.FallbacksUsed = fallbacks
	record.MessagesCount = getInt(predicates, semspec.LLMMessagesCount)
	record.ResponsePreview = getString(predicates, semspec.LLMResponsePreview)

	// Parse timestamps
	if startedAt := getString(predicates, semspec.ActivityStartedAt); startedAt != "" {
		if t, err := time.Parse(time.RFC3339, startedAt); err == nil {
			record.StartedAt = t
		}
	}
	if endedAt := getString(predicates, semspec.ActivityEndedAt); endedAt != "" {
		if t, err := time.Parse(time.RFC3339, endedAt); err == nil {
			record.CompletedAt = t
		}
	}

	return record
}

// getString extracts a string value from the predicates map.
func getString(predicates map[string]any, key string) string {
	if val, ok := predicates[key]; ok {
		switch v := val.(type) {
		case string:
			return v
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
	}
	return ""
}

// getInt extracts an int value from the predicates map.
func getInt(predicates map[string]any, key string) int {
	if val, ok := predicates[key]; ok {
		switch v := val.(type) {
		case float64:
			return int(v)
		case int:
			return v
		case string:
			if i, err := strconv.Atoi(v); err == nil {
				return i
			}
		}
	}
	return 0
}

// getInt64 extracts an int64 value from the predicates map.
func getInt64(predicates map[string]any, key string) int64 {
	if val, ok := predicates[key]; ok {
		switch v := val.(type) {
		case float64:
			return int64(v)
		case int64:
			return v
		case int:
			return int64(v)
		case string:
			if i, err := strconv.ParseInt(v, 10, 64); err == nil {
				return i
			}
		}
	}
	return 0
}

// getBool extracts a bool value from the predicates map.
func getBool(predicates map[string]any, key string) bool {
	if val, ok := predicates[key]; ok {
		switch v := val.(type) {
		case bool:
			return v
		case string:
			return strings.ToLower(v) == "true"
		}
	}
	return false
}

// sanitizeGraphQLString removes potentially dangerous characters from GraphQL string inputs.
// This provides defense-in-depth alongside parameterized queries.
func sanitizeGraphQLString(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return s
}
