package workflowapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/prompts"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
)

// Phase HTTP request/response types

// CreatePhaseHTTPRequest is the HTTP request body for POST /plans/{slug}/phases.
type CreatePhaseHTTPRequest struct {
	Name             string                     `json:"name"`
	Description      string                     `json:"description,omitempty"`
	DependsOn        []string                   `json:"depends_on,omitempty"`
	RequiresApproval bool                       `json:"requires_approval,omitempty"`
	AgentConfig      *workflow.PhaseAgentConfig `json:"agent_config,omitempty"`
}

// UpdatePhaseHTTPRequest is the HTTP request body for PATCH /plans/{slug}/phases/{phaseId}.
type UpdatePhaseHTTPRequest struct {
	Name             *string                    `json:"name,omitempty"`
	Description      *string                    `json:"description,omitempty"`
	DependsOn        []string                   `json:"depends_on,omitempty"`
	RequiresApproval *bool                      `json:"requires_approval,omitempty"`
	AgentConfig      *workflow.PhaseAgentConfig `json:"agent_config,omitempty"`
}

// ReorderPhasesHTTPRequest is the HTTP request body for PUT /plans/{slug}/phases/reorder.
type ReorderPhasesHTTPRequest struct {
	PhaseIDs []string `json:"phase_ids"`
}

// RejectPhaseHTTPRequest is the HTTP request body for POST /plans/{slug}/phases/{phaseId}/reject.
type RejectPhaseHTTPRequest struct {
	Reason string `json:"reason"`
}

// ApprovePhaseHTTPRequest is the HTTP request body for POST /plans/{slug}/phases/{phaseId}/approve.
type ApprovePhaseHTTPRequest struct {
	ApprovedBy string `json:"approved_by,omitempty"`
}

// PhaseStats computes aggregate status counts for phases.
type PhaseStats struct {
	Total    int `json:"total"`
	Pending  int `json:"pending"`
	Ready    int `json:"ready"`
	Active   int `json:"active"`
	Complete int `json:"complete"`
	Failed   int `json:"failed"`
	Blocked  int `json:"blocked"`
}

// computePhaseStats computes phase statistics from a list of phases.
func computePhaseStats(phases []workflow.Phase) *PhaseStats {
	if len(phases) == 0 {
		return nil
	}
	stats := &PhaseStats{Total: len(phases)}
	for _, p := range phases {
		switch p.Status {
		case workflow.PhaseStatusPending:
			stats.Pending++
		case workflow.PhaseStatusReady:
			stats.Ready++
		case workflow.PhaseStatusActive:
			stats.Active++
		case workflow.PhaseStatusComplete:
			stats.Complete++
		case workflow.PhaseStatusFailed:
			stats.Failed++
		case workflow.PhaseStatusBlocked:
			stats.Blocked++
		}
	}
	return stats
}

// extractSlugPhaseAndAction extracts slug, phaseID, and action from paths like:
// /workflow-api/plans/{slug}/phases/{phaseId}
// /workflow-api/plans/{slug}/phases/{phaseId}/approve
// /workflow-api/plans/{slug}/phases/{phaseId}/reject
func extractSlugPhaseAndAction(path string) (slug, phaseID, action string) {
	idx := strings.Index(path, "/plans/")
	if idx == -1 {
		return "", "", ""
	}

	remainder := path[idx+len("/plans/"):]
	parts := strings.Split(strings.TrimSuffix(remainder, "/"), "/")

	// Need at least 3 parts: slug, "phases", phaseID
	if len(parts) < 3 {
		return "", "", ""
	}

	if parts[1] != "phases" {
		return "", "", ""
	}

	slug = parts[0]
	phaseID = parts[2]

	if len(parts) > 3 {
		action = parts[3]
	}

	return slug, phaseID, action
}

