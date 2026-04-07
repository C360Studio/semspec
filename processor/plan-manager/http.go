package planmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/c360studio/semspec/pkg/paths"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

// planStoreOrFail returns the plan store, or writes 503 and returns nil if not ready.
func (c *Component) planStoreOrFail(w http.ResponseWriter) *planStore {
	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()
	if ps == nil {
		http.Error(w, "plan-manager not ready (still starting)", http.StatusServiceUnavailable)
	}
	return ps
}

// RegisterHTTPHandlers registers HTTP handlers for the plan-api component.
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

	// Workspace browser (proxied to sandbox server)
	if c.workspace != nil {
		mux.HandleFunc(prefix+"workspace/tasks", c.workspace.handleTasks)
		mux.HandleFunc(prefix+"workspace/tree", c.workspace.handleTree)
		mux.HandleFunc(prefix+"workspace/file", c.workspace.handleFile)
		mux.HandleFunc(prefix+"workspace/download", c.workspace.handleDownload)
	} else {
		// Return 503 for all workspace endpoints when sandbox is not configured.
		workspaceUnavailable := func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":"sandbox not configured"}`)) //nolint:errcheck
		}
		mux.HandleFunc(prefix+"workspace/tasks", workspaceUnavailable)
		mux.HandleFunc(prefix+"workspace/tree", workspaceUnavailable)
		mux.HandleFunc(prefix+"workspace/file", workspaceUnavailable)
		mux.HandleFunc(prefix+"workspace/download", workspaceUnavailable)
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

// writeJSONError writes a JSON-encoded {"error": msg} body with the given status code.
// Use this instead of http.Error when the client expects a JSON error envelope.
func writeJSONError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{Error: msg})
}

// handleGetPlanReviews handles GET /plans/{slug}/reviews
// Returns the review synthesis result for the given plan slug.
func (c *Component) handleGetPlanReviews(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract slug from path: /plan-api/plans/{slug}/reviews
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

// getRepoRoot returns the repository root path.
// Returns empty string and writes an HTTP error response if resolution fails.
func (c *Component) getRepoRoot(w http.ResponseWriter) string {
	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			c.logger.Error("Failed to get working directory", "error", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return ""
		}
	}
	return repoRoot
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

// extractSlugAndEndpoint extracts slug and endpoint from path like /plan-api/plans/{slug}/reviews
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
	// Title is the human-readable plan title. Slug is derived from this.
	Title string `json:"title"`
	// Description is an optional longer description (defaults to title if empty).
	Description string `json:"description"`
}

// CreatePlanResponse is the response body for POST /plans.
type CreatePlanResponse struct {
	Slug      string `json:"slug"`
	RequestID string `json:"request_id"`
	TraceID   string `json:"trace_id"`
	Message   string `json:"message"`
}

// ExecutionSummary holds requirement terminal-state counts for a plan.
// It is only populated when the plan is in the implementing status.
type ExecutionSummary struct {
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Pending   int `json:"pending"`
	Total     int `json:"total"`
}

