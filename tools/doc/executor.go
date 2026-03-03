// Package doc provides document management tools for the Semspec agent.
// These tools allow agents to import, list, search, and retrieve documents
// from the knowledge graph.
package doc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"

	"github.com/c360studio/semspec/source"
	sourceVocab "github.com/c360studio/semspec/vocabulary/source"
)

// entityIDPattern validates entity IDs to prevent GraphQL injection.
// Valid IDs contain only lowercase letters, numbers, dots, hyphens, and underscores.
var entityIDPattern = regexp.MustCompile(`^[a-z0-9.\-_]+$`)

// validateEntityID validates an entity ID for GraphQL queries.
func validateEntityID(id string) error {
	if id == "" {
		return nil
	}
	if !entityIDPattern.MatchString(id) {
		return fmt.Errorf("invalid entity ID format: must contain only lowercase letters, numbers, dots, hyphens, and underscores")
	}
	return nil
}

// Executor implements document management tools.
type Executor struct {
	gatewayURL string
	sourcesDir string
	httpClient *http.Client
}

// NewExecutor creates a new document executor.
func NewExecutor(sourcesDir string) *Executor {
	return &Executor{
		gatewayURL: getGatewayURL(),
		sourcesDir: sourcesDir,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:       10,
				IdleConnTimeout:    90 * time.Second,
				MaxConnsPerHost:    10,
				DisableCompression: false,
			},
		},
	}
}

// getGatewayURL returns the graph gateway URL from environment or default.
func getGatewayURL() string {
	if url := os.Getenv("SEMSPEC_GRAPH_GATEWAY_URL"); url != "" {
		return url
	}
	return "http://localhost:8082"
}

// Execute executes a document tool call.
func (e *Executor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	switch call.Name {
	case "doc_import":
		return e.docImport(ctx, call)
	case "doc_list":
		return e.docList(ctx, call)
	case "doc_search":
		return e.docSearch(ctx, call)
	case "doc_get":
		return e.docGet(ctx, call)
	default:
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("unknown tool: %s", call.Name),
		}, fmt.Errorf("unknown tool: %s", call.Name)
	}
}

// ListTools returns the tool definitions for document operations.
func (e *Executor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "doc_import",
			Description: "Import a document into the knowledge graph. The document will be parsed, analyzed, and chunked for semantic search and context assembly.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Path to the document file (relative to sources directory or absolute)",
					},
					"project_id": map[string]any{
						"type":        "string",
						"description": "Optional project entity ID to associate the document with",
					},
					"category": map[string]any{
						"type":        "string",
						"description": "Optional document category (sop, spec, datasheet, reference, api). If not provided, will be inferred by LLM analysis.",
						"enum":        []string{"sop", "spec", "datasheet", "reference", "api"},
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "doc_list",
			Description: "List documents in the knowledge graph. Returns document metadata including ID, name, category, and status.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"project_id": map[string]any{
						"type":        "string",
						"description": "Filter documents by project entity ID",
					},
					"category": map[string]any{
						"type":        "string",
						"description": "Filter documents by category",
						"enum":        []string{"sop", "spec", "datasheet", "reference", "api"},
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of documents to return (default: 50)",
					},
				},
			},
		},
		{
			Name:        "doc_search",
			Description: "Search documents in the knowledge graph by content, domain, or keywords. Returns matching documents ranked by relevance.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query text to match against document content and metadata",
					},
					"domain": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by semantic domain(s): auth, database, api, security, testing, etc.",
					},
					"category": map[string]any{
						"type":        "string",
						"description": "Filter by document category",
						"enum":        []string{"sop", "spec", "datasheet", "reference", "api"},
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 20)",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "doc_get",
			Description: "Get a specific document and its chunks from the knowledge graph by entity ID.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity_id": map[string]any{
						"type":        "string",
						"description": "The document entity ID (e.g., 'doc.error-handling-sop.abc123')",
					},
				},
				"required": []string{"entity_id"},
			},
		},
	}
}

