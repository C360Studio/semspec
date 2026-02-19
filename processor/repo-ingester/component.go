// Package repoingester provides a component for ingesting git repositories
// and triggering AST indexing for code entity extraction.
package repoingester

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/source"
	sourceVocab "github.com/c360studio/semspec/vocabulary/source"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// repoIngesterSchema defines the configuration schema.
var repoIngesterSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// graphIngestSubject is the subject for publishing entities.
const graphIngestSubject = "graph.ingest.entity"

// Component implements the repo-ingester processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger
	platform   component.PlatformMeta
	handler    *Handler

	// Lifecycle management
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	reposIngested  atomic.Int64
	pullsCompleted atomic.Int64
	errors         atomic.Int64
	lastActivityMu sync.RWMutex
	lastActivity   time.Time
}

// NewComponent creates a new repo-ingester processor component.
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

	c := &Component{
		name:       "repo-ingester",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		platform:   deps.Platform,
	}

	return c, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	return nil
}

// Start begins processing repository ingestion requests.
func (c *Component) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("component already running")
	}
	if c.natsClient == nil {
		c.mu.Unlock()
		return fmt.Errorf("NATS client required")
	}
	c.running = true
	c.startTime = time.Now()
	c.mu.Unlock()

	// Create handler
	c.handler = NewHandler(
		c.config.ReposDir,
		c.config.GetCloneTimeout(),
		c.config.CloneDepth,
	)

	// Set up consumer for repository requests
	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	// Start consumer in background
	go c.consumeMessages(runCtx)

	c.logger.Info("Repository ingester started",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"repos_dir", c.config.ReposDir)

	return nil
}

// consumeMessages processes incoming repository requests.
func (c *Component) consumeMessages(ctx context.Context) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Error("Failed to get JetStream context", "error", err)
		return
	}

	// Get or create consumer
	consumer, err := js.Consumer(ctx, c.config.StreamName, c.config.ConsumerName)
	if err != nil {
		c.logger.Error("Failed to get consumer", "error", err, "stream", c.config.StreamName, "consumer", c.config.ConsumerName)
		return
	}

	c.logger.Info("Consumer connected", "stream", c.config.StreamName, "consumer", c.config.ConsumerName)

	// Consume messages
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Fetch next message with timeout
		msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue // Timeout, try again
		}

		for msg := range msgs.Messages() {
			select {
			case <-ctx.Done():
				_ = msg.Nak()
				return
			default:
				c.handleMessage(ctx, msg)
			}
		}
	}
}

// handleMessage processes a single repository request.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	c.updateLastActivity()

	// Determine action from subject
	subject := msg.Subject()
	parts := subjectParts(subject)
	if len(parts) < 3 {
		c.logger.Warn("Invalid subject format", "subject", subject)
		_ = msg.Nak()
		return
	}

	action := parts[2] // source.repo.<action>.<id>

	switch action {
	case "ingest":
		c.handleIngestRequest(ctx, msg)
	case "pull":
		c.handlePullRequest(ctx, msg)
	case "reindex":
		c.handleReindexRequest(ctx, msg)
	case "update":
		c.handleUpdateRequest(ctx, msg)
	default:
		c.logger.Warn("Unknown action", "action", action, "subject", subject)
		_ = msg.Nak()
	}
}

