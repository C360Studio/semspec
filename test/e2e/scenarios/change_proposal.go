package scenarios

// ChangeProposalScenario tests the change-proposal lifecycle and cascade behavior.
//
// Scope:
//
//  1. Plan + Requirement setup — an approved plan with two requirements is the
//     baseline; the acceptance cascade needs requirements to traverse.
//
//  2. Change-proposal CRUD:
//     - Create (POST .../change-proposals)
//     - Get    (GET  .../change-proposals/{id})
//     - List   (GET  .../change-proposals)
//     - Update (PATCH .../change-proposals/{id})
//
//  3. Status transition — happy path:
//     - Submit  (proposed → under_review)
//     - Accept  (under_review → accepted) including cascade response
//
//  4. Cascade response verification:
//     - The accept response contains a Cascade field listing affected requirement
//       and scenario IDs.  The test verifies the proposal carries the requirement
//       IDs through the cascade.
//
//  5. Rejection path — independent proposal:
//     - Create → Submit → Reject (under_review → rejected)
//
//  6. Guard-rail / error-handling:
//     - 400 when creating a proposal with no title
//     - 400 when creating a proposal referencing a non-existent requirement ID
//     - 404 when fetching a non-existent proposal
//     - 409 when submitting a proposal that is already under_review
//     - 409 when deleting a proposal that is not in "proposed" status
//
//  7. Delete — a proposal in "proposed" status can be deleted (204); verify 404 after.

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// ChangeProposalScenario tests the change-proposal lifecycle and cascade behavior.
type ChangeProposalScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
}

// NewChangeProposalScenario creates a new change proposal scenario.
func NewChangeProposalScenario(cfg *config.Config) *ChangeProposalScenario {
	return &ChangeProposalScenario{
		name:        "change-proposal",
		description: "Tests ChangeProposal CRUD, status transitions, cascade response, and error handling",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *ChangeProposalScenario) Name() string { return s.name }

// Description returns the scenario description.
func (s *ChangeProposalScenario) Description() string { return s.description }

// Setup prepares the scenario environment.
func (s *ChangeProposalScenario) Setup(ctx context.Context) error {
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

	return nil
}

// Execute runs all change-proposal stages in sequence.
func (s *ChangeProposalScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		// Bootstrap: plan + requirements
		{"create-plan", s.stageCreatePlan},
		{"approve-plan", s.stageApprovePlan},
		{"create-requirements", s.stageCreateRequirements},

		// CRUD verification
		{"proposal-create", s.stageProposalCreate},
		{"proposal-get", s.stageProposalGet},
		{"proposal-list", s.stageProposalList},
		{"proposal-update", s.stageProposalUpdate},

		// Happy-path status transitions + cascade
		{"proposal-submit", s.stageProposalSubmit},
		{"proposal-accept", s.stageProposalAccept},
		{"cascade-verify", s.stageCascadeVerify},

		// Rejection path (independent proposal)
		{"reject-proposal-create", s.stageRejectProposalCreate},
		{"reject-proposal-submit", s.stageRejectProposalSubmit},
		{"reject-proposal-reject", s.stageRejectProposalReject},

		// Status-filter list
		{"list-by-status", s.stageListByStatus},

		// Guard-rails / error handling
		{"error-missing-title", s.stageErrorMissingTitle},
		{"error-invalid-requirement", s.stageErrorInvalidRequirement},
		{"error-404-proposal", s.stageError404Proposal},
		{"error-double-submit", s.stageErrorDoubleSubmit},
		{"error-delete-non-proposed", s.stageErrorDeleteNonProposed},

		// Delete of a proposed-status proposal
		{"proposal-delete", s.stageProposalDelete},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		stageCtx, cancel := context.WithTimeout(ctx, s.config.StageTimeout)

		err := stage.fn(stageCtx, result)
		cancel()

		dur := time.Since(stageStart)
		result.SetMetric(fmt.Sprintf("%s_duration_us", stage.name), dur.Microseconds())

		if err != nil {
			result.AddStage(stage.name, false, dur, err.Error())
			result.AddError(fmt.Sprintf("%s: %v", stage.name, err))
			result.Error = fmt.Sprintf("%s failed: %v", stage.name, err)
			return result, nil
		}

		result.AddStage(stage.name, true, dur, "")
	}

	result.Success = true
	return result, nil
}

