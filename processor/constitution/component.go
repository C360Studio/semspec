// Package constitution provides a processor component for managing and enforcing
// project constitution rules. The constitution defines project-wide constraints
// that are checked during development workflows.
package constitution

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/message"
	"github.com/c360/semstreams/natsclient"
	"gopkg.in/yaml.v3"
)

// constitutionSchema defines the configuration schema
var constitutionSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Component implements the constitution processor
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger
	platform   component.PlatformMeta

	// Constitution data
	constitution *Constitution
	mu           sync.RWMutex

	// Lifecycle management
	running   bool
	startTime time.Time

	// Metrics
	checksPerformed int64
	violations      int64
	lastCheck       time.Time

	// Cancel functions for background goroutines
	cancelFuncs []context.CancelFunc
}

// NewComponent creates a new constitution processor component
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
		name:       "constitution",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		platform:   deps.Platform,
	}, nil
}

// Initialize prepares the component
func (c *Component) Initialize() error {
	// Load constitution from file if path is specified
	if c.config.FilePath != "" {
		if err := c.loadConstitutionFromFile(c.config.FilePath); err != nil {
			return fmt.Errorf("failed to load constitution: %w", err)
		}
	} else {
		// Create empty constitution
		c.constitution = NewConstitution(c.config.Org, c.config.Project, "v1")
	}

	return nil
}

// loadConstitutionFromFile loads constitution from a YAML or JSON file
func (c *Component) loadConstitutionFromFile(filePath string) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Parse constitution file
	var fileConfig ConstitutionFile
	ext := filepath.Ext(absPath)
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &fileConfig); err != nil {
			return fmt.Errorf("failed to parse YAML: %w", err)
		}
	case ".json":
		if err := json.Unmarshal(data, &fileConfig); err != nil {
			return fmt.Errorf("failed to parse JSON: %w", err)
		}
	default:
		return fmt.Errorf("unsupported file format: %s", ext)
	}

	// Convert to Constitution
	version := fileConfig.Version
	if version == "" {
		version = "v1"
	}
	constitution := NewConstitution(c.config.Org, c.config.Project, version)

	// Add rules from each section
	for sectionName, rules := range map[SectionName][]string{
		SectionCodeQuality:  fileConfig.CodeQuality,
		SectionTesting:      fileConfig.Testing,
		SectionSecurity:     fileConfig.Security,
		SectionArchitecture: fileConfig.Architecture,
	} {
		for i, ruleText := range rules {
			constitution.AddRule(sectionName, Rule{
				ID:       fmt.Sprintf("%s-%d", sectionName, i+1),
				Text:     ruleText,
				Priority: PriorityShould, // Default priority
				Enforced: true,
			})
		}
	}

	c.mu.Lock()
	c.constitution = constitution
	c.mu.Unlock()

	c.logger.Info("Loaded constitution from file",
		"path", absPath,
		"rules", len(constitution.AllRules()))

	return nil
}

// ConstitutionFile represents the file format for constitution
type ConstitutionFile struct {
	Version      string   `json:"version" yaml:"version"`
	CodeQuality  []string `json:"code_quality" yaml:"code_quality"`
	Testing      []string `json:"testing" yaml:"testing"`
	Security     []string `json:"security" yaml:"security"`
	Architecture []string `json:"architecture" yaml:"architecture"`
}

// Start begins the constitution processor
func (c *Component) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return fmt.Errorf("component already running")
	}

	if c.natsClient == nil {
		return fmt.Errorf("NATS client required")
	}

	// Publish initial constitution to graph
	if err := c.publishConstitution(ctx); err != nil {
		c.logger.Warn("Failed to publish initial constitution", "error", err)
	}

	// Start check request handler
	checkCtx, cancel := context.WithCancel(ctx)
	c.cancelFuncs = append(c.cancelFuncs, cancel)
	go c.handleCheckRequests(checkCtx)

	c.running = true
	c.startTime = time.Now()

	c.logger.Info("Constitution processor started",
		"project", c.config.Project,
		"rules", len(c.constitution.AllRules()),
		"enforce_mode", c.config.EnforceMode)

	return nil
}

