package workflowapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/prompts"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

// RegisterHTTPHandlers registers HTTP handlers for the workflow-api component.
// The prefix may or may not include trailing slash.
// This includes both workflow endpoints and Q&A endpoints.
func (c *Component) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	// Ensure prefix has trailing slash
	if !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}

	// Workflow endpoints
	mux.HandleFunc(prefix+"plans", c.handlePlans)
	mux.HandleFunc(prefix+"plans/", c.handlePlansWithSlug)

	// Q&A endpoints (delegated to question handler)
	// These are registered at /workflow-api/questions/* instead of /questions/*
	// to keep them scoped under this component's prefix
	c.mu.RLock()
	questionHandler := c.questionHandler
	c.mu.RUnlock()

	if questionHandler != nil {
		questionHandler.RegisterHTTPHandlers(prefix+"questions", mux)
	}
}

// WorkflowExecution represents a workflow execution from the KV bucket.
// This mirrors the semstreams workflow execution structure.
type WorkflowExecution struct {
	ID           string                 `json:"id"`
	WorkflowID   string                 `json:"workflow_id"`
	WorkflowName string                 `json:"workflow_name"`
	State        string                 `json:"state"`
	StepResults  map[string]*StepResult `json:"step_results"`
	Trigger      json.RawMessage        `json:"trigger"`
	Error        string                 `json:"error,omitempty"`
	CreatedAt    string                 `json:"created_at"`
	UpdatedAt    string                 `json:"updated_at"`
}

// StepResult represents a single step's result within an execution.
type StepResult struct {
	StepName  string          `json:"step_name"`
	Status    string          `json:"status"`
	Output    json.RawMessage `json:"output"`
	Error     string          `json:"error,omitempty"`
	Iteration int             `json:"iteration"`
}

// TriggerPayload represents the trigger data structure.
type TriggerPayload struct {
	WorkflowID string       `json:"workflow_id"`
	Data       *TriggerData `json:"data,omitempty"`
}

