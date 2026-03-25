package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/c360studio/semstreams/natsclient"
)

// DefaultProjectSlug is the slug for the auto-created default project.
// Sources and plans without explicit project assignment use this project.
const DefaultProjectSlug = "default"

// Project directory constants.
const (
	// ProjectsDir is the directory name for projects within .semspec.
	ProjectsDir = "projects"
	// ProjectFile is the filename for project metadata.
	ProjectFile = "project.json"
	// PlansDir is the directory name for plans within a project.
	PlansDir = "plans"
)

// Sentinel errors for project operations.
var (
	ErrProjectNotFound = errors.New("project not found")
	ErrProjectExists   = errors.New("project already exists")
	ErrProjectArchived = errors.New("project is archived")
)

// projectLocks provides per-project locking for concurrent operations.
var (
	projectLocksMu sync.Mutex
	projectLocks   = make(map[string]*sync.Mutex)
)

// getProjectLock returns a mutex for the given project slug.
func getProjectLock(slug string) *sync.Mutex {
	projectLocksMu.Lock()
	defer projectLocksMu.Unlock()
	if projectLocks[slug] == nil {
		projectLocks[slug] = &sync.Mutex{}
	}
	return projectLocks[slug]
}

// Project represents a container for related sources and plans.
type Project struct {
	// ID is the entity ID for this project (format: {prefix}.wf.project.project.{slug}).
	ID string `json:"id"`

	// Slug is the unique identifier used in file paths.
	Slug string `json:"slug"`

	// Title is the human-readable display name.
	Title string `json:"title"`

	// Description provides additional context about the project.
	Description string `json:"description,omitempty"`

	// Status is the current state: "active" or "archived".
	Status string `json:"status"`

	// CreatedAt is when the project was created.
	CreatedAt time.Time `json:"created_at"`

	// CreatedBy is the user/agent who created the project.
	CreatedBy string `json:"created_by,omitempty"`

	// UpdatedAt is when the project was last modified.
	UpdatedAt time.Time `json:"updated_at"`

	// ArchivedAt is when the project was archived (if applicable).
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
}

// Project status constants.
const (
	ProjectStatusActive   = "active"
	ProjectStatusArchived = "archived"
)

// IsArchived returns true if the project has been soft-deleted.
func (p *Project) IsArchived() bool {
	return p.Status == ProjectStatusArchived
}

// CreateProject creates a new project.
func CreateProject(ctx context.Context, repoRoot, slug, title string) (*Project, error) {
	if err := EnsureDirectories(repoRoot); err != nil {
		return nil, err
	}

	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}
	if title == "" {
		return nil, ErrTitleRequired
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Use per-project lock to prevent concurrent creation
	lock := getProjectLock(slug)
	lock.Lock()
	defer lock.Unlock()

	projectDir := ProjectPath(repoRoot, slug)

	// Use atomic directory creation - os.Mkdir fails if directory exists
	// This prevents TOCTOU race between existence check and creation
	if err := os.Mkdir(projectDir, 0755); err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrProjectExists, slug)
		}
		return nil, fmt.Errorf("failed to create project directory: %w", err)
	}

	// Create plans subdirectory
	plansDir := ProjectPlansPath(repoRoot, slug)
	if err := os.Mkdir(plansDir, 0755); err != nil {
		// Clean up project directory on failure
		os.RemoveAll(projectDir)
		return nil, fmt.Errorf("failed to create plans directory: %w", err)
	}

	now := time.Now()
	project := &Project{
		ID:        ProjectEntityID(slug),
		Slug:      slug,
		Title:     title,
		Status:    ProjectStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := SaveProject(ctx, repoRoot, project); err != nil {
		os.RemoveAll(projectDir)
		return nil, err
	}

	return project, nil
}

// SaveProject saves a project to .semspec/projects/{slug}/project.json.
func SaveProject(ctx context.Context, repoRoot string, project *Project) error {
	if err := ValidateSlug(project.Slug); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	projectFile := filepath.Join(ProjectPath(repoRoot, project.Slug), ProjectFile)

	// Ensure directory exists
	dir := filepath.Dir(projectFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := json.MarshalIndent(project, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal project: %w", err)
	}

	if err := os.WriteFile(projectFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write project: %w", err)
	}

	return nil
}

// LoadProject loads a project from .semspec/projects/{slug}/project.json.
func LoadProject(ctx context.Context, repoRoot, slug string) (*Project, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	projectFile := filepath.Join(ProjectPath(repoRoot, slug), ProjectFile)

	data, err := os.ReadFile(projectFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrProjectNotFound, slug)
		}
		return nil, fmt.Errorf("failed to read project: %w", err)
	}

	var project Project
	if err := json.Unmarshal(data, &project); err != nil {
		return nil, fmt.Errorf("failed to parse project: %w", err)
	}

	return &project, nil
}

// GetOrCreateDefaultProject returns the default project, creating it if needed.
func GetOrCreateDefaultProject(ctx context.Context, repoRoot string) (*Project, error) {
	project, err := LoadProject(ctx, repoRoot, DefaultProjectSlug)
	if err == nil {
		return project, nil
	}

	if !errors.Is(err, ErrProjectNotFound) {
		return nil, err
	}

	// Create default project
	return CreateProject(ctx, repoRoot, DefaultProjectSlug, "Default Project")
}

// ProjectExists checks if a project exists.
func ProjectExists(repoRoot, slug string) bool {
	if err := ValidateSlug(slug); err != nil {
		return false
	}
	projectFile := filepath.Join(ProjectPath(repoRoot, slug), ProjectFile)
	_, err := os.Stat(projectFile)
	return err == nil
}

// ListProjectsResult contains the results of listing projects.
type ListProjectsResult struct {
	// Projects contains successfully loaded projects.
	Projects []*Project

	// Errors contains non-fatal errors encountered while loading projects.
	Errors []error
}

