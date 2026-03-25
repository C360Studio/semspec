// Package paths provides filesystem path helpers for .semspec directory structure.
package paths

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// Directory names within .semspec/.
const (
	RootDir    = ".semspec"
	SpecsDir   = "specs"
	PlansDir   = "plans"
	ArchiveDir = "archive"
	ProjectsDir = "projects"
)

// RootPath returns the .semspec directory path.
func RootPath(repoRoot string) string {
	return filepath.Join(repoRoot, RootDir)
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
			return err
		}
	}
	return nil
}

// Slugify creates a deterministic slug from a description.
func Slugify(description string) string {
	normalized := strings.ToLower(strings.TrimSpace(description))
	if normalized == "" {
		return ""
	}
	h := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(h[:6])
}

// FileExists checks if a file exists and is not a directory.
func FileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
