package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/c360studio/semspec/pkg/paths"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/c360studio/semspec/vocabulary/semspec"
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
	ErrPlanNotUpdatable  = errors.New("plan cannot be updated in current state")
	ErrPlanNotDeletable  = errors.New("plan cannot be deleted in current state")
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

// CreatePlan creates a new plan in draft mode (Approved=false).
// Plans are created in the default project at .semspec/projects/default/plans/{slug}/.
func CreatePlan(ctx context.Context, tw *graphutil.TripleWriter, slug, title string) (*Plan, error) {
	// Delegate to project-based function with default project
	return CreateProjectPlan(ctx, tw, DefaultProjectSlug, slug, title)
}

// LoadPlan loads a plan from ENTITY_STATES triples (default project).
func LoadPlan(ctx context.Context, tw *graphutil.TripleWriter, slug string) (*Plan, error) {
	// Delegate to project-based function with default project
	return LoadProjectPlan(ctx, tw, DefaultProjectSlug, slug)
}

// LoadPlanFromDisk loads a plan from its plan.json file on the filesystem.
// This is used by tools that operate against the local filesystem directly
// (e.g. DocumentExecutor) and do not have access to the KV store.
// Returns ErrPlanNotFound if the plan.json does not exist.
func LoadPlanFromDisk(repoRoot, slug string) (*Plan, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}
	planFile := filepath.Join(paths.ProjectPlanPath(repoRoot, DefaultProjectSlug, slug), PlanFile)
	data, err := os.ReadFile(planFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrPlanNotFound, slug)
		}
		return nil, fmt.Errorf("failed to read plan: %w", err)
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("failed to parse plan: %w", err)
	}
	return &plan, nil
}

// SavePlan saves a plan to ENTITY_STATES triples.
// The project is determined from plan.ProjectID, defaulting to "default" project.
func SavePlan(ctx context.Context, tw *graphutil.TripleWriter, plan *Plan) error {
	// Extract project slug from ProjectID or use default
	projectSlug := ExtractProjectSlug(plan.ProjectID)
	if projectSlug == "" {
		projectSlug = DefaultProjectSlug
	}
	return SaveProjectPlan(ctx, tw, projectSlug, plan)
}

// ExtractProjectSlug extracts the project slug from an entity ID.
// Format: {prefix}.wf.project.project.{slug}
// Returns empty string if the format is invalid.
func ExtractProjectSlug(projectID string) string {
	prefix := EntityPrefix() + ".wf.project.project."
	slug, ok := strings.CutPrefix(projectID, prefix)
	if !ok || slug == "" {
		return ""
	}
	return slug
}

// ApprovePlan transitions a plan from draft to approved status.
// Sets Approved=true, Status=StatusApproved, and records ApprovedAt timestamp.
func ApprovePlan(ctx context.Context, tw *graphutil.TripleWriter, plan *Plan) error {
	if plan.Approved {
		return fmt.Errorf("%w: %s", ErrAlreadyApproved, plan.Slug)
	}

	now := time.Now()
	plan.Approved = true
	plan.ApprovedAt = &now
	plan.Status = StatusApproved

	return SavePlan(ctx, tw, plan)
}

// SetPlanStatus transitions a plan to a new status, validating the transition.
// This is the low-level function for status changes that don't have dedicated functions.
func SetPlanStatus(ctx context.Context, tw *graphutil.TripleWriter, plan *Plan, target Status) error {
	current := plan.EffectiveStatus()
	if !current.CanTransitionTo(target) {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, current, target)
	}
	plan.Status = target
	return SavePlan(ctx, tw, plan)
}

