package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/c360studio/semstreams/agentic"
)

// GrepExecutor implements grep fallback tools for when graph lacks data.
// IMPORTANT: This should only be used as a fallback when the knowledge graph
// doesn't have the information needed. Prefer using workflow_query_graph first.
type GrepExecutor struct {
	repoRoot string
}

// NewGrepExecutor creates a new grep executor.
func NewGrepExecutor(repoRoot string) *GrepExecutor {
	return &GrepExecutor{repoRoot: repoRoot}
}

// Execute executes a grep tool call.
func (e *GrepExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	switch call.Name {
	case "workflow_grep_fallback":
		return e.grepFallback(ctx, call)
	case "workflow_find_files":
		return e.findFiles(ctx, call)
	default:
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("unknown tool: %s", call.Name),
		}, fmt.Errorf("unknown tool: %s", call.Name)
	}
}

// ListTools returns the tool definitions for grep operations.
func (e *GrepExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name: "workflow_grep_fallback",
			Description: `FALLBACK ONLY: Search codebase using grep when the knowledge graph doesn't have the information needed.

IMPORTANT: Before using this tool:
1. First try workflow_query_graph to search the indexed knowledge graph
2. First try workflow_get_codebase_summary to understand what's indexed
3. Only use grep if the graph doesn't contain what you need

This tool is slower and uses more tokens than graph queries. The graph contains pre-indexed, structured data about functions, types, and relationships that is more efficient to query.`,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{
						"type":        "string",
						"description": "The pattern to search for (regex supported)",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Optional subdirectory to search in (relative to repo root)",
					},
					"file_pattern": map[string]any{
						"type":        "string",
						"description": "Optional file glob pattern (e.g., '*.go', '*.ts')",
					},
					"context_lines": map[string]any{
						"type":        "integer",
						"description": "Number of context lines before and after match (default: 2, max: 5)",
					},
					"max_results": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 20, max: 50)",
					},
				},
				"required": []string{"pattern"},
			},
		},
		{
			Name: "workflow_find_files",
			Description: `FALLBACK ONLY: Find files matching a pattern when the knowledge graph doesn't have file information.

IMPORTANT: Before using this tool:
1. First try workflow_query_graph with code.artifact.path predicate
2. First try workflow_get_codebase_summary to see indexed packages
3. Only use this if the graph doesn't have file information

The knowledge graph contains indexed file paths that are faster to query.`,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{
						"type":        "string",
						"description": "Glob pattern to match files (e.g., '**/*.go', 'src/**/*.ts')",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Optional subdirectory to search in (relative to repo root)",
					},
					"max_results": map[string]any{
						"type":        "integer",
						"description": "Maximum number of files to return (default: 50, max: 100)",
					},
				},
				"required": []string{"pattern"},
			},
		},
	}
}

// grepFallback performs a grep search as a fallback.
func (e *GrepExecutor) grepFallback(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	pattern, ok := call.Arguments["pattern"].(string)
	if !ok || pattern == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "pattern argument is required",
		}, nil
	}

	searchPath := e.repoRoot
	if p, ok := call.Arguments["path"].(string); ok && p != "" {
		searchPath = filepath.Join(e.repoRoot, p)
	}

	filePattern := ""
	if fp, ok := call.Arguments["file_pattern"].(string); ok {
		filePattern = fp
	}

	contextLines := 2
	if cl, ok := call.Arguments["context_lines"].(float64); ok {
		contextLines = int(cl)
		if contextLines > 5 {
			contextLines = 5
		}
	}

	maxResults := 20
	if mr, ok := call.Arguments["max_results"].(float64); ok {
		maxResults = int(mr)
		if maxResults > 50 {
			maxResults = 50
		}
	}

	// Build grep command
	args := []string{
		"-r",                                          // recursive
		"-n",                                          // line numbers
		"-H",                                          // print filename
		fmt.Sprintf("-C%d", contextLines),             // context
		fmt.Sprintf("-m%d", maxResults),               // max count per file
		"--color=never",                               // no color codes
		"-E",                                          // extended regex
		"--exclude-dir=.git",                          // exclude git
		"--exclude-dir=node_modules",                  // exclude node_modules
		"--exclude-dir=vendor",                        // exclude vendor
		"--exclude-dir=.semspec",                      // exclude .semspec
		"--exclude-dir=dist",                          // exclude dist
		"--exclude-dir=build",                         // exclude build
		"--exclude=*.lock",                            // exclude lock files
		"--exclude=*.sum",                             // exclude sum files
	}

	if filePattern != "" {
		args = append(args, fmt.Sprintf("--include=%s", filePattern))
	}

	args = append(args, pattern, searchPath)

	cmd := exec.CommandContext(ctx, "grep", args...)
	output, err := cmd.Output()

	// grep returns exit code 1 when no matches found - this is not an error
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return agentic.ToolResult{
					CallID:  call.ID,
					Content: "No matches found.",
				}, nil
			}
		}
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("grep failed: %v", err),
		}, nil
	}

	// Limit output size
	result := string(output)
	lines := strings.Split(result, "\n")
	if len(lines) > maxResults*4 { // Account for context lines
		lines = lines[:maxResults*4]
		result = strings.Join(lines, "\n") + "\n... (truncated)"
	}

	// Add a note about fallback usage
	result = "NOTE: Using grep fallback. Consider if this data should be indexed in the knowledge graph.\n\n" + result

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: result,
	}, nil
}

// findFiles finds files matching a pattern.
func (e *GrepExecutor) findFiles(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	pattern, ok := call.Arguments["pattern"].(string)
	if !ok || pattern == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "pattern argument is required",
		}, nil
	}

	searchPath := e.repoRoot
	if p, ok := call.Arguments["path"].(string); ok && p != "" {
		searchPath = filepath.Join(e.repoRoot, p)
	}

	maxResults := 50
	if mr, ok := call.Arguments["max_results"].(float64); ok {
		maxResults = int(mr)
		if maxResults > 100 {
			maxResults = 100
		}
	}

	// Use find command with glob pattern
	var files []string
	err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip inaccessible paths
		}

		// Skip excluded directories
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" ||
				name == ".semspec" || name == "dist" || name == "build" {
				return filepath.SkipDir
			}
			return nil
		}

		// Match against pattern
		matched, err := filepath.Match(pattern, info.Name())
		if err != nil {
			return nil
		}

		// Also try matching with directory prefix for ** patterns
		if !matched && strings.Contains(pattern, "/") {
			relPath, _ := filepath.Rel(e.repoRoot, path)
			matched, _ = filepath.Match(pattern, relPath)
		}

		if matched {
			relPath, _ := filepath.Rel(e.repoRoot, path)
			files = append(files, relPath)
			if len(files) >= maxResults {
				return fmt.Errorf("max results reached")
			}
		}

		return nil
	})

	// "max results reached" is expected, not an error
	if err != nil && err.Error() != "max results reached" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("find failed: %v", err),
		}, nil
	}

	if len(files) == 0 {
		return agentic.ToolResult{
			CallID:  call.ID,
			Content: "No files found matching pattern.",
		}, nil
	}

	result := map[string]any{
		"note":    "Using find fallback. Consider if these files should be indexed in the knowledge graph.",
		"count":   len(files),
		"pattern": pattern,
		"files":   files,
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(output),
	}, nil
}
