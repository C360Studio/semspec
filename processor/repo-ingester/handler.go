package repoingester

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/c360studio/semspec/source"
	sourceVocab "github.com/c360studio/semspec/vocabulary/source"
	"github.com/c360studio/semstreams/message"
)

// allowedProtocols defines the git URL protocols that are permitted.
var allowedProtocols = map[string]bool{
	"https": true,
	"git":   true,
	"ssh":   true,
}

// slugPattern validates that a slug contains only safe characters.
var slugPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// validateGitURL validates that a git URL uses an allowed protocol.
func validateGitURL(rawURL string) error {
	// Handle SSH shorthand (git@github.com:owner/repo.git)
	if strings.HasPrefix(rawURL, "git@") {
		return nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if !allowedProtocols[scheme] {
		return fmt.Errorf("protocol %q not allowed; must be https, git, or ssh", scheme)
	}

	return nil
}

// validateSlug ensures a slug is safe for use in file paths.
func validateSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("slug is required")
	}
	if strings.Contains(slug, "..") {
		return fmt.Errorf("path traversal not allowed")
	}
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("invalid slug format")
	}
	if len(slug) > 255 {
		return fmt.Errorf("slug too long")
	}
	return nil
}

// Handler processes repository ingestion requests.
type Handler struct {
	reposDir     string
	cloneTimeout time.Duration
	cloneDepth   int
}

// NewHandler creates a new repository ingestion handler.
func NewHandler(reposDir string, cloneTimeout time.Duration, cloneDepth int) *Handler {
	return &Handler{
		reposDir:     reposDir,
		cloneTimeout: cloneTimeout,
		cloneDepth:   cloneDepth,
	}
}

// IngestRepository clones and processes a repository.
// Returns the repository entity payload and detected languages.
func (h *Handler) IngestRepository(ctx context.Context, req source.AddRepositoryRequest) (*RepoEntityPayload, []string, error) {
	// Validate URL
	if err := validateGitURL(req.URL); err != nil {
		return nil, nil, fmt.Errorf("invalid URL: %w", err)
	}

	// Generate entity ID and slug
	entityID := generateRepoEntityID(req.URL)
	slug := strings.TrimPrefix(entityID, "source.repo.")

	// Validate slug for path safety
	if err := validateSlug(slug); err != nil {
		return nil, nil, fmt.Errorf("invalid repository slug: %w", err)
	}

	// Determine destination path
	destPath := filepath.Join(h.reposDir, slug)

	// Clone the repository
	if err := h.cloneRepository(ctx, req.URL, destPath, req.Branch); err != nil {
		return nil, nil, fmt.Errorf("clone repository: %w", err)
	}

	// Get HEAD commit
	headCommit, err := h.getHeadCommit(ctx, destPath)
	if err != nil {
		return nil, nil, fmt.Errorf("get head commit: %w", err)
	}

	// Get current branch (may differ from requested if using default)
	currentBranch, err := h.getCurrentBranch(ctx, destPath)
	if err != nil {
		currentBranch = req.Branch
	}

	// Detect languages
	languages, err := DetectLanguages(destPath)
	if err != nil {
		// Non-fatal, continue without language detection
		languages = []string{}
	}

	// Build entity payload
	entity := h.buildRepoEntity(entityID, req, currentBranch, destPath, headCommit, languages)

	return entity, FilterASTLanguages(languages), nil
}

// PullRepository pulls updates for an existing repository.
func (h *Handler) PullRepository(ctx context.Context, entityID string) (string, error) {
	slug := strings.TrimPrefix(entityID, "source.repo.")

	// Validate slug for path safety
	if err := validateSlug(slug); err != nil {
		return "", fmt.Errorf("invalid repository slug: %w", err)
	}

	repoPath := filepath.Join(h.reposDir, slug)

	// Run git pull
	cmd := exec.CommandContext(ctx, "git", "pull")
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git pull failed: %w: %s", err, string(output))
	}

	// Get new HEAD
	return h.getHeadCommit(ctx, repoPath)
}

// cloneRepository clones a git repository to the destination path.
func (h *Handler) cloneRepository(ctx context.Context, url, destPath, branch string) error {
	cloneCtx, cancel := context.WithTimeout(ctx, h.cloneTimeout)
	defer cancel()

	args := []string{"clone"}

	if branch != "" {
		args = append(args, "--branch", branch)
	}

	if h.cloneDepth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", h.cloneDepth))
	}

	args = append(args, url, destPath)

	cmd := exec.CommandContext(cloneCtx, "git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %w: %s", err, string(output))
	}

	return nil
}

// getHeadCommit returns the HEAD commit SHA.
func (h *Handler) getHeadCommit(ctx context.Context, repoPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// getCurrentBranch returns the current branch name.
func (h *Handler) getCurrentBranch(ctx context.Context, repoPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// buildRepoEntity creates the repository entity payload.
func (h *Handler) buildRepoEntity(
	entityID string,
	req source.AddRepositoryRequest,
	branch string,
	destPath string,
	headCommit string,
	languages []string,
) *RepoEntityPayload {
	// Extract name from URL
	name := extractRepoName(req.URL)

	triples := []message.Triple{
		{Subject: entityID, Predicate: sourceVocab.SourceType, Object: "repository"},
		{Subject: entityID, Predicate: sourceVocab.RepoType, Object: "repository"},
		{Subject: entityID, Predicate: sourceVocab.SourceName, Object: name},
		{Subject: entityID, Predicate: sourceVocab.RepoURL, Object: req.URL},
		{Subject: entityID, Predicate: sourceVocab.RepoBranch, Object: branch},
		{Subject: entityID, Predicate: sourceVocab.RepoStatus, Object: "indexing"},
		{Subject: entityID, Predicate: sourceVocab.RepoLastCommit, Object: headCommit},
		{Subject: entityID, Predicate: sourceVocab.SourceStatus, Object: "indexing"},
		{Subject: entityID, Predicate: sourceVocab.SourceAddedAt, Object: time.Now().Format(time.RFC3339)},
	}

	if len(languages) > 0 {
		triples = append(triples, message.Triple{
			Subject: entityID, Predicate: sourceVocab.RepoLanguages, Object: languages,
		})
	}

	if req.Project != "" {
		triples = append(triples, message.Triple{
			Subject: entityID, Predicate: sourceVocab.SourceProject, Object: req.Project,
		})
	}

	if req.AutoPull {
		triples = append(triples, message.Triple{
			Subject: entityID, Predicate: sourceVocab.RepoAutoPull, Object: true,
		})
	}

	if req.PullInterval != "" {
		triples = append(triples, message.Triple{
			Subject: entityID, Predicate: sourceVocab.RepoPullInterval, Object: req.PullInterval,
		})
	}

	return &RepoEntityPayload{
		EntityID_:  entityID,
		TripleData: triples,
		UpdatedAt:  time.Now(),
		RepoPath:   destPath,
	}
}


// extractRepoName extracts a display name from a repository URL.
func extractRepoName(url string) string {
	url = strings.TrimSuffix(url, ".git")
	url = strings.TrimSuffix(url, "/")

	parts := strings.Split(url, "/")
	if len(parts) >= 1 {
		return parts[len(parts)-1]
	}
	return "repository"
}