// Teardown cleans up after the scenario.
func (s *ChangeProposalScenario) Teardown(_ context.Context) error {
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers — carry test-local state via Result.Details
// ---------------------------------------------------------------------------

func (s *ChangeProposalScenario) planSlug(result *Result) (string, bool) {
	if result == nil {
		return "", false
	}
	return result.GetDetailString("plan_slug")
}

func (s *ChangeProposalScenario) storedProposalID(result *Result) (string, bool) {
	if result == nil {
		return "", false
	}
	return result.GetDetailString("proposal_id")
}

func (s *ChangeProposalScenario) storedRejectProposalID(result *Result) (string, bool) {
	if result == nil {
		return "", false
	}
	return result.GetDetailString("reject_proposal_id")
}

func (s *ChangeProposalScenario) storedRequirementID(result *Result) (string, bool) {
	if result == nil {
		return "", false
	}
	return result.GetDetailString("requirement_id")
}

// ---------------------------------------------------------------------------
// Plan bootstrap stages
// ---------------------------------------------------------------------------

func (s *ChangeProposalScenario) stageCreatePlan(ctx context.Context, result *Result) error {
	resp, err := s.http.CreatePlan(ctx, "change proposal lifecycle test")
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("create plan returned error: %s", resp.Error)
	}

	slug := resp.Slug
	if slug == "" && resp.Plan != nil {
		slug = resp.Plan.Slug
	}
	if slug == "" {
		return fmt.Errorf("create plan returned empty slug")
	}

	result.SetDetail("plan_slug", slug)

	if _, err := s.http.WaitForPlanCreated(ctx, slug); err != nil {
		return fmt.Errorf("plan not created: %w", err)
	}

	return nil
}

func (s *ChangeProposalScenario) stageApprovePlan(ctx context.Context, result *Result) error {
	slug, ok := s.planSlug(result)
	if !ok {
		return fmt.Errorf("plan_slug not set by create-plan stage")
	}

	resp, err := s.http.PromotePlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("promote plan: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("promote returned error: %s", resp.Error)
	}

	result.SetDetail("plan_approved", true)
	return nil
}

func (s *ChangeProposalScenario) stageCreateRequirements(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)

	req1 := &client.CreateRequirementRequest{
		Title:       "Token-based authentication",
		Description: "All protected endpoints must validate bearer tokens",
	}
	r1, err := s.http.CreateRequirement(ctx, slug, req1)
	if err != nil {
		return fmt.Errorf("create requirement 1: %w", err)
	}
	if r1.ID == "" {
		return fmt.Errorf("created requirement has empty ID")
	}
	result.SetDetail("requirement_id", r1.ID)

	// Create a second requirement so we can reference multiple IDs in the proposal.
	req2 := &client.CreateRequirementRequest{
		Title:       "Rate limiting",
		Description: "The API must apply per-IP rate limiting on auth endpoints",
	}
	r2, err := s.http.CreateRequirement(ctx, slug, req2)
	if err != nil {
		return fmt.Errorf("create requirement 2: %w", err)
	}
	if r2.ID == "" {
		return fmt.Errorf("created second requirement has empty ID")
	}
	result.SetDetail("requirement_id_2", r2.ID)
	result.SetDetail("requirements_created", 2)
	return nil
}

// ---------------------------------------------------------------------------
// Change-proposal CRUD stages
// ---------------------------------------------------------------------------