// TriggerData contains semspec-specific fields.
type TriggerData struct {
	Slug        string `json:"slug,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// handleGetPlanReviews handles GET /plans/{slug}/reviews
// Returns the review synthesis result for the given plan slug.
func (c *Component) handleGetPlanReviews(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract slug from path: /workflow-api/plans/{slug}/reviews
	slug, endpoint := extractSlugAndEndpoint(r.URL.Path)
	if slug == "" {
		http.Error(w, "Plan slug required", http.StatusBadRequest)
		return
	}

	// Only handle /reviews endpoint
	if endpoint != "reviews" {
		http.Error(w, "Unknown endpoint", http.StatusNotFound)
		return
	}

	// Get execution bucket
	bucket, err := c.getExecBucket(r.Context())
	if err != nil {
		c.logger.Error("Failed to get execution bucket", "error", err)
		http.Error(w, "Workflow executions not available", http.StatusServiceUnavailable)
		return
	}

	// Find execution by slug
	exec, err := c.findExecutionBySlug(r.Context(), bucket, slug)
	if err != nil {
		c.logger.Error("Failed to find execution", "slug", slug, "error", err)
		http.Error(w, "Failed to retrieve execution", http.StatusInternalServerError)
		return
	}

	if exec == nil {
		http.Error(w, "Review not found", http.StatusNotFound)
		return
	}

	// Get review step result
	reviewResult := c.findReviewResult(exec)
	if reviewResult == nil {
		http.Error(w, "No completed review", http.StatusNotFound)
		return
	}

	// Return the review output directly (it's already SynthesisResult JSON)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(reviewResult.Output); err != nil {
		c.logger.Warn("Failed to write response", "error", err)
	}
}

// maxExecutionsToScan limits the number of executions to scan to prevent unbounded iteration.
const maxExecutionsToScan = 500

// maxJSONBodySize limits the size of JSON request bodies to prevent DoS.
const maxJSONBodySize = 1 << 20 // 1MB

// getManager returns a workflow manager with the correct repo root.
// Returns nil and writes an HTTP error response if initialization fails.
func (c *Component) getManager(w http.ResponseWriter) *workflow.Manager {
	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			c.logger.Error("Failed to get working directory", "error", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return nil
		}
	}
	return workflow.NewManager(repoRoot)
}

// findExecutionBySlug searches for a completed workflow execution with the given slug.
func (c *Component) findExecutionBySlug(ctx context.Context, bucket jetstream.KeyValue, slug string) (*WorkflowExecution, error) {
	if bucket == nil {
		return nil, nil
	}

	// List all keys
	keys, err := bucket.Keys(ctx)
	if err != nil {
		// No keys or empty bucket - return nil
		if errors.Is(err, jetstream.ErrNoKeysFound) {
			return nil, nil
		}
		return nil, err
	}

	var latestExec *WorkflowExecution

	for i, key := range keys {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Limit iterations to prevent unbounded scanning
		if i >= maxExecutionsToScan {
			c.logger.Warn("Execution scan limit reached", "limit", maxExecutionsToScan, "slug", slug)
			break
		}

		// Skip secondary index keys (e.g., TASK_xxx)
		if strings.HasPrefix(key, "TASK_") {
			continue
		}

		entry, err := bucket.Get(ctx, key)
		if err != nil {
			continue
		}

		var exec WorkflowExecution
		if err := json.Unmarshal(entry.Value(), &exec); err != nil {
			continue
		}

		// Parse trigger to check slug
		var trigger TriggerPayload
		if err := json.Unmarshal(exec.Trigger, &trigger); err != nil {
			continue
		}

		// Check if slug matches
		if trigger.Data != nil && trigger.Data.Slug == slug {
			// Check if this is a review workflow with completed state
			if exec.State == "completed" || exec.State == "running" {
				// Check if it has a review result
				if c.findReviewResult(&exec) != nil {
					// Return most recent completed one
					if latestExec == nil || exec.UpdatedAt > latestExec.UpdatedAt {
						execCopy := exec
						latestExec = &execCopy
					}
				}
			}
		}
	}

	return latestExec, nil
}

// findReviewResult looks for a completed review step result in the execution.
func (c *Component) findReviewResult(exec *WorkflowExecution) *StepResult {
	if exec.StepResults == nil {
		return nil
	}

	// Look for a step named "review" with success status
	if result, ok := exec.StepResults["review"]; ok && result.Status == "success" {
		return result
	}

	// Also check for "review-synthesis" or similar variants
	for name, result := range exec.StepResults {
		if strings.Contains(strings.ToLower(name), "review") && result.Status == "success" {
			// Verify it has output that looks like SynthesisResult
			if len(result.Output) > 0 {
				return result
			}
		}
	}

	return nil
}

// extractSlugAndEndpoint extracts slug and endpoint from path like /workflow-api/plans/{slug}/reviews
func extractSlugAndEndpoint(path string) (slug, endpoint string) {
	// Find /plans/ in the path
	idx := strings.Index(path, "/plans/")
	if idx == -1 {
		return "", ""
	}

	// Get everything after /plans/
	remainder := path[idx+len("/plans/"):]

	// Split by / to get slug and endpoint
	parts := strings.SplitN(remainder, "/", 2)
	if len(parts) == 0 {
		return "", ""
	}

	slug = parts[0]
	if len(parts) > 1 {
		endpoint = strings.TrimSuffix(parts[1], "/")
	}

	return slug, endpoint
}

// CreatePlanRequest is the request body for POST /plans.
type CreatePlanRequest struct {
	Description string `json:"description"`
}

// CreatePlanResponse is the response body for POST /plans.
type CreatePlanResponse struct {
	Slug      string `json:"slug"`
	RequestID string `json:"request_id"`
	TraceID   string `json:"trace_id"`
	Message   string `json:"message"`
}

// PlanWithStatus represents a plan with its current workflow status.
// This is the response format for GET /plans and GET /plans/{slug}.
type PlanWithStatus struct {
	*workflow.Plan
	Stage       string             `json:"stage"`
	ActiveLoops []ActiveLoopStatus `json:"active_loops"`
}

// ActiveLoopStatus represents an active agent loop for a plan.
type ActiveLoopStatus struct {
	LoopID string `json:"loop_id"`
	Role   string `json:"role"`
	State  string `json:"state"`
}

// AsyncOperationResponse is the response body for async operations like task generation.
type AsyncOperationResponse struct {
	Slug      string `json:"slug"`
	RequestID string `json:"request_id"`
	TraceID   string `json:"trace_id"`
	Message   string `json:"message"`
}

// handlePlans handles POST /workflow-api/plans (create plan).
func (c *Component) handlePlans(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		c.handleCreatePlan(w, r)
	case http.MethodGet:
		c.handleListPlans(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePlansWithSlug handles /workflow-api/plans/{slug}/*
func (c *Component) handlePlansWithSlug(w http.ResponseWriter, r *http.Request) {
	slug, endpoint := extractSlugAndEndpoint(r.URL.Path)
	if slug == "" {
		http.Error(w, "Plan slug required", http.StatusBadRequest)
		return
	}

	// Validate slug format at HTTP boundary
	if err := workflow.ValidateSlug(slug); err != nil {
		http.Error(w, "Invalid plan slug format", http.StatusBadRequest)
		return
	}

	switch endpoint {
	case "":
		// GET /plans/{slug}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		c.handleGetPlan(w, r, slug)
	case "promote":
		// POST /plans/{slug}/promote
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		c.handlePromotePlan(w, r, slug)
	case "tasks":
		// GET /plans/{slug}/tasks or POST /plans/{slug}/tasks/generate
		c.handlePlanTasks(w, r, slug)
	case "tasks/generate":
		// POST /plans/{slug}/tasks/generate
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		c.handleGenerateTasks(w, r, slug)
	case "tasks/approve":
		// POST /plans/{slug}/tasks/approve
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		c.handleApproveTasksPlan(w, r, slug)
	case "execute":
		// POST /plans/{slug}/execute
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		c.handleExecutePlan(w, r, slug)
	case "reviews":
		// GET /plans/{slug}/reviews
		c.handleGetPlanReviews(w, r)
	default:
		http.Error(w, "Unknown endpoint", http.StatusNotFound)
	}
}

// handleCreatePlan handles POST /workflow-api/plans.
// Creates a new plan and triggers the planner agent loop.
func (c *Component) handleCreatePlan(w http.ResponseWriter, r *http.Request) {
	// Limit request body size to prevent DoS
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req CreatePlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Description == "" {
		http.Error(w, "description is required", http.StatusBadRequest)
		return
	}

	manager := c.getManager(w)
	if manager == nil {
		return // Error already written
	}

	// Generate slug from description
	slug := workflow.Slugify(req.Description)

	// Create trace context early so we use it consistently
	tc := natsclient.NewTraceContext()
	ctx := natsclient.ContextWithTrace(r.Context(), tc)

	// Check if plan already exists
	if manager.PlanExists(slug) {
		// Load and return existing plan
		plan, err := manager.LoadPlan(ctx, slug)
		if err != nil {
			c.logger.Error("Failed to load existing plan", "slug", slug, "error", err)
			http.Error(w, "Failed to load existing plan", http.StatusInternalServerError)
			return
		}

		resp := &PlanWithStatus{
			Plan:  plan,
			Stage: c.determinePlanStage(plan),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			c.logger.Warn("Failed to encode response", "error", err)
		}
		return
	}

	// Create new plan
	plan, err := manager.CreatePlan(ctx, slug, req.Description)
	if err != nil {
		c.logger.Error("Failed to create plan", "slug", slug, "error", err)
		http.Error(w, fmt.Sprintf("Failed to create plan: %v", err), http.StatusInternalServerError)
		return
	}

	c.logger.Info("Created plan via REST API", "slug", slug, "plan_id", plan.ID)

	// Trigger plan-review-loop workflow (ADR-005 OODA feedback loop).
	// The workflow-processor handles: planner → reviewer → revise with findings → re-review.
	requestID := uuid.New().String()

	triggerPayload := &workflow.TriggerPayload{
		WorkflowID: "plan-review-loop",
		Role:       "planner",
		Prompt:     plan.Title,
		RequestID:  requestID,
		TraceID:    tc.TraceID,
		Data: &workflow.TriggerData{
			Slug:        plan.Slug,
			Title:       plan.Title,
			Description: plan.Title,
			ProjectID:   plan.ProjectID,
			TraceID:     tc.TraceID,
		},
	}

	baseMsg := message.NewBaseMessage(
		workflow.WorkflowTriggerType,
		triggerPayload,
		"workflow-api",
	)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal plan-review-loop trigger", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Publish trigger to JetStream — workflow-processor handles the OODA loop
	if err := c.natsClient.PublishToStream(ctx, "workflow.trigger.plan-review-loop", data); err != nil {
		c.logger.Error("Failed to trigger plan-review-loop workflow", "error", err)
		http.Error(w, "Failed to start planning", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Triggered plan-review-loop workflow",
		"request_id", requestID,
		"slug", plan.Slug,
		"trace_id", tc.TraceID)

	resp := &CreatePlanResponse{
		Slug:      plan.Slug,
		RequestID: requestID,
		TraceID:   tc.TraceID,
		Message:   "Plan created, generating Goal/Context/Scope",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleListPlans handles GET /workflow-api/plans.
func (c *Component) handleListPlans(w http.ResponseWriter, r *http.Request) {
	manager := c.getManager(w)
	if manager == nil {
		return // Error already written
	}

	result, err := manager.ListPlans(r.Context())
	if err != nil {
		c.logger.Error("Failed to list plans", "error", err)
		http.Error(w, "Failed to list plans", http.StatusInternalServerError)
		return
	}

	// Convert to PlanWithStatus
	plans := make([]*PlanWithStatus, 0, len(result.Plans))
	for _, plan := range result.Plans {
		plans = append(plans, &PlanWithStatus{
			Plan:  plan,
			Stage: c.determinePlanStage(plan),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(plans); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleGetPlan handles GET /workflow-api/plans/{slug}.
func (c *Component) handleGetPlan(w http.ResponseWriter, r *http.Request, slug string) {
	manager := c.getManager(w)
	if manager == nil {
		return // Error already written
	}

	plan, err := manager.LoadPlan(r.Context(), slug)
	if err != nil {
		if errors.Is(err, workflow.ErrPlanNotFound) {
			http.Error(w, "Plan not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to load plan", "slug", slug, "error", err)
		http.Error(w, "Failed to load plan", http.StatusInternalServerError)
		return
	}

	resp := &PlanWithStatus{
		Plan:  plan,
		Stage: c.determinePlanStage(plan),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handlePromotePlan handles POST /workflow-api/plans/{slug}/promote.
// With the plan-review-loop workflow (ADR-005), the review happens automatically
// via the workflow-processor. This endpoint returns the current plan stage.
// If the plan is already approved, it returns immediately.
// If the workflow is still running the review loop, it returns the current stage.
func (c *Component) handlePromotePlan(w http.ResponseWriter, r *http.Request, slug string) {
	manager := c.getManager(w)
	if manager == nil {
		return // Error already written
	}

	plan, err := manager.LoadPlan(r.Context(), slug)
	if err != nil {
		if errors.Is(err, workflow.ErrPlanNotFound) {
			http.Error(w, "Plan not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to load plan", "slug", slug, "error", err)
		http.Error(w, "Failed to load plan", http.StatusInternalServerError)
		return
	}

	// Return current plan stage — the workflow-processor handles the review loop.
	resp := &PlanWithStatus{
		Plan:  plan,
		Stage: c.determinePlanStage(plan),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handlePlanTasks handles GET /workflow-api/plans/{slug}/tasks.
func (c *Component) handlePlanTasks(w http.ResponseWriter, r *http.Request, slug string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	manager := c.getManager(w)
	if manager == nil {
		return // Error already written
	}

	tasks, err := manager.LoadTasks(r.Context(), slug)
	if err != nil {
		// Tasks might not exist yet - return empty array
		c.logger.Debug("No tasks found", "slug", slug, "error", err)
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte("[]")); err != nil {
			c.logger.Warn("Failed to write response", "error", err)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(tasks); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleGenerateTasks handles POST /workflow-api/plans/{slug}/tasks/generate.
func (c *Component) handleGenerateTasks(w http.ResponseWriter, r *http.Request, slug string) {
	manager := c.getManager(w)
	if manager == nil {
		return // Error already written
	}

	// Create trace context early for consistent usage
	tc := natsclient.NewTraceContext()
	ctx := natsclient.ContextWithTrace(r.Context(), tc)

	plan, err := manager.LoadPlan(ctx, slug)
	if err != nil {
		if errors.Is(err, workflow.ErrPlanNotFound) {
			http.Error(w, "Plan not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to load plan", "slug", slug, "error", err)
		http.Error(w, "Failed to load plan", http.StatusInternalServerError)
		return
	}

	// Check if plan is approved
	if !plan.Approved {
		http.Error(w, "Plan must be approved before generating tasks", http.StatusBadRequest)
		return
	}

	// Trigger task generator
	requestID := uuid.New().String()

	fullPrompt := prompts.TaskGeneratorPrompt(prompts.TaskGeneratorParams{
		Goal:           plan.Goal,
		Context:        plan.Context,
		ScopeInclude:   plan.Scope.Include,
		ScopeExclude:   plan.Scope.Exclude,
		ScopeProtected: plan.Scope.DoNotTouch,
		Title:          plan.Title,
	})

	triggerPayload := &workflow.WorkflowTriggerPayload{
		WorkflowID: "task-generator",
		Role:       "task-generator",
		Prompt:     fullPrompt,
		RequestID:  requestID,
		TraceID:    tc.TraceID,
		Data: &workflow.WorkflowTriggerData{
			Slug:        plan.Slug,
			Title:       plan.Title,
			Description: plan.Goal,
			Auto:        true,
		},
	}

	baseMsg := message.NewBaseMessage(
		workflow.WorkflowTriggerType,
		triggerPayload,
		"workflow-api",
	)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal task generator trigger", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if err := c.natsClient.PublishToStream(ctx, "workflow.trigger.task-generator", data); err != nil {
		c.logger.Error("Failed to publish task generator trigger", "error", err)
		http.Error(w, "Failed to start task generation", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Triggered task generation via REST API",
		"request_id", requestID,
		"slug", plan.Slug,
		"trace_id", tc.TraceID)

	resp := &AsyncOperationResponse{
		Slug:      plan.Slug,
		RequestID: requestID,
		TraceID:   tc.TraceID,
		Message:   "Task generation started",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleExecutePlan handles POST /workflow-api/plans/{slug}/execute.
func (c *Component) handleExecutePlan(w http.ResponseWriter, r *http.Request, slug string) {
	manager := c.getManager(w)
	if manager == nil {
		return // Error already written
	}

	// Create trace context early for consistent usage
	tc := natsclient.NewTraceContext()
	ctx := natsclient.ContextWithTrace(r.Context(), tc)

	plan, err := manager.LoadPlan(ctx, slug)
	if err != nil {
		if errors.Is(err, workflow.ErrPlanNotFound) {
			http.Error(w, "Plan not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to load plan", "slug", slug, "error", err)
		http.Error(w, "Failed to load plan", http.StatusInternalServerError)
		return
	}

	// Check if plan is approved
	if !plan.Approved {
		http.Error(w, "Plan must be approved before execution", http.StatusBadRequest)
		return
	}

	// For plans using the new status field, require tasks to be approved.
	// Legacy plans (Status empty) still work with just Approved=true.
	// Note: once plan.Status is set to any value (e.g., by ApprovePlan or
	// task-generator), the plan permanently requires task approval for execution.
	if plan.Status != "" && !plan.TasksApproved {
		http.Error(w, "Tasks must be approved before execution", http.StatusBadRequest)
		return
	}

	// Trigger batch task dispatcher
	requestID := uuid.New().String()
	batchID := uuid.New().String()

	triggerPayload := &workflow.BatchTriggerPayload{
		RequestID: requestID,
		Slug:      plan.Slug,
		BatchID:   batchID,
	}

	baseMsg := message.NewBaseMessage(
		workflow.BatchTriggerType,
		triggerPayload,
		"workflow-api",
	)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal batch trigger", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if err := c.natsClient.PublishToStream(ctx, "workflow.trigger.task-dispatcher", data); err != nil {
		c.logger.Error("Failed to publish batch trigger", "error", err)
		http.Error(w, "Failed to start execution", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Triggered plan execution via REST API",
		"request_id", requestID,
		"batch_id", batchID,
		"slug", plan.Slug,
		"trace_id", tc.TraceID)

	resp := &PlanWithStatus{
		Plan:  plan,
		Stage: "executing",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleApproveTasksPlan handles POST /workflow-api/plans/{slug}/tasks/approve.
// Approves the generated tasks for execution.
func (c *Component) handleApproveTasksPlan(w http.ResponseWriter, r *http.Request, slug string) {
	manager := c.getManager(w)
	if manager == nil {
		return // Error already written
	}

	plan, err := manager.LoadPlan(r.Context(), slug)
	if err != nil {
		if errors.Is(err, workflow.ErrPlanNotFound) {
			http.Error(w, "Plan not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to load plan", "slug", slug, "error", err)
		http.Error(w, "Failed to load plan", http.StatusInternalServerError)
		return
	}

	// Check preconditions
	if !plan.Approved {
		http.Error(w, "Plan must be approved before approving tasks", http.StatusBadRequest)
		return
	}

	// Verify tasks exist
	tasks, err := manager.LoadTasks(r.Context(), slug)
	if err != nil || len(tasks) == 0 {
		http.Error(w, "Tasks must be generated before they can be approved", http.StatusBadRequest)
		return
	}

	// Approve tasks
	if err := manager.ApproveTasksPlan(r.Context(), plan); err != nil {
		if errors.Is(err, workflow.ErrTasksAlreadyApproved) {
			http.Error(w, "Tasks are already approved", http.StatusConflict)
			return
		}
		c.logger.Error("Failed to approve tasks", "slug", slug, "error", err)
		http.Error(w, "Failed to approve tasks", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Tasks approved via REST API", "slug", slug)

	resp := &PlanWithStatus{
		Plan:  plan,
		Stage: c.determinePlanStage(plan),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// determinePlanStage determines the current stage of a plan.
func (c *Component) determinePlanStage(plan *workflow.Plan) string {
	switch plan.EffectiveStatus() {
	case workflow.StatusTasksApproved:
		return "tasks_approved"
	case workflow.StatusTasksGenerated:
		return "tasks_generated"
	case workflow.StatusApproved:
		return "approved"
	case workflow.StatusImplementing:
		return "implementing"
	case workflow.StatusComplete:
		return "complete"
	case workflow.StatusReviewed:
		if plan.ReviewVerdict == "needs_changes" {
			return "needs_changes"
		}
		return "reviewed"
	case workflow.StatusDrafted:
		return "ready_for_approval"
	case workflow.StatusRejected:
		return "rejected"
	case workflow.StatusArchived:
		return "archived"
	default:
		return "drafting"
	}
}
