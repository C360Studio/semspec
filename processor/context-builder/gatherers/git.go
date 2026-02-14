package gatherers

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// validGitRefPattern matches valid git ref formats.
// Allows: branch names, tags, HEAD, HEAD~N, commit hashes, and ref ranges.
var validGitRefPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_./-]*(?:~\d+)?(?:\.\.[a-zA-Z0-9][a-zA-Z0-9_./-]*(?:~\d+)?)?$|^HEAD(?:~\d+)?(?:\.\.HEAD(?:~\d+)?)?$`)

// GitGatherer gathers context from git operations.
type GitGatherer struct {
	repoPath string
}

// NewGitGatherer creates a new git gatherer.
func NewGitGatherer(repoPath string) *GitGatherer {
	return &GitGatherer{
		repoPath: repoPath,
	}
}

// ValidateGitRef validates that a git ref has a safe format.
// Returns an error if the ref contains potentially dangerous characters.
func ValidateGitRef(ref string) error {
	if ref == "" {
		return nil // Empty ref is valid (defaults to working tree)
	}

	// Check for null bytes or other control characters
	for _, c := range ref {
		if c < 32 || c == 127 {
			return fmt.Errorf("git ref contains invalid control character")
		}
	}

	// Check against the valid pattern
	if !validGitRefPattern.MatchString(ref) {
		return fmt.Errorf("invalid git ref format: %q", ref)
	}

	return nil
}

// GetDiff returns the git diff for the given ref and files.
// ref can be a commit, branch, or range (e.g., "HEAD~1..HEAD").
// If files is empty, returns diff for all changed files.
func (g *GitGatherer) GetDiff(ctx context.Context, ref string, files []string) (string, error) {
	if err := ValidateGitRef(ref); err != nil {
		return "", err
	}

	args := []string{"diff"}

	if ref != "" {
		args = append(args, ref)
	}

	// Add -- separator before files
	if len(files) > 0 {
		args = append(args, "--")
		args = append(args, files...)
	}

	return g.runGit(ctx, args...)
}

// GetDiffStat returns a summary of changes (files changed, insertions, deletions).
func (g *GitGatherer) GetDiffStat(ctx context.Context, ref string) (string, error) {
	if err := ValidateGitRef(ref); err != nil {
		return "", err
	}

	args := []string{"diff", "--stat"}
	if ref != "" {
		args = append(args, ref)
	}
	return g.runGit(ctx, args...)
}

// GetChangedFiles returns the list of files changed in the given ref.
func (g *GitGatherer) GetChangedFiles(ctx context.Context, ref string) ([]string, error) {
	if err := ValidateGitRef(ref); err != nil {
		return nil, err
	}

	args := []string{"diff", "--name-only"}
	if ref != "" {
		args = append(args, ref)
	}

	output, err := g.runGit(ctx, args...)
	if err != nil {
		return nil, err
	}

	if output == "" {
		return nil, nil
	}

	files := strings.Split(strings.TrimSpace(output), "\n")
	result := make([]string, 0, len(files))
	for _, f := range files {
		f = strings.TrimSpace(f)
		if f != "" {
			result = append(result, f)
		}
	}
	return result, nil
}

// GetFileDiff returns the diff for a specific file.
func (g *GitGatherer) GetFileDiff(ctx context.Context, ref, file string) (string, error) {
	if err := ValidateGitRef(ref); err != nil {
		return "", err
	}

	args := []string{"diff"}
	if ref != "" {
		args = append(args, ref)
	}
	args = append(args, "--", file)
	return g.runGit(ctx, args...)
}

// GetStagedDiff returns the diff of staged changes.
func (g *GitGatherer) GetStagedDiff(ctx context.Context) (string, error) {
	return g.runGit(ctx, "diff", "--staged")
}

// GetUnstagedDiff returns the diff of unstaged changes.
func (g *GitGatherer) GetUnstagedDiff(ctx context.Context) (string, error) {
	return g.runGit(ctx, "diff")
}

// GetWorkingTreeDiff returns the full working tree diff (staged + unstaged).
func (g *GitGatherer) GetWorkingTreeDiff(ctx context.Context) (string, error) {
	return g.runGit(ctx, "diff", "HEAD")
}

// GetCommitMessage returns the commit message for a ref.
func (g *GitGatherer) GetCommitMessage(ctx context.Context, ref string) (string, error) {
	if ref == "" {
		ref = "HEAD"
	}
	if err := ValidateGitRef(ref); err != nil {
		return "", err
	}
	return g.runGit(ctx, "log", "-1", "--format=%B", ref)
}

// GetCommitInfo returns commit information (hash, author, date, message).
func (g *GitGatherer) GetCommitInfo(ctx context.Context, ref string) (string, error) {
	if ref == "" {
		ref = "HEAD"
	}
	if err := ValidateGitRef(ref); err != nil {
		return "", err
	}
	return g.runGit(ctx, "log", "-1", "--format=Commit: %H%nAuthor: %an <%ae>%nDate: %ad%n%n%B", ref)
}

// GetBranch returns the current branch name.
func (g *GitGatherer) GetBranch(ctx context.Context) (string, error) {
	output, err := g.runGit(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

// GetRecentCommits returns the last n commits as a formatted string.
func (g *GitGatherer) GetRecentCommits(ctx context.Context, n int) (string, error) {
	if n <= 0 {
		n = 10
	}
	if n > 100 {
		n = 100 // Limit to prevent excessive output
	}
	return g.runGit(ctx, "log", fmt.Sprintf("-%d", n), "--oneline")
}

// TruncateDiffByFiles truncates a diff at file boundaries to fit within a size limit.
// Returns the truncated diff and whether truncation occurred.
func (g *GitGatherer) TruncateDiffByFiles(diff string, maxBytes int) (string, bool) {
	if len(diff) <= maxBytes {
		return diff, false
	}

	// Find file boundaries (lines starting with "diff --git")
	lines := strings.Split(diff, "\n")
	var result strings.Builder
	truncated := false

	for _, line := range lines {
		// Check if adding this line would exceed the limit
		if result.Len()+len(line)+1 > maxBytes {
			// Try to truncate at a file boundary
			if strings.HasPrefix(line, "diff --git") {
				truncated = true
				break
			}
			// Otherwise, stop here
			truncated = true
			break
		}
		result.WriteString(line)
		result.WriteString("\n")
	}

	if truncated {
		result.WriteString("\n...[diff truncated to fit token budget]\n")
	}

	return result.String(), truncated
}

// runGit executes a git command and returns its output.
func (g *GitGatherer) runGit(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.repoPath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Some git commands return non-zero for valid cases (e.g., no diff)
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 && stderr.Len() == 0 {
				// This is fine - just means no output
				return stdout.String(), nil
			}
		}
		return "", fmt.Errorf("git %v failed: %w (stderr: %s)", args, err, stderr.String())
	}

	return stdout.String(), nil
}
