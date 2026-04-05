// Package githubwatcher polls GitHub issues and creates plans from validated
// requests. Disabled by default — opt-in via config (ADR-031).
package githubwatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/github"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// Component implements the github-watcher processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger
	ghClient   *github.Client

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// State
	lastPollTime time.Time
	issuesBucket jetstream.KeyValue
	plansCreated atomic.Int64

	// Rate limiting
	recentPlans []time.Time // sliding window for rate limiting
	rateMu      sync.Mutex

	// Metrics
	pollCount     atomic.Int64
	issuesFound   atomic.Int64
	issuesSkipped atomic.Int64
	pollErrors    atomic.Int64
}

// NewComponent creates a new github-watcher processor.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	defaults := DefaultConfig()
	if config.PollInterval == "" {
		config.PollInterval = defaults.PollInterval
	}
	if config.IssueLabel == "" {
		config.IssueLabel = defaults.IssueLabel
	}
	if config.RequireLabel == nil {
		config.RequireLabel = defaults.RequireLabel
	}
	if config.MaxBodySize == 0 {
		config.MaxBodySize = defaults.MaxBodySize
	}
	if config.MaxPlansPerHour == 0 {
		config.MaxPlansPerHour = defaults.MaxPlansPerHour
	}
	if config.StreamName == "" {
		config.StreamName = defaults.StreamName
	}
	if config.TriggerSubject == "" {
		config.TriggerSubject = defaults.TriggerSubject
	}
	if config.IssuesBucket == "" {
		config.IssuesBucket = defaults.IssuesBucket
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	ghClient, err := github.NewClient(config.GitHubToken, config.Repository)
	if err != nil {
		return nil, fmt.Errorf("create github client: %w", err)
	}

	return &Component{
		name:       "github-watcher",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		ghClient:   ghClient,
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error { return nil }

// Start begins the issue polling loop.
func (c *Component) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("component already running")
	}
	c.running = true
	c.startTime = time.Now()

	subCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.mu.Unlock()

	// Initialize KV bucket for issue dedup.
	if c.natsClient != nil {
		js, err := c.natsClient.JetStream()
		if err == nil {
			bucket, kvErr := js.KeyValue(ctx, c.config.IssuesBucket)
			if kvErr != nil {
				// Try to create it.
				bucket, kvErr = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
					Bucket:      c.config.IssuesBucket,
					Description: "GitHub issue processing state (ADR-031)",
					TTL:         90 * 24 * time.Hour, // 90 days
				})
			}
			if kvErr == nil {
				c.issuesBucket = bucket
			} else {
				c.logger.Warn("GITHUB_ISSUES KV not available, dedup disabled",
					"bucket", c.config.IssuesBucket, "error", kvErr)
			}
		}
	}

	go c.pollLoop(subCtx)

	c.logger.Info("github-watcher started",
		"repository", c.config.Repository,
		"poll_interval", c.config.PollInterval,
		"label", c.config.IssueLabel)

	return nil
}

// pollLoop runs the issue polling loop until context is cancelled.
func (c *Component) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(c.config.GetPollInterval())
	defer ticker.Stop()

	// Initial poll immediately on start.
	c.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.poll(ctx)
		}
	}
}

// poll fetches new issues and processes them.
func (c *Component) poll(ctx context.Context) {
	c.pollCount.Add(1)

	issues, err := c.ghClient.ListIssues(ctx, c.lastPollTime, c.config.IssueLabel)
	if err != nil {
		c.pollErrors.Add(1)
		// Handle rate limiting gracefully.
		if _, ok := err.(*github.RateLimitError); ok {
			c.logger.Warn("GitHub API rate limited, will retry next interval", "error", err)
		} else {
			c.logger.Error("Failed to poll GitHub issues", "error", err)
		}
		return
	}

	c.lastPollTime = time.Now()

	for _, issue := range issues {
		if ctx.Err() != nil {
			return
		}
		c.processIssue(ctx, &issue)
	}
}

