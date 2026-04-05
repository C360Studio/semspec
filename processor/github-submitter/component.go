// Package githubsubmitter watches for plan completion (awaiting_review) and
// creates PRs, polls for review feedback, and handles PR merge detection.
// Disabled by default — opt-in via config (ADR-031).
package githubsubmitter

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/github"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// trackedPlan holds per-plan tracking state for review polling.
type trackedPlan struct {
	cancel    context.CancelFunc
	revisions int
}

// Component implements the github-submitter processor.
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

	// Tracked plans with open PRs (slug → tracking state).
	trackedPlans   map[string]*trackedPlan
	trackedPlansMu sync.Mutex

	// Metrics
	prsCreated     atomic.Int64
	feedbacksSent  atomic.Int64
	mergesDetected atomic.Int64
	errors         atomic.Int64
}

// NewComponent creates a new github-submitter processor.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	defaults := DefaultConfig()
	if config.RemoteName == "" {
		config.RemoteName = defaults.RemoteName
	}
	if config.BranchPrefix == "" {
		config.BranchPrefix = defaults.BranchPrefix
	}
	if config.ReviewPollInterval == "" {
		config.ReviewPollInterval = defaults.ReviewPollInterval
	}
	if config.MaxPRRevisions == 0 {
		config.MaxPRRevisions = defaults.MaxPRRevisions
	}
	if config.AutoAcceptFeedback == nil {
		config.AutoAcceptFeedback = defaults.AutoAcceptFeedback
	}
	if config.PlanStateBucket == "" {
		config.PlanStateBucket = defaults.PlanStateBucket
	}
	if config.StreamName == "" {
		config.StreamName = defaults.StreamName
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	ghClient, err := github.NewClient(config.GitHubToken, config.Repository)
	if err != nil {
		return nil, fmt.Errorf("create github client: %w", err)
	}

	return &Component{
		name:         "github-submitter",
		config:       config,
		natsClient:   deps.NATSClient,
		logger:       deps.GetLogger(),
		ghClient:     ghClient,
		trackedPlans: make(map[string]*trackedPlan),
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error { return nil }

// Start begins watching PLAN_STATES for GitHub plans reaching awaiting_review.
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

	js, err := c.natsClient.JetStream()
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("get jetstream: %w", err)
	}

	go c.watchPlanStates(subCtx, js)

	c.logger.Info("github-submitter started",
		"repository", c.config.Repository,
		"review_poll_interval", c.config.ReviewPollInterval,
		"max_pr_revisions", c.config.MaxPRRevisions)

	return nil
}

func (c *Component) rollbackStart(cancel context.CancelFunc) {
	c.mu.Lock()
	c.running = false
	c.cancel = nil
	c.mu.Unlock()
	cancel()
}

// watchPlanStates watches PLAN_STATES for GitHub plans reaching awaiting_review.
func (c *Component) watchPlanStates(ctx context.Context, js jetstream.JetStream) {
	bucket, err := workflow.WaitForKVBucket(ctx, js, c.config.PlanStateBucket)
	if err != nil {
		c.logger.Warn("PLAN_STATES bucket not available",
			"bucket", c.config.PlanStateBucket, "error", err)
		return
	}

	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		c.logger.Warn("Failed to watch PLAN_STATES",
			"bucket", c.config.PlanStateBucket, "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Watching PLAN_STATES for GitHub plans in awaiting_review",
		"bucket", c.config.PlanStateBucket)

	for entry := range watcher.Updates() {
		if entry == nil {
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}

		var plan workflow.Plan
		if err := json.Unmarshal(entry.Value(), &plan); err != nil {
			continue
		}

		// Only process GitHub-originated plans.
		if plan.GitHub == nil {
			continue
		}

		switch plan.Status {
		case workflow.StatusAwaitingReview:
			// Dedup: check trackedPlans to prevent duplicate PR creation on KV replay.
			c.trackedPlansMu.Lock()
			tp := c.trackedPlans[plan.Slug]
			if tp == nil && plan.GitHub.PRNumber == 0 {
				c.trackedPlans[plan.Slug] = &trackedPlan{} // placeholder until poller starts
			}
			alreadyTracked := tp != nil
			c.trackedPlansMu.Unlock()

			if !alreadyTracked && plan.GitHub.PRNumber == 0 {
				go c.createPR(ctx, &plan)
			}
			// If PR exists and plan returned to awaiting_review (re-execution complete),
			// push updated branch and comment.
			if plan.GitHub.PRNumber > 0 && plan.GitHub.PRRevision > 0 {
				go c.updatePR(ctx, &plan)
			}

		case workflow.StatusComplete:
			// Plan completed (PR merged) — stop tracking.
			c.stopTracking(plan.Slug)
		}
	}
}

