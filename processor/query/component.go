// Package query provides a graph query processor component for querying
// entities and relationships in the knowledge graph.
package query

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/natsclient"
)

// querySchema defines the configuration schema
var querySchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Component implements the query processor
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger
	platform   component.PlatformMeta

	// In-memory entity index for queries
	// Note: For production, this would be backed by a proper graph database
	entities map[string]*EntityResult
	index    *InvertedIndex
	mu       sync.RWMutex

	// Lifecycle management
	running   bool
	startTime time.Time

	// Metrics
	queriesProcessed int64
	lastQuery        time.Time

	// Cancel functions for background goroutines
	cancelFuncs []context.CancelFunc
}

// InvertedIndex provides fast lookup by various attributes
type InvertedIndex struct {
	byType     map[string][]string          // type -> entity IDs
	byPackage  map[string][]string          // package -> entity IDs
	byName     map[string][]string          // name -> entity IDs
	byPath     map[string][]string          // path -> entity IDs
	byRelation map[RelationType]map[string][]string // relation -> entity ID -> related IDs
}

// NewInvertedIndex creates a new inverted index
func NewInvertedIndex() *InvertedIndex {
	return &InvertedIndex{
		byType:     make(map[string][]string),
		byPackage:  make(map[string][]string),
		byName:     make(map[string][]string),
		byPath:     make(map[string][]string),
		byRelation: make(map[RelationType]map[string][]string),
	}
}

// NewComponent creates a new query processor component
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Use default config if ports not set
	if config.Ports == nil {
		config = DefaultConfig()
		// Re-unmarshal to get user-provided values
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Component{
		name:       "query",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		platform:   deps.Platform,
		entities:   make(map[string]*EntityResult),
		index:      NewInvertedIndex(),
	}, nil
}

// Initialize prepares the component
func (c *Component) Initialize() error {
	return nil
}

// Start begins the query processor
func (c *Component) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return fmt.Errorf("component already running")
	}

	if c.natsClient == nil {
		return fmt.Errorf("NATS client required")
	}

	// Start consuming entity updates to build index
	entityCtx, entityCancel := context.WithCancel(ctx)
	c.cancelFuncs = append(c.cancelFuncs, entityCancel)
	go c.consumeEntityUpdates(entityCtx)

	// Start consuming query requests
	queryCtx, queryCancel := context.WithCancel(ctx)
	c.cancelFuncs = append(c.cancelFuncs, queryCancel)
	go c.handleQueryRequests(queryCtx)

	c.running = true
	c.startTime = time.Now()

	c.logger.Info("Query processor started",
		"org", c.config.Org,
		"max_results", c.config.MaxResults)

	return nil
}

// consumeEntityUpdates consumes entity updates to build the index
func (c *Component) consumeEntityUpdates(ctx context.Context) {
	handler := func(data []byte) {
		var msg EntityIngestMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			c.logger.Warn("Invalid entity message", "error", err)
			return
		}

		c.indexEntity(msg)
	}

	if err := c.natsClient.ConsumeStream(ctx, c.config.StreamName, "graph.ingest.entity", handler); err != nil {
		if ctx.Err() == nil {
			c.logger.Error("Failed to consume entity updates", "error", err)
		}
	}
}

// EntityIngestMessage is the message format for graph ingestion
type EntityIngestMessage struct {
	ID        string    `json:"id"`
	Triples   []Triple  `json:"triples"`
	UpdatedAt time.Time `json:"updated_at"`
}

