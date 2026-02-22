package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// PlanFile is the filename for plan metadata within a plan directory.
const PlanFile = "plan.json"

// Sentinel errors for plan operations.
var (
	ErrSlugRequired         = errors.New("slug is required")
	ErrTitleRequired        = errors.New("title is required")
	ErrPlanNotFound         = errors.New("plan not found")
	ErrPlanExists           = errors.New("plan already exists")
	ErrInvalidSlug          = errors.New("invalid slug: must be lowercase alphanumeric with hyphens, no path separators")
	ErrAlreadyApproved      = errors.New("plan is already approved")
	ErrTasksAlreadyApproved = errors.New("tasks are already approved")
	ErrPlanNotUpdatable     = errors.New("plan cannot be updated in current state")
	ErrPlanNotDeletable     = errors.New("plan cannot be deleted in current state")
)

// slugPattern validates slugs: lowercase alphanumeric with hyphens, 1-50 chars.
var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,48}[a-z0-9])?$`)

// ValidateSlug checks if a slug is valid and safe for use in file paths.
func ValidateSlug(slug string) error {
	if slug == "" {
		return ErrSlugRequired
	}
	// Prevent path traversal attacks
	if strings.Contains(slug, "..") || strings.Contains(slug, "/") || strings.Contains(slug, "\\") {
		return ErrInvalidSlug
	}
	// Must match pattern: lowercase alphanumeric with hyphens
	if !slugPattern.MatchString(slug) {
		return ErrInvalidSlug
	}
	return nil
}

// CreatePlan creates a new plan in draft mode (Approved=false).
// Plans are created in the default project at .semspec/projects/default/plans/{slug}/.
func (m *Manager) CreatePlan(ctx context.Context, slug, title string) (*Plan, error) {
	// Delegate to project-based method with default project
	return m.CreateProjectPlan(ctx, DefaultProjectSlug, slug, title)
}

// LoadPlan loads a plan from .semspec/projects/default/plans/{slug}/plan.json.
func (m *Manager) LoadPlan(ctx context.Context, slug string) (*Plan, error) {
	// Delegate to project-based method with default project
	return m.LoadProjectPlan(ctx, DefaultProjectSlug, slug)
}

// SavePlan saves a plan to .semspec/projects/{project}/plans/{slug}/plan.json.
// The project is determined from plan.ProjectID, defaulting to "default" project.
func (m *Manager) SavePlan(ctx context.Context, plan *Plan) error {
	// Extract project slug from ProjectID or use default
	projectSlug := ExtractProjectSlug(plan.ProjectID)
	if projectSlug == "" {
		projectSlug = DefaultProjectSlug
	}
	return m.SaveProjectPlan(ctx, projectSlug, plan)
}

// ExtractProjectSlug extracts the project slug from an entity ID.
// For "semspec.local.project.my-project", returns "my-project".
// Returns empty string if the format is invalid.
func ExtractProjectSlug(projectID string) string {
	const prefix = "semspec.local.project."
	if strings.HasPrefix(projectID, prefix) {
		return strings.TrimPrefix(projectID, prefix)
	}
	return ""
}

// ApprovePlan transitions a plan from draft to approved status.
// Sets Approved=true, Status=StatusApproved, and records ApprovedAt timestamp.
func (m *Manager) ApprovePlan(ctx context.Context, plan *Plan) error {
	if plan.Approved {
		return fmt.Errorf("%w: %s", ErrAlreadyApproved, plan.Slug)
	}

	now := time.Now()
	plan.Approved = true
	plan.ApprovedAt = &now
	plan.Status = StatusApproved

	return m.SavePlan(ctx, plan)
}

// ApproveTasksPlan transitions a plan to tasks-approved status.
// Sets TasksApproved=true, Status=StatusTasksApproved, and records TasksApprovedAt.
// Requires the plan to be approved and tasks to have been generated (StatusTasksGenerated).
func (m *Manager) ApproveTasksPlan(ctx context.Context, plan *Plan) error {
	if plan.TasksApproved {
		return fmt.Errorf("%w: %s", ErrTasksAlreadyApproved, plan.Slug)
	}
	if !plan.Approved {
		return fmt.Errorf("plan must be approved before approving tasks: %s", plan.Slug)
	}

	// Require tasks_generated status if the status field is in use
	effectiveStatus := plan.EffectiveStatus()
	if effectiveStatus != StatusTasksGenerated && effectiveStatus != StatusApproved {
		return fmt.Errorf("%w: cannot approve tasks from status %q", ErrInvalidTransition, effectiveStatus)
	}

	now := time.Now()
	plan.TasksApproved = true
	plan.TasksApprovedAt = &now
	plan.Status = StatusTasksApproved

	return m.SavePlan(ctx, plan)
}

// SetPlanStatus transitions a plan to a new status, validating the transition.
// This is the low-level method for status changes that don't have dedicated methods.
func (m *Manager) SetPlanStatus(ctx context.Context, plan *Plan, target Status) error {
	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(target) {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, current, target)
	}
	plan.Status = target
	return m.SavePlan(ctx, plan)
}

// PlanExists checks if a plan exists for the given slug in the default project.
func (m *Manager) PlanExists(slug string) bool {
	if err := ValidateSlug(slug); err != nil {
		return false
	}
	planPath := filepath.Join(m.ProjectPlanPath(DefaultProjectSlug, slug), PlanFile)
	_, err := os.Stat(planPath)
	return err == nil
}

// ListPlansResult contains the results of listing plans, including any
// non-fatal errors encountered while loading individual plans.
type ListPlansResult struct {
	// Plans contains successfully loaded plans
	Plans []*Plan

	// Errors contains non-fatal errors encountered while loading plans.
	// Each error indicates a plan directory that could not be loaded.
	Errors []error
}

// ListPlans returns all plans in the default project.
// Returns partial results along with any errors encountered loading individual plans.
func (m *Manager) ListPlans(ctx context.Context) (*ListPlansResult, error) {
	// Delegate to project-based method with default project
	return m.ListProjectPlans(ctx, DefaultProjectSlug)
}

// UpdatePlanRequest contains parameters for updating a plan.
// All fields are optional - only non-nil fields will be updated.
type UpdatePlanRequest struct {
	Title   *string `json:"title,omitempty"`
	Goal    *string `json:"goal,omitempty"`
	Context *string `json:"context,omitempty"`
}

// UpdatePlan updates plan fields.
// Can update: Title, Goal, Context
// State guard: Cannot update if Status >= StatusImplementing
// State guard: Cannot update if any tasks are in_progress, completed, or failed
// Returns the updated plan.
func (m *Manager) UpdatePlan(ctx context.Context, slug string, req UpdatePlanRequest) (*Plan, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Load the plan
	plan, err := m.LoadPlan(ctx, slug)
	if err != nil {
		return nil, err
	}

	// State guard: Cannot update if status is implementing, complete, or archived
	effectiveStatus := plan.EffectiveStatus()
	if effectiveStatus == StatusImplementing || effectiveStatus == StatusComplete || effectiveStatus == StatusArchived {
		return nil, fmt.Errorf("%w: cannot update plan with status %s", ErrPlanNotUpdatable, effectiveStatus)
	}

	// State guard: Cannot update if any tasks are in_progress, completed, or failed
	tasks, err := m.LoadTasks(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("failed to load tasks: %w", err)
	}

	for _, task := range tasks {
		if task.Status == TaskStatusInProgress || task.Status == TaskStatusCompleted || task.Status == TaskStatusFailed {
			return nil, fmt.Errorf("%w: task %s is %s", ErrPlanNotUpdatable, task.ID, task.Status)
		}
	}

	// Apply updates
	if req.Title != nil {
		plan.Title = *req.Title
	}
	if req.Goal != nil {
		plan.Goal = *req.Goal
	}
	if req.Context != nil {
		plan.Context = *req.Context
	}

	// Save the updated plan
	if err := m.SavePlan(ctx, plan); err != nil {
		return nil, err
	}

	return plan, nil
}

// DeletePlan permanently deletes a plan.
// Removes the .semspec/plans/{slug} directory.
// State guard: Cannot delete if Status >= StatusImplementing
// State guard: Cannot delete if any tasks are in_progress, completed, or failed
func (m *Manager) DeletePlan(ctx context.Context, slug string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	// Verify plan exists
	plan, err := m.LoadPlan(ctx, slug)
	if err != nil {
		return err
	}

	// State guard: Cannot delete if status is implementing, complete, or archived
	effectiveStatus := plan.EffectiveStatus()
	if effectiveStatus == StatusImplementing || effectiveStatus == StatusComplete || effectiveStatus == StatusArchived {
		return fmt.Errorf("%w: cannot delete plan with status %s", ErrPlanNotDeletable, effectiveStatus)
	}

	// State guard: Cannot delete if any tasks are in_progress, completed, or failed
	tasks, err := m.LoadTasks(ctx, slug)
	if err != nil {
		return fmt.Errorf("failed to load tasks: %w", err)
	}

	for _, task := range tasks {
		if task.Status == TaskStatusInProgress || task.Status == TaskStatusCompleted || task.Status == TaskStatusFailed {
			return fmt.Errorf("%w: task %s is %s", ErrPlanNotDeletable, task.ID, task.Status)
		}
	}

	// Delete the plan directory
	projectSlug := ExtractProjectSlug(plan.ProjectID)
	if projectSlug == "" {
		projectSlug = DefaultProjectSlug
	}
	planPath := m.ProjectPlanPath(projectSlug, slug)

	if err := os.RemoveAll(planPath); err != nil {
		return fmt.Errorf("failed to delete plan directory: %w", err)
	}

	return nil
}

// ArchivePlan soft deletes a plan by setting status to archived.
// Keeps the plan files intact.
// Note: Unlike DeletePlan, archiving is allowed from any state.
func (m *Manager) ArchivePlan(ctx context.Context, slug string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	// Load the plan
	plan, err := m.LoadPlan(ctx, slug)
	if err != nil {
		return err
	}

	// State guard: Cannot archive if status is implementing, complete, or already archived
	effectiveStatus := plan.EffectiveStatus()
	if effectiveStatus == StatusImplementing || effectiveStatus == StatusComplete || effectiveStatus == StatusArchived {
		return fmt.Errorf("%w: cannot archive plan with status %s", ErrPlanNotDeletable, effectiveStatus)
	}

	// State guard: Cannot archive if any tasks are in_progress, completed, or failed
	tasks, err := m.LoadTasks(ctx, slug)
	if err != nil {
		return fmt.Errorf("failed to load tasks: %w", err)
	}

	for _, task := range tasks {
		if task.Status == TaskStatusInProgress || task.Status == TaskStatusCompleted || task.Status == TaskStatusFailed {
			return fmt.Errorf("%w: task %s is %s", ErrPlanNotDeletable, task.ID, task.Status)
		}
	}

	// Set status to archived
	plan.Status = StatusArchived

	// Save the updated plan
	return m.SavePlan(ctx, plan)
}