// createPR pushes the plan branch and creates a pull request.
func (c *Component) createPR(ctx context.Context, plan *workflow.Plan) {
	if plan.GitHub.PlanBranch == "" {
		c.logger.Error("Cannot create PR: plan has no branch",
			"slug", plan.Slug)
		c.errors.Add(1)
		return
	}

	// Build PR body.
	prBody := BuildPRBody(plan)

	pr, err := c.ghClient.CreatePR(
		ctx,
		plan.GitHub.PlanBranch,
		"main", // TODO: make configurable (default branch)
		fmt.Sprintf("[semspec] %s", plan.Title),
		prBody,
		c.config.DraftPR,
	)
	if err != nil {
		c.logger.Error("Failed to create PR",
			"slug", plan.Slug, "branch", plan.GitHub.PlanBranch, "error", err)
		c.errors.Add(1)
		return
	}

	c.prsCreated.Add(1)
	c.logger.Info("PR created",
		"slug", plan.Slug, "pr_number", pr.Number, "url", pr.HTMLURL)

	// Publish PR created event (uses github.request.> wildcard on WORKFLOW stream).
	evt := &payloads.GitHubPRCreatedEvent{
		Slug:       plan.Slug,
		PRNumber:   pr.Number,
		PRURL:      pr.HTMLURL,
		Repository: c.config.Repository,
	}
	baseMsg := message.NewBaseMessage(evt.Schema(), evt, "github-submitter")
	if data, err := json.Marshal(baseMsg); err == nil {
		if pubErr := c.natsClient.PublishToStream(ctx, "github.request.pr.created", data); pubErr != nil {
			c.logger.Warn("Failed to publish PR created event", "error", pubErr)
		}
	}

	// Update plan metadata via plan-manager (request/reply to keep single-writer).
	c.updatePlanGitHubMetadata(ctx, plan.Slug, pr.Number, pr.HTMLURL)

	// Comment on source issue.
	if c.config.CommentOnTransitions && plan.GitHub.IssueNumber > 0 {
		comment := fmt.Sprintf("Pull request created: %s\n\nSemspec will monitor for review feedback.", pr.HTMLURL)
		if err := c.ghClient.CreateComment(ctx, plan.GitHub.IssueNumber, comment); err != nil {
			c.logger.Warn("Failed to comment on source issue",
				"issue", plan.GitHub.IssueNumber, "error", err)
		}
	}

	// Start review polling for this plan.
	c.startReviewPoller(ctx, plan.Slug, pr.Number)
}

// updatePR pushes updated branch and comments on the PR after a feedback round.
func (c *Component) updatePR(ctx context.Context, plan *workflow.Plan) {
	if plan.GitHub.PRNumber == 0 {
		return
	}

	comment := fmt.Sprintf("Semspec has applied PR review feedback (round %d) and re-executed affected requirements.\n\nPlease review the updated changes.", plan.GitHub.PRRevision)
	if err := c.ghClient.CreateComment(ctx, plan.GitHub.PRNumber, comment); err != nil {
		c.logger.Warn("Failed to comment on PR after re-execution",
			"pr", plan.GitHub.PRNumber, "error", err)
	}

	c.logger.Info("PR updated after feedback round",
		"slug", plan.Slug, "pr", plan.GitHub.PRNumber, "revision", plan.GitHub.PRRevision)
}

// updatePlanGitHubMetadata sends a plan update with PR metadata to plan-manager.
// Uses the force-complete endpoint which accepts awaiting_review plans.
// TODO: Use a dedicated mutation subject for metadata updates.
func (c *Component) updatePlanGitHubMetadata(ctx context.Context, slug string, prNumber int, prURL string) {
	data, _ := json.Marshal(map[string]any{
		"slug":      slug,
		"pr_number": prNumber,
		"pr_url":    prURL,
	})
	// Best-effort metadata update — the PR is created regardless.
	if _, err := c.natsClient.RequestWithRetry(ctx, "plan.mutation.github.pr_metadata", data, 5*time.Second, natsclient.DefaultRetryConfig()); err != nil {
		c.logger.Warn("Failed to update plan GitHub metadata",
			"slug", slug, "error", err)
	}
}