// handleIngestRequest processes a new repository ingestion.
func (c *Component) handleIngestRequest(ctx context.Context, msg jetstream.Msg) {
	var req source.AddRepositoryRequest
	if err := json.Unmarshal(msg.Data(), &req); err != nil {
		c.logger.Warn("Failed to parse ingestion request", "error", err)
		c.errors.Add(1)
		_ = msg.Nak()
		return
	}

	c.logger.Info("Processing repository ingestion", "url", req.URL, "branch", req.Branch)

	// Ingest repository
	entity, astLanguages, err := c.handler.IngestRepository(ctx, req)
	if err != nil {
		c.logger.Error("Failed to ingest repository", "url", req.URL, "error", err)
		c.errors.Add(1)
		// Publish error entity
		c.publishErrorEntity(ctx, req.URL, err.Error())
		_ = msg.Nak()
		return
	}

	// Publish repository entity
	if err := c.publishEntity(ctx, entity); err != nil {
		c.logger.Error("Failed to publish repository entity", "entity_id", entity.ID, "error", err)
		c.errors.Add(1)
		_ = msg.Nak()
		return
	}

	// Trigger AST indexing if we have supported languages
	if len(astLanguages) > 0 {
		c.triggerASTIndexing(ctx, entity.ID, entity.RepoPath, astLanguages)
	}

	c.reposIngested.Add(1)
	_ = msg.Ack()

	c.logger.Info("Repository ingested successfully",
		"url", req.URL,
		"entity_id", entity.ID,
		"languages", astLanguages)
}

// handlePullRequest processes a pull request for an existing repository.
func (c *Component) handlePullRequest(ctx context.Context, msg jetstream.Msg) {
	var req map[string]string
	if err := json.Unmarshal(msg.Data(), &req); err != nil {
		c.logger.Warn("Failed to parse pull request", "error", err)
		c.errors.Add(1)
		_ = msg.Nak()
		return
	}

	entityID := req["id"]
	if entityID == "" {
		c.logger.Warn("Pull request missing entity ID")
		_ = msg.Nak()
		return
	}

	c.logger.Info("Processing repository pull", "entity_id", entityID)

	// Pull updates
	newCommit, err := c.handler.PullRepository(ctx, entityID)
	if err != nil {
		c.logger.Error("Failed to pull repository", "entity_id", entityID, "error", err)
		c.errors.Add(1)
		_ = msg.Nak()
		return
	}

	// Publish update with new commit
	c.publishCommitUpdate(ctx, entityID, newCommit)

	c.pullsCompleted.Add(1)
	_ = msg.Ack()

	c.logger.Info("Repository pulled successfully", "entity_id", entityID, "new_commit", newCommit)
}

// handleReindexRequest triggers re-indexing of a repository.
func (c *Component) handleReindexRequest(ctx context.Context, msg jetstream.Msg) {
	var req map[string]string
	if err := json.Unmarshal(msg.Data(), &req); err != nil {
		c.logger.Warn("Failed to parse reindex request", "error", err)
		_ = msg.Nak()
		return
	}

	entityID := req["id"]
	if entityID == "" {
		c.logger.Warn("Reindex request missing entity ID")
		_ = msg.Nak()
		return
	}

	c.logger.Info("Processing repository reindex", "entity_id", entityID)

	// Get repo path from entity ID
	slug := strings.TrimPrefix(entityID, "source.repo.")

	// Validate slug for path safety
	if err := validateSlug(slug); err != nil {
		c.logger.Warn("Invalid repository slug", "entity_id", entityID, "error", err)
		_ = msg.Nak()
		return
	}

	repoPath := filepath.Join(c.config.ReposDir, slug)

	// Detect languages again
	languages, _ := DetectLanguages(repoPath)
	astLanguages := FilterASTLanguages(languages)

	if len(astLanguages) > 0 {
		c.triggerASTIndexing(ctx, entityID, repoPath, astLanguages)
	}

	// Update status to indexing
	c.publishStatusUpdate(ctx, entityID, "indexing")

	_ = msg.Ack()
	c.logger.Info("Repository reindex triggered", "entity_id", entityID)
}