// processIssue validates and processes a single issue.
func (c *Component) processIssue(ctx context.Context, issue *github.Issue) {
	c.issuesFound.Add(1)

	// Gate: label requirement.
	if c.config.IsRequireLabel() && !issue.HasLabel(c.config.IssueLabel) {
		c.issuesSkipped.Add(1)
		return
	}

	// Gate: contributor whitelist.
	if c.config.RequireContributor && len(c.config.AllowedContributors) > 0 {
		if !slices.Contains(c.config.AllowedContributors, issue.User.Login) {
			c.logger.Debug("Issue skipped: contributor not in whitelist",
				"issue", issue.Number, "user", issue.User.Login)
			c.issuesSkipped.Add(1)
			return
		}
	}

	// Gate: body size.
	if len(issue.Body) > c.config.MaxBodySize {
		c.logger.Warn("Issue skipped: body too large",
			"issue", issue.Number, "size", len(issue.Body), "max", c.config.MaxBodySize)
		c.issuesSkipped.Add(1)
		return
	}

	// Gate: rate limit.
	if !c.checkRateLimit() {
		c.logger.Warn("Issue skipped: rate limit exceeded",
			"issue", issue.Number, "max_per_hour", c.config.MaxPlansPerHour)
		c.issuesSkipped.Add(1)
		return
	}

	// Gate: dedup via KV.
	issueKey := fmt.Sprintf("%s.%d", c.config.Repository, issue.Number)
	if c.issuesBucket != nil {
		if _, err := c.issuesBucket.Get(ctx, issueKey); err == nil {
			c.logger.Debug("Issue already processed, skipping",
				"issue", issue.Number, "key", issueKey)
			c.issuesSkipped.Add(1)
			return
		}
	}

	// Parse structured issue body.
	parsed := github.ParseIssueBody(issue.Body)
	description := parsed.Description
	if description == "" {
		description = issue.Body // fallback to raw body
	}

	issueURL := fmt.Sprintf("https://github.com/%s/issues/%d", c.config.Repository, issue.Number)

	// Publish plan creation request.
	req := &payloads.GitHubPlanCreationRequest{
		IssueNumber: issue.Number,
		IssueURL:    issueURL,
		Repository:  c.config.Repository,
		Title:       issue.Title,
		Description: description,
		Scope:       parsed.Scope,
		Constraints: parsed.Constraints,
		Priority:    parsed.Priority,
	}

	baseMsg := message.NewBaseMessage(req.Schema(), req, "github-watcher")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal plan creation request",
			"issue", issue.Number, "error", err)
		return
	}

	if err := c.natsClient.PublishToStream(ctx, c.config.TriggerSubject, data); err != nil {
		c.logger.Error("Failed to publish plan creation request",
			"issue", issue.Number, "subject", c.config.TriggerSubject, "error", err)
		return
	}

	// Record in KV for dedup.
	if c.issuesBucket != nil {
		record, _ := json.Marshal(map[string]any{
			"issue_number": issue.Number,
			"created_at":   time.Now().Format(time.RFC3339),
			"status":       "triggered",
		})
		if _, err := c.issuesBucket.Put(ctx, issueKey, record); err != nil {
			c.logger.Warn("Failed to record issue in KV", "key", issueKey, "error", err)
		}
	}

	// Record rate limit.
	c.recordPlanCreation()

	c.plansCreated.Add(1)
	c.logger.Info("Plan creation triggered from GitHub issue",
		"issue", issue.Number, "title", issue.Title, "key", issueKey)

	// Post acknowledgment comment on issue.
	comment := "Semspec is processing this issue. A plan will be created and executed automatically.\n\n---\n*Triggered by [semspec](https://github.com/c360studio/semspec)*"
	if err := c.ghClient.CreateComment(ctx, issue.Number, comment); err != nil {
		c.logger.Warn("Failed to post acknowledgment comment",
			"issue", issue.Number, "error", err)
	}
}

// checkRateLimit returns true if we haven't exceeded the plans-per-hour limit.
func (c *Component) checkRateLimit() bool {
	c.rateMu.Lock()
	defer c.rateMu.Unlock()

	cutoff := time.Now().Add(-1 * time.Hour)
	// Prune old entries.
	valid := c.recentPlans[:0]
	for _, t := range c.recentPlans {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	c.recentPlans = valid

	return len(c.recentPlans) < c.config.MaxPlansPerHour
}

// recordPlanCreation adds the current time to the rate limit window.
func (c *Component) recordPlanCreation() {
	c.rateMu.Lock()
	defer c.rateMu.Unlock()
	c.recentPlans = append(c.recentPlans, time.Now())
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

	c.logger.Info("github-watcher stopped",
		"plans_created", c.plansCreated.Load(),
		"polls", c.pollCount.Load(),
		"issues_found", c.issuesFound.Load(),
		"issues_skipped", c.issuesSkipped.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "github-watcher",
		Type:        "processor",
		Description: "Polls GitHub issues and creates plans from validated requests (ADR-031)",
		Version:     "0.1.0",
	}
}

// InputPorts returns configured input port definitions.
func (c *Component) InputPorts() []component.Port { return nil }

// OutputPorts returns configured output port definitions.
func (c *Component) OutputPorts() []component.Port { return nil }

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema { return githubWatcherSchema }

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
		ErrorCount: int(c.pollErrors.Load()),
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
	}
}

// IsRunning returns whether the component is currently running.
func (c *Component) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}