// publishConstitution publishes the constitution to the graph
func (c *Component) publishConstitution(ctx context.Context) error {
	c.mu.RLock()
	constitution := c.constitution
	c.mu.RUnlock()

	if constitution == nil {
		return nil
	}

	// Convert to graph ingest message
	msg := EntityIngestMessage{
		ID:        constitution.ID,
		Triples:   constitution.Triples(),
		UpdatedAt: constitution.ModifiedAt,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal constitution: %w", err)
	}

	if err := c.natsClient.PublishToStream(ctx, "graph.ingest.entity", data); err != nil {
		return fmt.Errorf("failed to publish constitution: %w", err)
	}

	c.logger.Debug("Published constitution to graph", "id", constitution.ID)
	return nil
}

// EntityIngestMessage is the message format for graph ingestion
type EntityIngestMessage struct {
	ID        string           `json:"id"`
	Triples   []message.Triple `json:"triples"`
	UpdatedAt time.Time        `json:"updated_at"`
}

// handleCheckRequests handles incoming constitution check requests
func (c *Component) handleCheckRequests(ctx context.Context) {
	// Consume check requests from stream
	handler := func(data []byte) {
		var req CheckRequest
		if err := json.Unmarshal(data, &req); err != nil {
			c.logger.Warn("Invalid check request", "error", err)
			return
		}

		result := c.Check(req.Content, req.Context)

		// Publish result
		resultData, _ := json.Marshal(CheckResponse{
			RequestID: req.RequestID,
			Result:    result,
		})

		if err := c.natsClient.PublishToStream(ctx, "constitution.check.result", resultData); err != nil {
			c.logger.Warn("Failed to publish check result", "error", err)
		}
	}

	// ConsumeStream blocks until context is cancelled
	if err := c.natsClient.ConsumeStream(ctx, c.config.StreamName, "constitution.check.request", handler); err != nil {
		if ctx.Err() == nil {
			c.logger.Error("Failed to consume check requests", "error", err)
		}
	}
}

// CheckRequest represents a request to check content against the constitution
type CheckRequest struct {
	RequestID string            `json:"request_id"`
	Content   string            `json:"content"`
	Context   map[string]string `json:"context,omitempty"`
}

// CheckResponse represents the result of a constitution check
type CheckResponse struct {
	RequestID string       `json:"request_id"`
	Result    *CheckResult `json:"result"`
}

// Check performs a constitution check on the given content
func (c *Component) Check(content string, checkContext map[string]string) *CheckResult {
	c.mu.RLock()
	constitution := c.constitution
	c.mu.RUnlock()

	result := NewCheckResult()

	if constitution == nil {
		return result
	}

	// Update metrics
	c.mu.Lock()
	c.checksPerformed++
	c.lastCheck = time.Now()
	c.mu.Unlock()

	// Check content against enforced rules
	// This is a basic implementation - real checks would be more sophisticated
	for sectionName, rules := range constitution.Sections {
		for _, rule := range rules {
			if !rule.Enforced {
				continue
			}

			// Basic keyword checking - real implementation would use more sophisticated analysis
			if !c.checkRule(rule, content, checkContext) {
				violation := Violation{
					Rule:    rule,
					Section: sectionName,
					Message: fmt.Sprintf("Content may violate rule: %s", rule.Text),
				}

				if rule.Priority == PriorityMust {
					result.AddViolation(violation)
					c.mu.Lock()
					c.violations++
					c.mu.Unlock()
				} else {
					result.AddWarning(violation)
				}
			}
		}
	}

	return result
}

// checkRule checks if content complies with a rule
// This is a placeholder for more sophisticated rule checking
func (c *Component) checkRule(rule Rule, content string, checkContext map[string]string) bool {
	// Basic implementation - always returns true
	// Real implementation would analyze content against rule requirements
	return true
}

// GetConstitution returns the current constitution
func (c *Component) GetConstitution() *Constitution {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.constitution
}

// UpdateConstitution updates the constitution
func (c *Component) UpdateConstitution(constitution *Constitution) {
	c.mu.Lock()
	c.constitution = constitution
	c.mu.Unlock()
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
	c.logger.Info("Constitution processor stopped",
		"checks", c.checksPerformed,
		"violations", c.violations)

	return nil
}

// Discoverable interface implementation

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "constitution",
		Type:        "processor",
		Description: "Constitution enforcement for project-wide constraints",
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
	return constitutionSchema
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
		LastActivity:      c.lastCheck,
	}
}
