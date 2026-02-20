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
