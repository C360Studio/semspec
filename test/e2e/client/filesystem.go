// Package client provides test clients for e2e scenarios.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// FilesystemClient provides file system operations for e2e tests.
// It observes the .semspec directory structure for validation.
type FilesystemClient struct {
	workspacePath string
}

// NewFilesystemClient creates a new filesystem client.
func NewFilesystemClient(workspacePath string) *FilesystemClient {
	return &FilesystemClient{
		workspacePath: workspacePath,
	}
}

// SemspecPath returns the path to the .semspec directory.
func (c *FilesystemClient) SemspecPath() string {
	return filepath.Join(c.workspacePath, ".semspec")
}

// ChangesPath returns the path to the changes directory.
func (c *FilesystemClient) ChangesPath() string {
	return filepath.Join(c.SemspecPath(), "changes")
}

// ChangePath returns the path to a specific change directory.
func (c *FilesystemClient) ChangePath(slug string) string {
	return filepath.Join(c.ChangesPath(), slug)
}

// FileExists checks if a file exists.
func (c *FilesystemClient) FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// FileExistsRelative checks if a file exists relative to the workspace.
func (c *FilesystemClient) FileExistsRelative(relativePath string) bool {
	return c.FileExists(filepath.Join(c.workspacePath, relativePath))
}

// ReadFile reads a file and returns its contents.
func (c *FilesystemClient) ReadFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ReadFileRelative reads a file relative to the workspace.
func (c *FilesystemClient) ReadFileRelative(relativePath string) (string, error) {
	return c.ReadFile(filepath.Join(c.workspacePath, relativePath))
}

// WriteFile writes content to a file, creating directories as needed.
func (c *FilesystemClient) WriteFile(path string, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// WriteFileRelative writes a file relative to the workspace.
func (c *FilesystemClient) WriteFileRelative(relativePath, content string) error {
	return c.WriteFile(filepath.Join(c.workspacePath, relativePath), content)
}

// ReadJSON reads a JSON file and unmarshals it into the provided value.
func (c *FilesystemClient) ReadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// ReadJSONRelative reads a JSON file relative to the workspace.
func (c *FilesystemClient) ReadJSONRelative(relativePath string, v any) error {
	return c.ReadJSON(filepath.Join(c.workspacePath, relativePath), v)
}

// ChangeMetadata represents the metadata.json structure for a change.
type ChangeMetadata struct {
	Slug        string         `json:"slug"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Status      string         `json:"status"`
	Author      string         `json:"author"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	Files       ChangeFiles    `json:"files"`
	GitHub      *GitHubMetadata `json:"github,omitempty"`
}

// ChangeFiles tracks which files exist for a change.
type ChangeFiles struct {
	HasProposal bool `json:"has_proposal"`
	HasDesign   bool `json:"has_design"`
	HasSpec     bool `json:"has_spec"`
	HasTasks    bool `json:"has_tasks"`
}

// GitHubMetadata tracks GitHub issue information.
type GitHubMetadata struct {
	EpicNumber int               `json:"epic_number,omitempty"`
	EpicURL    string            `json:"epic_url,omitempty"`
	Repository string            `json:"repository,omitempty"`
	TaskIssues map[string]int    `json:"task_issues,omitempty"`
	LastSynced time.Time         `json:"last_synced,omitempty"`
}

// LoadChangeMetadata loads the metadata for a change.
func (c *FilesystemClient) LoadChangeMetadata(slug string) (*ChangeMetadata, error) {
	path := filepath.Join(c.ChangePath(slug), "metadata.json")
	var metadata ChangeMetadata
	if err := c.ReadJSON(path, &metadata); err != nil {
		return nil, fmt.Errorf("read metadata for %s: %w", slug, err)
	}
	return &metadata, nil
}

// ChangeExists checks if a change directory exists.
func (c *FilesystemClient) ChangeExists(slug string) bool {
	return c.FileExists(c.ChangePath(slug))
}

// ChangeHasFile checks if a change has a specific file.
func (c *FilesystemClient) ChangeHasFile(slug, filename string) bool {
	return c.FileExists(filepath.Join(c.ChangePath(slug), filename))
}

// WaitForFile waits for a file to exist with timeout.
func (c *FilesystemClient) WaitForFile(ctx context.Context, path string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for file %s: %w", path, ctx.Err())
		case <-ticker.C:
			if c.FileExists(path) {
				return nil
			}
		}
	}
}