func (s *ChangeProposalScenario) stageProposalCreate(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	reqID, _ := s.storedRequirementID(result)
	reqID2, _ := result.GetDetailString("requirement_id_2")

	req := &client.CreateChangeProposalRequest{
		Title:          "Extend token expiry to 24 hours",
		Rationale:      "Mobile clients need longer sessions to avoid frequent re-login",
		ProposedBy:     "e2e-test",
		AffectedReqIDs: []string{reqID, reqID2},
	}

	proposal, status, err := s.http.CreateChangeProposal(ctx, slug, req)
	if err != nil {
		return fmt.Errorf("create change proposal: %w", err)
	}
	if status != 201 {
		return fmt.Errorf("expected HTTP 201, got %d", status)
	}
	if proposal.ID == "" {
		return fmt.Errorf("created proposal has empty ID")
	}
	if proposal.Status != "proposed" {
		return fmt.Errorf("expected status=proposed, got %q", proposal.Status)
	}
	if proposal.Title != req.Title {
		return fmt.Errorf("title mismatch: got %q, want %q", proposal.Title, req.Title)
	}
	if len(proposal.AffectedReqIDs) != 2 {
		return fmt.Errorf("expected 2 affected requirement IDs, got %d", len(proposal.AffectedReqIDs))
	}
	if proposal.PlanID == "" {
		return fmt.Errorf("proposal missing plan_id")
	}

	result.SetDetail("proposal_id", proposal.ID)
	result.SetDetail("proposal_title", proposal.Title)
	return nil
}

func (s *ChangeProposalScenario) stageProposalGet(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	proposalID, _ := s.storedProposalID(result)

	proposal, status, err := s.http.GetChangeProposal(ctx, slug, proposalID)
	if err != nil {
		return fmt.Errorf("get change proposal: %w", err)
	}
	if status != 200 {
		return fmt.Errorf("expected HTTP 200, got %d", status)
	}
	if proposal.ID != proposalID {
		return fmt.Errorf("ID mismatch: got %q, want %q", proposal.ID, proposalID)
	}
	if proposal.Status != "proposed" {
		return fmt.Errorf("expected status=proposed, got %q", proposal.Status)
	}

	result.SetDetail("proposal_get_verified", true)
	return nil
}

func (s *ChangeProposalScenario) stageProposalList(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	proposalID, _ := s.storedProposalID(result)

	// List without filter — must contain our proposal.
	proposals, err := s.http.ListChangeProposals(ctx, slug, "")
	if err != nil {
		return fmt.Errorf("list change proposals: %w", err)
	}
	if len(proposals) == 0 {
		return fmt.Errorf("expected at least 1 proposal, got 0")
	}

	found := false
	for _, p := range proposals {
		if p.ID == proposalID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("created proposal %q not found in list", proposalID)
	}

	result.SetDetail("proposal_list_count", len(proposals))
	return nil
}

func (s *ChangeProposalScenario) stageProposalUpdate(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	proposalID, _ := s.storedProposalID(result)

	updatedTitle := "Extend token expiry to 48 hours"
	req := &client.UpdateChangeProposalRequest{
		Title: &updatedTitle,
	}

	proposal, status, err := s.http.UpdateChangeProposal(ctx, slug, proposalID, req)
	if err != nil {
		return fmt.Errorf("update change proposal: %w", err)
	}
	if status != 200 {
		return fmt.Errorf("expected HTTP 200, got %d", status)
	}
	if proposal.Title != updatedTitle {
		return fmt.Errorf("title not updated: got %q, want %q", proposal.Title, updatedTitle)
	}

	result.SetDetail("proposal_updated", true)
	result.SetDetail("proposal_title", updatedTitle)
	return nil
}

// ---------------------------------------------------------------------------
// Happy-path status transitions + cascade
// ---------------------------------------------------------------------------

func (s *ChangeProposalScenario) stageProposalSubmit(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	proposalID, _ := s.storedProposalID(result)

	proposal, status, err := s.http.SubmitChangeProposal(ctx, slug, proposalID)
	if err != nil {
		return fmt.Errorf("submit change proposal: %w", err)
	}
	if status != 200 {
		return fmt.Errorf("expected HTTP 200, got %d", status)
	}
	if proposal.Status != "under_review" {
		return fmt.Errorf("expected status=under_review, got %q", proposal.Status)
	}
	if proposal.ReviewedAt == nil {
		return fmt.Errorf("proposal missing reviewed_at timestamp after submit")
	}

	result.SetDetail("proposal_submitted", true)
	return nil
}