// PlanExists checks if a plan exists for the given slug via ENTITY_STATES triples.
func PlanExists(ctx context.Context, tw *graphutil.TripleWriter, slug string) bool {
	if err := ValidateSlug(slug); err != nil {
		return false
	}
	if tw == nil {
		return false
	}
	triples, err := tw.ReadEntity(ctx, PlanEntityID(slug))
	return err == nil && len(triples) > 0 && triples[semspec.PredicatePlanStatus] != "deleted"
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
func ListPlans(ctx context.Context, tw *graphutil.TripleWriter) (*ListPlansResult, error) {
	// Delegate to project-based function with default project
	return ListProjectPlans(ctx, tw, DefaultProjectSlug)
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
// Returns the updated plan.
func UpdatePlan(ctx context.Context, tw *graphutil.TripleWriter, slug string, req UpdatePlanRequest) (*Plan, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Load the plan
	plan, err := LoadPlan(ctx, tw, slug)
	if err != nil {
		return nil, err
	}

	// State guard: Cannot update if status is implementing, complete, or archived
	effectiveStatus := plan.EffectiveStatus()
	if effectiveStatus == StatusImplementing || effectiveStatus == StatusComplete || effectiveStatus == StatusArchived {
		return nil, fmt.Errorf("%w: cannot update plan with status %s", ErrPlanNotUpdatable, effectiveStatus)
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
	if err := SavePlan(ctx, tw, plan); err != nil {
		return nil, err
	}

	return plan, nil
}

// DeletePlan soft-deletes a plan by writing a "deleted" status tombstone.
// State guard: Cannot delete if Status >= StatusImplementing
func DeletePlan(ctx context.Context, tw *graphutil.TripleWriter, slug string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	// Verify plan exists
	plan, err := LoadPlan(ctx, tw, slug)
	if err != nil {
		return err
	}

	// State guard: Cannot delete if status is implementing, complete, or archived
	effectiveStatus := plan.EffectiveStatus()
	if effectiveStatus == StatusImplementing || effectiveStatus == StatusComplete || effectiveStatus == StatusArchived {
		return fmt.Errorf("%w: cannot delete plan with status %s", ErrPlanNotDeletable, effectiveStatus)
	}

	// Write tombstone status triple — LoadPlan treats "deleted" as not found.
	return tw.WriteTriple(ctx, PlanEntityID(slug), semspec.PredicatePlanStatus, "deleted")
}

// ArchivePlan soft deletes a plan by setting status to archived.
// Note: Unlike DeletePlan, archiving is allowed from any non-terminal state.
func ArchivePlan(ctx context.Context, tw *graphutil.TripleWriter, slug string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	// Load the plan
	plan, err := LoadPlan(ctx, tw, slug)
	if err != nil {
		return err
	}

	// State guard: Cannot archive if status is implementing, complete, or already archived
	effectiveStatus := plan.EffectiveStatus()
	if effectiveStatus == StatusImplementing || effectiveStatus == StatusComplete || effectiveStatus == StatusArchived {
		return fmt.Errorf("%w: cannot archive plan with status %s", ErrPlanNotDeletable, effectiveStatus)
	}

	// Set status to archived
	plan.Status = StatusArchived

	// Save the updated plan
	return SavePlan(ctx, tw, plan)
}

// UnarchivePlan restores an archived plan to complete status.
func UnarchivePlan(ctx context.Context, tw *graphutil.TripleWriter, slug string) error {
	plan, err := LoadPlan(ctx, tw, slug)
	if err != nil {
		return err
	}

	if plan.EffectiveStatus() != StatusArchived {
		return fmt.Errorf("plan %q is not archived (status: %s)", slug, plan.EffectiveStatus())
	}

	plan.Status = StatusComplete
	return SavePlan(ctx, tw, plan)
}

// ResetPlan returns a failed/rejected plan to approved status and clears
// generated artifacts (requirements, scenarios) so the pipeline can retry
// from scratch. The plan's goal, context, and scope are preserved.
func ResetPlan(ctx context.Context, tw *graphutil.TripleWriter, slug string) error {
	plan, err := LoadPlan(ctx, tw, slug)
	if err != nil {
		return err
	}

	status := plan.EffectiveStatus()
	if !status.CanTransitionTo(StatusApproved) {
		return fmt.Errorf("cannot reset plan with status %s", status)
	}

	// Reset status and review fields.
	plan.Status = StatusApproved
	plan.ReviewVerdict = ""
	plan.ReviewSummary = ""
	plan.ReviewedAt = nil

	return SavePlan(ctx, tw, plan)
}
