package planmanager

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/message"
)

// AcceptChangeProposalResponse is returned by POST .../accept.
type AcceptChangeProposalResponse struct {
	Proposal workflow.ChangeProposal `json:"proposal"`
}

// ChangeProposal HTTP request/response types

// RejectionDetail carries the human's rejection reason for a requirement.
type RejectionDetail struct {
	Reason          string `json:"reason"`
	RejectScenarios bool   `json:"reject_scenarios"`
}

// CreateChangeProposalHTTPRequest is the HTTP request body for POST /plans/{slug}/change-proposals.
type CreateChangeProposalHTTPRequest struct {
	Title          string                     `json:"title"`
	Rationale      string                     `json:"rationale,omitempty"`
	ProposedBy     string                     `json:"proposed_by,omitempty"`
	AffectedReqIDs []string                   `json:"affected_requirement_ids,omitempty"`
	Rejections     map[string]RejectionDetail `json:"rejections,omitempty"`  // per-requirement rejection reasons
	AutoAccept     bool                       `json:"auto_accept,omitempty"` // skip review; deprecate + regenerate immediately
}

// UpdateChangeProposalHTTPRequest is the HTTP request body for PATCH /plans/{slug}/change-proposals/{proposalId}.
type UpdateChangeProposalHTTPRequest struct {
	Title          *string  `json:"title,omitempty"`
	Rationale      *string  `json:"rationale,omitempty"`
	AffectedReqIDs []string `json:"affected_requirement_ids,omitempty"`
}

// ReviewChangeProposalHTTPRequest is the HTTP request body for POST .../accept or .../reject.
type ReviewChangeProposalHTTPRequest struct {
	ReviewedBy string `json:"reviewed_by,omitempty"`
}