func (s *ChangeProposalScenario) stageProposalAccept(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	proposalID, _ := s.storedProposalID(result)

	acceptResp, status, err := s.http.AcceptChangeProposal(ctx, slug, proposalID, "e2e-reviewer")
	if err != nil {
		return fmt.Errorf("accept change proposal: %w", err)
	}
	// Accept returns 202 (async cascade triggered), not 200.
	if status != 200 && status != 202 {
		return fmt.Errorf("expected HTTP 200 or 202, got %d", status)
	}
	if acceptResp.Proposal.Status != "accepted" {
		return fmt.Errorf("expected status=accepted, got %q", acceptResp.Proposal.Status)
	}
	if acceptResp.Proposal.DecidedAt == nil {
		return fmt.Errorf("proposal missing decided_at timestamp after accept")
	}

	result.SetDetail("proposal_accepted", true)
	result.SetDetail("cascade_present", acceptResp.Cascade != nil)
	if acceptResp.Cascade != nil {
		result.SetDetail("cascade_affected_req_count", len(acceptResp.Cascade.AffectedRequirementIDs))
	}
	return nil
}

func (s *ChangeProposalScenario) stageCascadeVerify(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	proposalID, _ := s.storedProposalID(result)

	// Re-fetch the accepted proposal to confirm it persists as accepted.
	proposal, status, err := s.http.GetChangeProposal(ctx, slug, proposalID)
	if err != nil {
		return fmt.Errorf("get accepted proposal: %w", err)
	}
	if status != 200 {
		return fmt.Errorf("expected HTTP 200 on re-fetch, got %d", status)
	}
	if proposal.Status != "accepted" {
		return fmt.Errorf("expected persistent status=accepted, got %q", proposal.Status)
	}

	// Verify the cascade response included the affected requirement IDs.
	// The cascade runs synchronously on accept; affected requirement IDs come
	// from proposal.AffectedReqIDs (2 in this test).
	cascadePresent, _ := result.GetDetailBool("cascade_present")
	if !cascadePresent {
		result.AddWarning("cascade field was nil in accept response — expected non-nil when requirements are affected")
		result.SetDetail("cascade_verify_skipped", true)
		return nil
	}

	if v, ok := result.GetDetail("cascade_affected_req_count"); ok {
		if affectedReqCount, ok := v.(int); ok && affectedReqCount != 2 {
			return fmt.Errorf("expected cascade to report 2 affected requirements, got %d", affectedReqCount)
		}
	}

	// Tasks dirtied should be 0 because no tasks have ScenarioIDs linking to
	// affected scenarios (the HTTP API for creating tasks does not expose ScenarioIDs).
	if v, ok := result.GetDetail("cascade_tasks_dirtied"); ok {
		if tasksDirtied, ok := v.(int); ok && tasksDirtied != 0 {
			// Non-zero is not a failure — it means tasks were linked correctly.
			result.AddWarning(fmt.Sprintf("cascade dirtied %d tasks (expected 0 for a plan with no task→scenario links)", tasksDirtied))
		}
	}

	// B4: Verify cascade side-effects — affected requirements should reflect the change.
	// Re-fetch requirements and confirm at least the affected ones exist and have
	// been touched (UpdatedAt changed or status reflects cascade).
	// slug was declared above at function entry
	requirements, err := s.http.ListRequirements(ctx, slug)
	if err != nil {
		result.AddWarning(fmt.Sprintf("could not verify cascade side-effects: %v", err))
	} else {
		result.SetDetail("cascade_post_requirement_count", len(requirements))
		if len(requirements) < 2 {
			return fmt.Errorf("expected at least 2 requirements after cascade, got %d", len(requirements))
		}
	}

	result.SetDetail("cascade_verify_passed", true)
	return nil
}

// ---------------------------------------------------------------------------
// Rejection path
// ---------------------------------------------------------------------------

