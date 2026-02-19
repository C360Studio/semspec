// Package workflowvalidator provides a request/reply service for validating
// workflow documents against their type requirements.
package workflowvalidator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/workflow/validation"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

// Component implements the workflow-validator processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	validator *validation.Validator
	baseDir   string

	// Request subject
	requestSubject string

	// Lifecycle
	running      bool
	startTime    time.Time
	mu           sync.RWMutex
	cancel       context.CancelFunc
	subscription *natsclient.Subscription

	// Metrics
	requestsProcessed atomic.Int64
	validationsPassed atomic.Int64
	validationsFailed atomic.Int64
	lastActivityMu    sync.RWMutex
	lastActivity      time.Time
}

// NewComponent creates a new workflow-validator processor.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults if not specified
	defaults := DefaultConfig()
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}
	if config.TimeoutSecs == 0 {
		config.TimeoutSecs = defaults.TimeoutSecs
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Resolve base directory
	baseDir := config.BaseDir
	if baseDir == "" {
		baseDir = os.Getenv("SEMSPEC_REPO_PATH")
	}
	if baseDir == "" {
		var err error
		baseDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working directory: %w", err)
		}
	}

	// Resolve request subject from port definitions
	requestSubject := "workflow.validate.*"
	if config.Ports != nil && len(config.Ports.Inputs) > 0 {
		requestSubject = config.Ports.Inputs[0].Subject
	}

	return &Component{
		name:           "workflow-validator",
		config:         config,
		natsClient:     deps.NATSClient,
		logger:         deps.GetLogger(),
		validator:      validation.NewValidator(),
		baseDir:        baseDir,
		requestSubject: requestSubject,
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized workflow-validator",
		"base_dir", c.baseDir,
		"request_subject", c.requestSubject)
	return nil
}

// Start begins handling validation requests.
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

	// Set running state while holding lock to prevent race condition
	c.running = true
	c.startTime = time.Now()

	subCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.mu.Unlock()

	// Subscribe to validation requests using SubscribeForRequests
	sub, err := c.natsClient.SubscribeForRequests(subCtx, c.requestSubject, c.handleRequest)
	if err != nil {
		// Rollback running state on failure
		c.mu.Lock()
		c.running = false
		c.cancel = nil
		c.mu.Unlock()
		cancel()
		return fmt.Errorf("subscribe to %s: %w", c.requestSubject, err)
	}

	c.mu.Lock()
	c.subscription = sub
	c.mu.Unlock()

	c.logger.Info("workflow-validator started",
		"subject", c.requestSubject,
		"base_dir", c.baseDir)

	return nil
}