// docImport imports a document into the knowledge graph.
func (e *Executor) docImport(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	path, ok := call.Arguments["path"].(string)
	if !ok || path == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "path argument is required",
		}, nil
	}

	// Build ingest request
	req := source.IngestRequest{
		Path: path,
	}

	if projectID, ok := call.Arguments["project_id"].(string); ok {
		req.ProjectID = projectID
	}

	// Note: category is inferred during ingestion via frontmatter or LLM analysis
	// We could add it to IngestRequest if needed for explicit override

	// Publish to source ingestion endpoint
	jsonBody, err := json.Marshal(req)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("failed to marshal request: %v", err),
		}, nil
	}

	// POST to the HTTP gateway's ingest endpoint
	httpReq, err := http.NewRequestWithContext(ctx, "POST", e.gatewayURL+"/sources/ingest", bytes.NewReader(jsonBody))
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("failed to create request: %v", err),
		}, nil
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(httpReq)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("failed to submit import request: %v", err),
		}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("failed to read response: %v", err),
		}, nil
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("import failed with status %d: %s", resp.StatusCode, string(body)),
		}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Document import submitted for: %s\n%s", path, string(body)),
	}, nil
}

// docList lists documents in the knowledge graph.
func (e *Executor) docList(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	limit := 50
	if l, ok := call.Arguments["limit"].(float64); ok {
		limit = int(l)
	}

	// Query for document entities using entitiesByPredicate.
	entities, err := e.queryEntitiesByPredicate(ctx, sourceVocab.SourceType, "document", limit)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("failed to list documents: %v", err),
		}, nil
	}

	// Apply optional client-side filters for project_id and category.
	projectID, _ := call.Arguments["project_id"].(string)
	category, _ := call.Arguments["category"].(string)

	documents := e.formatEntityList(entities, projectID, category)

	output, _ := json.MarshalIndent(documents, "", "  ")
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(output),
	}, nil
}

// docSearch searches documents in the knowledge graph.
func (e *Executor) docSearch(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	searchQuery, ok := call.Arguments["query"].(string)
	if !ok || searchQuery == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "query argument is required",
		}, nil
	}

	limit := 20
	if l, ok := call.Arguments["limit"].(float64); ok {
		limit = int(l)
	}

	// Fetch more entities than needed, filter client-side for text match.
	entities, err := e.queryEntitiesByPredicate(ctx, sourceVocab.SourceType, "document", limit*2)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("search failed: %v", err),
		}, nil
	}

	// Filter results based on query and optional filters.
	documents := e.filterEntityDocuments(entities, call.Arguments, searchQuery, limit)

	output, _ := json.MarshalIndent(documents, "", "  ")
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(output),
	}, nil
}

// docGet retrieves a specific document and its chunks.
func (e *Executor) docGet(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	entityID, ok := call.Arguments["entity_id"].(string)
	if !ok || entityID == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "entity_id argument is required",
		}, nil
	}

	// Validate entity ID to prevent GraphQL injection
	if err := validateEntityID(entityID); err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  err.Error(),
		}, nil
	}

	// Get the parent document using parameterized query.
	docEntity, err := e.getEntityByID(ctx, entityID)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("failed to get document: %v", err),
		}, nil
	}

	// Get chunks that belong to this document.
	chunks, err := e.queryEntitiesByPredicate(ctx, sourceVocab.CodeBelongs, entityID, 100)
	if err != nil {
		// Document exists but chunks query failed - return doc without chunks.
		output, _ := json.MarshalIndent(map[string]any{
			"document": docEntity,
			"chunks":   []any{},
			"error":    fmt.Sprintf("failed to fetch chunks: %v", err),
		}, "", "  ")
		return agentic.ToolResult{
			CallID:  call.ID,
			Content: string(output),
		}, nil
	}

	// Format response.
	response := map[string]any{
		"document": docEntity,
		"chunks":   chunks,
	}

	output, _ := json.MarshalIndent(response, "", "  ")
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(output),
	}, nil
}

