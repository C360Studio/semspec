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
	RootDir         = ".semspec"
	ConstitutionMD  = "constitution.md"
	SpecsDir        = "specs"
	ChangesDir      = "changes"
	ArchiveDir      = "archive"
	MetadataFile    = "metadata.json"
	ProposalFile    = "proposal.md"
	DesignFile      = "design.md"
	SpecFile        = "spec.md"
	TasksFile       = "tasks.md"
	ChangeSpecsDir  = "specs" // Specs within a change directory
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

// ChangesPath returns the path to the changes directory.
func (m *Manager) ChangesPath() string {
	return filepath.Join(m.RootPath(), ChangesDir)
}

// ArchivePath returns the path to the archive directory.
func (m *Manager) ArchivePath() string {
	return filepath.Join(m.RootPath(), ArchiveDir)
}

// ChangePath returns the path to a specific change directory.
func (m *Manager) ChangePath(slug string) string {
	return filepath.Join(m.ChangesPath(), slug)
}

// EnsureDirectories creates the .semspec directory structure if it doesn't exist.
func (m *Manager) EnsureDirectories() error {
	dirs := []string{
		m.RootPath(),
		m.SpecsPath(),
		m.ChangesPath(),
		m.ArchivePath(),
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

// CreateChange creates a new change directory with initial metadata.
func (m *Manager) CreateChange(description, author string) (*Change, error) {
	if err := m.EnsureDirectories(); err != nil {
		return nil, err
	}

	slug := Slugify(description)
	if slug == "" {
		return nil, fmt.Errorf("description must produce a valid slug")
	}

	changePath := m.ChangePath(slug)

	// Check if change already exists
	if _, err := os.Stat(changePath); err == nil {
		return nil, fmt.Errorf("change '%s' already exists", slug)
	}

	// Create change directory
	if err := os.MkdirAll(changePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create change directory: %w", err)
	}

	// Create specs subdirectory
	specsSubdir := filepath.Join(changePath, ChangeSpecsDir)
	if err := os.MkdirAll(specsSubdir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create specs subdirectory: %w", err)
	}

	now := time.Now()
	change := &Change{
		Slug:        slug,
		Title:       description,
		Description: description,
		Status:      StatusCreated,
		Author:      author,
		CreatedAt:   now,
		UpdatedAt:   now,
		Files:       ChangeFiles{},
	}

	// Save metadata
	if err := m.SaveChangeMetadata(change); err != nil {
		// Clean up on failure
		os.RemoveAll(changePath)
		return nil, err
	}

	return change, nil
}

// SaveChangeMetadata saves the change metadata to metadata.json.
func (m *Manager) SaveChangeMetadata(change *Change) error {
	metadataPath := filepath.Join(m.ChangePath(change.Slug), MetadataFile)

	data, err := json.MarshalIndent(change, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	return nil
}

// LoadChange loads a change from its directory.
func (m *Manager) LoadChange(slug string) (*Change, error) {
	metadataPath := filepath.Join(m.ChangePath(slug), MetadataFile)

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("change '%s' not found", slug)
		}
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	var change Change
	if err := json.Unmarshal(data, &change); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	// Update file existence flags
	m.updateFileFlags(&change)

	return &change, nil
}

// updateFileFlags checks which files exist for a change.
func (m *Manager) updateFileFlags(change *Change) {
	changePath := m.ChangePath(change.Slug)

	change.Files.HasProposal = fileExists(filepath.Join(changePath, ProposalFile))
	change.Files.HasDesign = fileExists(filepath.Join(changePath, DesignFile))
	change.Files.HasSpec = fileExists(filepath.Join(changePath, SpecFile))
	change.Files.HasTasks = fileExists(filepath.Join(changePath, TasksFile))
}

// ListChanges returns all active changes.
func (m *Manager) ListChanges() ([]*Change, error) {
	changesPath := m.ChangesPath()

	entries, err := os.ReadDir(changesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Change{}, nil
		}
		return nil, fmt.Errorf("failed to read changes directory: %w", err)
	}

	var changes []*Change
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		change, err := m.LoadChange(entry.Name())
		if err != nil {
			// Skip invalid changes
			continue
		}

		changes = append(changes, change)
	}

	return changes, nil
}

// UpdateChangeStatus updates the status of a change.
func (m *Manager) UpdateChangeStatus(slug string, status Status) error {
	change, err := m.LoadChange(slug)
	if err != nil {
		return err
	}

	if !change.Status.CanTransitionTo(status) {
		return fmt.Errorf("cannot transition from %s to %s", change.Status, status)
	}

	change.Status = status
	change.UpdatedAt = time.Now()

	return m.SaveChangeMetadata(change)
}

// WriteProposal writes the proposal.md file for a change.
func (m *Manager) WriteProposal(slug, content string) error {
	proposalPath := filepath.Join(m.ChangePath(slug), ProposalFile)
	return m.writeFile(proposalPath, content)
}

// ReadProposal reads the proposal.md file for a change.
func (m *Manager) ReadProposal(slug string) (string, error) {
	proposalPath := filepath.Join(m.ChangePath(slug), ProposalFile)
	return m.readFile(proposalPath)
}

// WriteDesign writes the design.md file for a change.
func (m *Manager) WriteDesign(slug, content string) error {
	designPath := filepath.Join(m.ChangePath(slug), DesignFile)
	return m.writeFile(designPath, content)
}

// ReadDesign reads the design.md file for a change.
func (m *Manager) ReadDesign(slug string) (string, error) {
	designPath := filepath.Join(m.ChangePath(slug), DesignFile)
	return m.readFile(designPath)
}

// WriteSpec writes the spec.md file for a change.
func (m *Manager) WriteSpec(slug, content string) error {
	specPath := filepath.Join(m.ChangePath(slug), SpecFile)
	return m.writeFile(specPath, content)
}

// ReadSpec reads the spec.md file for a change.
func (m *Manager) ReadSpec(slug string) (string, error) {
	specPath := filepath.Join(m.ChangePath(slug), SpecFile)
	return m.readFile(specPath)
}

// WriteTasks writes the tasks.md file for a change.
func (m *Manager) WriteTasks(slug, content string) error {
	tasksPath := filepath.Join(m.ChangePath(slug), TasksFile)
	return m.writeFile(tasksPath, content)
}

// ReadTasks reads the tasks.md file for a change.
func (m *Manager) ReadTasks(slug string) (string, error) {
	tasksPath := filepath.Join(m.ChangePath(slug), TasksFile)
	return m.readFile(tasksPath)
}

// ArchiveChange moves a completed change to the archive.
func (m *Manager) ArchiveChange(slug string) error {
	change, err := m.LoadChange(slug)
	if err != nil {
		return err
	}

	if change.Status != StatusComplete {
		return fmt.Errorf("cannot archive change with status %s (must be complete)", change.Status)
	}

	srcPath := m.ChangePath(slug)
	dstPath := filepath.Join(m.ArchivePath(), slug)

	// Move specs to source of truth if they exist
	srcSpecs := filepath.Join(srcPath, ChangeSpecsDir)
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

	// Move change to archive
	if err := os.Rename(srcPath, dstPath); err != nil {
		return fmt.Errorf("failed to archive change: %w", err)
	}

	// Update metadata in archive
	change.Status = StatusArchived
	change.UpdatedAt = time.Now()
	archivedMetadataPath := filepath.Join(dstPath, MetadataFile)
	data, _ := json.MarshalIndent(change, "", "  ")
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