// indexEntity adds or updates an entity in the index
func (c *Component) indexEntity(msg EntityIngestMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entity := &EntityResult{
		ID:      msg.ID,
		Triples: msg.Triples,
		Related: make(map[RelationType][]string),
	}

	// Extract fields from triples
	for _, triple := range msg.Triples {
		if triple.Subject != msg.ID {
			continue
		}

		switch triple.Predicate {
		case "code.artifact.type":
			if s, ok := triple.Object.(string); ok {
				entity.Type = s
			}
		case "dc.terms.title":
			if s, ok := triple.Object.(string); ok {
				entity.Name = s
			}
		case "code.artifact.path":
			if s, ok := triple.Object.(string); ok {
				entity.Path = s
			}
		case "code.artifact.package":
			if s, ok := triple.Object.(string); ok {
				entity.Package = s
			}
		case "code.structure.contains":
			if s, ok := triple.Object.(string); ok {
				entity.Related[RelContains] = append(entity.Related[RelContains], s)
			}
		case "code.dependency.imports":
			if s, ok := triple.Object.(string); ok {
				entity.Related[RelImports] = append(entity.Related[RelImports], s)
			}
		case "code.relationship.implements":
			if s, ok := triple.Object.(string); ok {
				entity.Related[RelImplements] = append(entity.Related[RelImplements], s)
			}
		case "code.relationship.calls":
			if s, ok := triple.Object.(string); ok {
				entity.Related[RelCalls] = append(entity.Related[RelCalls], s)
			}
		case "code.relationship.references":
			if s, ok := triple.Object.(string); ok {
				entity.Related[RelReferences] = append(entity.Related[RelReferences], s)
			}
		}
	}

	// Store entity
	c.entities[msg.ID] = entity

	// Update indexes
	if entity.Type != "" {
		c.index.byType[entity.Type] = appendUnique(c.index.byType[entity.Type], msg.ID)
	}
	if entity.Package != "" {
		c.index.byPackage[entity.Package] = appendUnique(c.index.byPackage[entity.Package], msg.ID)
	}
	if entity.Name != "" {
		c.index.byName[entity.Name] = appendUnique(c.index.byName[entity.Name], msg.ID)
	}
	if entity.Path != "" {
		c.index.byPath[entity.Path] = appendUnique(c.index.byPath[entity.Path], msg.ID)
	}

	// Update relation index
	for rel, targets := range entity.Related {
		if c.index.byRelation[rel] == nil {
			c.index.byRelation[rel] = make(map[string][]string)
		}
		c.index.byRelation[rel][msg.ID] = targets
	}
}

// appendUnique appends a value if not already present
func appendUnique(slice []string, value string) []string {
	for _, v := range slice {
		if v == value {
			return slice
		}
	}
	return append(slice, value)
}

// handleQueryRequests handles incoming query requests
func (c *Component) handleQueryRequests(ctx context.Context) {
	handler := func(data []byte) {
		var req Request
		if err := json.Unmarshal(data, &req); err != nil {
			c.logger.Warn("Invalid query request", "error", err)
			return
		}

		resp := c.executeQuery(&req)

		// Publish result
		resultData, _ := json.Marshal(resp)
		if err := c.natsClient.PublishToStream(ctx, "graph.query.result", resultData); err != nil {
			c.logger.Warn("Failed to publish query result", "error", err)
		}
	}

	if err := c.natsClient.ConsumeStream(ctx, c.config.StreamName, "graph.query.request", handler); err != nil {
		if ctx.Err() == nil {
			c.logger.Error("Failed to consume query requests", "error", err)
		}
	}
}

// executeQuery executes a query and returns the response
func (c *Component) executeQuery(req *Request) *Response {
	start := time.Now()

	c.mu.Lock()
	c.queriesProcessed++
	c.lastQuery = time.Now()
	c.mu.Unlock()

	c.mu.RLock()
	defer c.mu.RUnlock()

	resp := NewResponse(req.RequestID)

	maxResults := req.MaxResults
	if maxResults <= 0 || maxResults > c.config.MaxResults {
		maxResults = c.config.MaxResults
	}

	switch req.Type {
	case QueryEntity:
		resp = c.queryEntity(req, maxResults)

	case QueryRelated:
		resp = c.queryRelated(req, maxResults)

	case QueryDependsOn:
		resp = c.queryDependsOn(req, maxResults)

	case QueryDependedBy:
		resp = c.queryDependedBy(req, maxResults)

	case QueryImplements:
		resp = c.queryImplements(req, maxResults)

	case QueryContains:
		resp = c.queryContains(req, maxResults)

	case QuerySearch:
		resp = c.querySearch(req, maxResults)

	default:
		resp = NewErrorResponse(req.RequestID, fmt.Sprintf("unknown query type: %s", req.Type))
	}

	resp.QueryTime = time.Since(start)
	return resp
}