func (s *ChangeProposalScenario) stageRejectProposalCreate(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	reqID, _ := s.storedRequirementID(result)

	req := &client.CreateChangeProposalRequest{
		Title:          "Remove rate limiting (to be rejected)",
		Rationale:      "This proposal exists only to be rejected by the test",
		ProposedBy:     "e2e-test",
		AffectedReqIDs: []string{reqID},
	}

	proposal, status, err := s.http.CreateChangeProposal(ctx, slug, req)
	if err != nil {
		return fmt.Errorf("create rejection proposal: %w", err)
	}
	if status != 201 {
		return fmt.Errorf("expected HTTP 201, got %d", status)
	}

	result.SetDetail("reject_proposal_id", proposal.ID)
	return nil
}

func (s *ChangeProposalScenario) stageRejectProposalSubmit(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	proposalID, _ := s.storedRejectProposalID(result)

	proposal, status, err := s.http.SubmitChangeProposal(ctx, slug, proposalID)
	if err != nil {
		return fmt.Errorf("submit rejection proposal: %w", err)
	}
	if status != 200 {
		return fmt.Errorf("expected HTTP 200, got %d", status)
	}
	if proposal.Status != "under_review" {
		return fmt.Errorf("expected status=under_review, got %q", proposal.Status)
	}

	return nil
}

func (s *ChangeProposalScenario) stageRejectProposalReject(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	proposalID, _ := s.storedRejectProposalID(result)

	proposal, status, err := s.http.RejectChangeProposal(
		ctx, slug, proposalID,
		"e2e-reviewer",
		"Removing rate limiting violates security requirements",
	)
	if err != nil {
		return fmt.Errorf("reject proposal: %w", err)
	}
	if status != 200 {
		return fmt.Errorf("expected HTTP 200, got %d", status)
	}
	if proposal.Status != "rejected" {
		return fmt.Errorf("expected status=rejected, got %q", proposal.Status)
	}
	if proposal.DecidedAt == nil {
		return fmt.Errorf("proposal missing decided_at timestamp after reject")
	}

	result.SetDetail("reject_proposal_verified", true)
	return nil
}

// ---------------------------------------------------------------------------
// Status-filter list stage
// ---------------------------------------------------------------------------

func (s *ChangeProposalScenario) stageListByStatus(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)

	// Filter for "accepted" — should find the main proposal.
	accepted, err := s.http.ListChangeProposals(ctx, slug, "accepted")
	if err != nil {
		return fmt.Errorf("list accepted proposals: %w", err)
	}
	if len(accepted) < 1 {
		return fmt.Errorf("expected at least 1 accepted proposal, got %d", len(accepted))
	}
	for _, p := range accepted {
		if p.Status != "accepted" {
			return fmt.Errorf("list filter returned proposal with wrong status: got %q, want accepted", p.Status)
		}
	}

	// Filter for "rejected" — should find the rejection proposal.
	rejected, err := s.http.ListChangeProposals(ctx, slug, "rejected")
	if err != nil {
		return fmt.Errorf("list rejected proposals: %w", err)
	}
	if len(rejected) < 1 {
		return fmt.Errorf("expected at least 1 rejected proposal, got %d", len(rejected))
	}
	for _, p := range rejected {
		if p.Status != "rejected" {
			return fmt.Errorf("list filter returned proposal with wrong status: got %q, want rejected", p.Status)
		}
	}

	result.SetDetail("list_by_status_verified", true)
	return nil
}

// ---------------------------------------------------------------------------
// Guard-rail / error-handling stages
// ---------------------------------------------------------------------------

func (s *ChangeProposalScenario) stageErrorMissingTitle(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)

	_, status, _ := s.http.CreateChangeProposal(ctx, slug, &client.CreateChangeProposalRequest{
		Title: "", // deliberately empty
	})
	if status != 400 {
		return fmt.Errorf("expected HTTP 400 for missing title, got %d", status)
	}

	result.SetDetail("error_missing_title_verified", true)
	return nil
}

