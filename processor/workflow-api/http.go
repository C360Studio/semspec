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
	"github.com/c360studio/semspec/workflow/reactive"
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

// TriggerPayload represents the trigger data structure for parsing stored executions.
// It supports both flattened (new format) and nested Data (old format) for backward compat.
type TriggerPayload struct {
	WorkflowID string `json:"workflow_id"`

	// Flattened fields (new format)
	Slug        string `json:"slug,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`

	// Nested data (old format - for backward compat with stored executions)
	Data json.RawMessage `json:"data,omitempty"`
}

// GetSlug returns the slug from either flattened or nested format.
func (t *TriggerPayload) GetSlug() string {
	if t.Slug != "" {
		return t.Slug
	}
	if len(t.Data) > 0 {
		var nested struct {
			Slug string `json:"slug,omitempty"`
		}
		if json.Unmarshal(t.Data, &nested) == nil {
			return nested.Slug
		}
	}
	return ""
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

	// Get execution bucket - treat missing bucket as "not found"
	// (no workflow executions exist yet)
	bucket, err := c.getExecBucket(r.Context())
	if err != nil {
		c.logger.Debug("Execution bucket not available", "error", err)
		http.Error(w, "Review not found", http.StatusNotFound)
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
		if trigger.GetSlug() == slug {
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

// extractSlugTaskAndAction extracts slug, taskID, and action from paths like:
// /workflow-api/plans/{slug}/tasks/{taskId}
// /workflow-api/plans/{slug}/tasks/{taskId}/approve
// /workflow-api/plans/{slug}/tasks/{taskId}/reject
func extractSlugTaskAndAction(path string) (slug, taskID, action string) {
	// Find /plans/ in the path
	idx := strings.Index(path, "/plans/")
	if idx == -1 {
		return "", "", ""
	}

	// Get everything after /plans/
	remainder := path[idx+len("/plans/"):]

	// Split into parts: {slug}/tasks/{taskId}[/action]
	parts := strings.Split(strings.TrimSuffix(remainder, "/"), "/")

	// Need at least 3 parts: slug, "tasks", taskID
	if len(parts) < 3 {
		return "", "", ""
	}

	// Verify second part is "tasks"
	if parts[1] != "tasks" {
		return "", "", ""
	}

	slug = parts[0]
	taskID = parts[2]

	// Optional action (approve/reject)
	if len(parts) > 3 {
		action = parts[3]
	}

	return slug, taskID, action
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

// ApproveTaskRequest is the request body for POST /plans/{slug}/tasks/{taskId}/approve.
type ApproveTaskRequest struct {
	ApprovedBy string `json:"approved_by,omitempty"`
}

// RejectTaskRequest is the request body for POST /plans/{slug}/tasks/{taskId}/reject.
type RejectTaskRequest struct {
	Reason string `json:"reason"`
}

// CreateTaskHTTPRequest is the HTTP request body for POST /plans/{slug}/tasks.
// This is separate from workflow.CreateTaskRequest to include JSON tags.
type CreateTaskHTTPRequest struct {
	Description        string                         `json:"description"`
	Type               workflow.TaskType              `json:"type"`
	AcceptanceCriteria []workflow.AcceptanceCriterion `json:"acceptance_criteria,omitempty"`
	Files              []string                       `json:"files,omitempty"`
	DependsOn          []string                       `json:"depends_on,omitempty"`
}

// UpdateTaskHTTPRequest is the HTTP request body for PATCH /plans/{slug}/tasks/{taskId}.
// This is separate from workflow.UpdateTaskRequest to include JSON tags.
type UpdateTaskHTTPRequest struct {
	Description        *string                        `json:"description,omitempty"`
	Type               *workflow.TaskType             `json:"type,omitempty"`
	AcceptanceCriteria []workflow.AcceptanceCriterion `json:"acceptance_criteria,omitempty"`
	Files              []string                       `json:"files,omitempty"`
	DependsOn          []string                       `json:"depends_on,omitempty"`
	Sequence           *int                           `json:"sequence,omitempty"`
}

// UpdatePlanHTTPRequest is the HTTP request body for PATCH /plans/{slug}.
// All fields are optional (partial update).
type UpdatePlanHTTPRequest struct {
	Title   *string `json:"title,omitempty"`
	Goal    *string `json:"goal,omitempty"`
	Context *string `json:"context,omitempty"`
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

	if err := workflow.ValidateSlug(slug); err != nil {
		http.Error(w, "Invalid plan slug format", http.StatusBadRequest)
		return
	}

	// Route phase-by-ID endpoints (e.g. /phases/{phaseId}/approve).
	if strings.HasPrefix(endpoint, "phases/") && endpoint != "phases/generate" && endpoint != "phases/approve" && endpoint != "phases/reorder" {
		_, phaseID, action := extractSlugPhaseAndAction(r.URL.Path)
		if phaseID != "" {
			c.handlePhaseByID(w, r, slug, phaseID, action)
			return
		}
	}

	// Route task-by-ID endpoints (e.g. /tasks/{taskId}/approve).
	if strings.HasPrefix(endpoint, "tasks/") && endpoint != "tasks/generate" && endpoint != "tasks/approve" {
		_, taskID, action := extractSlugTaskAndAction(r.URL.Path)
		if taskID != "" {
			c.handlePlanTask(w, r, slug, taskID, action)
			return
		}
	}

	// Route collection and action endpoints.
	switch endpoint {
	case "":
		c.handlePlanCRUD(w, r, slug)
	case "promote":
		requireMethod(w, r, http.MethodPost, func() { c.handlePromotePlan(w, r, slug) })
	case "reviews":
		c.handleGetPlanReviews(w, r)
	default:
		if handled := c.handlePhaseCollectionEndpoint(w, r, slug, endpoint); handled {
			return
		}
		if handled := c.handleTaskCollectionEndpoint(w, r, slug, endpoint); handled {
			return
		}
		http.Error(w, "Unknown endpoint", http.StatusNotFound)
	}
}

// requireMethod responds with 405 when the request method does not match, otherwise calls fn.
func requireMethod(w http.ResponseWriter, r *http.Request, method string, fn func()) {
	if r.Method != method {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	fn()
}

// handlePlanCRUD dispatches GET / PATCH / DELETE on /plans/{slug}.
func (c *Component) handlePlanCRUD(w http.ResponseWriter, r *http.Request, slug string) {
	switch r.Method {
	case http.MethodGet:
		c.handleGetPlan(w, r, slug)
	case http.MethodPatch:
		c.handleUpdatePlan(w, r, slug)
	case http.MethodDelete:
		c.handleDeletePlan(w, r, slug)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePhaseCollectionEndpoint routes phase collection endpoints.
// Returns true when the endpoint was recognised and handled.
func (c *Component) handlePhaseCollectionEndpoint(w http.ResponseWriter, r *http.Request, slug, endpoint string) bool {
	switch endpoint {
	case "phases":
		c.handlePlanPhases(w, r, slug)
	case "phases/generate":
		requireMethod(w, r, http.MethodPost, func() { c.handleGeneratePhases(w, r, slug) })
	case "phases/approve":
		requireMethod(w, r, http.MethodPost, func() { c.handleApproveAllPhases(w, r, slug) })
	case "phases/reorder":
		requireMethod(w, r, http.MethodPut, func() { c.handleReorderPhases(w, r, slug) })
	default:
		return false
	}
	return true
}

// handleTaskCollectionEndpoint routes task collection endpoints.
// Returns true when the endpoint was recognised and handled.
func (c *Component) handleTaskCollectionEndpoint(w http.ResponseWriter, r *http.Request, slug, endpoint string) bool {
	switch endpoint {
	case "tasks":
		c.handlePlanTasks(w, r, slug)
	case "tasks/generate":
		requireMethod(w, r, http.MethodPost, func() { c.handleGenerateTasks(w, r, slug) })
	case "tasks/approve":
		requireMethod(w, r, http.MethodPost, func() { c.handleApproveTasksPlan(w, r, slug) })
	case "execute":
		requireMethod(w, r, http.MethodPost, func() { c.handleExecutePlan(w, r, slug) })
	default:
		return false
	}
	return true
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

	// Publish plan entity to graph (best-effort)
	if pubErr := c.publishPlanEntity(ctx, plan); pubErr != nil {
		c.logger.Warn("Failed to publish plan entity", "slug", plan.Slug, "error", pubErr)
	}

	// Trigger plan-review-loop workflow (ADR-005 OODA feedback loop).
	// The workflow-processor handles: planner → reviewer → revise with findings → re-review.
	requestID := uuid.New().String()

	triggerPayload := workflow.NewSemstreamsTrigger(
		"plan-review-loop", // workflowID
		"planner",          // role
		plan.Title,         // prompt
		requestID,          // requestID
		plan.Slug,          // slug
		plan.Title,         // title
		plan.Title,         // description
		tc.TraceID,         // traceID
		plan.ProjectID,     // projectID
		nil,                // scopePatterns
		false,              // auto
	)

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

// handlePlanTasks handles GET /workflow-api/plans/{slug}/tasks and POST /workflow-api/plans/{slug}/tasks.
func (c *Component) handlePlanTasks(w http.ResponseWriter, r *http.Request, slug string) {
	switch r.Method {
	case http.MethodGet:
		c.handleListTasks(w, r, slug)
	case http.MethodPost:
		c.handleCreateTask(w, r, slug)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleListTasks handles GET /workflow-api/plans/{slug}/tasks.
func (c *Component) handleListTasks(w http.ResponseWriter, r *http.Request, slug string) {
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

	// Check if plan is approved and phases are approved
	if !plan.Approved {
		http.Error(w, "Plan must be approved before generating tasks", http.StatusBadRequest)
		return
	}
	if !plan.PhasesApproved {
		http.Error(w, "Phases must be approved before generating tasks. Use POST /phases/generate first.", http.StatusBadRequest)
		return
	}

	// Trigger task generator
	requestID := uuid.New().String()

	// Load phases for phase-aware task generation
	var phaseInfos []prompts.PhaseInfo
	phases, err := manager.LoadPhases(ctx, slug)
	if err == nil && len(phases) > 0 {
		phaseInfos = make([]prompts.PhaseInfo, len(phases))
		for i, p := range phases {
			phaseInfos[i] = prompts.PhaseInfo{
				ID:          p.ID,
				Sequence:    p.Sequence,
				Name:        p.Name,
				Description: p.Description,
			}
		}
	}

	fullPrompt := prompts.TaskGeneratorPrompt(prompts.TaskGeneratorParams{
		Goal:           plan.Goal,
		Context:        plan.Context,
		ScopeInclude:   plan.Scope.Include,
		ScopeExclude:   plan.Scope.Exclude,
		ScopeProtected: plan.Scope.DoNotTouch,
		Title:          plan.Title,
		Phases:         phaseInfos,
	})

	triggerPayload := workflow.NewSemstreamsTrigger(
		"task-review-loop", // workflowID
		"task-generator",   // role
		fullPrompt,         // prompt
		requestID,          // requestID
		plan.Slug,          // slug
		plan.Title,         // title
		plan.Goal,          // description
		tc.TraceID,         // traceID
		plan.ProjectID,     // projectID
		plan.Scope.Include, // scopePatterns
		true,               // auto
	)

	baseMsg := message.NewBaseMessage(
		workflow.WorkflowTriggerType,
		triggerPayload,
		"workflow-api",
	)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal task-review-loop trigger", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if err := c.natsClient.PublishToStream(ctx, "workflow.trigger.task-review-loop", data); err != nil {
		c.logger.Error("Failed to publish task-review-loop trigger", "error", err)
		http.Error(w, "Failed to start task generation", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Triggered task-review-loop via REST API",
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

	triggerPayload := &reactive.TaskDispatchRequest{
		RequestID: requestID,
		Slug:      plan.Slug,
		BatchID:   batchID,
	}

	baseMsg := message.NewBaseMessage(
		reactive.TaskDispatchRequestType,
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

// handlePlanTask handles endpoints for individual tasks:
// GET /plans/{slug}/tasks/{taskId}
// PATCH /plans/{slug}/tasks/{taskId}
// DELETE /plans/{slug}/tasks/{taskId}
// POST /plans/{slug}/tasks/{taskId}/approve
// POST /plans/{slug}/tasks/{taskId}/reject
func (c *Component) handlePlanTask(w http.ResponseWriter, r *http.Request, slug, taskID, action string) {
	switch action {
	case "":
		// Task-level operations (GET, PATCH, DELETE)
		switch r.Method {
		case http.MethodGet:
			c.handleGetTask(w, r, slug, taskID)
		case http.MethodPatch:
			c.handleUpdateTask(w, r, slug, taskID)
		case http.MethodDelete:
			c.handleDeleteTask(w, r, slug, taskID)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	case "approve":
		// POST /plans/{slug}/tasks/{taskId}/approve
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		c.handleApproveTask(w, r, slug, taskID)
	case "reject":
		// POST /plans/{slug}/tasks/{taskId}/reject
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		c.handleRejectTask(w, r, slug, taskID)
	default:
		http.Error(w, "Unknown task action", http.StatusNotFound)
	}
}

// handleGetTask handles GET /plans/{slug}/tasks/{taskId}.
func (c *Component) handleGetTask(w http.ResponseWriter, r *http.Request, slug, taskID string) {
	manager := c.getManager(w)
	if manager == nil {
		return // Error already written
	}

	task, err := manager.GetTask(r.Context(), slug, taskID)
	if err != nil {
		if errors.Is(err, workflow.ErrTaskNotFound) {
			http.Error(w, "Task not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to get task", "slug", slug, "task_id", taskID, "error", err)
		http.Error(w, "Failed to get task", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(task); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleApproveTask handles POST /plans/{slug}/tasks/{taskId}/approve.
func (c *Component) handleApproveTask(w http.ResponseWriter, r *http.Request, slug, taskID string) {
	manager := c.getManager(w)
	if manager == nil {
		return // Error already written
	}

	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req ApproveTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Default approver to "system" if not provided
	approvedBy := req.ApprovedBy
	if approvedBy == "" {
		approvedBy = "system"
	}

	task, err := manager.ApproveTask(r.Context(), slug, taskID, approvedBy)
	if err != nil {
		if errors.Is(err, workflow.ErrTaskNotFound) {
			http.Error(w, "Task not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, workflow.ErrTaskNotPendingApproval) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		c.logger.Error("Failed to approve task", "slug", slug, "task_id", taskID, "error", err)
		http.Error(w, "Failed to approve task", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Task approved via REST API", "slug", slug, "task_id", taskID, "approved_by", approvedBy)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(task); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleRejectTask handles POST /plans/{slug}/tasks/{taskId}/reject.
func (c *Component) handleRejectTask(w http.ResponseWriter, r *http.Request, slug, taskID string) {
	manager := c.getManager(w)
	if manager == nil {
		return // Error already written
	}

	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req RejectTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Reason == "" {
		http.Error(w, "Rejection reason is required", http.StatusBadRequest)
		return
	}

	task, err := manager.RejectTask(r.Context(), slug, taskID, req.Reason)
	if err != nil {
		if errors.Is(err, workflow.ErrTaskNotFound) {
			http.Error(w, "Task not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, workflow.ErrTaskNotPendingApproval) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		c.logger.Error("Failed to reject task", "slug", slug, "task_id", taskID, "error", err)
		http.Error(w, "Failed to reject task", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Task rejected via REST API", "slug", slug, "task_id", taskID, "reason", req.Reason)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(task); err != nil {
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
	case workflow.StatusPhasesApproved:
		return "phases_approved"
	case workflow.StatusPhasesGenerated:
		return "phases_generated"
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

// handleCreateTask handles POST /plans/{slug}/tasks.
func (c *Component) handleCreateTask(w http.ResponseWriter, r *http.Request, slug string) {
	manager := c.getManager(w)
	if manager == nil {
		return // Error already written
	}

	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req CreateTaskHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Description == "" {
		http.Error(w, "description is required", http.StatusBadRequest)
		return
	}

	// Validate task type if provided (empty is allowed and will default to TaskTypeImplement)
	if req.Type != "" {
		validTypes := []workflow.TaskType{
			workflow.TaskTypeImplement,
			workflow.TaskTypeTest,
			workflow.TaskTypeDocument,
			workflow.TaskTypeReview,
			workflow.TaskTypeRefactor,
		}
		isValid := false
		for _, vt := range validTypes {
			if req.Type == vt {
				isValid = true
				break
			}
		}
		if !isValid {
			http.Error(w, "invalid task type", http.StatusBadRequest)
			return
		}
	}

	// Convert to Manager request
	managerReq := workflow.CreateTaskRequest{
		Description:        req.Description,
		Type:               req.Type,
		AcceptanceCriteria: req.AcceptanceCriteria,
		Files:              req.Files,
		DependsOn:          req.DependsOn,
	}

	task, err := manager.CreateTaskManual(r.Context(), slug, managerReq)
	if err != nil {
		if errors.Is(err, workflow.ErrPlanNotFound) {
			http.Error(w, "Plan not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to create task", "slug", slug, "error", err)
		http.Error(w, "Failed to create task", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Task created via REST API", "slug", slug, "task_id", task.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(task); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleUpdateTask handles PATCH /plans/{slug}/tasks/{taskId}.
func (c *Component) handleUpdateTask(w http.ResponseWriter, r *http.Request, slug, taskID string) {
	manager := c.getManager(w)
	if manager == nil {
		return // Error already written
	}

	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req UpdateTaskHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate task type if provided
	if req.Type != nil && *req.Type != "" {
		validTypes := []workflow.TaskType{
			workflow.TaskTypeImplement,
			workflow.TaskTypeTest,
			workflow.TaskTypeDocument,
			workflow.TaskTypeReview,
			workflow.TaskTypeRefactor,
		}
		isValid := false
		for _, vt := range validTypes {
			if *req.Type == vt {
				isValid = true
				break
			}
		}
		if !isValid {
			http.Error(w, "Invalid task type", http.StatusBadRequest)
			return
		}
	}

	// Convert to Manager request
	managerReq := workflow.UpdateTaskRequest{
		Description:        req.Description,
		Type:               req.Type,
		AcceptanceCriteria: req.AcceptanceCriteria,
		Files:              req.Files,
		DependsOn:          req.DependsOn,
		Sequence:           req.Sequence,
	}

	task, err := manager.UpdateTask(r.Context(), slug, taskID, managerReq)
	if err != nil {
		if errors.Is(err, workflow.ErrTaskNotFound) {
			http.Error(w, "Task not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, workflow.ErrInvalidTransition) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		c.logger.Error("Failed to update task", "slug", slug, "task_id", taskID, "error", err)
		http.Error(w, "Failed to update task", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Task updated via REST API", "slug", slug, "task_id", taskID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(task); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleDeleteTask handles DELETE /plans/{slug}/tasks/{taskId}.
func (c *Component) handleDeleteTask(w http.ResponseWriter, r *http.Request, slug, taskID string) {
	manager := c.getManager(w)
	if manager == nil {
		return // Error already written
	}

	err := manager.DeleteTask(r.Context(), slug, taskID)
	if err != nil {
		if errors.Is(err, workflow.ErrTaskNotFound) {
			http.Error(w, "Task not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, workflow.ErrInvalidTransition) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		c.logger.Error("Failed to delete task", "slug", slug, "task_id", taskID, "error", err)
		http.Error(w, "Failed to delete task", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Task deleted via REST API", "slug", slug, "task_id", taskID)

	w.WriteHeader(http.StatusNoContent)
}

// handleUpdatePlan handles PATCH /plans/{slug}.
func (c *Component) handleUpdatePlan(w http.ResponseWriter, r *http.Request, slug string) {
	manager := c.getManager(w)
	if manager == nil {
		return // Error already written
	}

	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req UpdatePlanHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Convert to Manager request
	managerReq := workflow.UpdatePlanRequest{
		Title:   req.Title,
		Goal:    req.Goal,
		Context: req.Context,
	}

	plan, err := manager.UpdatePlan(r.Context(), slug, managerReq)
	if err != nil {
		if errors.Is(err, workflow.ErrPlanNotFound) {
			http.Error(w, "Plan not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, workflow.ErrPlanNotUpdatable) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		c.logger.Error("Failed to update plan", "slug", slug, "error", err)
		http.Error(w, "Failed to update plan", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Plan updated via REST API", "slug", slug)

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

// handleDeletePlan handles DELETE /plans/{slug}.
// Supports ?archive=true for soft delete (sets status to archived).
// Without archive param or archive=false: hard delete (removes files).
func (c *Component) handleDeletePlan(w http.ResponseWriter, r *http.Request, slug string) {
	manager := c.getManager(w)
	if manager == nil {
		return // Error already written
	}

	// Check for archive query parameter
	archive := r.URL.Query().Get("archive") == "true"

	var err error
	if archive {
		// Soft delete - set status to archived
		err = manager.ArchivePlan(r.Context(), slug)
		if err == nil {
			c.logger.Info("Plan archived via REST API", "slug", slug)
		}
	} else {
		// Hard delete - remove files
		err = manager.DeletePlan(r.Context(), slug)
		if err == nil {
			c.logger.Info("Plan deleted via REST API", "slug", slug)
		}
	}

	if err != nil {
		if errors.Is(err, workflow.ErrPlanNotFound) {
			http.Error(w, "Plan not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, workflow.ErrPlanNotDeletable) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		c.logger.Error("Failed to delete/archive plan", "slug", slug, "archive", archive, "error", err)
		http.Error(w, "Failed to delete plan", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