// queryEntity retrieves a single entity by ID
func (c *Component) queryEntity(req *Request, maxResults int) *Response {
	resp := NewResponse(req.RequestID)

	entity, ok := c.entities[req.EntityID]
	if !ok {
		resp.Success = false
		resp.Error = "entity not found"
		return resp
	}

	result := *entity
	if !req.IncludeTriples {
		result.Triples = nil
	}

	resp.Entities = []EntityResult{result}
	resp.TotalCount = 1
	return resp
}

// queryRelated finds entities related to a given entity
func (c *Component) queryRelated(req *Request, maxResults int) *Response {
	resp := NewResponse(req.RequestID)

	entity, ok := c.entities[req.EntityID]
	if !ok {
		resp.Success = false
		resp.Error = "entity not found"
		return resp
	}

	var relatedIDs []string
	if req.Relation != "" {
		relatedIDs = entity.Related[req.Relation]
	} else {
		// Get all related
		for _, ids := range entity.Related {
			relatedIDs = append(relatedIDs, ids...)
		}
	}

	for i, id := range relatedIDs {
		if i >= maxResults {
			break
		}
		if related, ok := c.entities[id]; ok {
			result := *related
			if !req.IncludeTriples {
				result.Triples = nil
			}
			resp.Entities = append(resp.Entities, result)
		}
	}

	resp.TotalCount = len(relatedIDs)
	return resp
}

// queryDependsOn finds what the given entity depends on
func (c *Component) queryDependsOn(req *Request, maxResults int) *Response {
	resp := NewResponse(req.RequestID)

	entity, ok := c.entities[req.EntityID]
	if !ok {
		resp.Success = false
		resp.Error = "entity not found"
		return resp
	}

	// Collect all dependency relationships
	var deps []string
	deps = append(deps, entity.Related[RelImports]...)
	deps = append(deps, entity.Related[RelCalls]...)
	deps = append(deps, entity.Related[RelReferences]...)

	for i, id := range deps {
		if i >= maxResults {
			break
		}
		if dep, ok := c.entities[id]; ok {
			result := *dep
			if !req.IncludeTriples {
				result.Triples = nil
			}
			resp.Entities = append(resp.Entities, result)
		}
	}

	resp.TotalCount = len(deps)
	return resp
}

// queryDependedBy finds what depends on the given entity (reverse lookup)
func (c *Component) queryDependedBy(req *Request, maxResults int) *Response {
	resp := NewResponse(req.RequestID)

	var dependents []string
	for id, entity := range c.entities {
		for _, rel := range []RelationType{RelImports, RelCalls, RelReferences} {
			for _, target := range entity.Related[rel] {
				if target == req.EntityID {
					dependents = appendUnique(dependents, id)
					break
				}
			}
		}
	}

	for i, id := range dependents {
		if i >= maxResults {
			break
		}
		if dep, ok := c.entities[id]; ok {
			result := *dep
			if !req.IncludeTriples {
				result.Triples = nil
			}
			resp.Entities = append(resp.Entities, result)
		}
	}

	resp.TotalCount = len(dependents)
	return resp
}

// queryImplements finds types that implement an interface
func (c *Component) queryImplements(req *Request, maxResults int) *Response {
	resp := NewResponse(req.RequestID)

	var implementors []string
	for id, entity := range c.entities {
		for _, impl := range entity.Related[RelImplements] {
			if impl == req.EntityID {
				implementors = append(implementors, id)
				break
			}
		}
	}

	for i, id := range implementors {
		if i >= maxResults {
			break
		}
		if impl, ok := c.entities[id]; ok {
			result := *impl
			if !req.IncludeTriples {
				result.Triples = nil
			}
			resp.Entities = append(resp.Entities, result)
		}
	}

	resp.TotalCount = len(implementors)
	return resp
}

// queryContains finds entities contained by a given entity
func (c *Component) queryContains(req *Request, maxResults int) *Response {
	resp := NewResponse(req.RequestID)

	entity, ok := c.entities[req.EntityID]
	if !ok {
		resp.Success = false
		resp.Error = "entity not found"
		return resp
	}

	children := entity.Related[RelContains]
	for i, id := range children {
		if i >= maxResults {
			break
		}
		if child, ok := c.entities[id]; ok {
			result := *child
			if !req.IncludeTriples {
				result.Triples = nil
			}
			resp.Entities = append(resp.Entities, result)
		}
	}

	resp.TotalCount = len(children)
	return resp
}

