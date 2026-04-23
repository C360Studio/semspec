package planmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/message"
)

// AcceptPlanDecisionResponse is returned by POST .../accept.
type AcceptPlanDecisionResponse struct {
	Proposal workflow.PlanDecision `json:"proposal"`
}

// PlanDecision HTTP request/response types

// RejectionDetail carries the human's rejection reason for a requirement.
type RejectionDetail struct {
	Reason          string `json:"reason"`
	RejectScenarios bool   `json:"reject_scenarios"`
}

// CreatePlanDecisionHTTPRequest is the HTTP request body for POST /plans/{slug}/plan-decisions.
type CreatePlanDecisionHTTPRequest struct {
	Title          string                     `json:"title"`
	Rationale      string                     `json:"rationale,omitempty"`
	ProposedBy     string                     `json:"proposed_by,omitempty"`
	AffectedReqIDs []string                   `json:"affected_requirement_ids,omitempty"`
	Rejections     map[string]RejectionDetail `json:"rejections,omitempty"`  // per-requirement rejection reasons
	AutoAccept     bool                       `json:"auto_accept,omitempty"` // skip review; deprecate + regenerate immediately
}

// UpdatePlanDecisionHTTPRequest is the HTTP request body for PATCH /plans/{slug}/plan-decisions/{proposalId}.
type UpdatePlanDecisionHTTPRequest struct {
	Title          *string  `json:"title,omitempty"`
	Rationale      *string  `json:"rationale,omitempty"`
	AffectedReqIDs []string `json:"affected_requirement_ids,omitempty"`
}

// ReviewPlanDecisionHTTPRequest is the HTTP request body for POST .../accept or .../reject.
type ReviewPlanDecisionHTTPRequest struct {
	ReviewedBy string `json:"reviewed_by,omitempty"`
}

