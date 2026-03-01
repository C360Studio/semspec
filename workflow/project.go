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

// getPlanLock returns a mutex for the given project+plan combination.
func getPlanLock(projectSlug, planSlug string) *sync.Mutex {
	key := projectSlug + ":" + planSlug
	projectLocksMu.Lock()
	defer projectLocksMu.Unlock()
	if projectLocks[key] == nil {
		projectLocks[key] = &sync.Mutex{}
	}
	return projectLocks[key]
}

// Project represents a container for related sources and plans.
type Project struct {
	// ID is the entity ID for this project (format: c360.semspec.workflow.project.project.{slug}).
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

// ProjectsPath returns the path to the projects directory.
func (m *Manager) ProjectsPath() string {
	return filepath.Join(m.RootPath(), ProjectsDir)
}

// ProjectPath returns the path to a specific project directory.
func (m *Manager) ProjectPath(slug string) string {
	return filepath.Join(m.ProjectsPath(), slug)
}

// ProjectPlansPath returns the path to plans within a project.
func (m *Manager) ProjectPlansPath(slug string) string {
	return filepath.Join(m.ProjectPath(slug), PlansDir)
}

// ProjectPlanPath returns the path to a specific plan within a project.
func (m *Manager) ProjectPlanPath(projectSlug, planSlug string) string {
	return filepath.Join(m.ProjectPlansPath(projectSlug), planSlug)
}

// CreateProject creates a new project.
func (m *Manager) CreateProject(ctx context.Context, slug, title string) (*Project, error) {
	if err := m.EnsureDirectories(); err != nil {
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

	projectPath := m.ProjectPath(slug)

	// Use atomic directory creation - os.Mkdir fails if directory exists
	// This prevents TOCTOU race between existence check and creation
	if err := os.Mkdir(projectPath, 0755); err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrProjectExists, slug)
		}
		return nil, fmt.Errorf("failed to create project directory: %w", err)
	}

	// Create plans subdirectory
	plansPath := m.ProjectPlansPath(slug)
	if err := os.Mkdir(plansPath, 0755); err != nil {
		// Clean up project directory on failure
		os.RemoveAll(projectPath)
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

	if err := m.SaveProject(ctx, project); err != nil {
		os.RemoveAll(projectPath)
		return nil, err
	}

	return project, nil
}

