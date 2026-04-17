package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

// ensureHEAD checks whether the repository at dir has a valid HEAD (at least
// one commit). If not, it creates an initial commit so that git worktree
// operations — which reference HEAD — always succeed.
//
// When files exist in the working tree they are staged and committed. When the
// working tree is empty, an --allow-empty commit is created instead.
func ensureHEAD(ctx context.Context, dir string) error {
	if _, err := gitOutput(ctx, dir, "rev-parse", "--verify", "HEAD"); err == nil {
		return nil // HEAD is valid, nothing to do
	}

	slog.Warn("Repository has no commits — creating initial commit for sandbox operation",
		"path", dir,
	)

	// Stage everything present in the workspace.
	_ = runGit(ctx, dir, "add", "-A")

	// Try a normal commit first (captures workspace contents).
	if err := runGit(ctx, dir, "commit", "-m", "chore: semspec sandbox initial commit"); err != nil {
		// Nothing to commit (empty directory) — create an empty commit.
		if err2 := runGit(ctx, dir, "commit", "--allow-empty", "-m", "chore: semspec sandbox initial commit"); err2 != nil {
			return fmt.Errorf("create initial commit: %w", err2)
		}
	}

	slog.Info("Initial commit created — sandbox worktree operations are now available",
		"path", dir,
	)
	return nil
}

// runGit runs a git subcommand in dir, returning an error with combined output
// if the command fails. Pass "-C <path>" as args rather than setting Dir when
// you need git to operate on a specific repository.
func runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), msg, err)
	}
	return nil
}

// gitOutput runs a git subcommand and returns its stdout. Stderr is included
// in the error message on failure.
func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), msg, err)
	}
	return stdout.String(), nil
}
