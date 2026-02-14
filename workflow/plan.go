package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Plan file constant.
const PlanFile = "plan.json"

// Sentinel errors for plan operations.
var (
	ErrSlugRequired    = errors.New("slug is required")
	ErrTitleRequired   = errors.New("title is required")
	ErrPlanNotFound    = errors.New("plan not found")
	ErrPlanExists      = errors.New("plan already exists")
	ErrInvalidSlug     = errors.New("invalid slug: must be lowercase alphanumeric with hyphens, no path separators")
	ErrAlreadyCommitted = errors.New("plan is already committed")
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

// CreatePlan creates a new plan in exploration mode (Committed=false).
func (m *Manager) CreatePlan(ctx context.Context, slug, title string) (*Plan, error) {
	if err := m.EnsureDirectories(); err != nil {
		return nil, err
	}

	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}
	if title == "" {
		return nil, ErrTitleRequired
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	changePath := m.ChangePath(slug)

	// Check if plan already exists
	planPath := filepath.Join(changePath, PlanFile)
	if _, err := os.Stat(planPath); err == nil {
		return nil, fmt.Errorf("%w: %s", ErrPlanExists, slug)
	}

	// Create change directory if it doesn't exist
	if err := os.MkdirAll(changePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create change directory: %w", err)
	}

	now := time.Now()
	plan := &Plan{
		ID:        fmt.Sprintf("plan.%s", slug),
		Slug:      slug,
		Title:     title,
		Committed: false,
		CreatedAt: now,
		// Initialize Scope field
		Scope: Scope{
			Include:    []string{},
			Exclude:    []string{},
			DoNotTouch: []string{},
		},
	}

	if err := m.SavePlan(ctx, plan); err != nil {
		return nil, err
	}

	return plan, nil
}

// LoadPlan loads a plan from .semspec/changes/{slug}/plan.json.
func (m *Manager) LoadPlan(ctx context.Context, slug string) (*Plan, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	planPath := filepath.Join(m.ChangePath(slug), PlanFile)

	data, err := os.ReadFile(planPath)
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

// SavePlan saves a plan to .semspec/changes/{slug}/plan.json.
func (m *Manager) SavePlan(ctx context.Context, plan *Plan) error {
	if err := ValidateSlug(plan.Slug); err != nil {
		return err
	}

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return err
	}

	planPath := filepath.Join(m.ChangePath(plan.Slug), PlanFile)

	// Ensure directory exists
	dir := filepath.Dir(planPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal plan: %w", err)
	}

	if err := os.WriteFile(planPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write plan: %w", err)
	}

	return nil
}

// PromotePlan transitions a plan from exploration to committed status.
// Sets Committed=true and records CommittedAt timestamp.
func (m *Manager) PromotePlan(ctx context.Context, plan *Plan) error {
	if plan.Committed {
		return fmt.Errorf("%w: %s", ErrAlreadyCommitted, plan.Slug)
	}

	now := time.Now()
	plan.Committed = true
	plan.CommittedAt = &now

	return m.SavePlan(ctx, plan)
}

// PlanExists checks if a plan exists for the given slug.
func (m *Manager) PlanExists(slug string) bool {
	if err := ValidateSlug(slug); err != nil {
		return false
	}
	planPath := filepath.Join(m.ChangePath(slug), PlanFile)
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

// ListPlans returns all plans in the changes directory.
// Returns partial results along with any errors encountered loading individual plans.
func (m *Manager) ListPlans(ctx context.Context) (*ListPlansResult, error) {
	result := &ListPlansResult{
		Plans:  []*Plan{},
		Errors: []error{},
	}

	changesPath := m.ChangesPath()

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(changesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, fmt.Errorf("failed to read changes directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check context cancellation between iterations
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		plan, err := m.LoadPlan(ctx, entry.Name())
		if err != nil {
			// Record the error but continue processing other plans
			result.Errors = append(result.Errors,
				fmt.Errorf("failed to load plan %s: %w", entry.Name(), err))
			continue
		}

		result.Plans = append(result.Plans, plan)
	}

	return result, nil
}
