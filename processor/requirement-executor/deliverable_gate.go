package requirementexecutor

import (
	"context"
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
)

const deliverableFilesTimeoutMs = 15000

// handleApprovedDeliverableGapLocked prevents a scenario-approved Story from
// becoming terminal evidence while its accepted scope.create obligations are
// still absent from the requirement branch.
//
// Caller must hold exec.mu.
func (c *Component) handleApprovedDeliverableGapLocked(ctx context.Context, exec *requirementExecution) bool {
	if c.sandbox == nil || c.natsClient == nil || len(exec.SortedStoryIDs) == 0 {
		return false
	}
	plan, err := c.loadPlanFromKV(ctx, exec.Slug)
	if err != nil {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("deliverable closure gate could not load plan: %v", err))
		return true
	}
	if plan == nil {
		c.markFailedLocked(ctx, exec, "deliverable closure gate could not load plan from PLAN_STATES")
		return true
	}
	return c.handleApprovedDeliverableGapForPlanLocked(ctx, exec, plan)
}

// handleApprovedDeliverableGapForPlanLocked is the deterministic core of the
// approval gate. Tests call it directly with an in-memory Plan; production uses
// handleApprovedDeliverableGapLocked to load the authoritative PLAN_STATES copy.
//
// Caller must hold exec.mu.
func (c *Component) handleApprovedDeliverableGapForPlanLocked(ctx context.Context, exec *requirementExecution, plan *workflow.Plan) bool {
	if c.sandbox == nil || plan == nil || len(exec.SortedStoryIDs) == 0 {
		return false
	}
	if exec.CurrentStoryIdx < 0 || exec.CurrentStoryIdx >= len(exec.SortedStoryIDs) {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("deliverable closure gate could not resolve CurrentStoryIdx %d in %d stories", exec.CurrentStoryIdx, len(exec.SortedStoryIDs)))
		return true
	}

	storyID := exec.SortedStoryIDs[exec.CurrentStoryIdx]
	story, ok := findStoryByID(plan, storyID)
	if !ok {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("deliverable closure gate could not find Story %s on plan", storyID))
		return true
	}

	expected := storyScopeCreateObligations(plan, story)
	if len(expected) == 0 {
		return false
	}
	delivered, err := c.deliveredFilesForReviewerWorktree(ctx, exec)
	if err != nil {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("deliverable closure gate could not inspect requirement branch: %v", err))
		return true
	}
	missing := missingDeliverableFiles(expected, delivered)
	if len(missing) == 0 {
		return false
	}

	feedback := fmt.Sprintf(
		"Reviewer approved scenarios, but Story %s has not delivered %d accepted scope.create file(s): %s. "+
			"These paths remain required by the plan contract. Implement the missing files inside the declared Story file scope and resubmit.",
		story.ID, len(missing), strings.Join(missing, ", "),
	)
	if exec.RetryCount < exec.MaxRetries && exec.MaxRetries > 0 {
		c.startFixableRetryLocked(ctx, exec, feedback)
		return true
	}

	c.emitExhaustionDecision(ctx, exec, "scope_incomplete", feedback)
	if c.deferToAwaitingRecoveryLocked(ctx, exec, fmt.Sprintf("deliverable closure gate retries exhausted: %s", feedback)) {
		return true
	}
	c.markFailedLocked(ctx, exec, fmt.Sprintf("deliverable closure gate retries exhausted: %s", feedback))
	return true
}

// handleRequirementDeliverableGapLocked is a final local backstop before a
// requirement enters completed. Story approval owns fixable retries; this gate
// prevents terminal completion when the requirement branch is inspectable and
// accepted create obligations remain absent.
//
// Caller must hold exec.mu.
func (c *Component) handleRequirementDeliverableGapLocked(ctx context.Context, exec *requirementExecution) bool {
	if c.sandbox == nil || c.natsClient == nil || len(exec.SortedStoryIDs) == 0 || exec.ReviewerTaskID == "" {
		return false
	}
	plan, err := c.loadPlanFromKV(ctx, exec.Slug)
	if err != nil {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("requirement deliverable closure gate could not load plan: %v", err))
		return true
	}
	if plan == nil {
		c.markFailedLocked(ctx, exec, "requirement deliverable closure gate could not load plan from PLAN_STATES")
		return true
	}
	return c.handleRequirementDeliverableGapForPlanLocked(ctx, exec, plan)
}