// WaitForFileRelative waits for a file relative to workspace.
func (c *FilesystemClient) WaitForFileRelative(ctx context.Context, relativePath string) error {
	return c.WaitForFile(ctx, filepath.Join(c.workspacePath, relativePath))
}

// WaitForChange waits for a change directory to exist.
func (c *FilesystemClient) WaitForChange(ctx context.Context, slug string) error {
	return c.WaitForFile(ctx, c.ChangePath(slug))
}

// WaitForChangeFile waits for a specific file in a change directory.
func (c *FilesystemClient) WaitForChangeFile(ctx context.Context, slug, filename string) error {
	return c.WaitForFile(ctx, filepath.Join(c.ChangePath(slug), filename))
}

// WaitForChangeStatus waits for a change to reach a specific status.
func (c *FilesystemClient) WaitForChangeStatus(ctx context.Context, slug, status string) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for status %s: %w", status, ctx.Err())
		case <-ticker.C:
			metadata, err := c.LoadChangeMetadata(slug)
			if err != nil {
				continue
			}
			if metadata.Status == status {
				return nil
			}
		}
	}
}

// ListChanges returns all change slugs in the changes directory.
func (c *FilesystemClient) ListChanges() ([]string, error) {
	changesPath := c.ChangesPath()
	entries, err := os.ReadDir(changesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var slugs []string
	for _, entry := range entries {
		if entry.IsDir() {
			slugs = append(slugs, entry.Name())
		}
	}
	return slugs, nil
}

// CleanWorkspace removes the .semspec directory from the workspace.
func (c *FilesystemClient) CleanWorkspace() error {
	semspecPath := c.SemspecPath()
	if c.FileExists(semspecPath) {
		return os.RemoveAll(semspecPath)
	}
	return nil
}

// SetupWorkspace creates a clean .semspec directory structure.
func (c *FilesystemClient) SetupWorkspace() error {
	if err := c.CleanWorkspace(); err != nil {
		return fmt.Errorf("clean workspace: %w", err)
	}

	dirs := []string{
		c.SemspecPath(),
		c.ChangesPath(),
		filepath.Join(c.SemspecPath(), "specs"),
		filepath.Join(c.SemspecPath(), "archive"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}

	return nil
}

// WriteConstitution writes a constitution.md file.
func (c *FilesystemClient) WriteConstitution(content string) error {
	path := filepath.Join(c.SemspecPath(), "constitution.md")
	return c.WriteFile(path, content)
}

// ConstitutionExists checks if a constitution.md file exists.
func (c *FilesystemClient) ConstitutionExists() bool {
	return c.FileExists(filepath.Join(c.SemspecPath(), "constitution.md"))
}

// WorkspacePath returns the workspace root path.
func (c *FilesystemClient) WorkspacePath() string {
	return c.workspacePath
}

// CopyFixture copies a fixture directory to the workspace.
// The fixture is copied to the workspace root, merging with existing files.
func (c *FilesystemClient) CopyFixture(fixturePath string) error {
	return filepath.WalkDir(fixturePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(fixturePath, path)
		if err != nil {
			return fmt.Errorf("get relative path: %w", err)
		}

		destPath := filepath.Join(c.workspacePath, relPath)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		return copyFile(path, destPath)
	})
}

// CopyFixtureToSubdir copies a fixture directory to a subdirectory of the workspace.
func (c *FilesystemClient) CopyFixtureToSubdir(fixturePath, subdir string) error {
	destDir := filepath.Join(c.workspacePath, subdir)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	return filepath.WalkDir(fixturePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(fixturePath, path)
		if err != nil {
			return fmt.Errorf("get relative path: %w", err)
		}

		destPath := filepath.Join(destDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		return copyFile(path, destPath)
	})
}

// InitGit initializes a git repository in the workspace.
// If there's an existing .git directory, it will be removed first.
func (c *FilesystemClient) InitGit() error {
	gitDir := filepath.Join(c.workspacePath, ".git")

	// Remove existing .git if present
	if c.FileExists(gitDir) {
		if err := os.RemoveAll(gitDir); err != nil {
			return fmt.Errorf("remove existing .git: %w", err)
		}
	}

	// Initialize new git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = c.workspacePath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git init: %w\nOutput: %s", err, output)
	}

	// Configure git user for commits
	if err := c.gitConfig("user.email", "test@e2e.local"); err != nil {
		return err
	}
	if err := c.gitConfig("user.name", "E2E Test"); err != nil {
		return err
	}

	return nil
}