// RejectChangeProposalHTTPRequest is the HTTP request body for POST .../reject.
type RejectChangeProposalHTTPRequest struct {
	ReviewedBy string `json:"reviewed_by,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// extractSlugChangeProposalAndAction extracts slug, proposalID, and action from paths like:
// /plan-api/plans/{slug}/change-proposals/{proposalId}
// /plan-api/plans/{slug}/change-proposals/{proposalId}/accept
// /plan-api/plans/{slug}/change-proposals/{proposalId}/reject
func extractSlugChangeProposalAndAction(path string) (slug, proposalID, action string) {
	idx := strings.Index(path, "/plans/")
	if idx == -1 {
		return "", "", ""
	}

	remainder := path[idx+len("/plans/"):]
	parts := strings.Split(strings.TrimSuffix(remainder, "/"), "/")

	// Need at least 3 parts: slug, "change-proposals", proposalID
	if len(parts) < 3 {
		return "", "", ""
	}

	if parts[1] != "change-proposals" {
		return "", "", ""
	}

	slug = parts[0]
	proposalID = parts[2]

	if len(parts) > 3 {
		action = parts[3]
	}

	return slug, proposalID, action
}

// handlePlanChangeProposals handles top-level change-proposal collection endpoints.
func (c *Component) handlePlanChangeProposals(w http.ResponseWriter, r *http.Request, slug string) {
	switch r.Method {
	case http.MethodGet:
		c.handleListChangeProposals(w, r, slug)
	case http.MethodPost:
		c.handleCreateChangeProposal(w, r, slug)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleChangeProposalByID handles change-proposal-specific endpoints: GET, PATCH, DELETE, and lifecycle actions.
func (c *Component) handleChangeProposalByID(w http.ResponseWriter, r *http.Request, slug, proposalID, action string) {
	switch action {
	case "":
		switch r.Method {
		case http.MethodGet:
			c.handleGetChangeProposal(w, r, slug, proposalID)
		case http.MethodPatch:
			c.handleUpdateChangeProposal(w, r, slug, proposalID)
		case http.MethodDelete:
			c.handleDeleteChangeProposal(w, r, slug, proposalID)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	case "submit":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		c.handleSubmitChangeProposal(w, r, slug, proposalID)
	case "accept":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		c.handleAcceptChangeProposal(w, r, slug, proposalID)
	case "reject":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		c.handleRejectChangeProposal(w, r, slug, proposalID)
	default:
		http.Error(w, "Unknown endpoint", http.StatusNotFound)
	}
}

// handleListChangeProposals handles GET /plans/{slug}/change-proposals.
func (c *Component) handleListChangeProposals(w http.ResponseWriter, r *http.Request, slug string) {
	c.mu.RLock()
	tw := c.tripleWriter
	c.mu.RUnlock()

	proposals, err := workflow.LoadChangeProposals(r.Context(), tw, slug)
	if err != nil {
		c.logger.Error("Failed to load change proposals", "slug", slug, "error", err)
		http.Error(w, "Failed to load change proposals", http.StatusInternalServerError)
		return
	}

	// Optional filter by status
	if statusFilter := r.URL.Query().Get("status"); statusFilter != "" {
		filtered := proposals[:0]
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

// handleGetChangeProposal handles GET /plans/{slug}/change-proposals/{proposalId}.
func (c *Component) handleGetChangeProposal(w http.ResponseWriter, r *http.Request, slug, proposalID string) {
	c.mu.RLock()
	tw := c.tripleWriter
	c.mu.RUnlock()

	proposals, err := workflow.LoadChangeProposals(r.Context(), tw, slug)
	if err != nil {
		c.logger.Error("Failed to load change proposals", "slug", slug, "error", err)
		http.Error(w, "Failed to load change proposals", http.StatusInternalServerError)
		return
	}

	for _, p := range proposals {
		if p.ID == proposalID {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(p); err != nil {
				c.logger.Warn("Failed to encode response", "error", err)
			}
			return
		}
	}

	http.Error(w, "Change proposal not found", http.StatusNotFound)
}

// handleCreateChangeProposal handles POST /plans/{slug}/change-proposals.
func (c *Component) handleCreateChangeProposal(w http.ResponseWriter, r *http.Request, slug string) {
	c.mu.RLock()
	tw := c.tripleWriter
	c.mu.RUnlock()

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req CreateChangeProposalHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}

	proposals, err := workflow.LoadChangeProposals(r.Context(), tw, slug)
	if err != nil {
		c.logger.Error("Failed to load change proposals", "slug", slug, "error", err)
		http.Error(w, "Failed to load change proposals", http.StatusInternalServerError)
		return
	}

	// Validate that all affected requirement IDs exist in this plan
	if len(req.AffectedReqIDs) > 0 {
		requirements, err := workflow.LoadRequirements(r.Context(), tw, slug)
		if err != nil {
			c.logger.Error("Failed to load requirements for validation", "slug", slug, "error", err)
			http.Error(w, "Failed to validate requirement IDs", http.StatusInternalServerError)
			return
		}
		reqIndex := make(map[string]bool, len(requirements))
		for _, r := range requirements {
			reqIndex[r.ID] = true
		}
		for _, reqID := range req.AffectedReqIDs {
			if !reqIndex[reqID] {
				http.Error(w, fmt.Sprintf("requirement %q not found in plan", reqID), http.StatusBadRequest)
				return
			}
		}
	}

	proposedBy := req.ProposedBy
	if proposedBy == "" {
		proposedBy = "user"
	}

	now := time.Now()
	id := fmt.Sprintf("change-proposal.%s.%d", slug, len(proposals)+1)

	newProposal := workflow.ChangeProposal{
		ID:             id,
		PlanID:         workflow.PlanEntityID(slug),
		Title:          req.Title,
		Rationale:      req.Rationale,
		Status:         workflow.ChangeProposalStatusProposed,
		ProposedBy:     proposedBy,
		AffectedReqIDs: req.AffectedReqIDs,
		CreatedAt:      now,
	}

	proposals = append(proposals, newProposal)

	if err := workflow.SaveChangeProposals(r.Context(), tw, proposals, slug); err != nil {
		c.logger.Error("Failed to save change proposals", "slug", slug, "error", err)
		http.Error(w, "Failed to save change proposal", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Change proposal created via REST API", "slug", slug, "proposal_id", newProposal.ID)

	// Auto-accept: skip manual review, deprecate affected requirements, delete their
	// scenarios, and trigger partial requirement regeneration immediately.
	if req.AutoAccept && len(req.AffectedReqIDs) > 0 {
		// Mark proposal accepted and re-save.
		for i := range proposals {
			if proposals[i].ID == newProposal.ID {
				proposals[i].Status = workflow.ChangeProposalStatusAccepted
				newProposal.Status = workflow.ChangeProposalStatusAccepted
				break
			}
		}
		if err := workflow.SaveChangeProposals(r.Context(), tw, proposals, slug); err != nil {
			c.logger.Error("Failed to save auto-accepted proposal status", "slug", slug, "error", err)
		}

		// Deprecate affected requirements.
		existingReqs, err := workflow.LoadRequirements(r.Context(), tw, slug)
		if err == nil {
			affected := make(map[string]bool, len(req.AffectedReqIDs))
			for _, id := range req.AffectedReqIDs {
				affected[id] = true
			}
			for i := range existingReqs {
				if affected[existingReqs[i].ID] {
					existingReqs[i].Status = workflow.RequirementStatusDeprecated
				}
			}
			if saveErr := workflow.SaveRequirements(r.Context(), tw, existingReqs, slug); saveErr != nil {
				c.logger.Error("Failed to save deprecated requirements", "slug", slug, "error", saveErr)
			}
		} else {
			c.logger.Error("Failed to load requirements for auto-accept deprecation", "slug", slug, "error", err)
		}

		// Delete scenarios for deprecated requirements.
		existingScenarios, err := workflow.LoadScenarios(r.Context(), tw, slug)
		if err == nil {
			affected := make(map[string]bool, len(req.AffectedReqIDs))
			for _, id := range req.AffectedReqIDs {
				affected[id] = true
			}
			kept := existingScenarios[:0]
			for _, s := range existingScenarios {
				if !affected[s.RequirementID] {
					kept = append(kept, s)
				}
			}
			if saveErr := workflow.SaveScenarios(r.Context(), tw, kept, slug); saveErr != nil {
				c.logger.Error("Failed to save pruned scenarios", "slug", slug, "error", saveErr)
			}
		} else {
			c.logger.Error("Failed to load scenarios for auto-accept pruning", "slug", slug, "error", err)
		}

		// Build per-requirement rejection reasons map and trigger partial regen.
		rejectionReasons := make(map[string]string, len(req.Rejections))
		for reqID, detail := range req.Rejections {
			rejectionReasons[reqID] = detail.Reason
		}

		plan, planErr := c.loadPlanCached(r.Context(), slug)
		if planErr != nil {
			c.logger.Error("Failed to load plan for partial requirement regeneration", "slug", slug, "error", planErr)
		} else {
			c.triggerPartialRequirementGeneration(r.Context(), plan, req.AffectedReqIDs, rejectionReasons)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(newProposal); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleUpdateChangeProposal handles PATCH /plans/{slug}/change-proposals/{proposalId}.
func (c *Component) handleUpdateChangeProposal(w http.ResponseWriter, r *http.Request, slug, proposalID string) {
	c.mu.RLock()
	tw := c.tripleWriter
	c.mu.RUnlock()

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req UpdateChangeProposalHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	proposals, err := workflow.LoadChangeProposals(r.Context(), tw, slug)
	if err != nil {
		c.logger.Error("Failed to load change proposals", "slug", slug, "error", err)
		http.Error(w, "Failed to load change proposals", http.StatusInternalServerError)
		return
	}

	idx := -1
	for i, p := range proposals {
		if p.ID == proposalID {
			idx = i
			break
		}
	}
	if idx == -1 {
		http.Error(w, "Change proposal not found", http.StatusNotFound)
		return
	}

	// Only allow edits on proposed or under_review proposals
	if proposals[idx].Status != workflow.ChangeProposalStatusProposed &&
		proposals[idx].Status != workflow.ChangeProposalStatusUnderReview {
		http.Error(w, "Can only update proposals in proposed or under_review status", http.StatusConflict)
		return
	}

	if req.Title != nil {
		proposals[idx].Title = *req.Title
	}
	if req.Rationale != nil {
		proposals[idx].Rationale = *req.Rationale
	}
	if req.AffectedReqIDs != nil {
		proposals[idx].AffectedReqIDs = req.AffectedReqIDs
	}

	if err := workflow.SaveChangeProposals(r.Context(), tw, proposals, slug); err != nil {
		c.logger.Error("Failed to save change proposals", "slug", slug, "error", err)
		http.Error(w, "Failed to save change proposal", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(proposals[idx]); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleDeleteChangeProposal handles DELETE /plans/{slug}/change-proposals/{proposalId}.
func (c *Component) handleDeleteChangeProposal(w http.ResponseWriter, r *http.Request, slug, proposalID string) {
	c.mu.RLock()
	tw := c.tripleWriter
	c.mu.RUnlock()

	proposals, err := workflow.LoadChangeProposals(r.Context(), tw, slug)
	if err != nil {
		c.logger.Error("Failed to load change proposals", "slug", slug, "error", err)
		http.Error(w, "Failed to load change proposals", http.StatusInternalServerError)
		return
	}

	idx := -1
	for i, p := range proposals {
		if p.ID == proposalID {
			idx = i
			break
		}
	}
	if idx == -1 {
		http.Error(w, "Change proposal not found", http.StatusNotFound)
		return
	}

	// Only allow deletion of proposed proposals (not accepted/archived)
	if proposals[idx].Status != workflow.ChangeProposalStatusProposed {
		http.Error(w, "Can only delete proposals in proposed status", http.StatusConflict)
		return
	}

	proposals = append(proposals[:idx], proposals[idx+1:]...)

	if err := workflow.SaveChangeProposals(r.Context(), tw, proposals, slug); err != nil {
		c.logger.Error("Failed to save change proposals", "slug", slug, "error", err)
		http.Error(w, "Failed to delete change proposal", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleSubmitChangeProposal handles POST /plans/{slug}/change-proposals/{proposalId}/submit.
// Transitions proposal from proposed → under_review.
func (c *Component) handleSubmitChangeProposal(w http.ResponseWriter, r *http.Request, slug, proposalID string) {
	c.mu.RLock()
	tw := c.tripleWriter
	c.mu.RUnlock()

	proposals, err := workflow.LoadChangeProposals(r.Context(), tw, slug)
	if err != nil {
		c.logger.Error("Failed to load change proposals", "slug", slug, "error", err)
		http.Error(w, "Failed to load change proposals", http.StatusInternalServerError)
		return
	}

	idx := -1
	for i, p := range proposals {
		if p.ID == proposalID {
			idx = i
			break
		}
	}
	if idx == -1 {
		http.Error(w, "Change proposal not found", http.StatusNotFound)
		return
	}

	if !proposals[idx].Status.CanTransitionTo(workflow.ChangeProposalStatusUnderReview) {
		http.Error(w, "Cannot submit proposal in current status", http.StatusConflict)
		return
	}

	now := time.Now()
	proposals[idx].Status = workflow.ChangeProposalStatusUnderReview
	proposals[idx].ReviewedAt = &now

	if err := workflow.SaveChangeProposals(r.Context(), tw, proposals, slug); err != nil {
		c.logger.Error("Failed to save change proposals", "slug", slug, "error", err)
		http.Error(w, "Failed to submit change proposal", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(proposals[idx]); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleAcceptChangeProposal handles POST /plans/{slug}/change-proposals/{proposalId}/accept.
// Transitions proposal to accepted and archives it.
func (c *Component) handleAcceptChangeProposal(w http.ResponseWriter, r *http.Request, slug, proposalID string) {
	c.mu.RLock()
	tw := c.tripleWriter
	c.mu.RUnlock()

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req ReviewChangeProposalHTTPRequest
	// Body is optional
	_ = json.NewDecoder(r.Body).Decode(&req)

	proposals, err := workflow.LoadChangeProposals(r.Context(), tw, slug)
	if err != nil {
		c.logger.Error("Failed to load change proposals", "slug", slug, "error", err)
		http.Error(w, "Failed to load change proposals", http.StatusInternalServerError)
		return
	}

	idx := -1
	for i, p := range proposals {
		if p.ID == proposalID {
			idx = i
			break
		}
	}
	if idx == -1 {
		http.Error(w, "Change proposal not found", http.StatusNotFound)
		return
	}

	if !proposals[idx].Status.CanTransitionTo(workflow.ChangeProposalStatusAccepted) {
		http.Error(w, "Cannot accept proposal in current status", http.StatusConflict)
		return
	}

	now := time.Now()
	proposals[idx].Status = workflow.ChangeProposalStatusAccepted
	proposals[idx].DecidedAt = &now

	if err := workflow.SaveChangeProposals(r.Context(), tw, proposals, slug); err != nil {
		c.logger.Error("Failed to save change proposals", "slug", slug, "error", err)
		http.Error(w, "Failed to accept change proposal", http.StatusInternalServerError)
		return
	}

	c.logger.Info("Change proposal accepted via REST API", "slug", slug, "proposal_id", proposalID)

	// Publish cascade request to JetStream for async processing by change-proposal-handler.
	if c.natsClient != nil {
		cascadeReq := &payloads.ChangeProposalCascadeRequest{
			ProposalID: proposalID,
			Slug:       slug,
		}
		baseMsg := message.NewBaseMessage(cascadeReq.Schema(), cascadeReq, "plan-manager")
		cascadeData, err := json.Marshal(baseMsg)
		if err != nil {
			c.logger.Error("Failed to marshal cascade request", "proposal_id", proposalID, "error", err)
		} else if err := c.natsClient.PublishToStream(r.Context(), "workflow.trigger.change-proposal-cascade", cascadeData); err != nil {
			c.logger.Error("Failed to publish cascade request", "proposal_id", proposalID, "error", err)
		} else {
			c.logger.Info("Published cascade request", "slug", slug, "proposal_id", proposalID)
		}
	}

	resp := AcceptChangeProposalResponse{
		Proposal: proposals[idx],
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleRejectChangeProposal handles POST /plans/{slug}/change-proposals/{proposalId}/reject.
func (c *Component) handleRejectChangeProposal(w http.ResponseWriter, r *http.Request, slug, proposalID string) {
	c.mu.RLock()
	tw := c.tripleWriter
	c.mu.RUnlock()

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req RejectChangeProposalHTTPRequest
	// Body is optional for reject
	_ = json.NewDecoder(r.Body).Decode(&req)

	proposals, err := workflow.LoadChangeProposals(r.Context(), tw, slug)
	if err != nil {
		c.logger.Error("Failed to load change proposals", "slug", slug, "error", err)
		http.Error(w, "Failed to load change proposals", http.StatusInternalServerError)
		return
	}

	idx := -1
	for i, p := range proposals {
		if p.ID == proposalID {
			idx = i
			break
		}
	}
	if idx == -1 {
		http.Error(w, "Change proposal not found", http.StatusNotFound)
		return
	}

	if !proposals[idx].Status.CanTransitionTo(workflow.ChangeProposalStatusRejected) {
		http.Error(w, "Cannot reject proposal in current status", http.StatusConflict)
		return
	}

	now := time.Now()
	proposals[idx].Status = workflow.ChangeProposalStatusRejected
	proposals[idx].DecidedAt = &now

	if err := workflow.SaveChangeProposals(r.Context(), tw, proposals, slug); err != nil {
		c.logger.Error("Failed to save change proposals", "slug", slug, "error", err)
		http.Error(w, "Failed to reject change proposal", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(proposals[idx]); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}