// PlanWithStatus represents a plan with its current workflow status.
// This is the response format for GET /plans and GET /plans/{slug}.
type PlanWithStatus struct {
	*workflow.Plan
	Stage            string             `json:"stage"`
	ActiveLoops      []ActiveLoopStatus `json:"active_loops"`
	ExecutionSummary *ExecutionSummary  `json:"execution_summary,omitempty"`
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

// UpdatePlanHTTPRequest is the HTTP request body for PATCH /plans/{slug}.
// All fields are optional (partial update).
type UpdatePlanHTTPRequest struct {
	Title   *string `json:"title,omitempty"`
	Goal    *string `json:"goal,omitempty"`
	Context *string `json:"context,omitempty"`
}

// handlePlans handles POST /plan-api/plans (create plan).
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

// handlePlansWithSlug handles /plan-api/plans/{slug}/*
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

	// Route requirement-by-ID endpoints (e.g. /requirements/{reqId}/deprecate).
	if strings.HasPrefix(endpoint, "requirements/") {
		_, requirementID, action := extractSlugRequirementAndAction(r.URL.Path)
		if requirementID != "" {
			c.handleRequirementByID(w, r, slug, requirementID, action)
			return
		}
	}

	// Route scenario-by-ID endpoints (e.g. /scenarios/{scenarioId}).
	if strings.HasPrefix(endpoint, "scenarios/") {
		_, scenarioID, action := extractSlugScenarioAndAction(r.URL.Path)
		if scenarioID != "" {
			c.handleScenarioByID(w, r, slug, scenarioID, action)
			return
		}
	}

	// Route change-proposal-by-ID endpoints (e.g. /change-proposals/{proposalId}/accept).
	if strings.HasPrefix(endpoint, "change-proposals/") {
		_, proposalID, action := extractSlugChangeProposalAndAction(r.URL.Path)
		if proposalID != "" {
			c.handleChangeProposalByID(w, r, slug, proposalID, action)
			return
		}
	}

	// Route collection and action endpoints.
	switch endpoint {
	case "":
		c.handlePlanCRUD(w, r, slug)
	case "stream":
		c.handlePlanStream(w, r, slug)
	case "promote":
		requireMethod(w, r, http.MethodPost, func() { c.handlePromotePlan(w, r, slug) })
	case "reviews":
		c.handleGetPlanReviews(w, r)
	case "export-specs":
		requireMethod(w, r, http.MethodPost, func() { c.handleExportSpecs(w, r, slug) })
	case "archive":
		requireMethod(w, r, http.MethodPost, func() { c.handleGenerateArchive(w, r, slug) })
	case "unarchive":
		requireMethod(w, r, http.MethodPost, func() { c.handleUnarchivePlan(w, r, slug) })
	case "retry":
		requireMethod(w, r, http.MethodPost, func() { c.handleRetryPlan(w, r, slug) })
	case "complete":
		requireMethod(w, r, http.MethodPost, func() { c.handleForceCompletePlan(w, r, slug) })
	case "reject":
		requireMethod(w, r, http.MethodPost, func() { c.handleRejectPlan(w, r, slug) })
	default:
		if handled := c.handlePhaseCollectionEndpoint(w, r, slug, endpoint); handled {
			return
		}
		if handled := c.handleRequirementCollectionEndpoint(w, r, slug, endpoint); handled {
			return
		}
		if handled := c.handleScenarioCollectionEndpoint(w, r, slug, endpoint); handled {
			return
		}
		if handled := c.handleChangeProposalCollectionEndpoint(w, r, slug, endpoint); handled {
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
	case "phases/retrospective":
		requireMethod(w, r, http.MethodGet, func() { c.handlePhasesRetrospective(w, r, slug) })
	default:
		return false
	}
	return true
}

// handleRequirementCollectionEndpoint routes requirement collection endpoints.
// Returns true when the endpoint was recognised and handled.
func (c *Component) handleRequirementCollectionEndpoint(w http.ResponseWriter, r *http.Request, slug, endpoint string) bool {
	switch endpoint {
	case "requirements":
		c.handlePlanRequirements(w, r, slug)
	default:
		return false
	}
	return true
}

// handleScenarioCollectionEndpoint routes scenario collection endpoints.
// Returns true when the endpoint was recognised and handled.
func (c *Component) handleScenarioCollectionEndpoint(w http.ResponseWriter, r *http.Request, slug, endpoint string) bool {
	switch endpoint {
	case "scenarios":
		c.handlePlanScenarios(w, r, slug)
	default:
		return false
	}
	return true
}

// handleChangeProposalCollectionEndpoint routes change-proposal collection endpoints.
// Returns true when the endpoint was recognised and handled.
func (c *Component) handleChangeProposalCollectionEndpoint(w http.ResponseWriter, r *http.Request, slug, endpoint string) bool {
	switch endpoint {
	case "change-proposals":
		c.handlePlanChangeProposals(w, r, slug)
	default:
		return false
	}
	return true
}

// handleTaskCollectionEndpoint routes task collection endpoints.
// Returns true when the endpoint was recognised and handled.
// Note: "tasks" endpoint has been removed (pre-generated Tasks no longer exist).
func (c *Component) handleTaskCollectionEndpoint(w http.ResponseWriter, r *http.Request, slug, endpoint string) bool {
	switch endpoint {
	case "execute":
		requireMethod(w, r, http.MethodPost, func() { c.handleExecutePlan(w, r, slug) })
	default:
		return false
	}
	return true
}

// handleCreatePlan handles POST /plan-api/plans.
// Creates a new plan and triggers the planner agent loop.
func (c *Component) handleCreatePlan(w http.ResponseWriter, r *http.Request) {
	// Limit request body size to prevent DoS
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req CreatePlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Accept title or description (backward compat: description-only requests still work).
	title := req.Title
	if title == "" {
		title = req.Description
	}
	if title == "" {
		http.Error(w, "title or description is required", http.StatusBadRequest)
		return
	}
	if req.Description == "" {
		req.Description = title
	}

	ps := c.planStoreOrFail(w)
	if ps == nil {
		return
	}

	// Generate slug from title
	slug := paths.Slugify(title)

	// Create trace context early so we use it consistently
	tc := natsclient.NewTraceContext()
	ctx := natsclient.ContextWithTrace(r.Context(), tc)

	// Return existing plan without re-triggering the workflow
	if ps.exists(slug) {
		c.respondWithExistingPlan(w, r, slug)
		return
	}

	// Create new plan
	plan, err := ps.create(ctx, slug, title)
	if err != nil {
		c.logger.Error("Failed to create plan", "slug", slug, "error", err)
		http.Error(w, fmt.Sprintf("Failed to create plan: %v", err), http.StatusInternalServerError)
		return
	}

	c.logger.Info("Created plan via REST API", "slug", slug, "plan_id", plan.ID)

	// The KV write from ps.create() IS the event — the planner component
	// watches PLAN_STATES for new plans and starts processing.
	resp := &CreatePlanResponse{
		Slug:    plan.Slug,
		TraceID: tc.TraceID,
		Message: "Plan created",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// respondWithExistingPlan loads an already-existing plan and writes a 200 JSON response.
// It is called when the plan slug is already present on disk.
func (c *Component) respondWithExistingPlan(w http.ResponseWriter, r *http.Request, slug string) {
	ps := c.planStoreOrFail(w)
	if ps == nil {
		return
	}

	plan, ok := ps.get(slug)
	if !ok {
		http.Error(w, "Plan not found", http.StatusNotFound)
		return
	}

	resp := &PlanWithStatus{
		Plan:  plan,
		Stage: c.determinePlanStage(plan),
	}
	resp.ExecutionSummary = c.computeExecutionSummary(r.Context(), plan)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// triggerPlanCoordinator builds and publishes a plan-coordinator trigger.
// It returns the generated requestID that callers include in their response.
func (c *Component) triggerPlanCoordinator(ctx context.Context, plan *workflow.Plan, traceID string) (string, error) {
	requestID := uuid.New().String()

	req := &payloads.PlanCoordinatorRequest{
		RequestID:   requestID,
		Slug:        plan.Slug,
		Title:       plan.Title,
		Description: plan.Title,
		ProjectID:   plan.ProjectID,
		TraceID:     traceID,
	}

	baseMsg := message.NewBaseMessage(req.Schema(), req, "plan-manager")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal plan-coordinator trigger", "error", err)
		return "", fmt.Errorf("Internal error")
	}

	if err := c.natsClient.PublishToStream(ctx, "workflow.trigger.plan-coordinator", data); err != nil {
		c.logger.Error("Failed to trigger plan-coordinator", "error", err)
		return "", fmt.Errorf("Failed to start planning")
	}

	c.logger.Info("Triggered plan-coordinator",
		"request_id", requestID,
		"slug", plan.Slug,
		"trace_id", traceID)

	return requestID, nil
}

// handleListPlans handles GET /plan-api/plans.
// Reads from the component-owned cache — never hits the graph.
func (c *Component) handleListPlans(w http.ResponseWriter, r *http.Request) {
	ps := c.planStoreOrFail(w)
	if ps == nil {
		return
	}

	allPlans := ps.list()

	// Convert to PlanWithStatus
	plans := make([]*PlanWithStatus, 0, len(allPlans))
	for _, plan := range allPlans {
		pws := &PlanWithStatus{
			Plan:  plan,
			Stage: c.determinePlanStage(plan),
		}
		pws.ExecutionSummary = c.computeExecutionSummary(r.Context(), plan)
		plans = append(plans, pws)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(plans); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleGetPlan handles GET /plan-api/plans/{slug}.
// Reads from cache with graph fallback on cache miss.
func (c *Component) handleGetPlan(w http.ResponseWriter, r *http.Request, slug string) {
	plan, err := c.loadPlanCached(r.Context(), slug)
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
	resp.ExecutionSummary = c.computeExecutionSummary(r.Context(), plan)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handlePromotePlan handles POST /plan-api/plans/{slug}/promote.
// Approves the plan directly (manual approval via REST API).
// If the plan is already approved, it returns immediately.
func (c *Component) handlePromotePlan(w http.ResponseWriter, r *http.Request, slug string) {
	plan, err := c.loadPlanCached(r.Context(), slug)
	if err != nil {
		if errors.Is(err, workflow.ErrPlanNotFound) {
			http.Error(w, "Plan not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to load plan", "slug", slug, "error", err)
		http.Error(w, "Failed to load plan", http.StatusInternalServerError)
		return
	}

	// Approve the plan if not already approved.
	if !plan.Approved {
		if err := c.approvePlanCached(r.Context(), plan); err != nil {
			if errors.Is(err, workflow.ErrInvalidTransition) {
				writeJSONError(w, fmt.Sprintf("Cannot approve plan in %s status", plan.EffectiveStatus()), http.StatusConflict)
				return
			}
			if !errors.Is(err, workflow.ErrAlreadyApproved) {
				// ErrAlreadyApproved is a race (approved between load and approve) — idempotent success.
				c.logger.Error("Failed to approve plan", "slug", slug, "error", err)
				http.Error(w, "Failed to approve plan", http.StatusInternalServerError)
				return
			}
		}
		c.logger.Info("Plan approved via REST API", "slug", slug, "status", plan.Status)

		// Publish approval to graph (best-effort)
		planEntityID := workflow.PlanEntityID(slug)
		if pubErr := c.publishApprovalEntity(r.Context(), "plan", planEntityID, "approved", "user", ""); pubErr != nil {
			c.logger.Warn("Failed to publish plan approval entity", "slug", slug, "error", pubErr)
		}
	}

	// The plan is now approved. The coordinator's KV watcher sees the status
	// change and drives the pipeline forward (requirement generation → scenario
	// generation → review). No manual dispatch needed — the KV write IS the event.
	//
	// For round 2 (requirements+scenarios already exist), check if we need to
	// advance to ready_for_execution.
	if len(plan.Requirements) > 0 && len(plan.Scenarios) > 0 {
		if plan.Status != workflow.StatusReadyForExecution && plan.Status != workflow.StatusImplementing {
			c.logger.Info("Round 2 human approval: plan ready for execution", "slug", slug)
			if err := c.setPlanStatusCached(r.Context(), plan, workflow.StatusReadyForExecution); err != nil {
				c.logger.Error("Failed to set plan ready for execution", "slug", slug, "error", err)
				http.Error(w, "Failed to update plan status", http.StatusInternalServerError)
				return
			}
		}
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

// handleExecutePlan handles POST /plan-api/plans/{slug}/execute.
func (c *Component) handleExecutePlan(w http.ResponseWriter, r *http.Request, slug string) {
	tc := natsclient.NewTraceContext()
	ctx := natsclient.ContextWithTrace(r.Context(), tc)

	plan, err := c.loadPlanCached(ctx, slug)
	if err != nil {
		if errors.Is(err, workflow.ErrPlanNotFound) {
			http.Error(w, "Plan not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to load plan", "slug", slug, "error", err)
		http.Error(w, "Failed to load plan", http.StatusInternalServerError)
		return
	}

	effectiveStatus := plan.EffectiveStatus()
	if effectiveStatus != workflow.StatusReadyForExecution {
		http.Error(w, fmt.Sprintf("Plan must be in ready_for_execution status to execute (current: %s)", effectiveStatus), http.StatusBadRequest)
		return
	}

	if err := c.setPlanStatusCached(ctx, plan, workflow.StatusImplementing); err != nil {
		c.logger.Error("Failed to set plan status to implementing", "slug", slug, "error", err)
		http.Error(w, "Failed to update plan status", http.StatusInternalServerError)
		return
	}

	requestID := uuid.New().String()
	subject := fmt.Sprintf("scenario.orchestrate.%s", plan.Slug)

	trigger := &payloads.ScenarioOrchestrationTrigger{
		PlanSlug:     plan.Slug,
		TraceID:      tc.TraceID,
		Requirements: plan.Requirements,
	}
	if plan.GitHub != nil {
		trigger.PlanBranch = plan.GitHub.PlanBranch
	}

	baseMsg := message.NewBaseMessage(trigger.Schema(), trigger, "plan-manager")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal execution trigger", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Detach from request cancellation — the JetStream ack round-trip must
	// complete even if the browser drops the connection. WithoutCancel
	// preserves trace values but won't cancel when the client disconnects.
	pubCtx, pubCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer pubCancel()

	if err := c.natsClient.PublishToStream(pubCtx, subject, data); err != nil {
		c.logger.Error("Failed to trigger execution, rolling back status", "slug", slug, "error", err)
		_ = c.setPlanStatusCached(pubCtx, plan, workflow.StatusReadyForExecution)
		http.Error(w, "Failed to start execution", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Triggered scenario execution via REST API",
		"request_id", requestID,
		"slug", plan.Slug,
		"subject", subject,
		"trace_id", tc.TraceID)

	resp := &PlanWithStatus{
		Plan:  plan,
		Stage: c.determinePlanStage(plan),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// determinePlanStage determines the current stage of a plan.
func (c *Component) determinePlanStage(plan *workflow.Plan) string {
	switch plan.EffectiveStatus() {
	case workflow.StatusCreated, workflow.StatusDrafting:
		return "drafting"
	case workflow.StatusDrafted, workflow.StatusReviewingDraft:
		return "ready_for_approval"
	case workflow.StatusReviewed:
		if plan.ReviewVerdict == "needs_changes" {
			return "needs_changes"
		}
		return "reviewed"
	case workflow.StatusApproved:
		return "approved"
	case workflow.StatusGeneratingRequirements:
		return "generating_requirements"
	case workflow.StatusRequirementsGenerated:
		return "requirements_generated"
	case workflow.StatusGeneratingArchitecture:
		return "generating_architecture"
	case workflow.StatusArchitectureGenerated:
		return "architecture_generated"
	case workflow.StatusGeneratingScenarios:
		return "generating_scenarios"
	case workflow.StatusReviewingScenarios:
		return "reviewing_scenarios"
	case workflow.StatusScenariosGenerated:
		return "scenarios_generated"
	case workflow.StatusScenariosReviewed:
		return "scenarios_reviewed"
	case workflow.StatusReadyForExecution:
		return "ready_for_execution"
	case workflow.StatusImplementing:
		return "implementing"
	case workflow.StatusReviewingRollup:
		return "reviewing_rollup"
	case workflow.StatusAwaitingReview:
		return "awaiting_review"
	case workflow.StatusComplete:
		return "complete"
	case workflow.StatusChanged:
		return "changed"
	case workflow.StatusRejected:
		return "rejected"
	case workflow.StatusArchived:
		return "archived"
	default:
		return "drafting"
	}
}

// computeExecutionSummary returns a populated ExecutionSummary for plans in the
// implementing status. Returns nil for any other status or when counts cannot
// be retrieved, so callers can rely on graceful degradation.
func (c *Component) computeExecutionSummary(ctx context.Context, plan *workflow.Plan) *ExecutionSummary {
	if plan.EffectiveStatus() != workflow.StatusImplementing {
		return nil
	}
	total := len(plan.Requirements)
	if total == 0 {
		return nil
	}
	bucket, err := c.getExecBucket(ctx)
	if err != nil {
		c.logger.Warn("computeExecutionSummary: could not get exec bucket", "slug", plan.Slug, "error", err)
		return nil
	}
	completed, failed, err := c.countTerminalRequirements(ctx, bucket, plan.Slug)
	if err != nil {
		c.logger.Warn("computeExecutionSummary: could not count terminal requirements", "slug", plan.Slug, "error", err)
		return nil
	}
	return &ExecutionSummary{
		Completed: completed,
		Failed:    failed,
		Pending:   total - completed - failed,
		Total:     total,
	}
}

// handleUpdatePlan handles PATCH /plans/{slug}.
func (c *Component) handleUpdatePlan(w http.ResponseWriter, r *http.Request, slug string) {
	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req UpdatePlanHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ps := c.planStoreOrFail(w)
	if ps == nil {
		return
	}

	plan, ok := ps.get(slug)
	if !ok {
		http.Error(w, "Plan not found", http.StatusNotFound)
		return
	}

	// State guard: cannot update if status is implementing, complete, or archived.
	effectiveStatus := plan.EffectiveStatus()
	if effectiveStatus == workflow.StatusImplementing || effectiveStatus == workflow.StatusComplete || effectiveStatus == workflow.StatusArchived {
		http.Error(w, fmt.Sprintf("cannot update plan with status %s", effectiveStatus), http.StatusConflict)
		return
	}

	if req.Title != nil {
		plan.Title = *req.Title
	}
	if req.Goal != nil {
		plan.Goal = *req.Goal
	}
	if req.Context != nil {
		plan.Context = *req.Context
	}

	if err := ps.save(r.Context(), plan); err != nil {
		c.logger.Error("Failed to update plan", "slug", slug, "error", err)
		http.Error(w, "Failed to update plan", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Plan updated via REST API", "slug", slug)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(&PlanWithStatus{
		Plan:  plan,
		Stage: c.determinePlanStage(plan),
	}); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleDeletePlan handles DELETE /plans/{slug}.
// Supports ?archive=true for soft delete (sets status to archived).
// Without archive param or archive=false: hard delete (tombstone).
func (c *Component) handleDeletePlan(w http.ResponseWriter, r *http.Request, slug string) {
	ps := c.planStoreOrFail(w)
	if ps == nil {
		return
	}

	archive := r.URL.Query().Get("archive") == "true"

	if archive {
		// Soft delete — transition to archived status via state machine.
		plan, ok := ps.get(slug)
		if !ok {
			http.Error(w, "Plan not found", http.StatusNotFound)
			return
		}
		if err := c.setPlanStatusCached(r.Context(), plan, workflow.StatusArchived); err != nil {
			if errors.Is(err, workflow.ErrInvalidTransition) {
				http.Error(w, fmt.Sprintf("cannot archive plan with status %s", plan.EffectiveStatus()), http.StatusConflict)
				return
			}
			c.logger.Error("Failed to archive plan", "slug", slug, "error", err)
			http.Error(w, "Failed to archive plan", http.StatusInternalServerError)
			return
		}
		c.logger.Info("Plan archived via REST API", "slug", slug)
	} else {
		// Hard delete — graph tombstone + cache/KV eviction.
		if err := ps.delete(r.Context(), slug); err != nil {
			if errors.Is(err, workflow.ErrPlanNotFound) {
				http.Error(w, "Plan not found", http.StatusNotFound)
				return
			}
			c.logger.Error("Failed to delete plan", "slug", slug, "error", err)
			http.Error(w, "Failed to delete plan", http.StatusInternalServerError)
			return
		}
		c.logger.Info("Plan deleted via REST API", "slug", slug)
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleUnarchivePlan handles POST /plans/{slug}/unarchive.
// Restores an archived plan to complete status.
func (c *Component) handleUnarchivePlan(w http.ResponseWriter, r *http.Request, slug string) {
	ps := c.planStoreOrFail(w)
	if ps == nil {
		return
	}

	plan, ok := ps.get(slug)
	if !ok {
		http.Error(w, "Plan not found", http.StatusNotFound)
		return
	}
	if plan.EffectiveStatus() != workflow.StatusArchived {
		http.Error(w, fmt.Sprintf("plan %q is not archived (status: %s)", slug, plan.EffectiveStatus()), http.StatusConflict)
		return
	}

	plan.Status = workflow.StatusComplete
	if err := ps.save(r.Context(), plan); err != nil {
		c.logger.Error("Failed to unarchive plan", "slug", slug, "error", err)
		http.Error(w, "Failed to unarchive plan", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Plan unarchived", "slug", slug)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(&PlanWithStatus{
		Plan:  plan,
		Stage: c.determinePlanStage(plan),
	})
}

// retryPlanRequest is the request body for POST /plans/{slug}/retry.
type retryPlanRequest struct {
	// Scope controls which requirement executions are reset.
	// "failed" (default) resets only entries in "failed" or "error" stage.
	// "all" resets every requirement execution for the plan.
	Scope string `json:"scope"`
}

// retryPlanResponse is the response body for POST /plans/{slug}/retry.
type retryPlanResponse struct {
	Success    bool   `json:"success"`
	Scope      string `json:"scope"`
	ResetCount int    `json:"reset_count"`
}

// execMutationResponse mirrors ExecMutationResponse from execution-manager.
// Defined locally to avoid a cross-package import.
type execMutationResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// sendReqReset sends a single execution.mutation.req.reset request/reply to execution-manager.
func (c *Component) sendReqReset(ctx context.Context, key string) error {
	data, err := json.Marshal(map[string]string{"key": key})
	if err != nil {
		return fmt.Errorf("marshal reset request: %w", err)
	}

	respData, err := c.natsClient.RequestWithRetry(ctx, "execution.mutation.req.reset", data, 5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("reset request for %s: %w", key, err)
	}

	var resp execMutationResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("unmarshal reset response for %s: %w", key, err)
	}
	if !resp.Success {
		return fmt.Errorf("execution-manager rejected reset for %s: %s", key, resp.Error)
	}
	return nil
}

// handleRetryPlan handles POST /plans/{slug}/retry.
// Resets failed (or all) requirement executions and re-dispatches the plan to the
// scenario orchestrator. Valid for plans in implementing, rejected, complete, or archived status.
func (c *Component) handleRetryPlan(w http.ResponseWriter, r *http.Request, slug string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req retryPlanRequest
	// Body is optional — ignore decode errors; default scope applies.
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Scope == "" {
		req.Scope = "failed"
	}
	if req.Scope != "failed" && req.Scope != "all" {
		writeJSONError(w, `scope must be "failed" or "all"`, http.StatusBadRequest)
		return
	}

	plan, err := c.loadPlanCached(r.Context(), slug)
	if err != nil {
		if errors.Is(err, workflow.ErrPlanNotFound) {
			http.Error(w, "Plan not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to load plan", "slug", slug, "error", err)
		http.Error(w, "Failed to load plan", http.StatusInternalServerError)
		return
	}

	effectiveStatus := plan.EffectiveStatus()
	switch effectiveStatus {
	case workflow.StatusImplementing, workflow.StatusRejected,
		workflow.StatusComplete, workflow.StatusArchived,
		workflow.StatusAwaitingReview:
		// valid
	default:
		writeJSONError(w, fmt.Sprintf("plan status %s is not eligible for retry", effectiveStatus), http.StatusConflict)
		return
	}

	// Reset requirement executions via execution-manager mutations.
	resetCount, err := c.resetRequirementExecutions(r.Context(), slug, req.Scope)
	if err != nil {
		c.logger.Error("Failed to reset requirement executions", "slug", slug, "scope", req.Scope, "error", err)
		writeJSONError(w, "Failed to reset requirement executions: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Determine target status and re-trigger orchestration.
	pubCtx, pubCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Second)
	defer pubCancel()

	if effectiveStatus == workflow.StatusImplementing {
		// Already implementing — re-trigger orchestrator without a status change.
		if err := c.triggerScenarioOrchestrator(pubCtx, plan); err != nil {
			c.logger.Error("Failed to re-trigger orchestrator", "slug", slug, "error", err)
			writeJSONError(w, "Failed to re-trigger orchestrator: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		// Transition back to ready_for_execution so the orchestrator picks it up.
		if err := c.setPlanStatusCached(pubCtx, plan, workflow.StatusReadyForExecution); err != nil {
			c.logger.Error("Failed to set plan ready for execution", "slug", slug, "error", err)
			writeJSONError(w, "Failed to update plan status: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	c.logger.Info("Plan retry initiated", "slug", slug, "scope", req.Scope, "reset_count", resetCount)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(&retryPlanResponse{
		Success:    true,
		Scope:      req.Scope,
		ResetCount: resetCount,
	})
}

// resetRequirementExecutions scans EXECUTION_STATES for req.<slug>.* keys and resets
// entries matching the scope. Returns the number of entries reset.
func (c *Component) resetRequirementExecutions(ctx context.Context, slug, scope string) (int, error) {
	bucket, err := c.getExecBucket(ctx)
	if err != nil {
		// No bucket means no executions to reset — treat as success.
		c.logger.Debug("Execution bucket not available, skipping reset", "slug", slug, "error", err)
		return 0, nil
	}

	prefix := "req." + slug + "."
	keys, err := bucket.Keys(ctx, jetstream.MetaOnly())
	if err != nil {
		if errors.Is(err, jetstream.ErrNoKeysFound) {
			return 0, nil
		}
		return 0, fmt.Errorf("list execution keys: %w", err)
	}

	var resetCount int
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}

		if scope == "failed" {
			// Only reset entries in a failed or error stage.
			entry, getErr := bucket.Get(ctx, key)
			if getErr != nil {
				c.logger.Debug("Failed to get execution entry during retry scan", "key", key, "error", getErr)
				continue
			}
			var reqExec struct {
				Stage string `json:"stage"`
			}
			if jsonErr := json.Unmarshal(entry.Value(), &reqExec); jsonErr != nil {
				c.logger.Debug("Failed to unmarshal execution entry during retry scan", "key", key, "error", jsonErr)
				continue
			}
			if reqExec.Stage != "failed" && reqExec.Stage != "error" {
				continue
			}
		}

		if resetErr := c.sendReqReset(ctx, key); resetErr != nil {
			c.logger.Warn("Failed to reset execution entry", "key", key, "error", resetErr)
			continue
		}
		resetCount++
	}

	return resetCount, nil
}

// triggerScenarioOrchestrator publishes a scenario orchestration trigger for the plan.
// Used by retry when the plan is already in implementing status.
func (c *Component) triggerScenarioOrchestrator(ctx context.Context, plan *workflow.Plan) error {
	subject := fmt.Sprintf("scenario.orchestrate.%s", plan.Slug)
	tc := natsclient.NewTraceContext()

	trigger := &payloads.ScenarioOrchestrationTrigger{
		PlanSlug:     plan.Slug,
		TraceID:      tc.TraceID,
		Requirements: plan.Requirements,
	}
	if plan.GitHub != nil {
		trigger.PlanBranch = plan.GitHub.PlanBranch
	}

	baseMsg := message.NewBaseMessage(trigger.Schema(), trigger, "plan-manager")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal orchestration trigger: %w", err)
	}

	return c.natsClient.PublishToStream(ctx, subject, data)
}

// handleForceCompletePlan handles POST /plans/{slug}/complete.
// Force-completes a stalled implementing plan by transitioning to reviewing_rollup
// (falling back to complete if that transition is not permitted).
func (c *Component) handleForceCompletePlan(w http.ResponseWriter, r *http.Request, slug string) {
	plan, err := c.loadPlanCached(r.Context(), slug)
	if err != nil {
		if errors.Is(err, workflow.ErrPlanNotFound) {
			http.Error(w, "Plan not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to load plan", "slug", slug, "error", err)
		http.Error(w, "Failed to load plan", http.StatusInternalServerError)
		return
	}

	status := plan.EffectiveStatus()

	switch status {
	case workflow.StatusAwaitingReview:
		// Human approval — this IS the approval action.
		if err := c.setPlanStatusCached(r.Context(), plan, workflow.StatusComplete); err != nil {
			c.logger.Error("Failed to complete plan from awaiting_review", "slug", slug, "error", err)
			writeJSONError(w, "Failed to update plan status: "+err.Error(), http.StatusInternalServerError)
			return
		}

	case workflow.StatusImplementing:
		// Force-complete: attempt rollup review first; fall back based on review gate config.
		if err := c.setPlanStatusCached(r.Context(), plan, workflow.StatusReviewingRollup); err != nil {
			c.logger.Warn("Rollup transition rejected, routing based on review gate config", "slug", slug, "error", err)
			target := workflow.StatusComplete
			if c.shouldGateReview(plan) {
				target = workflow.StatusAwaitingReview
			}
			if err := c.setPlanStatusCached(r.Context(), plan, target); err != nil {
				c.logger.Error("Failed to force-complete plan", "slug", slug, "error", err)
				writeJSONError(w, "Failed to update plan status: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}

	default:
		writeJSONError(w, fmt.Sprintf("plan must be in implementing or awaiting_review status to complete (current: %s)", status), http.StatusConflict)
		return
	}

	c.logger.Info("Plan force-completed", "slug", slug, "status", plan.Status)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(&PlanWithStatus{
		Plan:  plan,
		Stage: c.determinePlanStage(plan),
	})
}

// handleRejectPlan handles POST /plans/{slug}/reject.
// Explicitly rejects a plan that is stalled in implementing status.
func (c *Component) handleRejectPlan(w http.ResponseWriter, r *http.Request, slug string) {
	plan, err := c.loadPlanCached(r.Context(), slug)
	if err != nil {
		if errors.Is(err, workflow.ErrPlanNotFound) {
			http.Error(w, "Plan not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to load plan", "slug", slug, "error", err)
		http.Error(w, "Failed to load plan", http.StatusInternalServerError)
		return
	}

	if plan.EffectiveStatus() != workflow.StatusImplementing {
		writeJSONError(w, fmt.Sprintf("plan must be in implementing status to reject (current: %s)", plan.EffectiveStatus()), http.StatusConflict)
		return
	}

	if err := c.setPlanStatusCached(r.Context(), plan, workflow.StatusRejected); err != nil {
		c.logger.Error("Failed to reject plan", "slug", slug, "error", err)
		writeJSONError(w, "Failed to update plan status: "+err.Error(), http.StatusInternalServerError)
		return
	}

	c.logger.Info("Plan explicitly rejected", "slug", slug)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(&PlanWithStatus{
		Plan:  plan,
		Stage: c.determinePlanStage(plan),
	})
}