// ListProjects returns all projects in the projects directory.
func ListProjects(ctx context.Context, repoRoot string) (*ListProjectsResult, error) {
	result := &ListProjectsResult{
		Projects: []*Project{},
		Errors:   []error{},
	}

	projectsDir := ProjectsPath(repoRoot)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, fmt.Errorf("failed to read projects directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		if err := ctx.Err(); err != nil {
			return nil, err
		}

		project, err := LoadProject(ctx, repoRoot, entry.Name())
		if err != nil {
			result.Errors = append(result.Errors,
				fmt.Errorf("failed to load project %s: %w", entry.Name(), err))
			continue
		}

		result.Projects = append(result.Projects, project)
	}

	return result, nil
}

// UpdateProject updates a project's mutable fields.
// Uses per-project locking to ensure atomic read-modify-write.
func UpdateProject(ctx context.Context, repoRoot, slug string, updates func(*Project)) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	// Acquire per-project lock to prevent concurrent updates
	lock := getProjectLock(slug)
	lock.Lock()
	defer lock.Unlock()

	project, err := LoadProject(ctx, repoRoot, slug)
	if err != nil {
		return err
	}

	if project.IsArchived() {
		return fmt.Errorf("%w: %s", ErrProjectArchived, slug)
	}

	updates(project)
	project.UpdatedAt = time.Now()

	return SaveProject(ctx, repoRoot, project)
}

// ArchiveProject soft-deletes a project by setting its status to archived.
func ArchiveProject(ctx context.Context, repoRoot, slug string) error {
	return UpdateProject(ctx, repoRoot, slug, func(p *Project) {
		now := time.Now()
		p.Status = ProjectStatusArchived
		p.ArchivedAt = &now
	})
}

// DeleteProject permanently removes a project and all its contents.
// This is a destructive operation and cannot be undone.
// Uses per-project locking to prevent race with concurrent updates.
func DeleteProject(ctx context.Context, repoRoot, slug string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	// Acquire lock to prevent race with concurrent updates
	lock := getProjectLock(slug)
	lock.Lock()
	defer lock.Unlock()

	projectDir := ProjectPath(repoRoot, slug)

	if _, err := os.Stat(projectDir); os.IsNotExist(err) {
		return fmt.Errorf("%w: %s", ErrProjectNotFound, slug)
	}

	if err := os.RemoveAll(projectDir); err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}

	return nil
}

// ListProjectPlans returns all plans for a specific project from ENTITY_STATES KV.
func ListProjectPlans(ctx context.Context, kv *natsclient.KVStore, projectSlug string) (*ListPlansResult, error) {
	result := &ListPlansResult{
		Plans:  []*Plan{},
		Errors: []error{},
	}

	if kv == nil {
		return result, nil
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	prefix := EntityPrefix() + ".wf.plan.plan."
	keys, err := kv.KeysByPrefix(ctx, prefix)
	if err != nil {
		return result, nil
	}

	projectEntityID := ProjectEntityID(projectSlug)
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var plan Plan
		if err := kvGet(ctx, kv, key, &plan); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to load plan %s: %w", key, err))
			continue
		}

		// Filter by project
		if projectSlug != "" && plan.ProjectID != "" && plan.ProjectID != projectEntityID {
			continue
		}

		result.Plans = append(result.Plans, &plan)
	}

	return result, nil
}

// CreateProjectPlan creates a new plan within a project.
// Uses KV existence check (fails if entity already exists).
func CreateProjectPlan(ctx context.Context, kv *natsclient.KVStore, projectSlug, planSlug, title string) (*Plan, error) {
	if err := ValidateSlug(projectSlug); err != nil {
		return nil, err
	}
	if err := ValidateSlug(planSlug); err != nil {
		return nil, err
	}
	if title == "" {
		return nil, ErrTitleRequired
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Check if plan already exists via KV
	entityID := PlanEntityID(planSlug)
	if kvExists(ctx, kv, entityID) {
		return nil, fmt.Errorf("%w: %s", ErrPlanExists, planSlug)
	}

	now := time.Now()
	plan := &Plan{
		ID:        entityID,
		Slug:      planSlug,
		Title:     title,
		ProjectID: ProjectEntityID(projectSlug),
		Approved:  false,
		CreatedAt: now,
		Scope: Scope{
			Include:    []string{},
			Exclude:    []string{},
			DoNotTouch: []string{},
		},
	}

	if err := SaveProjectPlan(ctx, kv, projectSlug, plan); err != nil {
		return nil, err
	}

	return plan, nil
}

// LoadProjectPlan loads a plan from ENTITY_STATES KV bucket.
func LoadProjectPlan(ctx context.Context, kv *natsclient.KVStore, projectSlug, planSlug string) (*Plan, error) {
	if err := ValidateSlug(planSlug); err != nil {
		return nil, err
	}

	if kv == nil {
		return nil, fmt.Errorf("%w: %s", ErrPlanNotFound, planSlug)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var plan Plan
	if err := kvGet(ctx, kv, PlanEntityID(planSlug), &plan); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrPlanNotFound, planSlug)
	}

	return &plan, nil
}

// SaveProjectPlan saves a plan to ENTITY_STATES KV bucket.
func SaveProjectPlan(ctx context.Context, kv *natsclient.KVStore, projectSlug string, plan *Plan) error {
	if err := ValidateSlug(plan.Slug); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := kvPut(ctx, kv, PlanEntityID(plan.Slug), plan); err != nil {
		return err
	}

	// Write-through: publish key facts to ENTITY_STATES for graph/rules
	publishEntitySideEffect(ctx, PlanEntityID(plan.Slug), EntityType, PlanTriples(plan))
	return nil
}
