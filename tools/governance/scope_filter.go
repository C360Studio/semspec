package governance

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/c360studio/semstreams/agentic"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// writeTools is the set of tool names that perform destructive file operations.
// file_read and file_list are reads — they are never blocked by scope.
var writeTools = map[string]bool{
	"file_write":  true,
	"file_create": true,
	"file_delete": true,
}

// FileScopeFilter intercepts tool calls and blocks file operations outside
// the declared scope. It wraps a ToolExecutor and validates file paths
// against the allowed glob patterns before delegating to the inner executor.
//
// FileScopeFilter is stateless and safe for concurrent use.
type FileScopeFilter struct {
	inner     agentictools.ToolExecutor
	fileScope []string // glob patterns from TaskNode.FileScope
}

// NewFileScopeFilter returns a FileScopeFilter that enforces fileScope patterns
// on write operations delegated to inner. A nil or empty fileScope causes all
// write operations to be blocked (deny-all policy).
func NewFileScopeFilter(inner agentictools.ToolExecutor, fileScope []string) *FileScopeFilter {
	// Copy to avoid external mutation of the slice.
	scope := make([]string, len(fileScope))
	copy(scope, fileScope)
	return &FileScopeFilter{
		inner:     inner,
		fileScope: scope,
	}
}

// Execute checks if the tool call targets files within scope.
//   - For file_write, file_create, file_delete: validates the "path" argument
//     against fileScope patterns. Returns an error ToolResult if out of scope.
//   - For all other tools: delegates to the inner executor without restriction.
func (f *FileScopeFilter) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if writeTools[call.Name] {
		path, ok := call.Arguments["path"].(string)
		if !ok || path == "" {
			// Let the inner executor handle missing/invalid path arguments.
			return f.inner.Execute(ctx, call)
		}

		// Normalize to a clean relative path for pattern matching.
		rel := toRelativePath(path)

		if !matchFileScope(rel, f.fileScope) {
			return agentic.ToolResult{
				CallID: call.ID,
				Error: fmt.Sprintf(
					"%s blocked: path %q is outside declared file scope %v",
					call.Name, path, f.fileScope,
				),
			}, nil
		}
	}

	return f.inner.Execute(ctx, call)
}

// ListTools delegates to the inner executor.
func (f *FileScopeFilter) ListTools() []agentic.ToolDefinition {
	return f.inner.ListTools()
}

// toRelativePath converts an agent-supplied path to a clean relative path
// suitable for glob matching against file scope patterns.
//
// Absolute paths have their leading separator stripped. Relative paths are
// cleaned in place. In both cases the result uses forward slashes and has no
// leading "./" prefix.
func toRelativePath(path string) string {
	// Clean to remove redundant separators and dots.
	clean := filepath.ToSlash(filepath.Clean(path))

	// Reject path traversal outright — the inner executor will also reject it,
	// but we surface a governance block first.
	if strings.HasPrefix(clean, "..") || strings.Contains(clean, "/../") {
		return clean // matchFileScope will return false for this.
	}

	// Strip any leading "/" to make the path relative.
	clean = strings.TrimPrefix(clean, "/")

	// Strip any leading "./" produced by filepath.Clean.
	clean = strings.TrimPrefix(clean, "./")

	return clean
}

// matchFileScope reports whether path matches any of the glob patterns.
// It uses doublestar semantics, where "**" matches across path separators.
//
// An empty patterns slice always returns false (deny-all).
func matchFileScope(path string, patterns []string) bool {
	// Reject path traversal attempts regardless of patterns.
	if strings.Contains(path, "..") {
		return false
	}

	for _, pattern := range patterns {
		matched, err := doublestar.Match(pattern, path)
		if err != nil {
			// Invalid glob pattern — treat as non-matching rather than panicking.
			continue
		}
		if matched {
			return true
		}
	}
	return false
}