// startReviewPoller starts a goroutine that polls PR reviews for a tracked plan.
func (c *Component) startReviewPoller(ctx context.Context, slug string, prNumber int) {
	c.trackedPlansMu.Lock()
	if tp := c.trackedPlans[slug]; tp != nil && tp.cancel != nil {
		c.trackedPlansMu.Unlock()
		return // already tracking
	}
	pollerCtx, pollerCancel := context.WithCancel(ctx)
	c.trackedPlans[slug] = &trackedPlan{cancel: pollerCancel}
	c.trackedPlansMu.Unlock()

	go c.reviewPollLoop(pollerCtx, slug, prNumber)
}

// stopTracking stops the review poller for a plan.
func (c *Component) stopTracking(slug string) {
	c.trackedPlansMu.Lock()
	if tp, ok := c.trackedPlans[slug]; ok {
		if tp.cancel != nil {
			tp.cancel()
		}
		delete(c.trackedPlans, slug)
	}
	c.trackedPlansMu.Unlock()
}

// reviewPollLoop polls PR reviews and dispatches feedback.
func (c *Component) reviewPollLoop(ctx context.Context, slug string, prNumber int) {
	ticker := time.NewTicker(c.config.GetReviewPollInterval())
	defer ticker.Stop()

	var lastProcessedReviewID int64

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.pollReviews(ctx, slug, prNumber, &lastProcessedReviewID)
		}
	}
}

// pollReviews checks for new PR reviews and dispatches feedback requests.
func (c *Component) pollReviews(ctx context.Context, slug string, prNumber int, lastProcessedReviewID *int64) {
	// Check PR state first — stop if merged or closed.
	pr, err := c.ghClient.GetPR(ctx, prNumber)
	if err != nil {
		c.logger.Warn("Failed to get PR state", "pr", prNumber, "error", err)
		return
	}

	if pr.Merged {
		c.logger.Info("PR merged — publishing approval",
			"slug", slug, "pr", prNumber)
		c.publishReviewApproval(ctx, slug)
		c.mergesDetected.Add(1)
		c.stopTracking(slug)
		return
	}

	if pr.State == "closed" {
		c.logger.Info("PR closed without merge — stopping review polling",
			"slug", slug, "pr", prNumber)
		c.stopTracking(slug)
		return
	}

	// Fetch reviews.
	reviews, err := c.ghClient.ListReviews(ctx, prNumber)
	if err != nil {
		c.logger.Warn("Failed to list PR reviews", "pr", prNumber, "error", err)
		return
	}

	// Batch all unprocessed CHANGES_REQUESTED reviews.
	var feedbackReviews []github.Review
	for _, review := range reviews {
		if review.ID <= *lastProcessedReviewID {
			continue
		}
		if review.State == "CHANGES_REQUESTED" {
			feedbackReviews = append(feedbackReviews, review)
		}
		if review.ID > *lastProcessedReviewID {
			*lastProcessedReviewID = review.ID
		}
	}

	if len(feedbackReviews) == 0 {
		return
	}

	// Enforce max PR revisions.
	c.trackedPlansMu.Lock()
	tp := c.trackedPlans[slug]
	if tp != nil && tp.revisions >= c.config.MaxPRRevisions {
		c.trackedPlansMu.Unlock()
		c.logger.Warn("PR revision limit reached, stopping review polling",
			"slug", slug, "pr", prNumber, "max", c.config.MaxPRRevisions)
		comment := fmt.Sprintf("Semspec has reached the maximum revision limit (%d). Please review the current state and either merge, close, or manually re-trigger execution.", c.config.MaxPRRevisions)
		_ = c.ghClient.CreateComment(ctx, prNumber, comment)
		c.stopTracking(slug)
		return
	}
	if tp != nil {
		tp.revisions++
	}
	c.trackedPlansMu.Unlock()

	c.dispatchFeedback(ctx, slug, prNumber, feedbackReviews)
}

