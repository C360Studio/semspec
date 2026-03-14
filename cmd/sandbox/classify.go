package main

import (
	"regexp"
	"strings"
)

// ExecClassification categorizes command execution outcomes.
type ExecClassification string

const (
	ClassSuccess          ExecClassification = "success"
	ClassFailure          ExecClassification = "failure"
	ClassCommandNotFound  ExecClassification = "command_not_found"
	ClassPermissionDenied ExecClassification = "permission_denied"
	ClassTimeout          ExecClassification = "timeout"
)

// commandNotFoundRe matches common "command not found" patterns from various shells.
// Examples:
//
//	bash: cargo: command not found
//	sh: 1: foo: not found
//	zsh: command not found: bar
//	/bin/sh: protoc: not found
//
// IMPORTANT: The zsh branch ("command not found: X") must precede the generic
// bash branch ("X: command not found") because the latter's \S+ would greedily
// match "zsh" from zsh-style output.
var commandNotFoundRe = regexp.MustCompile(
	`(?m)(?:` +
		`command not found: (\S+)` + // zsh: command not found: bar (must precede generic)
		`|sh: \d+: (\S+): not found` + // sh: 1: foo: not found
		`|/bin/sh: (\S+): not found` + // /bin/sh: protoc: not found
		`|\S+: (\S+): command not found` + // bash: cargo: command not found
		`)`,
)

// classifyExec classifies a command execution result and optionally extracts
// the missing command name for command_not_found failures.
func classifyExec(stderr string, exitCode int, timedOut bool) (ExecClassification, string) {
	if timedOut {
		return ClassTimeout, ""
	}
	if exitCode == 0 {
		return ClassSuccess, ""
	}

	// Exit code 127 is the standard "command not found" code.
	if exitCode == 127 {
		return ClassCommandNotFound, extractMissingCommand(stderr)
	}

	// Exit code 126 is "permission denied" (command found but not executable).
	if exitCode == 126 {
		return ClassPermissionDenied, ""
	}

	// Check stderr patterns for permission denied even with other exit codes.
	lower := strings.ToLower(stderr)
	if strings.Contains(lower, "permission denied") || strings.Contains(lower, "eacces") {
		return ClassPermissionDenied, ""
	}

	// Also catch command-not-found patterns in stderr regardless of exit code,
	// since some shells may not use 127 consistently.
	if cmd := extractMissingCommand(stderr); cmd != "" {
		return ClassCommandNotFound, cmd
	}

	return ClassFailure, ""
}

// extractMissingCommand tries to parse the binary name from a "command not found"
// error message in stderr.
func extractMissingCommand(stderr string) string {
	matches := commandNotFoundRe.FindStringSubmatch(stderr)
	if matches == nil {
		return ""
	}
	// Return the first non-empty capture group.
	for _, m := range matches[1:] {
		if m != "" {
			return m
		}
	}
	return ""
}