// GitAdd stages files for commit.
func (c *FilesystemClient) GitAdd(paths ...string) error {
	args := append([]string{"add"}, paths...)
	cmd := exec.Command("git", args...)
	cmd.Dir = c.workspacePath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w\nOutput: %s", err, output)
	}
	return nil
}

// GitCommit creates a commit with the given message.
func (c *FilesystemClient) GitCommit(message string) error {
	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Dir = c.workspacePath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %w\nOutput: %s", err, output)
	}
	return nil
}

// GitStatus returns the output of git status --porcelain.
func (c *FilesystemClient) GitStatus() (string, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = c.workspacePath
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git status: %w", err)
	}
	return string(output), nil
}

// GitLog returns recent commit history.
func (c *FilesystemClient) GitLog(n int) (string, error) {
	cmd := exec.Command("git", "log", "--oneline", fmt.Sprintf("-n%d", n))
	cmd.Dir = c.workspacePath
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git log: %w", err)
	}
	return string(output), nil
}

// IsGitRepo checks if the workspace is a git repository.
func (c *FilesystemClient) IsGitRepo() bool {
	return c.FileExists(filepath.Join(c.workspacePath, ".git"))
}

// CleanWorkspaceAll removes all files from workspace except .gitkeep and .gitignore.
func (c *FilesystemClient) CleanWorkspaceAll() error {
	entries, err := os.ReadDir(c.workspacePath)
	if err != nil {
		return fmt.Errorf("read workspace directory: %w", err)
	}

	preserveFiles := map[string]bool{
		".gitkeep":  true,
		".gitignore": true,
	}

	for _, entry := range entries {
		if preserveFiles[entry.Name()] {
			continue
		}
		path := filepath.Join(c.workspacePath, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove %s: %w", entry.Name(), err)
		}
	}

	return nil
}

// ListFiles returns all files in the workspace (excluding .git and .semspec).
func (c *FilesystemClient) ListFiles() ([]string, error) {
	var files []string

	err := filepath.WalkDir(c.workspacePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(c.workspacePath, path)
		if err != nil {
			return err
		}

		// Skip .git and .semspec directories
		if d.IsDir() && (d.Name() == ".git" || d.Name() == ".semspec") {
			return filepath.SkipDir
		}

		if !d.IsDir() {
			files = append(files, relPath)
		}
		return nil
	})

	return files, err
}

// gitConfig sets a git configuration value.
func (c *FilesystemClient) gitConfig(key, value string) error {
	cmd := exec.Command("git", "config", key, value)
	cmd.Dir = c.workspacePath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config %s: %w\nOutput: %s", key, err, output)
	}
	return nil
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	// Get source file info to preserve permissions
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	// Create parent directory if needed
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("copy contents: %w", err)
	}

	return nil
}