// handlePlanPhases handles top-level phase endpoints.
func (c *Component) handlePlanPhases(w http.ResponseWriter, r *http.Request, slug string) {
	switch r.Method {
	case http.MethodGet:
		c.handleListPhases(w, r, slug)
	case http.MethodPost:
		c.handleCreatePhase(w, r, slug)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePhaseByID handles phase-specific endpoints: GET, PATCH, DELETE.
func (c *Component) handlePhaseByID(w http.ResponseWriter, r *http.Request, slug, phaseID, action string) {
	switch action {
	case "":
		switch r.Method {
		case http.MethodGet:
			c.handleGetPhase(w, r, slug, phaseID)
		case http.MethodPatch:
			c.handleUpdatePhase(w, r, slug, phaseID)
		case http.MethodDelete:
			c.handleDeletePhase(w, r, slug, phaseID)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	case "approve":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		c.handleApprovePhase(w, r, slug, phaseID)
	case "reject":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		c.handleRejectPhase(w, r, slug, phaseID)
	case "tasks":
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		c.handlePhaseTasks(w, r, slug, phaseID)
	default:
		http.Error(w, "Unknown endpoint", http.StatusNotFound)
	}
}

// handleListPhases handles GET /plans/{slug}/phases.
func (c *Component) handleListPhases(w http.ResponseWriter, r *http.Request, slug string) {
	manager := c.getManager(w)
	if manager == nil {
		return
	}

	phases, err := manager.LoadPhases(r.Context(), slug)
	if err != nil {
		c.logger.Error("Failed to load phases", "slug", slug, "error", err)
		http.Error(w, "Failed to load phases", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(phases); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleGetPhase handles GET /plans/{slug}/phases/{phaseId}.
func (c *Component) handleGetPhase(w http.ResponseWriter, r *http.Request, slug, phaseID string) {
	manager := c.getManager(w)
	if manager == nil {
		return
	}

	phase, err := manager.GetPhase(r.Context(), slug, phaseID)
	if err != nil {
		if errors.Is(err, workflow.ErrPhaseNotFound) {
			http.Error(w, "Phase not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to get phase", "slug", slug, "phase_id", phaseID, "error", err)
		http.Error(w, "Failed to get phase", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(phase); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleCreatePhase handles POST /plans/{slug}/phases.
func (c *Component) handleCreatePhase(w http.ResponseWriter, r *http.Request, slug string) {
	manager := c.getManager(w)
	if manager == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req CreatePhaseHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	managerReq := workflow.CreatePhaseRequest{
		Name:             req.Name,
		Description:      req.Description,
		DependsOn:        req.DependsOn,
		RequiresApproval: req.RequiresApproval,
		AgentConfig:      req.AgentConfig,
	}

	phase, err := manager.CreatePhaseManual(r.Context(), slug, managerReq)
	if err != nil {
		if errors.Is(err, workflow.ErrPlanNotFound) {
			http.Error(w, "Plan not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to create phase", "slug", slug, "error", err)
		http.Error(w, "Failed to create phase", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Phase created via REST API", "slug", slug, "phase_id", phase.ID)

	// Publish phase entity to graph (best-effort)
	if err := c.publishPhaseEntity(r.Context(), slug, phase); err != nil {
		c.logger.Warn("Failed to publish phase entity to graph", "phase_id", phase.ID, "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(phase); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleUpdatePhase handles PATCH /plans/{slug}/phases/{phaseId}.
func (c *Component) handleUpdatePhase(w http.ResponseWriter, r *http.Request, slug, phaseID string) {
	manager := c.getManager(w)
	if manager == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req UpdatePhaseHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	managerReq := workflow.UpdatePhaseRequest{
		PhaseID:          phaseID,
		Name:             req.Name,
		Description:      req.Description,
		DependsOn:        req.DependsOn,
		RequiresApproval: req.RequiresApproval,
		AgentConfig:      req.AgentConfig,
	}

	phase, err := manager.UpdatePhase(r.Context(), slug, managerReq)
	if err != nil {
		if errors.Is(err, workflow.ErrPhaseNotFound) {
			http.Error(w, "Phase not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, workflow.ErrPhaseInvalidStatus) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		c.logger.Error("Failed to update phase", "slug", slug, "phase_id", phaseID, "error", err)
		http.Error(w, "Failed to update phase", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(phase); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleDeletePhase handles DELETE /plans/{slug}/phases/{phaseId}.
func (c *Component) handleDeletePhase(w http.ResponseWriter, r *http.Request, slug, phaseID string) {
	manager := c.getManager(w)
	if manager == nil {
		return
	}

	if err := manager.DeletePhase(r.Context(), slug, phaseID); err != nil {
		if errors.Is(err, workflow.ErrPhaseNotFound) {
			http.Error(w, "Phase not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, workflow.ErrPhaseInvalidStatus) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		c.logger.Error("Failed to delete phase", "slug", slug, "phase_id", phaseID, "error", err)
		http.Error(w, "Failed to delete phase", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleApprovePhase handles POST /plans/{slug}/phases/{phaseId}/approve.
func (c *Component) handleApprovePhase(w http.ResponseWriter, r *http.Request, slug, phaseID string) {
	manager := c.getManager(w)
	if manager == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req ApprovePhaseHTTPRequest
	// Body is optional for approve
	_ = json.NewDecoder(r.Body).Decode(&req)

	approvedBy := req.ApprovedBy
	if approvedBy == "" {
		approvedBy = "user"
	}

	phase, err := manager.ApprovePhase(r.Context(), slug, phaseID, approvedBy)
	if err != nil {
		if errors.Is(err, workflow.ErrPhaseNotFound) {
			http.Error(w, "Phase not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to approve phase", "slug", slug, "phase_id", phaseID, "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Publish approval entity + phase status update to graph (best-effort)
	phaseEntityID := workflow.PhaseEntityID(slug, phase.Sequence)
	if err := c.publishApprovalEntity(r.Context(), "phase", phaseEntityID, "approved", approvedBy, ""); err != nil {
		c.logger.Warn("Failed to publish phase approval entity", "phase_id", phaseID, "error", err)
	}
	if err := c.publishPhaseStatusUpdate(r.Context(), slug, phase); err != nil {
		c.logger.Warn("Failed to publish phase status update", "phase_id", phaseID, "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(phase); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleRejectPhase handles POST /plans/{slug}/phases/{phaseId}/reject.
func (c *Component) handleRejectPhase(w http.ResponseWriter, r *http.Request, slug, phaseID string) {
	manager := c.getManager(w)
	if manager == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req RejectPhaseHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Reason == "" {
		http.Error(w, "reason is required", http.StatusBadRequest)
		return
	}

	phase, err := manager.RejectPhase(r.Context(), slug, phaseID, req.Reason)
	if err != nil {
		if errors.Is(err, workflow.ErrPhaseNotFound) {
			http.Error(w, "Phase not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to reject phase", "slug", slug, "phase_id", phaseID, "error", err)
		http.Error(w, "Failed to reject phase", http.StatusInternalServerError)
		return
	}

	// Publish rejection entity to graph (best-effort)
	phaseEntityID := workflow.PhaseEntityID(slug, phase.Sequence)
	if err := c.publishApprovalEntity(r.Context(), "phase", phaseEntityID, "rejected", "", req.Reason); err != nil {
		c.logger.Warn("Failed to publish phase rejection entity", "phase_id", phaseID, "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(phase); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleApproveAllPhases handles POST /plans/{slug}/phases/approve.
func (c *Component) handleApproveAllPhases(w http.ResponseWriter, r *http.Request, slug string) {
	manager := c.getManager(w)
	if manager == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req ApprovePhaseHTTPRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	approvedBy := req.ApprovedBy
	if approvedBy == "" {
		approvedBy = "user"
	}

	approved, err := manager.ApproveAllPhases(r.Context(), slug, approvedBy)
	if err != nil {
		c.logger.Error("Failed to approve all phases", "slug", slug, "error", err)
		http.Error(w, "Failed to approve phases", http.StatusInternalServerError)
		return
	}

	// Also transition plan status to phases_approved
	plan, err := manager.LoadPlan(r.Context(), slug)
	if err == nil {
		_ = manager.ApprovePhasePlan(r.Context(), plan)
		_ = manager.SavePlan(r.Context(), plan)
	}

	c.logger.Info("All phases approved via REST API", "slug", slug, "count", len(approved))

	// Publish approval entities for each phase + plan phases link (best-effort)
	for i := range approved {
		phaseEntityID := workflow.PhaseEntityID(slug, approved[i].Sequence)
		if err := c.publishApprovalEntity(r.Context(), "phase", phaseEntityID, "approved", approvedBy, ""); err != nil {
			c.logger.Warn("Failed to publish phase approval entity", "phase_id", approved[i].ID, "error", err)
		}
		if err := c.publishPhaseStatusUpdate(r.Context(), slug, &approved[i]); err != nil {
			c.logger.Warn("Failed to publish phase status update", "phase_id", approved[i].ID, "error", err)
		}
	}
	// Publish plan-level approval entity
	planEntityID := workflow.PlanEntityID(slug)
	if err := c.publishApprovalEntity(r.Context(), "plan_phases", planEntityID, "approved", approvedBy, ""); err != nil {
		c.logger.Warn("Failed to publish plan phases approval entity", "slug", slug, "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(approved); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleReorderPhases handles PUT /plans/{slug}/phases/reorder.
func (c *Component) handleReorderPhases(w http.ResponseWriter, r *http.Request, slug string) {
	manager := c.getManager(w)
	if manager == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req ReorderPhasesHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.PhaseIDs) == 0 {
		http.Error(w, "phase_ids is required", http.StatusBadRequest)
		return
	}

	if err := manager.ReorderPhases(r.Context(), slug, req.PhaseIDs); err != nil {
		if errors.Is(err, workflow.ErrPhaseNotFound) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		c.logger.Error("Failed to reorder phases", "slug", slug, "error", err)
		http.Error(w, "Failed to reorder phases", http.StatusInternalServerError)
		return
	}

	// Reload and return reordered phases
	phases, err := manager.LoadPhases(r.Context(), slug)
	if err != nil {
		c.logger.Error("Failed to load reordered phases", "slug", slug, "error", err)
		http.Error(w, "Phases reordered but failed to load result", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(phases); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleGeneratePhases handles POST /plans/{slug}/phases/generate.
// Triggers the phase-review-loop workflow.
func (c *Component) handleGeneratePhases(w http.ResponseWriter, r *http.Request, slug string) {
	manager := c.getManager(w)
	if manager == nil {
		return
	}

	ctx := r.Context()

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

	// Require plan to be approved before generating phases
	if !plan.Approved {
		http.Error(w, "Plan must be approved before generating phases", http.StatusBadRequest)
		return
	}

	tc := natsclient.NewTraceContext()
	requestID := uuid.New().String()

	fullPrompt := prompts.PhaseGeneratorPrompt(prompts.PhaseGeneratorParams{
		Goal:           plan.Goal,
		Context:        plan.Context,
		Title:          plan.Title,
		ScopeInclude:   plan.Scope.Include,
		ScopeExclude:   plan.Scope.Exclude,
		ScopeProtected: plan.Scope.DoNotTouch,
	})

	triggerPayload := workflow.NewSemstreamsTrigger(
		"phase-review-loop", // workflowID
		"phase-generator",   // role
		fullPrompt,          // prompt
		requestID,           // requestID
		plan.Slug,           // slug
		plan.Title,          // title
		plan.Goal,           // description
		tc.TraceID,          // traceID
		plan.ProjectID,      // projectID
		plan.Scope.Include,  // scopePatterns
		true,                // auto
	)

	baseMsg := message.NewBaseMessage(
		workflow.WorkflowTriggerType,
		triggerPayload,
		"workflow-api",
	)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal phase-review-loop trigger", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if err := c.natsClient.PublishToStream(ctx, "workflow.trigger.phase-review-loop", data); err != nil {
		c.logger.Error("Failed to publish phase-review-loop trigger", "error", err)
		http.Error(w, "Failed to start phase generation", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Triggered phase-review-loop via REST API",
		"request_id", requestID,
		"slug", plan.Slug,
		"trace_id", tc.TraceID)

	resp := &AsyncOperationResponse{
		Slug:      plan.Slug,
		RequestID: requestID,
		TraceID:   tc.TraceID,
		Message:   fmt.Sprintf("Phase generation started for plan '%s'", plan.Title),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handlePhaseTasks handles GET /plans/{slug}/phases/{phaseId}/tasks.
func (c *Component) handlePhaseTasks(w http.ResponseWriter, r *http.Request, slug, phaseID string) {
	manager := c.getManager(w)
	if manager == nil {
		return
	}

	tasks, err := manager.LoadTasksByPhase(r.Context(), slug, phaseID)
	if err != nil {
		c.logger.Error("Failed to load phase tasks", "slug", slug, "phase_id", phaseID, "error", err)
		http.Error(w, "Failed to load tasks", http.StatusInternalServerError)
		return
	}

	if tasks == nil {
		tasks = []workflow.Task{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(tasks); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}