// handleUpdateRequest processes a settings update request.
func (c *Component) handleUpdateRequest(ctx context.Context, msg jetstream.Msg) {
	var req map[string]any
	if err := json.Unmarshal(msg.Data(), &req); err != nil {
		c.logger.Warn("Failed to parse update request", "error", err)
		_ = msg.Nak()
		return
	}

	entityID, _ := req["id"].(string)
	if entityID == "" {
		c.logger.Warn("Update request missing entity ID")
		_ = msg.Nak()
		return
	}

	// Build update triples
	var triples []message.Triple
	if autoPull, ok := req["auto_pull"].(bool); ok {
		triples = append(triples, message.Triple{
			Subject: entityID, Predicate: sourceVocab.RepoAutoPull, Object: autoPull,
		})
	}
	if pullInterval, ok := req["pull_interval"].(string); ok {
		triples = append(triples, message.Triple{
			Subject: entityID, Predicate: sourceVocab.RepoPullInterval, Object: pullInterval,
		})
	}
	if project, ok := req["project"].(string); ok {
		triples = append(triples, message.Triple{
			Subject: entityID, Predicate: sourceVocab.SourceProject, Object: project,
		})
	}

	if len(triples) > 0 {
		entity := &RepoEntityPayload{
			ID:         entityID,
			TripleData: triples,
			UpdatedAt:  time.Now(),
		}
		if err := c.publishEntity(ctx, entity); err != nil {
			c.logger.Error("Failed to publish settings update", "entity_id", entityID, "error", err)
			_ = msg.Nak()
			return
		}
	}

	_ = msg.Ack()
	c.logger.Info("Repository settings updated", "entity_id", entityID)
}

// publishEntity wraps a RepoEntityPayload and publishes it to the graph ingestion stream.
func (c *Component) publishEntity(ctx context.Context, entity *RepoEntityPayload) error {
	msg := message.NewBaseMessage(RepoEntityType, entity, "semspec")
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal entity message: %w", err)
	}
	return c.natsClient.PublishToStream(ctx, graphIngestSubject, data)
}

// generateRepoEntityID creates a repository entity ID from a URL.
func generateRepoEntityID(repoURL string) string {
	repoURL = strings.TrimSuffix(repoURL, ".git")
	repoURL = strings.TrimSuffix(repoURL, "/")

	parts := strings.Split(repoURL, "/")
	var slug string
	if len(parts) >= 2 {
		slug = parts[len(parts)-2] + "-" + parts[len(parts)-1]
	} else if len(parts) >= 1 {
		slug = parts[len(parts)-1]
	} else {
		slug = "repo"
	}

	slug = strings.ToLower(slug)
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "_", "-")

	return "source.repo." + slug
}

// publishErrorEntity publishes an error entity for a failed ingestion.
func (c *Component) publishErrorEntity(ctx context.Context, url, errMsg string) {
	entityID := generateRepoEntityID(url)
	entity := &RepoEntityPayload{
		ID: entityID,
		TripleData: []message.Triple{
			{Subject: entityID, Predicate: sourceVocab.SourceType, Object: "repository"},
			{Subject: entityID, Predicate: sourceVocab.SourceStatus, Object: "error"},
			{Subject: entityID, Predicate: sourceVocab.RepoError, Object: errMsg},
			{Subject: entityID, Predicate: sourceVocab.RepoURL, Object: url},
		},
		UpdatedAt: time.Now(),
	}
	_ = c.publishEntity(ctx, entity)
}

// publishStatusUpdate publishes a status update for a repository.
func (c *Component) publishStatusUpdate(ctx context.Context, entityID, status string) {
	entity := &RepoEntityPayload{
		ID: entityID,
		TripleData: []message.Triple{
			{Subject: entityID, Predicate: sourceVocab.SourceStatus, Object: status},
			{Subject: entityID, Predicate: sourceVocab.RepoStatus, Object: status},
		},
		UpdatedAt: time.Now(),
	}
	_ = c.publishEntity(ctx, entity)
}

// publishCommitUpdate publishes a commit update for a repository.
func (c *Component) publishCommitUpdate(ctx context.Context, entityID, commit string) {
	entity := &RepoEntityPayload{
		ID: entityID,
		TripleData: []message.Triple{
			{Subject: entityID, Predicate: sourceVocab.RepoLastCommit, Object: commit},
			{Subject: entityID, Predicate: sourceVocab.RepoLastIndexed, Object: time.Now().Format(time.RFC3339)},
		},
		UpdatedAt: time.Now(),
	}
	_ = c.publishEntity(ctx, entity)
}

