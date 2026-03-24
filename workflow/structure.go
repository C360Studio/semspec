package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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

// RootPath returns the full path to .semspec directory.
func RootPath(repoRoot string) string {
	return filepath.Join(repoRoot, RootDir)
}

// ConstitutionPath returns the path to constitution.md.
func ConstitutionPath(repoRoot string) string {
	return filepath.Join(RootPath(repoRoot), ConstitutionMD)
}

// SpecsPath returns the path to the specs directory.
func SpecsPath(repoRoot string) string {
	return filepath.Join(RootPath(repoRoot), SpecsDir)
}

// PlansPath returns the path to the plans directory.
func PlansPath(repoRoot string) string {
	return filepath.Join(RootPath(repoRoot), PlansDir)
}

// ArchivePath returns the path to the archive directory.
func ArchivePath(repoRoot string) string {
	return filepath.Join(RootPath(repoRoot), ArchiveDir)
}

// PlanPath returns the path to a specific plan directory.
func PlanPath(repoRoot, slug string) string {
	return filepath.Join(PlansPath(repoRoot), slug)
}

// ProjectsPath returns the path to the projects directory.
func ProjectsPath(repoRoot string) string {
	return filepath.Join(RootPath(repoRoot), ProjectsDir)
}

// ProjectPath returns the path to a specific project directory.
func ProjectPath(repoRoot, slug string) string {
	return filepath.Join(ProjectsPath(repoRoot), slug)
}

// ProjectPlansPath returns the path to plans within a project.
func ProjectPlansPath(repoRoot, slug string) string {
	return filepath.Join(ProjectPath(repoRoot, slug), PlansDir)
}

// ProjectPlanPath returns the path to a specific plan within a project.
func ProjectPlanPath(repoRoot, projectSlug, planSlug string) string {
	return filepath.Join(ProjectPlansPath(repoRoot, projectSlug), planSlug)
}

// EnsureDirectories creates the .semspec directory structure if it doesn't exist.
func EnsureDirectories(repoRoot string) error {
	dirs := []string{
		RootPath(repoRoot),
		SpecsPath(repoRoot),
		PlansPath(repoRoot),
		ArchivePath(repoRoot),
		ProjectsPath(repoRoot),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// Slugify converts a description to a stable 12-character hex slug using SHA-256.
// The slug is deterministic: same input always produces the same output.
func Slugify(description string) string {
	normalized := strings.ToLower(strings.TrimSpace(description))
	if normalized == "" {
		return ""
	}
	h := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(h[:6])
}

// LoadConstitution loads the constitution from .semspec/constitution.md.
func LoadConstitution(repoRoot string) (*Constitution, error) {
	path := ConstitutionPath(repoRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("constitution not found at %s", path)
		}
		return nil, err
	}

	return ParseConstitution(string(data))
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

// fileExists returns true if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
