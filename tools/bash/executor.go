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
		return e.execSandbox(ctx, call.ID, command)
	}
	return e.execLocal(ctx, call.ID, command)
}

// execSandbox routes the command to the sandbox container.
func (e *Executor) execSandbox(ctx context.Context, callID, command string) (agentic.ToolResult, error) {
	result, err := e.sandbox.Exec(ctx, "bash", command, int(defaultTimeout.Milliseconds()))
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

// execLocal runs the command locally via os/exec.
func (e *Executor) execLocal(ctx context.Context, callID, command string) (agentic.ToolResult, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = e.workDir

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
		if ctx.Err() == context.DeadlineExceeded {
			output += "\n[command timed out]"
		}
		return agentic.ToolResult{
			CallID: callID,
			Error:  fmt.Sprintf("exit code %d\n%s", cmd.ProcessState.ExitCode(), output),
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