// querySearch performs text search across entities
func (c *Component) querySearch(req *Request, maxResults int) *Response {
	resp := NewResponse(req.RequestID)

	searchText := strings.ToLower(req.SearchText)
	var matches []string

	for id, entity := range c.entities {
		if strings.Contains(strings.ToLower(entity.Name), searchText) ||
			strings.Contains(strings.ToLower(entity.Path), searchText) ||
			strings.Contains(strings.ToLower(id), searchText) {
			matches = append(matches, id)
		}
	}

	for i, id := range matches {
		if i >= maxResults {
			break
		}
		if entity, ok := c.entities[id]; ok {
			result := *entity
			if !req.IncludeTriples {
				result.Triples = nil
			}
			resp.Entities = append(resp.Entities, result)
		}
	}

	resp.TotalCount = len(matches)
	return resp
}

// Query provides a direct query interface (for programmatic use)
func (c *Component) Query(req *Request) *Response {
	return c.executeQuery(req)
}

// GetEntity retrieves a single entity by ID
func (c *Component) GetEntity(id string) (*EntityResult, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entity, ok := c.entities[id]
	if !ok {
		return nil, false
	}

	// Return a copy
	result := *entity
	return &result, true
}

// EntityCount returns the number of indexed entities
func (c *Component) EntityCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entities)
}

// Stop gracefully stops the component
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	// Cancel all background goroutines
	for _, cancel := range c.cancelFuncs {
		cancel()
	}
	c.cancelFuncs = nil

	c.running = false
	c.logger.Info("Query processor stopped",
		"queries", c.queriesProcessed,
		"entities", len(c.entities))

	return nil
}

// Discoverable interface implementation

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "query",
		Type:        "processor",
		Description: "Graph query processor for entity and relationship queries",
		Version:     "0.1.0",
	}
}

// InputPorts returns configured input port definitions
func (c *Component) InputPorts() []component.Port {
	if c.config.Ports == nil || len(c.config.Ports.Inputs) == 0 {
		return []component.Port{}
	}

	ports := make([]component.Port, len(c.config.Ports.Inputs))
	for i, portDef := range c.config.Ports.Inputs {
		ports[i] = component.Port{
			Name:        portDef.Name,
			Direction:   component.DirectionInput,
			Required:    portDef.Required,
			Description: portDef.Description,
			Config: component.NATSPort{
				Subject: portDef.Subject,
			},
		}
	}
	return ports
}

// OutputPorts returns configured output port definitions
func (c *Component) OutputPorts() []component.Port {
	if c.config.Ports == nil || len(c.config.Ports.Outputs) == 0 {
		return []component.Port{}
	}

	ports := make([]component.Port, len(c.config.Ports.Outputs))
	for i, portDef := range c.config.Ports.Outputs {
		ports[i] = component.Port{
			Name:        portDef.Name,
			Direction:   component.DirectionOutput,
			Required:    portDef.Required,
			Description: portDef.Description,
			Config: component.NATSPort{
				Subject: portDef.Subject,
			},
		}
	}
	return ports
}

// ConfigSchema returns the configuration schema
func (c *Component) ConfigSchema() component.ConfigSchema {
	return querySchema
}

// Health returns the current health status
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return component.HealthStatus{
		Healthy:    c.running,
		LastCheck:  time.Now(),
		ErrorCount: 0,
		Uptime:     time.Since(c.startTime),
		Status:     c.getStatus(),
	}
}

// getStatus returns a status string
func (c *Component) getStatus() string {
	if c.running {
		return "running"
	}
	return "stopped"
}

// DataFlow returns current data flow metrics
func (c *Component) DataFlow() component.FlowMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return component.FlowMetrics{
		MessagesPerSecond: 0,
		BytesPerSecond:    0,
		ErrorRate:         0,
		LastActivity:      c.lastQuery,
	}
}
