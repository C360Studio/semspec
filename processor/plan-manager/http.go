package planmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/c360studio/semspec/pkg/paths"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

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

	// Q&A endpoints (delegated to question handler)
	// These are registered at /plan-api/questions/* instead of /questions/*
	// to keep them scoped under this component's prefix
	c.mu.RLock()
	questionHandler := c.questionHandler
	c.mu.RUnlock()

	if questionHandler != nil {
		questionHandler.RegisterHTTPHandlers(prefix+"questions", mux)
	}

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

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	// Generate slug from title
	slug := paths.Slugify(title)

	// Create trace context early so we use it consistently
	tc := natsclient.NewTraceContext()
	ctx := natsclient.ContextWithTrace(r.Context(), tc)

	// Return existing plan without re-triggering the workflow
	if ps.exists(slug) {
		c.respondWithExistingPlan(w, slug)
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

	// Start coordination pipeline directly (in-process, no NATS round-trip).
	requestID := uuid.New().String()
	c.coordinator.StartCoordination(ctx, plan.Slug, plan.Title, plan.Title, plan.ProjectID, tc.TraceID, "", requestID, nil)

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

// respondWithExistingPlan loads an already-existing plan and writes a 200 JSON response.
// It is called when the plan slug is already present on disk.
func (c *Component) respondWithExistingPlan(w http.ResponseWriter, slug string) {
	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	plan, ok := ps.get(slug)
	if !ok {
		http.Error(w, "Plan not found", http.StatusNotFound)
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
	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	allPlans := ps.list()

	// Convert to PlanWithStatus
	plans := make([]*PlanWithStatus, 0, len(allPlans))
	for _, plan := range allPlans {
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
			c.logger.Error("Failed to approve plan", "slug", slug, "error", err)
			http.Error(w, "Failed to approve plan", http.StatusInternalServerError)
			return
		}
		c.logger.Info("Plan approved via REST API", "slug", slug, "status", plan.Status)

		// Publish approval to graph (best-effort)
		planEntityID := workflow.PlanEntityID(slug)
		if pubErr := c.publishApprovalEntity(r.Context(), "plan", planEntityID, "approved", "user", ""); pubErr != nil {
			c.logger.Warn("Failed to publish plan approval entity", "slug", slug, "error", pubErr)
		}
	}

	// Determine which round this promote advances.
	// Round 1: no requirements yet → trigger requirement/scenario generation.
	// Round 2: requirements+scenarios exist → plan is ready for execution.
	// This runs regardless of Approved flag since promote serves both rounds.
	// Cancel any active coordination for this slug — the human has taken over.
	c.coordinator.Cancel(slug, "plan promoted via REST API")

	requirements := c.requirements.listByPlan(slug)
	scenarios := c.scenarios.listByPlan(slug, c.requirements)

	switch {
	case len(requirements) == 0:
		// Round 1 — plan approved, start requirement/scenario generation.
		// Goal must be set (by coordinator synthesis or manual PATCH) before generation.
		if plan.Goal == "" {
			http.Error(w, "Plan must have a goal before requirement generation — wait for synthesis to complete or set goal via PATCH", http.StatusConflict)
			return
		}
		c.logger.Info("Round 1 human approval: triggering requirement generation", "slug", slug)
		c.triggerRequirementGeneration(r.Context(), plan)

	case len(scenarios) == 0:
		// Requirements exist but no scenarios — cascade in progress, nothing to do.
		c.logger.Info("Requirements exist but scenarios pending — cascade in progress", "slug", slug)

	case plan.Status == workflow.StatusReadyForExecution || plan.Status == workflow.StatusImplementing:
		// Already advanced — idempotent.
		c.logger.Debug("Plan already at or past ready_for_execution", "slug", slug)

	default:
		// Round 2 — requirements+scenarios exist, mark ready for execution.
		c.logger.Info("Round 2 human approval: plan ready for execution", "slug", slug)
		if err := c.setPlanStatusCached(r.Context(), plan, workflow.StatusReadyForExecution); err != nil {
			c.logger.Error("Failed to set plan ready for execution", "slug", slug, "error", err)
			http.Error(w, "Failed to update plan status", http.StatusInternalServerError)
			return
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
	c.mu.RLock()
	c.mu.RUnlock()

	// Create trace context early for consistent usage
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

	// Plan must be in ready_for_execution to start execution.
	// The full pipeline is: approved → requirements_generated → scenarios_generated → ready_for_execution → implementing
	effectiveStatus := plan.EffectiveStatus()
	if effectiveStatus != workflow.StatusReadyForExecution {
		http.Error(w, fmt.Sprintf("Plan must be in ready_for_execution status to execute (current: %s)", effectiveStatus), http.StatusBadRequest)
		return
	}

	// Transition plan status to implementing before triggering execution.
	// This must happen before the publish so that subsequent GET requests
	// see the correct stage (determinePlanStage derives from Status).
	if err := c.setPlanStatusCached(ctx, plan, workflow.StatusImplementing); err != nil {
		c.logger.Error("Failed to set plan status to implementing", "slug", slug, "error", err)
		http.Error(w, "Failed to update plan status", http.StatusInternalServerError)
		return
	}

	// Trigger scenario orchestration for execution.
	requestID := uuid.New().String()
	subject := fmt.Sprintf("scenario.orchestrate.%s", plan.Slug)

	trigger := &payloads.ScenarioOrchestrationTrigger{
		PlanSlug:     plan.Slug,
		TraceID:      tc.TraceID,
		Requirements: c.requirements.listByPlan(plan.Slug),
	}

	baseMsg := message.NewBaseMessage(trigger.Schema(), trigger, "plan-manager")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal execution trigger", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		c.logger.Error("Failed to trigger execution", "error", err)
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
	case workflow.StatusCreated:
		return "drafting"
	case workflow.StatusDrafted:
		return "ready_for_approval"
	case workflow.StatusReviewed:
		if plan.ReviewVerdict == "needs_changes" {
			return "needs_changes"
		}
		return "reviewed"
	case workflow.StatusApproved:
		return "approved"
	case workflow.StatusRequirementsGenerated:
		return "requirements_generated"
	case workflow.StatusScenariosGenerated:
		return "scenarios_generated"
	case workflow.StatusReadyForExecution:
		return "ready_for_execution"
	case workflow.StatusImplementing:
		return "implementing"
	case workflow.StatusReviewingRollup:
		return "reviewing_rollup"
	case workflow.StatusComplete:
		return "complete"
	case workflow.StatusRejected:
		return "rejected"
	case workflow.StatusArchived:
		return "archived"
	default:
		return "drafting"
	}
}




// handleUpdatePlan handles PATCH /plans/{slug}.
func (c *Component) handleUpdatePlan(w http.ResponseWriter, r *http.Request, slug string) {
	c.mu.RLock()
	tw := c.tripleWriter
	c.mu.RUnlock()

	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req UpdatePlanHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Convert to UpdatePlanRequest
	updateReq := workflow.UpdatePlanRequest{
		Title:   req.Title,
		Goal:    req.Goal,
		Context: req.Context,
	}

	plan, err := workflow.UpdatePlan(r.Context(), tw, slug, updateReq)
	if err == nil {
		c.plans.put(plan)
	}
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
	c.mu.RLock()
	tw := c.tripleWriter
	c.mu.RUnlock()

	// Check for archive query parameter
	archive := r.URL.Query().Get("archive") == "true"

	var err error
	if archive {
		// Soft delete - set status to archived
		err = workflow.ArchivePlan(r.Context(), tw, slug)
		if err == nil {
			// Update cache — reload from graph to get the archived status.
			if archivedPlan, loadErr := workflow.LoadPlan(r.Context(), tw, slug); loadErr == nil {
				c.plans.put(archivedPlan)
			} else {
				c.plans.remove(slug) // Can't reload; evict stale entry.
			}
			c.logger.Info("Plan archived via REST API", "slug", slug)
		}
	} else {
		// Hard delete - remove files
		err = workflow.DeletePlan(r.Context(), tw, slug)
		if err == nil {
			c.plans.remove(slug)
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

// handleUnarchivePlan handles POST /plans/{slug}/unarchive.
// Restores an archived plan to complete status.
func (c *Component) handleUnarchivePlan(w http.ResponseWriter, r *http.Request, slug string) {
	c.mu.RLock()
	tw := c.tripleWriter
	c.mu.RUnlock()

	if err := workflow.UnarchivePlan(r.Context(), tw, slug); err != nil {
		if errors.Is(err, workflow.ErrPlanNotFound) {
			http.Error(w, "Plan not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	c.logger.Info("Plan unarchived", "slug", slug)

	plan, err := c.loadPlanCached(r.Context(), slug)
	if err != nil {
		http.Error(w, "Plan unarchived but failed to reload", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(&PlanWithStatus{
		Plan:  plan,
		Stage: c.determinePlanStage(plan),
	})
}
