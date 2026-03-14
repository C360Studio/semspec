package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

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