// executeGraphQL executes a GraphQL query against the graph gateway.
func (e *Executor) executeGraphQL(ctx context.Context, query string, variables map[string]any) (map[string]any, error) {
	reqBody := map[string]any{"query": query}
	if variables != nil {
		reqBody["variables"] = variables
	}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.gatewayURL+"/graphql", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("graph gateway returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data   map[string]any `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", result.Errors[0].Message)
	}

	return result.Data, nil
}

// queryEntitiesByPredicate uses entitiesByPredicate to get entity IDs,
// then hydrates each entity. Returns the entities as raw maps.
func (e *Executor) queryEntitiesByPredicate(ctx context.Context, predicate, value string, limit int) ([]map[string]any, error) {
	query := `query($predicate: String!, $value: String, $limit: Int) {
		entitiesByPredicate(predicate: $predicate, value: $value, limit: $limit)
	}`
	vars := map[string]any{
		"predicate": predicate,
		"limit":     limit,
	}
	if value != "" {
		vars["value"] = value
	}

	data, err := e.executeGraphQL(ctx, query, vars)
	if err != nil {
		return nil, err
	}

	idsRaw, _ := data["entitiesByPredicate"].([]any)
	if len(idsRaw) == 0 {
		return nil, nil
	}

	entities := make([]map[string]any, 0, len(idsRaw))
	for _, idRaw := range idsRaw {
		id, ok := idRaw.(string)
		if !ok {
			continue
		}
		entity, err := e.getEntityByID(ctx, id)
		if err != nil {
			continue
		}
		entities = append(entities, entity)
	}

	return entities, nil
}

// getEntityByID retrieves a single entity by ID with parameterized query.
func (e *Executor) getEntityByID(ctx context.Context, id string) (map[string]any, error) {
	query := `query($id: String!) {
		entity(id: $id) {
			id
			triples { predicate object }
		}
	}`
	data, err := e.executeGraphQL(ctx, query, map[string]any{"id": id})
	if err != nil {
		return nil, err
	}
	entity, ok := data["entity"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("entity not found: %s", id)
	}
	return entity, nil
}

// formatEntityList formats hydrated entity maps into document summaries.
// Applies optional client-side filters for projectID and category.
func (e *Executor) formatEntityList(entities []map[string]any, projectID, category string) []map[string]any {
	documents := make([]map[string]any, 0, len(entities))
	for _, entityMap := range entities {
		doc := extractDocFromEntity(entityMap)

		// Apply client-side filters.
		if projectID != "" {
			if pid, _ := doc["project_id"].(string); pid != projectID {
				continue
			}
		}
		if category != "" {
			if cat, _ := doc["category"].(string); cat != category {
				continue
			}
		}

		documents = append(documents, doc)
	}
	return documents
}

// filterEntityDocuments filters hydrated entity maps based on search criteria.
func (e *Executor) filterEntityDocuments(entities []map[string]any, args map[string]any, query string, limit int) []map[string]any {
	domains := extractDomainFilter(args)
	category, _ := args["category"].(string)

	documents := make([]map[string]any, 0)
	for _, entityMap := range entities {
		if len(documents) >= limit {
			break
		}
		doc, matchesQuery, matchesCategory, docDomains := extractDocFields(entityMap, query, category)
		matchesDomain := len(domains) == 0 || hasDomainMatch(domains, docDomains)
		if matchesQuery && matchesDomain && matchesCategory {
			documents = append(documents, doc)
		}
	}

	return documents
}

// extractDocFromEntity extracts document metadata fields from an entity map.
func extractDocFromEntity(entityMap map[string]any) map[string]any {
	doc := map[string]any{}
	if id, ok := entityMap["id"].(string); ok {
		doc["id"] = id
	}
	if triples, ok := entityMap["triples"].([]any); ok {
		for _, t := range triples {
			triple, ok := t.(map[string]any)
			if !ok {
				continue
			}
			pred, _ := triple["predicate"].(string)
			obj := triple["object"]
			switch pred {
			case sourceVocab.SourceName:
				doc["name"] = obj
			case sourceVocab.DocCategory:
				doc["category"] = obj
			case sourceVocab.SourceStatus:
				doc["status"] = obj
			case sourceVocab.SourceProject:
				doc["project_id"] = obj
			case sourceVocab.DocFilePath:
				doc["file_path"] = obj
			case sourceVocab.DocSummary:
				doc["summary"] = obj
			case sourceVocab.DocChunkCount:
				doc["chunk_count"] = obj
			}
		}
	}
	return doc
}