// handleRequest processes a validation request and returns response data.
// Accepts both raw ValidateRequest JSON and BaseMessage-wrapped requests.
func (c *Component) handleRequest(ctx context.Context, data []byte) ([]byte, error) {
	// Check for cancellation before processing.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.requestsProcessed.Add(1)
	c.updateLastActivity()

	preview := string(data)
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	c.logger.Debug("Received validation request", "size", len(data), "preview", preview)

	// Try to parse as raw ValidateRequest first (from workflow call actions)
	var req ValidateRequest
	if err := json.Unmarshal(data, &req); err == nil && (req.Document != "" || req.Content != "" || req.Path != "") {
		// Successfully parsed as direct request
		c.logger.Debug("Parsed as raw ValidateRequest", "document", req.Document, "has_content", req.Content != "")
	} else {
		// Try to parse as BaseMessage-wrapped request
		var baseMsg message.BaseMessage
		if err := json.Unmarshal(data, &baseMsg); err != nil {
			return c.errorResponse("failed to parse request: " + err.Error())
		}

		// Extract request payload
		payloadBytes, err := json.Marshal(baseMsg.Payload())
		if err != nil {
			return c.errorResponse("failed to marshal payload: " + err.Error())
		}
		if err := json.Unmarshal(payloadBytes, &req); err != nil {
			return c.errorResponse("failed to unmarshal request: " + err.Error())
		}
	}

	// Validate request
	if err := req.Validate(); err != nil {
		return c.errorResponse(err.Error())
	}

	// Get content
	content := req.Content
	if content == "" && req.Path != "" {
		// Read from file with path traversal protection
		filePath := req.Path
		if !filepath.IsAbs(filePath) {
			filePath = filepath.Join(c.baseDir, filePath)
		}

		// Clean the path and verify it stays within baseDir
		filePath = filepath.Clean(filePath)
		absBaseDir, err := filepath.Abs(c.baseDir)
		if err != nil {
			return c.errorResponse("failed to resolve base directory: " + err.Error())
		}
		absFilePath, err := filepath.Abs(filePath)
		if err != nil {
			return c.errorResponse("failed to resolve file path: " + err.Error())
		}

		// Ensure the resolved path is within the base directory
		if !isPathWithin(absFilePath, absBaseDir) {
			c.logger.Warn("Path traversal attempt blocked",
				"requested_path", req.Path,
				"resolved_path", absFilePath,
				"base_dir", absBaseDir)
			return c.errorResponse("path must be within repository directory")
		}

		fileData, err := os.ReadFile(filePath)
		if err != nil {
			return c.errorResponse("failed to read file: " + err.Error())
		}
		content = string(fileData)
	}

	// Determine document type from request
	docType := validation.DocumentType(req.Document)

	// Validate document
	result := c.validator.Validate(content, docType)

	// Track metrics
	if result.Valid {
		c.validationsPassed.Add(1)
	} else {
		c.validationsFailed.Add(1)
	}

	// Build response
	response := FromValidationResult(result)

	c.logger.Debug("Validated document",
		"slug", req.Slug,
		"document", req.Document,
		"valid", result.Valid)

	return c.marshalResponse(response)
}

// marshalResponse marshals a validation response.
// For request/reply services, we return the raw payload without BaseMessage wrapper
// so workflow interpolation can access fields directly (e.g., ${steps.validate_plan.output.valid})
func (c *Component) marshalResponse(response *ValidateResponse) ([]byte, error) {
	return json.Marshal(response)
}

// errorResponse builds an error response.
func (c *Component) errorResponse(errMsg string) ([]byte, error) {
	response := &ValidateResponse{
		Valid: false,
		Error: errMsg,
	}
	return c.marshalResponse(response)
}

// Stop gracefully stops the component.
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
	c.logger.Info("workflow-validator stopped",
		"requests_processed", c.requestsProcessed.Load(),
		"validations_passed", c.validationsPassed.Load(),
		"validations_failed", c.validationsFailed.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "workflow-validator",
		Type:        "processor",
		Description: "Request/reply service for validating workflow documents",
		Version:     "1.0.0",
	}
}

// InputPorts returns configured input port definitions.
func (c *Component) InputPorts() []component.Port {
	if c.config.Ports == nil {
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

// OutputPorts returns configured output port definitions.
func (c *Component) OutputPorts() []component.Port {
	if c.config.Ports == nil {
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

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return workflowValidatorSchema
}

// Health returns the current health status.
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	running := c.running
	startTime := c.startTime
	c.mu.RUnlock()

	status := "stopped"
	if running {
		status = "running"
	}

	return component.HealthStatus{
		Healthy:    running,
		LastCheck:  time.Now(),
		ErrorCount: 0,
		Uptime:     time.Since(startTime),
		Status:     status,
	}
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

func (c *Component) updateLastActivity() {
	c.lastActivityMu.Lock()
	c.lastActivity = time.Now()
	c.lastActivityMu.Unlock()
}

func (c *Component) getLastActivity() time.Time {
	c.lastActivityMu.RLock()
	defer c.lastActivityMu.RUnlock()
	return c.lastActivity
}

// isPathWithin checks if child path is within the parent directory.
// Both paths should be absolute and cleaned.
func isPathWithin(child, parent string) bool {
	// Ensure parent ends with separator for accurate prefix matching
	if !strings.HasSuffix(parent, string(filepath.Separator)) {
		parent = parent + string(filepath.Separator)
	}
	return strings.HasPrefix(child, parent) || child == strings.TrimSuffix(parent, string(filepath.Separator))
}
