package repoingester

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/source"
)

func TestValidateGitURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		// Valid URLs
		{"https", "https://github.com/owner/repo.git", false},
		{"git protocol", "git://github.com/owner/repo.git", false},
		{"ssh protocol", "ssh://git@github.com/owner/repo.git", false},
		{"ssh shorthand", "git@github.com:owner/repo.git", false},

		// Invalid URLs
		{"file protocol", "file:///path/to/repo", true},
		{"http insecure", "http://github.com/owner/repo.git", true},
		{"ftp", "ftp://example.com/repo", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGitURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateGitURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestValidateSlug(t *testing.T) {
	tests := []struct {
		name    string
		slug    string
		wantErr bool
	}{
		// Valid
		{"simple", "owner-repo", false},
		{"with dots", "owner.repo", false},
		{"alphanumeric", "repo123", false},

		// Invalid
		{"empty", "", true},
		{"traversal", "../escape", true},
		{"starts with dot", ".hidden", true},
		{"too long", strings.Repeat("a", 256), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSlug(tt.slug)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSlug(%q) error = %v, wantErr %v", tt.slug, err, tt.wantErr)
			}
		})
	}
}

func TestGenerateRepoEntityID(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/owner/repo.git", "source.repo.owner-repo"},
		{"https://github.com/owner/repo", "source.repo.owner-repo"},
		{"https://gitlab.com/group/project.git", "source.repo.group-project"},
		{"https://github.com/owner/repo/", "source.repo.owner-repo"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := generateRepoEntityID(tt.url)
			if got != tt.want {
				t.Errorf("generateRepoEntityID(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestExtractRepoName(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/owner/repo.git", "repo"},
		{"https://github.com/owner/repo", "repo"},
		{"https://gitlab.com/group/project/", "project"},
		{"repo", "repo"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := extractRepoName(tt.url)
			if got != tt.want {
				t.Errorf("extractRepoName(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestNewHandler(t *testing.T) {
	h := NewHandler("/tmp/repos", 5*time.Minute, 1)

	if h.reposDir != "/tmp/repos" {
		t.Errorf("reposDir = %q, want %q", h.reposDir, "/tmp/repos")
	}
	if h.cloneTimeout != 5*time.Minute {
		t.Errorf("cloneTimeout = %v, want %v", h.cloneTimeout, 5*time.Minute)
	}
	if h.cloneDepth != 1 {
		t.Errorf("cloneDepth = %d, want %d", h.cloneDepth, 1)
	}
}

func TestIngestRepository_InvalidURL(t *testing.T) {
	h := NewHandler(t.TempDir(), time.Minute, 1)

	req := source.AddRepositoryRequest{
		URL: "file:///path/to/repo",
	}

	_, _, err := h.IngestRepository(context.Background(), req)
	if err == nil {
		t.Error("expected error for invalid URL protocol")
	}
	if !strings.Contains(err.Error(), "invalid URL") {
		t.Errorf("expected 'invalid URL' error, got: %v", err)
	}
}

func TestPullRepository_InvalidSlug(t *testing.T) {
	h := NewHandler(t.TempDir(), time.Minute, 1)

	// Test with path traversal attempt
	_, err := h.PullRepository(context.Background(), "source.repo.../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

func TestPullRepository_NonexistentRepo(t *testing.T) {
	h := NewHandler(t.TempDir(), time.Minute, 1)

	_, err := h.PullRepository(context.Background(), "source.repo.nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent repo")
	}
}

// setupLocalGitRepo creates a local bare repository for testing
func setupLocalGitRepo(t *testing.T) string {
	t.Helper()

	bareDir := t.TempDir()

	// Initialize bare repo
	cmd := exec.Command("git", "init", "--bare")
	cmd.Dir = bareDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init bare repo: %v", err)
	}

	// Create a working copy and make initial commit
	workDir := t.TempDir()

	cmd = exec.Command("git", "clone", bareDir, workDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to clone: %v", err)
	}

	// Configure git
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = workDir
	cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = workDir
	cmd.Run()

	// Create initial commit
	testFile := filepath.Join(workDir, "README.md")
	os.WriteFile(testFile, []byte("# Test Repo"), 0644)

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = workDir
	cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = workDir
	cmd.Run()

	cmd = exec.Command("git", "push", "origin", "HEAD")
	cmd.Dir = workDir
	cmd.Run()

	return bareDir
}

func TestHandler_GetHeadCommit(t *testing.T) {
	// Create a test repo
	workDir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = workDir
	cmd.Run()

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = workDir
	cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = workDir
	cmd.Run()

	testFile := filepath.Join(workDir, "test.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = workDir
	cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "test commit")
	cmd.Dir = workDir
	cmd.Run()

	h := NewHandler(t.TempDir(), time.Minute, 1)
	commit, err := h.getHeadCommit(context.Background(), workDir)

	if err != nil {
		t.Fatalf("getHeadCommit failed: %v", err)
	}
	if len(commit) != 40 {
		t.Errorf("expected 40-char SHA, got %d chars: %s", len(commit), commit)
	}
}

func TestHandler_GetCurrentBranch(t *testing.T) {
	// Create a test repo
	workDir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = workDir
	cmd.Run()

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = workDir
	cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = workDir
	cmd.Run()

	testFile := filepath.Join(workDir, "test.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = workDir
	cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "test commit")
	cmd.Dir = workDir
	cmd.Run()

	h := NewHandler(t.TempDir(), time.Minute, 1)
	branch, err := h.getCurrentBranch(context.Background(), workDir)

	if err != nil {
		t.Fatalf("getCurrentBranch failed: %v", err)
	}
	// Branch could be "main" or "master" depending on git version
	if branch != "main" && branch != "master" {
		t.Errorf("expected 'main' or 'master', got %q", branch)
	}
}

func TestHandler_BuildRepoEntity(t *testing.T) {
	h := NewHandler("/tmp/repos", time.Minute, 1)

	req := source.AddRepositoryRequest{
		URL:          "https://github.com/owner/repo.git",
		Branch:       "main",
		ProjectID:    "semspec.local.project.test-project",
		AutoPull:     true,
		PullInterval: "1h",
	}

	entity := h.buildRepoEntity(
		"source.repo.owner-repo",
		req,
		"main",
		"/tmp/repos/owner-repo",
		"abc123def456",
		[]string{"go", "typescript"},
	)

	if entity.EntityID_ != "source.repo.owner-repo" {
		t.Errorf("EntityID = %q, want %q", entity.EntityID_, "source.repo.owner-repo")
	}
	if entity.RepoPath != "/tmp/repos/owner-repo" {
		t.Errorf("RepoPath = %q, want %q", entity.RepoPath, "/tmp/repos/owner-repo")
	}
	if len(entity.TripleData) == 0 {
		t.Error("expected triples to be set")
	}
}
