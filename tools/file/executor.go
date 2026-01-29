// Package file provides file operation tools for the Semspec agent.
package file

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/c360/semstreams/agentic"
)

// Executor implements file operation tools
type Executor struct {
	repoRoot string
}

// NewExecutor creates a new file executor with the given repository root
func NewExecutor(repoRoot string) *Executor {
	return &Executor{repoRoot: repoRoot}
}

// Execute executes a file tool call
func (e *Executor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	switch call.Name {
	case "file_read":
		return e.fileRead(ctx, call)
	case "file_write":
		return e.fileWrite(ctx, call)
	case "file_list":
		return e.fileList(ctx, call)
	default:
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("unknown tool: %s", call.Name),
		}, fmt.Errorf("unknown tool: %s", call.Name)
	}
}

// ListTools returns the tool definitions for file operations
func (e *Executor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "file_read",
			Description: "Read the contents of a file",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Path to the file to read (relative to repo root)",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "file_write",
			Description: "Write content to a file (creates parent directories if needed)",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Path to the file to write (relative to repo root)",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "Content to write to the file",
					},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "file_list",
			Description: "List files in a directory",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Path to the directory to list (relative to repo root)",
					},
					"pattern": map[string]any{
						"type":        "string",
						"description": "Optional glob pattern to filter files (e.g., '*.go')",
					},
				},
				"required": []string{"path"},
			},
		},
	}
}

// fileRead reads the contents of a file
func (e *Executor) fileRead(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	path, ok := call.Arguments["path"].(string)
	if !ok {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "path argument is required",
		}, nil
	}

	fullPath, err := e.validatePath(path)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  err.Error(),
		}, nil
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return agentic.ToolResult{
				CallID: call.ID,
				Error:  fmt.Sprintf("file not found: %s", path),
			}, nil
		}
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("failed to read file: %s", err.Error()),
		}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(content),
	}, nil
}

// fileWrite writes content to a file
func (e *Executor) fileWrite(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	path, ok := call.Arguments["path"].(string)
	if !ok {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "path argument is required",
		}, nil
	}

	content, ok := call.Arguments["content"].(string)
	if !ok {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "content argument is required",
		}, nil
	}

	fullPath, err := e.validatePath(path)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  err.Error(),
		}, nil
	}

	// Create parent directories if needed
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("failed to create directory: %s", err.Error()),
		}, nil
	}

	// Write file
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("failed to write file: %s", err.Error()),
		}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path),
	}, nil
}

// fileList lists files in a directory
func (e *Executor) fileList(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	path, ok := call.Arguments["path"].(string)
	if !ok {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "path argument is required",
		}, nil
	}

	pattern, _ := call.Arguments["pattern"].(string)

	fullPath, err := e.validatePath(path)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  err.Error(),
		}, nil
	}

	// Check if path is a directory
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return agentic.ToolResult{
				CallID: call.ID,
				Error:  fmt.Sprintf("directory not found: %s", path),
			}, nil
		}
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("failed to stat path: %s", err.Error()),
		}, nil
	}

	if !info.IsDir() {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("path is not a directory: %s", path),
		}, nil
	}

	// List entries
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("failed to read directory: %s", err.Error()),
		}, nil
	}

	var files []string
	for _, entry := range entries {
		name := entry.Name()

		// Apply pattern filter if specified
		if pattern != "" {
			matched, err := filepath.Match(pattern, name)
			if err != nil {
				return agentic.ToolResult{
					CallID: call.ID,
					Error:  fmt.Sprintf("invalid pattern: %s", err.Error()),
				}, nil
			}
			if !matched {
				continue
			}
		}

		// Add directory indicator
		if entry.IsDir() {
			name += "/"
		}
		files = append(files, name)
	}

	// Format output
	result, err := json.Marshal(files)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("failed to marshal result: %s", err.Error()),
		}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(result),
	}, nil
}

// validatePath validates and resolves a path, ensuring it's within the repo root
func (e *Executor) validatePath(path string) (string, error) {
	// Handle both absolute and relative paths
	var fullPath string
	if filepath.IsAbs(path) {
		fullPath = filepath.Clean(path)
	} else {
		fullPath = filepath.Clean(filepath.Join(e.repoRoot, path))
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}

	// Get absolute repo root
	absRoot, err := filepath.Abs(e.repoRoot)
	if err != nil {
		return "", fmt.Errorf("failed to resolve repo root: %w", err)
	}

	// Ensure path is within repo root
	if !strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) && absPath != absRoot {
		return "", fmt.Errorf("access denied: path is outside repository root")
	}

	return absPath, nil
}