// extractDomainFilter extracts the list of requested domains from tool arguments.
func extractDomainFilter(args map[string]any) []string {
	var domains []string
	if d, ok := args["domain"].([]any); ok {
		for _, v := range d {
			if s, ok := v.(string); ok {
				domains = append(domains, s)
			}
		}
	}
	return domains
}

// hasDomainMatch reports whether any requested domain matches any document domain.
func hasDomainMatch(requested, docDomains []string) bool {
	for _, d := range requested {
		for _, dd := range docDomains {
			if d == dd {
				return true
			}
		}
	}
	return false
}

// extractDocFields extracts document metadata from an entity map and evaluates query and category filters.
// Returns the doc map, whether it matches the query, whether it matches the category, and its domains.
func extractDocFields(entityMap map[string]any, query, category string) (doc map[string]any, matchesQuery, matchesCategory bool, docDomains []string) {
	doc = map[string]any{}
	if id, ok := entityMap["id"].(string); ok {
		doc["id"] = id
	}
	matchesCategory = category == ""

	triples, ok := entityMap["triples"].([]any)
	if !ok {
		return doc, matchesQuery, matchesCategory, docDomains
	}
	for _, t := range triples {
		triple, ok := t.(map[string]any)
		if !ok {
			continue
		}
		pred, _ := triple["predicate"].(string)
		obj := triple["object"]
		matchesQuery, matchesCategory, docDomains = applyTriple(doc, pred, obj, query, category, matchesQuery, matchesCategory, docDomains)
	}
	return doc, matchesQuery, matchesCategory, docDomains
}

// applyTriple processes a single predicate-object triple, updating the doc map and filter state.
func applyTriple(doc map[string]any, pred string, obj any, query, category string, matchesQuery, matchesCategory bool, docDomains []string) (bool, bool, []string) {
	switch pred {
	case sourceVocab.SourceName:
		doc["name"] = obj
		if containsIgnoreCase(obj, query) {
			matchesQuery = true
		}
	case sourceVocab.DocCategory:
		doc["category"] = obj
		if s, ok := obj.(string); ok && (category == "" || s == category) {
			matchesCategory = true
		}
	case sourceVocab.SourceStatus:
		doc["status"] = obj
	case sourceVocab.DocSummary:
		doc["summary"] = obj
		if containsIgnoreCase(obj, query) {
			matchesQuery = true
		}
	case sourceVocab.DocKeywords:
		doc["keywords"] = obj
		if containsIgnoreCase(obj, query) {
			matchesQuery = true
		}
	case sourceVocab.DocDomain:
		docDomains = appendDomains(docDomains, obj)
		doc["domain"] = obj
	case sourceVocab.DocFilePath:
		doc["file_path"] = obj
	}
	return matchesQuery, matchesCategory, docDomains
}

// appendDomains appends domain values from a predicate object (string or []any) to the slice.
func appendDomains(docDomains []string, obj any) []string {
	if arr, ok := obj.([]any); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok {
				docDomains = append(docDomains, s)
			}
		}
	} else if s, ok := obj.(string); ok {
		docDomains = append(docDomains, s)
	}
	return docDomains
}

// containsIgnoreCase checks if obj (string or []string) contains query (case-insensitive).
func containsIgnoreCase(obj any, query string) bool {
	if obj == nil || query == "" {
		return false
	}

	queryLower := strings.ToLower(query)

	switch v := obj.(type) {
	case string:
		return strings.Contains(strings.ToLower(v), queryLower)
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				if strings.Contains(strings.ToLower(s), queryLower) {
					return true
				}
			}
		}
	case []string:
		for _, s := range v {
			if strings.Contains(strings.ToLower(s), queryLower) {
				return true
			}
		}
	}

	return false
}
