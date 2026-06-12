package workflow

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/c360studio/semspec/workflow/graphutil"
)

// PlanFile is the filename for plan metadata within a plan directory.
const PlanFile = "plan.json"

// Sentinel errors for plan operations.
var (
	ErrSlugRequired      = errors.New("slug is required")
	ErrTitleRequired     = errors.New("title is required")
	ErrPlanNotFound      = errors.New("plan not found")
	ErrPlanExists        = errors.New("plan already exists")
	ErrInvalidSlug       = errors.New("invalid slug: must be lowercase alphanumeric with hyphens, no path separators")
	ErrAlreadyApproved   = errors.New("plan is already approved")
	ErrInvalidTransition = errors.New("invalid status transition")
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

// QAWorktreeID returns the deterministic sandbox task_id of the per-plan QA
// worktree — a throwaway checkout of the assembled plan branch
// ("semspec/plan-<slug>") that both the unit-test runner and the release-gate
// Murat loop inspect, so QA evaluates the merged implementation rather than the
// pre-implementation main HEAD. Determinism lets plan-manager (creator),
// qa-reviewer (release loop), and the sandbox unit runner converge on the same
// worktree without extra plumbing. The slug is already path-safe (ValidateSlug),
// so the "qa-" prefix yields a safe task_id token.
func QAWorktreeID(slug string) string {
	return "qa-" + slug
}

// CreatePlan creates a new plan in draft mode (Approved=false).
// Plans are created in the default project at .semspec/projects/default/plans/{slug}/.
func CreatePlan(ctx context.Context, tw *graphutil.TripleWriter, slug, title string) (*Plan, error) {
	// Delegate to project-based function with default project
	return CreateProjectPlan(ctx, tw, DefaultProjectSlug, slug, title)
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
