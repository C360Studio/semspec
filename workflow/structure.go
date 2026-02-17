package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Directory constants for the .semspec structure.
const (
	RootDir        = ".semspec"
	ConstitutionMD = "constitution.md"
	SpecsDir       = "specs"
	ArchiveDir     = "archive"
	MetadataFile   = "metadata.json"
	TasksFile      = "tasks.md"
	PlanSpecsDir   = "specs" // Specs within a plan directory

	// New project-based structure
	// Projects live in .semspec/projects/{project-slug}/
	// Plans within projects live in .semspec/projects/{project-slug}/plans/{plan-slug}/
)

// Manager provides file operations for the Semspec workflow.
type Manager struct {
	repoRoot string
}

// NewManager creates a new workflow manager for the given repository root.
func NewManager(repoRoot string) *Manager {
	return &Manager{repoRoot: repoRoot}
}

// RootPath returns the full path to .semspec directory.
func (m *Manager) RootPath() string {
	return filepath.Join(m.repoRoot, RootDir)
}

// ConstitutionPath returns the path to constitution.md.
func (m *Manager) ConstitutionPath() string {
	return filepath.Join(m.RootPath(), ConstitutionMD)
}

// SpecsPath returns the path to the specs directory.
func (m *Manager) SpecsPath() string {
	return filepath.Join(m.RootPath(), SpecsDir)
}

// PlansPath returns the path to the plans directory.
func (m *Manager) PlansPath() string {
	return filepath.Join(m.RootPath(), PlansDir)
}

// ArchivePath returns the path to the archive directory.
func (m *Manager) ArchivePath() string {
	return filepath.Join(m.RootPath(), ArchiveDir)
}

// PlanPath returns the path to a specific plan directory.
func (m *Manager) PlanPath(slug string) string {
	return filepath.Join(m.PlansPath(), slug)
}

// EnsureDirectories creates the .semspec directory structure if it doesn't exist.
func (m *Manager) EnsureDirectories() error {
	dirs := []string{
		m.RootPath(),
		m.SpecsPath(),
		m.PlansPath(),
		m.ArchivePath(),
		m.ProjectsPath(),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// Slugify converts a description to a URL-friendly slug.
func Slugify(description string) string {
	// Convert to lowercase
	slug := strings.ToLower(description)

	// Replace spaces and underscores with hyphens
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "_", "-")

	// Remove non-alphanumeric characters except hyphens
	reg := regexp.MustCompile(`[^a-z0-9-]`)
	slug = reg.ReplaceAllString(slug, "")

	// Replace multiple hyphens with single hyphen
	reg = regexp.MustCompile(`-+`)
	slug = reg.ReplaceAllString(slug, "-")

	// Trim hyphens from ends
	slug = strings.Trim(slug, "-")

	// Limit length
	if len(slug) > 50 {
		slug = slug[:50]
		// Don't end on a hyphen
		slug = strings.TrimRight(slug, "-")
	}

	return slug
}

// CreatePlanRecord creates a new plan directory with initial metadata.
func (m *Manager) CreatePlanRecord(description, author string) (*PlanRecord, error) {
	if err := m.EnsureDirectories(); err != nil {
		return nil, err
	}

	slug := Slugify(description)
	if slug == "" {
		return nil, fmt.Errorf("description must produce a valid slug")
	}

	planPath := m.PlanPath(slug)

	// Check if plan already exists
	if _, err := os.Stat(planPath); err == nil {
		return nil, fmt.Errorf("plan '%s' already exists", slug)
	}

	// Create plan directory
	if err := os.MkdirAll(planPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create plan directory: %w", err)
	}

	// Create specs subdirectory
	specsSubdir := filepath.Join(planPath, PlanSpecsDir)
	if err := os.MkdirAll(specsSubdir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create specs subdirectory: %w", err)
	}

	now := time.Now()
	plan := &PlanRecord{
		Slug:        slug,
		Title:       description,
		Description: description,
		Status:      StatusCreated,
		Author:      author,
		CreatedAt:   now,
		UpdatedAt:   now,
		Files:       PlanFiles{},
	}

	// Save metadata
	if err := m.SavePlanMetadata(plan); err != nil {
		// Clean up on failure
		os.RemoveAll(planPath)
		return nil, err
	}

	return plan, nil
}

// SavePlanMetadata saves the plan metadata to metadata.json.
func (m *Manager) SavePlanMetadata(plan *PlanRecord) error {
	metadataPath := filepath.Join(m.PlanPath(plan.Slug), MetadataFile)

	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	return nil
}

// LoadPlanRecord loads a plan record from its directory.
func (m *Manager) LoadPlanRecord(slug string) (*PlanRecord, error) {
	metadataPath := filepath.Join(m.PlanPath(slug), MetadataFile)

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("plan '%s' not found", slug)
		}
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	var plan PlanRecord
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	// Update file existence flags
	m.updateFileFlags(&plan)

	return &plan, nil
}