// triggerASTIndexing publishes a request to trigger AST indexing.
func (c *Component) triggerASTIndexing(_ context.Context, entityID, repoPath string, languages []string) {
	// For now, log that indexing would be triggered
	// In a full implementation, this would publish to an AST indexer subject
	c.logger.Info("AST indexing triggered",
		"entity_id", entityID,
		"repo_path", repoPath,
		"languages", languages)

	// TODO: Publish to AST indexer subject when integration is ready
	// request := map[string]any{
	// 	"entity_id": entityID,
	// 	"repo_path": repoPath,
	// 	"languages": languages,
	// }
	// data, _ := json.Marshal(request)
	// c.natsClient.PublishToStream(ctx, c.config.ASTIndexerSubject, data)
}

// subjectParts splits a NATS subject into parts.
func subjectParts(subject string) []string {
	return strings.Split(subject, ".")
}

// updateLastActivity safely updates the last activity timestamp.
func (c *Component) updateLastActivity() {
	c.lastActivityMu.Lock()
	c.lastActivity = time.Now()
	c.lastActivityMu.Unlock()
}

// getLastActivity safely retrieves the last activity timestamp.
func (c *Component) getLastActivity() time.Time {
	c.lastActivityMu.RLock()
	defer c.lastActivityMu.RUnlock()
	return c.lastActivity
}

// Stop gracefully stops the component within the given timeout.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	if c.cancel != nil {
		c.cancel()
	}

	c.running = false
	c.logger.Info("Repository ingester stopped",
		"repos_ingested", c.reposIngested.Load(),
		"pulls_completed", c.pullsCompleted.Load(),
		"errors", c.errors.Load())

	return nil
}

// Discoverable interface implementation

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "repo-ingester",
		Type:        "processor",
		Description: "Git repository ingester for code indexing and knowledge graph population",
		Version:     "0.1.0",
	}
}

// InputPorts returns configured input port definitions.
func (c *Component) InputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}

	ports := make([]component.Port, len(c.config.Ports.Inputs))
	for i, portDef := range c.config.Ports.Inputs {
		ports[i] = buildPort(portDef, component.DirectionInput)
	}
	return ports
}

// OutputPorts returns configured output port definitions.
func (c *Component) OutputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}

	ports := make([]component.Port, len(c.config.Ports.Outputs))
	for i, portDef := range c.config.Ports.Outputs {
		ports[i] = buildPort(portDef, component.DirectionOutput)
	}
	return ports
}

// buildPort creates a component.Port from a PortDefinition.
func buildPort(portDef component.PortDefinition, direction component.Direction) component.Port {
	port := component.Port{
		Name:        portDef.Name,
		Direction:   direction,
		Required:    portDef.Required,
		Description: portDef.Description,
	}
	if portDef.Type == "jetstream" {
		port.Config = component.JetStreamPort{
			StreamName: portDef.StreamName,
			Subjects:   []string{portDef.Subject},
		}
	} else {
		port.Config = component.NATSPort{
			Subject: portDef.Subject,
		}
	}
	return port
}

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return repoIngesterSchema
}

// Health returns the current health status.
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	running := c.running
	startTime := c.startTime
	c.mu.RUnlock()

	return component.HealthStatus{
		Healthy:    running,
		LastCheck:  time.Now(),
		ErrorCount: int(c.errors.Load()),
		Uptime:     time.Since(startTime),
		Status:     c.getStatusString(running),
	}
}

// getStatusString returns a status string based on running state.
func (c *Component) getStatusString(running bool) string {
	if running {
		return "running"
	}
	return "stopped"
}

// DataFlow returns current data flow metrics.
func (c *Component) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{
		MessagesPerSecond: 0,
		BytesPerSecond:    0,
		ErrorRate:         0,
		LastActivity:      c.getLastActivity(),
	}
}