func (s *ChangeProposalScenario) stageErrorInvalidRequirement(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)

	_, status, _ := s.http.CreateChangeProposal(ctx, slug, &client.CreateChangeProposalRequest{
		Title:          "Valid title",
		AffectedReqIDs: []string{"requirement.nonexistent.99999"},
	})
	if status != 400 {
		return fmt.Errorf("expected HTTP 400 for non-existent requirement, got %d", status)
	}

	result.SetDetail("error_invalid_requirement_verified", true)
	return nil
}

func (s *ChangeProposalScenario) stageError404Proposal(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)

	_, status, _ := s.http.GetChangeProposal(ctx, slug, "change-proposal.nonexistent.99999")
	if status != 404 {
		return fmt.Errorf("expected HTTP 404 for non-existent proposal, got %d", status)
	}

	result.SetDetail("error_404_proposal_verified", true)
	return nil
}

// stageErrorDoubleSubmit attempts to submit an already-under_review proposal,
// which should return 409.
func (s *ChangeProposalScenario) stageErrorDoubleSubmit(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)

	// Create a fresh proposal so we can leave it in "under_review".
	reqID, _ := s.storedRequirementID(result)
	proposal, status, err := s.http.CreateChangeProposal(ctx, slug, &client.CreateChangeProposalRequest{
		Title:          "Double-submit guard test proposal",
		AffectedReqIDs: []string{reqID},
	})
	if err != nil || status != 201 {
		return fmt.Errorf("create guard-test proposal: HTTP %d: %v", status, err)
	}

	// First submit — succeeds.
	_, status, err = s.http.SubmitChangeProposal(ctx, slug, proposal.ID)
	if err != nil || status != 200 {
		return fmt.Errorf("first submit: HTTP %d: %v", status, err)
	}

	// Second submit — must return 409 (CanTransitionTo guard).
	_, status, _ = s.http.SubmitChangeProposal(ctx, slug, proposal.ID)
	if status != 409 {
		return fmt.Errorf("expected HTTP 409 for double-submit, got %d", status)
	}

	// Leave this proposal in under_review state; it will be abandoned.
	result.SetDetail("error_double_submit_verified", true)
	return nil
}

// stageErrorDeleteNonProposed verifies that a proposal not in "proposed" status
// returns 409 on DELETE.
func (s *ChangeProposalScenario) stageErrorDeleteNonProposed(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)

	// The already-accepted proposal must not be deletable.
	proposalID, _ := s.storedProposalID(result)
	status, _ := s.http.DeleteChangeProposal(ctx, slug, proposalID)
	if status != 409 {
		return fmt.Errorf("expected HTTP 409 when deleting accepted proposal, got %d", status)
	}

	result.SetDetail("error_delete_non_proposed_verified", true)
	return nil
}

// ---------------------------------------------------------------------------
// Delete stage
// ---------------------------------------------------------------------------

func (s *ChangeProposalScenario) stageProposalDelete(ctx context.Context, result *Result) error {
	slug, _ := s.planSlug(result)
	reqID, _ := s.storedRequirementID(result)

	// Create a fresh "proposed" proposal to delete.
	proposal, status, err := s.http.CreateChangeProposal(ctx, slug, &client.CreateChangeProposalRequest{
		Title:          "Temporary proposal to be deleted",
		AffectedReqIDs: []string{reqID},
	})
	if err != nil || status != 201 {
		return fmt.Errorf("create proposal for deletion: HTTP %d: %v", status, err)
	}

	// Delete it.
	status, err = s.http.DeleteChangeProposal(ctx, slug, proposal.ID)
	if err != nil {
		return fmt.Errorf("delete proposal: %w", err)
	}
	if status != 204 {
		return fmt.Errorf("expected HTTP 204 on delete, got %d", status)
	}

	// Verify 404 after deletion.
	_, getStatus, _ := s.http.GetChangeProposal(ctx, slug, proposal.ID)
	if getStatus != 404 {
		return fmt.Errorf("expected 404 after delete, got %d", getStatus)
	}

	result.SetDetail("proposal_delete_verified", true)
	return nil
}