// dispatchFeedback aggregates review feedback and publishes a GitHubPRFeedbackRequest.
func (c *Component) dispatchFeedback(ctx context.Context, slug string, prNumber int, feedbackReviews []github.Review) {
	body, allComments := c.aggregateReviewFeedback(ctx, prNumber, feedbackReviews)
	lastReview := feedbackReviews[len(feedbackReviews)-1]

	req := &payloads.GitHubPRFeedbackRequest{
		Slug:     slug,
		PRNumber: prNumber,
		ReviewID: lastReview.ID,
		Reviewer: lastReview.User.Login,
		State:    "CHANGES_REQUESTED",
		Body:     body,
		Comments: allComments,
	}

	baseMsg := message.NewBaseMessage(req.Schema(), req, "github-submitter")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal PR feedback request", "error", err)
		return
	}

	if err := c.natsClient.PublishToStream(ctx, "plan.mutation.github.pr_feedback", data); err != nil {
		c.logger.Error("Failed to publish PR feedback request",
			"slug", slug, "pr", prNumber, "error", err)
		return
	}

	c.feedbacksSent.Add(1)
	c.logger.Info("PR feedback dispatched",
		"slug", slug, "pr", prNumber,
		"reviews", len(feedbackReviews),
		"comments", len(allComments))
}

// aggregateReviewFeedback collects review bodies and inline comments from
// multiple reviews into a single body string and comment list.
func (c *Component) aggregateReviewFeedback(ctx context.Context, prNumber int, reviews []github.Review) (string, []payloads.PRReviewComment) {
	var allComments []payloads.PRReviewComment
	var body string
	for _, review := range reviews {
		if review.Body != "" {
			if body != "" {
				body += "\n\n---\n\n"
			}
			body += fmt.Sprintf("**@%s:**\n%s", review.User.Login, review.Body)
		}

		comments, err := c.ghClient.ListReviewComments(ctx, prNumber, review.ID)
		if err != nil {
			c.logger.Warn("Failed to list review comments",
				"pr", prNumber, "review", review.ID, "error", err)
			continue
		}
		for _, rc := range comments {
			allComments = append(allComments, payloads.PRReviewComment{
				ID:       rc.ID,
				Path:     rc.Path,
				Line:     rc.Line,
				Body:     rc.Body,
				DiffHunk: rc.DiffHunk,
			})
		}
	}
	return body, allComments
}

// publishReviewApproval publishes plan.mutation.review.approve when a PR is merged.
func (c *Component) publishReviewApproval(ctx context.Context, slug string) {
	data, _ := json.Marshal(map[string]string{
		"slug":     slug,
		"reviewer": "github-pr-merge",
	})
	if _, err := c.natsClient.RequestWithRetry(ctx, "plan.mutation.review.approve", data, 5*time.Second, natsclient.DefaultRetryConfig()); err != nil {
		c.logger.Error("Failed to publish review approval on PR merge",
			"slug", slug, "error", err)
	}
}

// Stop gracefully stops the component.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	// Stop all review pollers.
	c.trackedPlansMu.Lock()
	for slug, tp := range c.trackedPlans {
		if tp.cancel != nil {
			tp.cancel()
		}
		delete(c.trackedPlans, slug)
	}
	c.trackedPlansMu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}
	c.running = false

	c.logger.Info("github-submitter stopped",
		"prs_created", c.prsCreated.Load(),
		"feedbacks_sent", c.feedbacksSent.Load(),
		"merges_detected", c.mergesDetected.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "github-submitter",
		Type:        "processor",
		Description: "Creates PRs from completed plans and handles PR review feedback (ADR-031)",
		Version:     "0.1.0",
	}
}

// InputPorts returns configured input port definitions.
func (c *Component) InputPorts() []component.Port { return nil }

// OutputPorts returns configured output port definitions.
func (c *Component) OutputPorts() []component.Port { return nil }

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema { return githubSubmitterSchema }

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
		ErrorCount: int(c.errors.Load()),
		Uptime:     time.Since(startTime),
		Status:     status,
	}
}

// DataFlow returns current data flow metrics.
func (c *Component) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{}
}

// IsRunning returns whether the component is currently running.
func (c *Component) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}