// updateFileFlags checks which files exist for a plan.
func (m *Manager) updateFileFlags(plan *PlanRecord) {
	planPath := m.PlanPath(plan.Slug)

	plan.Files.HasPlan = fileExists(filepath.Join(planPath, "plan.md"))
	plan.Files.HasTasks = fileExists(filepath.Join(planPath, TasksFile))
}

// ListPlanRecords returns all active plan records.
func (m *Manager) ListPlanRecords() ([]*PlanRecord, error) {
	plansPath := m.PlansPath()

	entries, err := os.ReadDir(plansPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []*PlanRecord{}, nil
		}
		return nil, fmt.Errorf("failed to read plans directory: %w", err)
	}

	var plans []*PlanRecord
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		plan, err := m.LoadPlanRecord(entry.Name())
		if err != nil {
			// Skip invalid plans
			continue
		}

		plans = append(plans, plan)
	}

	return plans, nil
}

// UpdatePlanStatus updates the status of a plan record.
func (m *Manager) UpdatePlanStatus(slug string, status Status) error {
	plan, err := m.LoadPlanRecord(slug)
	if err != nil {
		return err
	}

	if !plan.Status.CanTransitionTo(status) {
		return fmt.Errorf("cannot transition from %s to %s", plan.Status, status)
	}

	plan.Status = status
	plan.UpdatedAt = time.Now()

	return m.SavePlanMetadata(plan)
}

// WriteTasks writes the tasks.md file for a plan.
func (m *Manager) WriteTasks(slug, content string) error {
	tasksPath := filepath.Join(m.PlanPath(slug), TasksFile)
	return m.writeFile(tasksPath, content)
}

// ReadTasks reads the tasks.md file for a plan.
func (m *Manager) ReadTasks(slug string) (string, error) {
	tasksPath := filepath.Join(m.PlanPath(slug), TasksFile)
	return m.readFile(tasksPath)
}

// ArchivePlanRecord moves a completed plan to the archive.
func (m *Manager) ArchivePlanRecord(slug string) error {
	plan, err := m.LoadPlanRecord(slug)
	if err != nil {
		return err
	}

	if plan.Status != StatusComplete {
		return fmt.Errorf("cannot archive plan with status %s (must be complete)", plan.Status)
	}

	srcPath := m.PlanPath(slug)
	dstPath := filepath.Join(m.ArchivePath(), slug)

	// Move specs to source of truth if they exist
	srcSpecs := filepath.Join(srcPath, PlanSpecsDir)
	if _, err := os.Stat(srcSpecs); err == nil {
		entries, err := os.ReadDir(srcSpecs)
		if err == nil && len(entries) > 0 {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				srcSpec := filepath.Join(srcSpecs, entry.Name())
				dstSpec := filepath.Join(m.SpecsPath(), entry.Name())
				if err := os.Rename(srcSpec, dstSpec); err != nil {
					return fmt.Errorf("failed to move spec %s: %w", entry.Name(), err)
				}
			}
		}
	}

	// Move plan to archive
	if err := os.Rename(srcPath, dstPath); err != nil {
		return fmt.Errorf("failed to archive plan: %w", err)
	}

	// Update metadata in archive
	plan.Status = StatusArchived
	plan.UpdatedAt = time.Now()
	archivedMetadataPath := filepath.Join(dstPath, MetadataFile)
	data, _ := json.MarshalIndent(plan, "", "  ")
	os.WriteFile(archivedMetadataPath, data, 0644)

	return nil
}