func (c *Component) handleRequirementDeliverableGapForPlanLocked(ctx context.Context, exec *requirementExecution, plan *workflow.Plan) bool {
	if c.sandbox == nil || plan == nil || len(exec.SortedStoryIDs) == 0 || exec.ReviewerTaskID == "" {
		return false
	}
	expected := requirementScopeCreateObligations(plan, exec.SortedStoryIDs)
	if len(expected) == 0 {
		return false
	}
	delivered, err := c.deliveredFilesForReviewerWorktree(ctx, exec)
	if err != nil {
		c.markFailedLocked(ctx, exec, fmt.Sprintf("requirement deliverable closure gate could not inspect requirement branch: %v", err))
		return true
	}
	missing := missingDeliverableFiles(expected, delivered)
	if len(missing) == 0 {
		return false
	}

	feedback := fmt.Sprintf(
		"Requirement %s cannot complete because %d accepted scope.create file(s) assigned to its Stories are still missing from the requirement branch: %s.",
		exec.RequirementID, len(missing), strings.Join(missing, ", "),
	)
	c.emitExhaustionDecision(ctx, exec, "scope_incomplete", feedback)
	if c.deferToAwaitingRecoveryLocked(ctx, exec, fmt.Sprintf("requirement deliverable closure gate failed: %s", feedback)) {
		return true
	}
	c.markFailedLocked(ctx, exec, fmt.Sprintf("requirement deliverable closure gate failed: %s", feedback))
	return true
}

func storyScopeCreateObligations(plan *workflow.Plan, story workflow.Story) []string {
	if plan == nil || len(plan.Scope.Create) == 0 || len(story.FilesOwned) == 0 {
		return nil
	}
	owned := make(map[string]struct{}, len(story.FilesOwned))
	for _, raw := range story.FilesOwned {
		if p := workflow.NormalizeFilePath(raw); p != "" {
			owned[p] = struct{}{}
		}
	}
	if len(owned) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(plan.Scope.Create))
	out := make([]string, 0, len(plan.Scope.Create))
	for _, raw := range plan.Scope.Create {
		p := workflow.NormalizeFilePath(raw)
		if p == "" {
			continue
		}
		if _, ok := owned[p]; !ok {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func requirementScopeCreateObligations(plan *workflow.Plan, storyIDs []string) []string {
	if plan == nil || len(plan.Scope.Create) == 0 || len(storyIDs) == 0 {
		return nil
	}
	wantedStories := make(map[string]struct{}, len(storyIDs))
	for _, id := range storyIDs {
		if id != "" {
			wantedStories[id] = struct{}{}
		}
	}
	owned := make(map[string]struct{})
	for _, story := range plan.Stories {
		if _, ok := wantedStories[story.ID]; !ok {
			continue
		}
		for _, raw := range story.FilesOwned {
			if p := workflow.NormalizeFilePath(raw); p != "" {
				owned[p] = struct{}{}
			}
		}
	}
	if len(owned) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(plan.Scope.Create))
	out := make([]string, 0, len(plan.Scope.Create))
	for _, raw := range plan.Scope.Create {
		p := workflow.NormalizeFilePath(raw)
		if p == "" {
			continue
		}
		if _, ok := owned[p]; !ok {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func (c *Component) deliveredFilesForReviewerWorktree(ctx context.Context, exec *requirementExecution) ([]string, error) {
	if exec.ReviewerTaskID == "" {
		return nil, fmt.Errorf("reviewer worktree id is empty")
	}
	res, err := c.sandbox.Exec(ctx, exec.ReviewerTaskID, "git -c core.quotePath=false ls-files -z", deliverableFilesTimeoutMs)
	if err != nil {
		return nil, err
	}
	if res == nil || res.ExitCode != 0 {
		code := -1
		if res != nil {
			code = res.ExitCode
		}
		return nil, fmt.Errorf("git ls-files in reviewer worktree %q exited %d", exec.ReviewerTaskID, code)
	}
	raw := strings.Trim(res.Stdout, "\x00")
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\x00"), nil
}

func missingDeliverableFiles(expected, delivered []string) []string {
	have := make(map[string]struct{}, len(delivered))
	for _, raw := range delivered {
		if p := workflow.NormalizeFilePath(raw); p != "" {
			have[p] = struct{}{}
		}
	}
	var missing []string
	seen := make(map[string]struct{}, len(expected))
	for _, raw := range expected {
		p := workflow.NormalizeFilePath(raw)
		if p == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		if _, ok := have[p]; !ok {
			missing = append(missing, p)
		}
	}
	return missing
}