// SaveProject saves a project to .semspec/projects/{slug}/project.json.
func (m *Manager) SaveProject(ctx context.Context, project *Project) error {
	if err := ValidateSlug(project.Slug); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	projectPath := filepath.Join(m.ProjectPath(project.Slug), ProjectFile)

	// Ensure directory exists
	dir := filepath.Dir(projectPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := json.MarshalIndent(project, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal project: %w", err)
	}

	if err := os.WriteFile(projectPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write project: %w", err)
	}

	return nil
}

// LoadProject loads a project from .semspec/projects/{slug}/project.json.
func (m *Manager) LoadProject(ctx context.Context, slug string) (*Project, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	projectPath := filepath.Join(m.ProjectPath(slug), ProjectFile)

	data, err := os.ReadFile(projectPath)
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
func (m *Manager) GetOrCreateDefaultProject(ctx context.Context) (*Project, error) {
	project, err := m.LoadProject(ctx, DefaultProjectSlug)
	if err == nil {
		return project, nil
	}

	if !errors.Is(err, ErrProjectNotFound) {
		return nil, err
	}

	// Create default project
	return m.CreateProject(ctx, DefaultProjectSlug, "Default Project")
}

// ProjectExists checks if a project exists.
func (m *Manager) ProjectExists(slug string) bool {
	if err := ValidateSlug(slug); err != nil {
		return false
	}
	projectPath := filepath.Join(m.ProjectPath(slug), ProjectFile)
	_, err := os.Stat(projectPath)
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
func (m *Manager) ListProjects(ctx context.Context) (*ListProjectsResult, error) {
	result := &ListProjectsResult{
		Projects: []*Project{},
		Errors:   []error{},
	}

	projectsPath := m.ProjectsPath()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(projectsPath)
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

		project, err := m.LoadProject(ctx, entry.Name())
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
func (m *Manager) UpdateProject(ctx context.Context, slug string, updates func(*Project)) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	// Acquire per-project lock to prevent concurrent updates
	lock := getProjectLock(slug)
	lock.Lock()
	defer lock.Unlock()

	project, err := m.LoadProject(ctx, slug)
	if err != nil {
		return err
	}

	if project.IsArchived() {
		return fmt.Errorf("%w: %s", ErrProjectArchived, slug)
	}

	updates(project)
	project.UpdatedAt = time.Now()

	return m.SaveProject(ctx, project)
}

// ArchiveProject soft-deletes a project by setting its status to archived.
func (m *Manager) ArchiveProject(ctx context.Context, slug string) error {
	return m.UpdateProject(ctx, slug, func(p *Project) {
		now := time.Now()
		p.Status = ProjectStatusArchived
		p.ArchivedAt = &now
	})
}

// DeleteProject permanently removes a project and all its contents.
// This is a destructive operation and cannot be undone.
// Uses per-project locking to prevent race with concurrent updates.
func (m *Manager) DeleteProject(ctx context.Context, slug string) error {
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

	projectPath := m.ProjectPath(slug)

	if _, err := os.Stat(projectPath); os.IsNotExist(err) {
		return fmt.Errorf("%w: %s", ErrProjectNotFound, slug)
	}

	if err := os.RemoveAll(projectPath); err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}

	return nil
}

// ListProjectPlans returns all plans for a specific project.
func (m *Manager) ListProjectPlans(ctx context.Context, projectSlug string) (*ListPlansResult, error) {
	result := &ListPlansResult{
		Plans:  []*Plan{},
		Errors: []error{},
	}

	if err := ValidateSlug(projectSlug); err != nil {
		return nil, err
	}

	plansPath := m.ProjectPlansPath(projectSlug)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(plansPath)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, fmt.Errorf("failed to read plans directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		if err := ctx.Err(); err != nil {
			return nil, err
		}

		plan, err := m.LoadProjectPlan(ctx, projectSlug, entry.Name())
		if err != nil {
			result.Errors = append(result.Errors,
				fmt.Errorf("failed to load plan %s: %w", entry.Name(), err))
			continue
		}

		result.Plans = append(result.Plans, plan)
	}

	return result, nil
}

// CreateProjectPlan creates a new plan within a project.
// Uses per-plan locking and atomic directory creation to prevent races.
func (m *Manager) CreateProjectPlan(ctx context.Context, projectSlug, planSlug, title string) (*Plan, error) {
	if err := ValidateSlug(projectSlug); err != nil {
		return nil, err
	}
	if err := ValidateSlug(planSlug); err != nil {
		return nil, err
	}
	if title == "" {
		return nil, ErrTitleRequired
	}

	// Ensure project exists (create default if needed)
	if projectSlug == DefaultProjectSlug {
		if _, err := m.GetOrCreateDefaultProject(ctx); err != nil {
			return nil, err
		}
	} else if !m.ProjectExists(projectSlug) {
		return nil, fmt.Errorf("%w: %s", ErrProjectNotFound, projectSlug)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Acquire per-plan lock to prevent concurrent plan creation
	lock := getPlanLock(projectSlug, planSlug)
	lock.Lock()
	defer lock.Unlock()

	planPath := m.ProjectPlanPath(projectSlug, planSlug)
	planFile := filepath.Join(planPath, PlanFile)

	// Check if plan already exists (under lock, so this is safe)
	if _, err := os.Stat(planFile); err == nil {
		return nil, fmt.Errorf("%w: %s", ErrPlanExists, planSlug)
	}

	// Create plan directory if it doesn't exist
	if err := os.MkdirAll(planPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create plan directory: %w", err)
	}

	now := time.Now()
	plan := &Plan{
		ID:        PlanEntityID(planSlug),
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

	if err := m.SaveProjectPlan(ctx, projectSlug, plan); err != nil {
		os.RemoveAll(planPath)
		return nil, err
	}

	return plan, nil
}

// LoadProjectPlan loads a plan from .semspec/projects/{project}/plans/{plan}/plan.json.
func (m *Manager) LoadProjectPlan(ctx context.Context, projectSlug, planSlug string) (*Plan, error) {
	if err := ValidateSlug(projectSlug); err != nil {
		return nil, err
	}
	if err := ValidateSlug(planSlug); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	planPath := filepath.Join(m.ProjectPlanPath(projectSlug, planSlug), PlanFile)

	data, err := os.ReadFile(planPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrPlanNotFound, planSlug)
		}
		return nil, fmt.Errorf("failed to read plan: %w", err)
	}

	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("failed to parse plan: %w", err)
	}

	return &plan, nil
}

// SaveProjectPlan saves a plan to .semspec/projects/{project}/plans/{plan}/plan.json.
func (m *Manager) SaveProjectPlan(ctx context.Context, projectSlug string, plan *Plan) error {
	if err := ValidateSlug(projectSlug); err != nil {
		return err
	}
	if err := ValidateSlug(plan.Slug); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	planPath := filepath.Join(m.ProjectPlanPath(projectSlug, plan.Slug), PlanFile)

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