// LoadConstitution loads the constitution from .semspec/constitution.md.
func (m *Manager) LoadConstitution() (*Constitution, error) {
	content, err := m.readFile(m.ConstitutionPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("constitution not found at %s", m.ConstitutionPath())
		}
		return nil, err
	}

	return ParseConstitution(content)
}

// ParseConstitution parses a constitution markdown file.
func ParseConstitution(content string) (*Constitution, error) {
	constitution := &Constitution{
		Version:    "1.0.0",
		Principles: []Principle{},
	}

	lines := strings.Split(content, "\n")
	var currentPrinciple *Principle
	var inRationale bool
	principleNum := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Parse version
		if strings.HasPrefix(trimmed, "Version:") {
			constitution.Version = strings.TrimSpace(strings.TrimPrefix(trimmed, "Version:"))
			continue
		}

		// Parse ratified date
		if strings.HasPrefix(trimmed, "Ratified:") {
			dateStr := strings.TrimSpace(strings.TrimPrefix(trimmed, "Ratified:"))
			if t, err := time.Parse("2006-01-02", dateStr); err == nil {
				constitution.Ratified = t
			}
			continue
		}

		// Parse principle headers (### 1. Title)
		if strings.HasPrefix(trimmed, "### ") {
			// Save previous principle
			if currentPrinciple != nil {
				constitution.Principles = append(constitution.Principles, *currentPrinciple)
			}

			principleNum++
			title := strings.TrimPrefix(trimmed, "### ")
			// Remove number prefix if present
			if idx := strings.Index(title, ". "); idx != -1 {
				title = title[idx+2:]
			}

			currentPrinciple = &Principle{
				Number: principleNum,
				Title:  title,
			}
			inRationale = false
			continue
		}

		// Parse rationale
		if strings.HasPrefix(trimmed, "Rationale:") {
			if currentPrinciple != nil {
				inRationale = true
				currentPrinciple.Rationale = strings.TrimSpace(strings.TrimPrefix(trimmed, "Rationale:"))
			}
			continue
		}

		// Accumulate description or rationale
		if currentPrinciple != nil && trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			if inRationale {
				if currentPrinciple.Rationale != "" {
					currentPrinciple.Rationale += " "
				}
				currentPrinciple.Rationale += trimmed
			} else {
				if currentPrinciple.Description != "" {
					currentPrinciple.Description += " "
				}
				currentPrinciple.Description += trimmed
			}
		}
	}

	// Save last principle
	if currentPrinciple != nil {
		constitution.Principles = append(constitution.Principles, *currentPrinciple)
	}

	return constitution, nil
}

// ListSpecs returns all specs in the specs directory.
func (m *Manager) ListSpecs() ([]*Spec, error) {
	specsPath := m.SpecsPath()

	entries, err := os.ReadDir(specsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Spec{}, nil
		}
		return nil, fmt.Errorf("failed to read specs directory: %w", err)
	}

	var specs []*Spec
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		specPath := filepath.Join(specsPath, entry.Name(), "spec.md")
		info, err := os.Stat(specPath)
		if err != nil {
			continue
		}

		specs = append(specs, &Spec{
			Name:      entry.Name(),
			Title:     entry.Name(),
			Version:   "1.0.0",
			CreatedAt: info.ModTime(),
			UpdatedAt: info.ModTime(),
		})
	}

	return specs, nil
}

// writeFile writes content to a file, creating parent directories if needed.
func (m *Manager) writeFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// readFile reads content from a file.
func (m *Manager) readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// fileExists returns true if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