// RejectPlanDecisionHTTPRequest is the HTTP request body for POST .../reject.
type RejectPlanDecisionHTTPRequest struct {
	ReviewedBy string `json:"reviewed_by,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// extractSlugPlanDecisionAndAction extracts slug, proposalID, and action from paths like:
// /plan-api/plans/{slug}/plan-decisions/{proposalId}
// /plan-api/plans/{slug}/plan-decisions/{proposalId}/accept
// /plan-api/plans/{slug}/plan-decisions/{proposalId}/reject
func extractSlugPlanDecisionAndAction(path string) (slug, proposalID, action string) {
	idx := strings.Index(path, "/plans/")
	if idx == -1 {
		return "", "", ""
	}

	remainder := path[idx+len("/plans/"):]
	parts := strings.Split(strings.TrimSuffix(remainder, "/"), "/")

	// Need at least 3 parts: slug, "plan-decisions", proposalID
	if len(parts) < 3 {
		return "", "", ""
	}

	if parts[1] != "plan-decisions" {
		return "", "", ""
	}

	slug = parts[0]
	proposalID = parts[2]

	if len(parts) > 3 {
		action = parts[3]
	}

	return slug, proposalID, action
}

// handlePlanPlanDecisions handles top-level plan-decision collection endpoints.
func (c *Component) handlePlanPlanDecisions(w http.ResponseWriter, r *http.Request, slug string) {
	switch r.Method {
	case http.MethodGet:
		c.handleListPlanDecisions(w, r, slug)
	case http.MethodPost:
		c.handleCreatePlanDecision(w, r, slug)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePlanDecisionByID handles plan-decision-specific endpoints: GET, PATCH, DELETE, and lifecycle actions.
func (c *Component) handlePlanDecisionByID(w http.ResponseWriter, r *http.Request, slug, proposalID, action string) {
	switch action {
	case "":
		switch r.Method {
		case http.MethodGet:
			c.handleGetPlanDecision(w, r, slug, proposalID)
		case http.MethodPatch:
			c.handleUpdatePlanDecision(w, r, slug, proposalID)
		case http.MethodDelete:
			c.handleDeletePlanDecision(w, r, slug, proposalID)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	case "submit":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		c.handleSubmitPlanDecision(w, r, slug, proposalID)
	case "accept":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		c.handleAcceptPlanDecision(w, r, slug, proposalID)
	case "reject":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		c.handleRejectPlanDecision(w, r, slug, proposalID)
	default:
		http.Error(w, "Unknown endpoint", http.StatusNotFound)
	}
}

// handleListPlanDecisions handles GET /plans/{slug}/plan-decisions.
func (c *Component) handleListPlanDecisions(w http.ResponseWriter, r *http.Request, slug string) {
	plan, ok := c.plans.get(slug)
	if !ok {
		http.Error(w, "Plan not found", http.StatusNotFound)
		return
	}

	proposals := plan.PlanDecisions
	if proposals == nil {
		proposals = []workflow.PlanDecision{}
	}

	// Optional filter by status. Allocate a new slice to avoid mutating the
	// shared backing array from the planStore cache (shallow copy).
	if statusFilter := r.URL.Query().Get("status"); statusFilter != "" {
		var filtered []workflow.PlanDecision
		for _, p := range proposals {
			if string(p.Status) == statusFilter {
				filtered = append(filtered, p)
			}
		}
		proposals = filtered
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(proposals); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleGetPlanDecision handles GET /plans/{slug}/plan-decisions/{proposalId}.
func (c *Component) handleGetPlanDecision(w http.ResponseWriter, _ *http.Request, slug, proposalID string) {
	plan, ok := c.plans.get(slug)
	if !ok {
		http.Error(w, "Plan not found", http.StatusNotFound)
		return
	}

	proposal, _ := plan.FindPlanDecision(proposalID)
	if proposal == nil {
		http.Error(w, "Change proposal not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(proposal); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleCreatePlanDecision handles POST /plans/{slug}/plan-decisions.
func (c *Component) handleCreatePlanDecision(w http.ResponseWriter, r *http.Request, slug string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req CreatePlanDecisionHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}

	plan, ok := c.plans.get(slug)
	if !ok {
		http.Error(w, "Plan not found", http.StatusNotFound)
		return
	}

	// Validate that all affected requirement IDs exist in this plan.
	for _, reqID := range req.AffectedReqIDs {
		if _, idx := plan.FindRequirement(reqID); idx == -1 {
			http.Error(w, fmt.Sprintf("requirement %q not found in plan", reqID), http.StatusBadRequest)
			return
		}
	}

	proposedBy := req.ProposedBy
	if proposedBy == "" {
		proposedBy = "user"
	}

	now := time.Now()
	id := fmt.Sprintf("plan-decision.%s.%d", slug, len(plan.PlanDecisions)+1)

	newProposal := workflow.PlanDecision{
		ID:             id,
		PlanID:         workflow.PlanEntityID(slug),
		Title:          req.Title,
		Rationale:      req.Rationale,
		Status:         workflow.PlanDecisionStatusProposed,
		ProposedBy:     proposedBy,
		AffectedReqIDs: req.AffectedReqIDs,
		CreatedAt:      now,
	}

	plan.PlanDecisions = append(plan.PlanDecisions, newProposal)

	if err := c.plans.save(r.Context(), plan); err != nil {
		c.logger.Error("Failed to save plan after creating change proposal", "slug", slug, "error", err)
		http.Error(w, "Failed to save change proposal", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Change proposal created via REST API", "slug", slug, "proposal_id", newProposal.ID)

	// Auto-accept: skip manual review, deprecate affected requirements, delete their
	// scenarios, and trigger partial requirement regeneration immediately.
	if req.AutoAccept && len(req.AffectedReqIDs) > 0 {
		c.autoAcceptPlanDecision(r, c.plans, slug, &newProposal, req)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(newProposal); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// autoAcceptPlanDecision marks the proposal accepted, deprecates affected
// requirements, deletes their scenarios, and transitions the plan to "changed"
// so the requirement-generator watches it and triggers partial regeneration.
func (c *Component) autoAcceptPlanDecision(
	r *http.Request,
	ps *planStore,
	slug string,
	newProposal *workflow.PlanDecision,
	req CreatePlanDecisionHTTPRequest,
) {
	plan, ok := ps.get(slug)
	if !ok {
		c.logger.Error("Plan not found during auto-accept", "slug", slug)
		return
	}

	// Mark proposal accepted and store rejection reasons for requirement-generator.
	proposal, _ := plan.FindPlanDecision(newProposal.ID)
	if proposal != nil {
		now := time.Now()
		proposal.Status = workflow.PlanDecisionStatusAccepted
		proposal.DecidedAt = &now
		newProposal.Status = workflow.PlanDecisionStatusAccepted
		newProposal.DecidedAt = &now
		if len(req.Rejections) > 0 {
			proposal.RejectionReasons = make(map[string]string, len(req.Rejections))
			for id, detail := range req.Rejections {
				proposal.RejectionReasons[id] = detail.Reason
			}
			newProposal.RejectionReasons = proposal.RejectionReasons
		}
	}

	// Deprecate affected requirements and delete their scenarios.
	affected := c.deprecateAffectedRequirements(r, plan, req.AffectedReqIDs)
	deleteDeprecatedScenarios(plan, affected)

	// Transition to "changed" — triggers requirement-generator KV watcher
	// for partial regeneration of deprecated requirements.
	// Detach from request cancellation — the write must complete even if
	// the client disconnects, otherwise the plan is left with deprecated
	// requirements but no status transition to trigger regeneration.
	durableCtx := context.WithoutCancel(r.Context())
	if err := c.setPlanStatusCached(durableCtx, plan, workflow.StatusChanged); err != nil {
		c.logger.Error("Failed to transition plan to changed after auto-accept",
			"slug", slug, "error", err)
		// Still save even if transition fails, so deprecation is persisted.
		if saveErr := ps.save(durableCtx, plan); saveErr != nil {
			c.logger.Error("Failed to save plan after auto-accept deprecation", "slug", slug, "error", saveErr)
		}
		return
	}

	c.logger.Info("Auto-accept change proposal: plan transitioned to changed",
		"slug", slug, "affected_ids", req.AffectedReqIDs)
}

// deprecateAffectedRequirements marks each requirement as deprecated in the plan
// and returns the set of affected IDs for scenario cleanup.
// The caller is responsible for calling ps.save after this.
func (c *Component) deprecateAffectedRequirements(_ *http.Request, plan *workflow.Plan, ids []string) map[string]bool {
	affected := make(map[string]bool, len(ids))
	for _, id := range ids {
		affected[id] = true
	}
	now := time.Now()
	for i := range plan.Requirements {
		if affected[plan.Requirements[i].ID] {
			plan.Requirements[i].Status = workflow.RequirementStatusDeprecated
			plan.Requirements[i].UpdatedAt = now
		}
	}
	return affected
}

// deleteDeprecatedScenarios removes scenarios whose requirement is in the affected set.
// Mutates plan.Scenarios in-place. The caller is responsible for calling ps.save.
func deleteDeprecatedScenarios(plan *workflow.Plan, affected map[string]bool) {
	surviving := plan.Scenarios[:0]
	for _, s := range plan.Scenarios {
		if !affected[s.RequirementID] {
			surviving = append(surviving, s)
		}
	}
	plan.Scenarios = surviving
}

// handleUpdatePlanDecision handles PATCH /plans/{slug}/plan-decisions/{proposalId}.
func (c *Component) handleUpdatePlanDecision(w http.ResponseWriter, r *http.Request, slug, proposalID string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req UpdatePlanDecisionHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	plan, ok := c.plans.get(slug)
	if !ok {
		http.Error(w, "Plan not found", http.StatusNotFound)
		return
	}

	proposal, idx := plan.FindPlanDecision(proposalID)
	if idx == -1 {
		http.Error(w, "Change proposal not found", http.StatusNotFound)
		return
	}

	// Only allow edits on proposed or under_review proposals
	if proposal.Status != workflow.PlanDecisionStatusProposed &&
		proposal.Status != workflow.PlanDecisionStatusUnderReview {
		http.Error(w, "Can only update proposals in proposed or under_review status", http.StatusConflict)
		return
	}

	if req.Title != nil {
		proposal.Title = *req.Title
	}
	if req.Rationale != nil {
		proposal.Rationale = *req.Rationale
	}
	if req.AffectedReqIDs != nil {
		proposal.AffectedReqIDs = req.AffectedReqIDs
	}

	if err := c.plans.save(r.Context(), plan); err != nil {
		c.logger.Error("Failed to save plan after updating change proposal", "slug", slug, "error", err)
		http.Error(w, "Failed to save change proposal", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(proposal); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleDeletePlanDecision handles DELETE /plans/{slug}/plan-decisions/{proposalId}.
func (c *Component) handleDeletePlanDecision(w http.ResponseWriter, r *http.Request, slug, proposalID string) {
	plan, ok := c.plans.get(slug)
	if !ok {
		http.Error(w, "Plan not found", http.StatusNotFound)
		return
	}

	_, idx := plan.FindPlanDecision(proposalID)
	if idx == -1 {
		http.Error(w, "Change proposal not found", http.StatusNotFound)
		return
	}

	// Only allow deletion of proposed proposals (not accepted/archived)
	if plan.PlanDecisions[idx].Status != workflow.PlanDecisionStatusProposed {
		http.Error(w, "Can only delete proposals in proposed status", http.StatusConflict)
		return
	}

	plan.PlanDecisions = append(plan.PlanDecisions[:idx], plan.PlanDecisions[idx+1:]...)

	if err := c.plans.save(r.Context(), plan); err != nil {
		c.logger.Error("Failed to save plan after deleting change proposal", "slug", slug, "error", err)
		http.Error(w, "Failed to delete change proposal", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleSubmitPlanDecision handles POST /plans/{slug}/plan-decisions/{proposalId}/submit.
// Transitions proposal from proposed → under_review.
func (c *Component) handleSubmitPlanDecision(w http.ResponseWriter, r *http.Request, slug, proposalID string) {
	plan, ok := c.plans.get(slug)
	if !ok {
		http.Error(w, "Plan not found", http.StatusNotFound)
		return
	}

	proposal, idx := plan.FindPlanDecision(proposalID)
	if idx == -1 {
		http.Error(w, "Change proposal not found", http.StatusNotFound)
		return
	}

	if !proposal.Status.CanTransitionTo(workflow.PlanDecisionStatusUnderReview) {
		http.Error(w, "Cannot submit proposal in current status", http.StatusConflict)
		return
	}

	now := time.Now()
	proposal.Status = workflow.PlanDecisionStatusUnderReview
	proposal.ReviewedAt = &now

	if err := c.plans.save(r.Context(), plan); err != nil {
		c.logger.Error("Failed to save plan after submitting change proposal", "slug", slug, "error", err)
		http.Error(w, "Failed to submit change proposal", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(proposal); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleAcceptPlanDecision handles POST /plans/{slug}/plan-decisions/{proposalId}/accept.
// Transitions proposal to accepted and archives it.
func (c *Component) handleAcceptPlanDecision(w http.ResponseWriter, r *http.Request, slug, proposalID string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req ReviewPlanDecisionHTTPRequest
	// Body is optional
	_ = json.NewDecoder(r.Body).Decode(&req)

	plan, ok := c.plans.get(slug)
	if !ok {
		http.Error(w, "Plan not found", http.StatusNotFound)
		return
	}

	proposal, idx := plan.FindPlanDecision(proposalID)
	if idx == -1 {
		http.Error(w, "Change proposal not found", http.StatusNotFound)
		return
	}

	if !proposal.Status.CanTransitionTo(workflow.PlanDecisionStatusAccepted) {
		http.Error(w, "Cannot accept proposal in current status", http.StatusConflict)
		return
	}

	now := time.Now()
	proposal.Status = workflow.PlanDecisionStatusAccepted
	proposal.DecidedAt = &now

	if err := c.plans.save(r.Context(), plan); err != nil {
		c.logger.Error("Failed to save plan after accepting change proposal", "slug", slug, "error", err)
		http.Error(w, "Failed to accept change proposal", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Change proposal accepted via REST API", "slug", slug, "proposal_id", proposalID)

	// Publish cascade request to JetStream for async processing by plan-decision-handler.
	// Detach from request cancellation — the ack round-trip must complete.
	if c.natsClient != nil {
		cascadeReq := &payloads.PlanDecisionCascadeRequest{
			ProposalID: proposalID,
			Slug:       slug,
		}
		baseMsg := message.NewBaseMessage(cascadeReq.Schema(), cascadeReq, "plan-manager")
		cascadeData, err := json.Marshal(baseMsg)
		if err != nil {
			c.logger.Error("Failed to marshal cascade request", "proposal_id", proposalID, "error", err)
		} else {
			pubCtx, pubCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Second)
			defer pubCancel()
			if err := c.natsClient.PublishToStream(pubCtx, "workflow.trigger.plan-decision-cascade", cascadeData); err != nil {
				c.logger.Error("Failed to publish cascade request", "proposal_id", proposalID, "error", err)
			} else {
				c.logger.Info("Published cascade request", "slug", slug, "proposal_id", proposalID)
			}
		}
	}

	resp := AcceptPlanDecisionResponse{
		Proposal: *proposal,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleRejectPlanDecision handles POST /plans/{slug}/plan-decisions/{proposalId}/reject.
func (c *Component) handleRejectPlanDecision(w http.ResponseWriter, r *http.Request, slug, proposalID string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req RejectPlanDecisionHTTPRequest
	// Body is optional for reject
	_ = json.NewDecoder(r.Body).Decode(&req)

	plan, ok := c.plans.get(slug)
	if !ok {
		http.Error(w, "Plan not found", http.StatusNotFound)
		return
	}

	proposal, idx := plan.FindPlanDecision(proposalID)
	if idx == -1 {
		http.Error(w, "Change proposal not found", http.StatusNotFound)
		return
	}

	if !proposal.Status.CanTransitionTo(workflow.PlanDecisionStatusRejected) {
		http.Error(w, "Cannot reject proposal in current status", http.StatusConflict)
		return
	}

	now := time.Now()
	proposal.Status = workflow.PlanDecisionStatusRejected
	proposal.DecidedAt = &now

	if err := c.plans.save(r.Context(), plan); err != nil {
		c.logger.Error("Failed to save plan after rejecting change proposal", "slug", slug, "error", err)
		http.Error(w, "Failed to reject change proposal", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(proposal); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}
