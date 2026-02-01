// Package client provides test clients for e2e scenarios.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
