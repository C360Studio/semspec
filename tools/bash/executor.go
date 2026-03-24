// Package bash provides a universal shell command tool for agents.
// When a sandbox is configured (SANDBOX_URL), commands execute inside the
// sandbox container. Otherwise, they run locally via os/exec.
package bash

import (
	"bytes"
	"context"
	"fmt"
"os"
	"os/exec"
	"strings"
	"time"

	"github.com/c360studio/semspec/tools/sandbox"
	"github.com/c360studio/semstreams/agentic"
)

const (
	maxOutputBytes = 100 * 1024 // 100KB output cap
	defaultTimeout = 120 * time.Second
)

// Executor runs shell commands.
type Executor struct {
	workDir    string
	sandboxURL string
	sandbox    *sandbox.Client
}

// NewExecutor creates a bash executor. If sandboxURL is non-empty, commands
// are routed to the sandbox container.
func NewExecutor(workDir, sandboxURL string) *Executor {
	e := &Executor{
		workDir:    workDir,
		sandboxURL: sandboxURL,
	}
	if sandboxURL != "" {
		e.sandbox = sandbox.NewClient(sandboxURL)
	}
	return e
}

// ListTools returns the bash tool definition.
func (e *Executor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "bash",
			Description: "Run a shell command. Use for file operations, git, builds, tests, and any other shell task.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "Shell command to execute",
					},
				},
				"required": []string{"command"},
			},
		},
	}
}

// Execute runs a shell command and returns the output.
func (e *Executor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	command, ok := call.Arguments["command"].(string)
	if !ok || command == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "command argument is required",
		}, nil
	}

	if e.sandbox != nil {
		taskID := "default"
		if m := call.Metadata; m != nil {
			if tid, ok := m["task_id"].(string); ok && tid != "" {
				taskID = tid
			}
		}
		return e.execSandbox(ctx, call.ID, command, taskID)
	}
	return e.execLocal(ctx, call.ID, command)
}

// execSandbox routes the command to the sandbox container.
func (e *Executor) execSandbox(ctx context.Context, callID, command, taskID string) (agentic.ToolResult, error) {
	result, err := e.sandbox.Exec(ctx, taskID, command, int(defaultTimeout.Milliseconds()))
	if err != nil {
		return agentic.ToolResult{
			CallID: callID,
			Error:  fmt.Sprintf("sandbox exec failed: %v", err),
		}, nil
	}

	output := result.Stdout
	if result.Stderr != "" {
		if output != "" {
			output += "\n"
		}
		output += result.Stderr
	}

	if result.TimedOut {
		output += "\n[command timed out]"
	}

	if len(output) > maxOutputBytes {
		output = output[:maxOutputBytes] + "\n[output truncated]"
	}

	if result.ExitCode != 0 {
		return agentic.ToolResult{
			CallID: callID,
			Error:  fmt.Sprintf("exit code %d\n%s", result.ExitCode, output),
		}, nil
	}

	return agentic.ToolResult{
		CallID:  callID,
		Content: output,
	}, nil
}

// sensitiveEnvSuffixes lists environment variable name suffixes that indicate
// secrets. Variables matching these are stripped in local mode to prevent
// accidental leakage to agent-generated commands.
var sensitiveEnvSuffixes = []string{
	"_KEY", "_SECRET", "_TOKEN", "_PASSWORD", "_CREDENTIAL",
	"_CREDENTIALS", "_AUTH", "_API_KEY", "_APIKEY",
}

// sensitiveEnvPrefixes lists environment variable name prefixes that indicate
// secrets or cloud credentials.
var sensitiveEnvPrefixes = []string{
	"AWS_", "AZURE_", "GCP_", "GOOGLE_", "GITHUB_TOKEN",
	"OPENAI_", "ANTHROPIC_", "OPENROUTER_",
}

// sensitiveEnvExact lists specific environment variables to always strip.
var sensitiveEnvExact = map[string]bool{
	"BRAVE_SEARCH_API_KEY": true,
	"DATABASE_URL":         true,
	"REDIS_URL":            true,
	"NATS_TOKEN":           true,
	"NATS_NKEY":            true,
	"SSH_AUTH_SOCK":         true,
	"GPG_AGENT_INFO":       true,
}

// filterEnv returns a copy of the environment with sensitive variables removed.
func filterEnv() []string {
	var filtered []string
	for _, env := range os.Environ() {
		name, _, ok := strings.Cut(env, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(name)

		if sensitiveEnvExact[name] {
			continue
		}

		skip := false
		for _, suffix := range sensitiveEnvSuffixes {
			if strings.HasSuffix(upper, suffix) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		for _, prefix := range sensitiveEnvPrefixes {
			if strings.HasPrefix(upper, prefix) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		filtered = append(filtered, env)
	}
	return filtered
}

// execLocal runs the command locally via os/exec.
// Environment variables containing secrets are filtered out to prevent
// accidental leakage. For full isolation, use sandbox mode (SANDBOX_URL).
func (e *Executor) execLocal(ctx context.Context, callID, command string) (agentic.ToolResult, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = e.workDir
	cmd.Env = filterEnv()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	if len(output) > maxOutputBytes {
		output = output[:maxOutputBytes] + "\n[output truncated]"
	}

	if err != nil {
		exitCode := -1
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		if ctx.Err() == context.DeadlineExceeded {
			output += "\n[command timed out]"
		}
		return agentic.ToolResult{
			CallID: callID,
			Error:  fmt.Sprintf("exit code %d\n%s", exitCode, output),
		}, nil
	}

	return agentic.ToolResult{
		CallID:  callID,
		Content: output,
	}, nil
}

// NewExecutorFromEnv creates a bash executor using environment variables.
func NewExecutorFromEnv() *Executor {
	workDir := os.Getenv("SEMSPEC_REPO_PATH")
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	return NewExecutor(workDir, os.Getenv("SANDBOX_URL"))
}
