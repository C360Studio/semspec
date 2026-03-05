package governance

import (
	"context"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockExecutor records calls and returns a canned success result.
type mockExecutor struct {
	calls []agentic.ToolCall
}

func (m *mockExecutor) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	m.calls = append(m.calls, call)
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: "ok",
	}, nil
}

func (m *mockExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{Name: "file_write"},
		{Name: "file_read"},
		{Name: "file_list"},
		{Name: "git_commit"},
	}
}

func (m *mockExecutor) reset() {
	m.calls = nil
}

// toolCall builds an agentic.ToolCall for testing.
func toolCall(id, name string, args map[string]any) agentic.ToolCall {
	return agentic.ToolCall{
		ID:        id,
		Name:      name,
		Arguments: args,
	}
}

func TestFileScopeFilter_AllowsFileWithinScope(t *testing.T) {
	inner := &mockExecutor{}
	f := NewFileScopeFilter(inner, []string{"src/auth/**"})

	result, err := f.Execute(context.Background(), toolCall("c1", "file_write", map[string]any{
		"path":    "src/auth/login.go",
		"content": "package auth",
	}))

	require.NoError(t, err)
	assert.Empty(t, result.Error, "expected no error for file within scope")
	assert.Equal(t, "ok", result.Content)
	assert.Len(t, inner.calls, 1, "inner executor should have been called")
}

func TestFileScopeFilter_BlocksFileOutsideScope(t *testing.T) {
	inner := &mockExecutor{}
	f := NewFileScopeFilter(inner, []string{"src/auth/**"})

	result, err := f.Execute(context.Background(), toolCall("c2", "file_write", map[string]any{
		"path":    "config/settings.go",
		"content": "package config",
	}))

	require.NoError(t, err)
	assert.NotEmpty(t, result.Error, "expected a block error for file outside scope")
	assert.Contains(t, result.Error, "file_write blocked")
	assert.Contains(t, result.Error, "config/settings.go")
	assert.Empty(t, inner.calls, "inner executor must not be called when blocked")
}

func TestFileScopeFilter_AllowsExactMatch(t *testing.T) {
	inner := &mockExecutor{}
	f := NewFileScopeFilter(inner, []string{"README.md"})

	result, err := f.Execute(context.Background(), toolCall("c3", "file_write", map[string]any{
		"path":    "README.md",
		"content": "# Hello",
	}))

	require.NoError(t, err)
	assert.Empty(t, result.Error)
	assert.Len(t, inner.calls, 1)
}

func TestFileScopeFilter_AllowsGlobPattern(t *testing.T) {
	inner := &mockExecutor{}
	f := NewFileScopeFilter(inner, []string{"src/**/*.go"})

	result, err := f.Execute(context.Background(), toolCall("c4", "file_write", map[string]any{
		"path":    "src/auth/handler.go",
		"content": "package auth",
	}))

	require.NoError(t, err)
	assert.Empty(t, result.Error)
	assert.Len(t, inner.calls, 1)
}

func TestFileScopeFilter_BlocksPathTraversal(t *testing.T) {
	inner := &mockExecutor{}
	f := NewFileScopeFilter(inner, []string{"src/**"})

	result, err := f.Execute(context.Background(), toolCall("c5", "file_write", map[string]any{
		"path":    "../etc/passwd",
		"content": "root:x:0:0",
	}))

	require.NoError(t, err)
	assert.NotEmpty(t, result.Error, "path traversal must be blocked")
	assert.Contains(t, result.Error, "file_write blocked")
	assert.Empty(t, inner.calls)
}

func TestFileScopeFilter_AllowsReadOperations(t *testing.T) {
	inner := &mockExecutor{}
	// Scope only covers src/ — but reads should always pass through.
	f := NewFileScopeFilter(inner, []string{"src/**"})

	result, err := f.Execute(context.Background(), toolCall("c6", "file_read", map[string]any{
		"path": "config/settings.go",
	}))

	require.NoError(t, err)
	assert.Empty(t, result.Error, "file_read must not be blocked by scope")
	assert.Len(t, inner.calls, 1)
}

func TestFileScopeFilter_PassesThroughUnknownTools(t *testing.T) {
	inner := &mockExecutor{}
	f := NewFileScopeFilter(inner, []string{"src/**"})

	// git_commit and arbitrary tools should pass through.
	result, err := f.Execute(context.Background(), toolCall("c7", "git_commit", map[string]any{
		"message": "feat(auth): add handler",
	}))

	require.NoError(t, err)
	assert.Empty(t, result.Error)
	assert.Len(t, inner.calls, 1)
}

func TestFileScopeFilter_EmptyScopeBlocksAll(t *testing.T) {
	inner := &mockExecutor{}
	f := NewFileScopeFilter(inner, []string{})

	for _, name := range []string{"file_write", "file_create", "file_delete"} {
		inner.reset()
		result, err := f.Execute(context.Background(), toolCall("c8", name, map[string]any{
			"path":    "src/anything.go",
			"content": "package x",
		}))

		require.NoError(t, err, "tool: %s", name)
		assert.NotEmpty(t, result.Error, "empty scope must block %s", name)
		assert.Empty(t, inner.calls, "inner must not be called for %s with empty scope", name)
	}
}

func TestFileScopeFilter_MultipleScopes(t *testing.T) {
	inner := &mockExecutor{}
	f := NewFileScopeFilter(inner, []string{
		"src/auth/**",
		"src/user/**",
		"docs/*.md",
	})

	tests := []struct {
		path    string
		allowed bool
	}{
		{"src/auth/login.go", true},
		{"src/user/profile.go", true},
		{"docs/api.md", true},
		{"src/billing/invoice.go", false},
		{"config/db.go", false},
	}

	for _, tc := range tests {
		inner.reset()
		result, err := f.Execute(context.Background(), toolCall("cx", "file_write", map[string]any{
			"path":    tc.path,
			"content": "package x",
		}))
		require.NoError(t, err, "path: %s", tc.path)
		if tc.allowed {
			assert.Empty(t, result.Error, "path %q should be allowed", tc.path)
			assert.Len(t, inner.calls, 1, "inner should be called for %q", tc.path)
		} else {
			assert.NotEmpty(t, result.Error, "path %q should be blocked", tc.path)
			assert.Empty(t, inner.calls, "inner must not be called for %q", tc.path)
		}
	}
}

func TestFileScopeFilter_DoubleStarGlob(t *testing.T) {
	inner := &mockExecutor{}
	f := NewFileScopeFilter(inner, []string{"pkg/**"})

	deepPath := "pkg/sub/deep/file.go"
	result, err := f.Execute(context.Background(), toolCall("c9", "file_write", map[string]any{
		"path":    deepPath,
		"content": "package deep",
	}))

	require.NoError(t, err)
	assert.Empty(t, result.Error, "pkg/** must match nested path %q", deepPath)
	assert.Len(t, inner.calls, 1)
}

func TestFileScopeFilter_ListToolsDelegates(t *testing.T) {
	inner := &mockExecutor{}
	f := NewFileScopeFilter(inner, []string{"src/**"})

	tools := f.ListTools()
	assert.Equal(t, inner.ListTools(), tools, "ListTools must delegate to inner executor")
}

func TestFileScopeFilter_FileCreateBlocked(t *testing.T) {
	inner := &mockExecutor{}
	f := NewFileScopeFilter(inner, []string{"src/auth/**"})

	result, err := f.Execute(context.Background(), toolCall("c10", "file_create", map[string]any{
		"path":    "internal/secret.go",
		"content": "package internal",
	}))

	require.NoError(t, err)
	assert.Contains(t, result.Error, "file_create blocked")
	assert.Empty(t, inner.calls)
}

func TestFileScopeFilter_FileDeleteBlocked(t *testing.T) {
	inner := &mockExecutor{}
	f := NewFileScopeFilter(inner, []string{"src/auth/**"})

	result, err := f.Execute(context.Background(), toolCall("c11", "file_delete", map[string]any{
		"path": "internal/secret.go",
	}))

	require.NoError(t, err)
	assert.Contains(t, result.Error, "file_delete blocked")
	assert.Empty(t, inner.calls)
}

// TestMatchFileScope tests the internal path matching helper directly.
func TestMatchFileScope(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		patterns []string
		want     bool
	}{
		{
			name:     "exact match",
			path:     "README.md",
			patterns: []string{"README.md"},
			want:     true,
		},
		{
			name:     "single star does not cross separators",
			path:     "src/auth/login.go",
			patterns: []string{"src/*.go"},
			want:     false,
		},
		{
			name:     "double star crosses separators",
			path:     "src/auth/login.go",
			patterns: []string{"src/**/*.go"},
			want:     true,
		},
		{
			name:     "double star directory prefix",
			path:     "pkg/sub/deep/file.go",
			patterns: []string{"pkg/**"},
			want:     true,
		},
		{
			name:     "path traversal is rejected",
			path:     "../etc/passwd",
			patterns: []string{"**"},
			want:     false,
		},
		{
			name:     "empty patterns deny-all",
			path:     "any/file.go",
			patterns: []string{},
			want:     false,
		},
		{
			name:     "first pattern matches",
			path:     "a/b.go",
			patterns: []string{"a/**", "c/**"},
			want:     true,
		},
		{
			name:     "second pattern matches",
			path:     "c/d.go",
			patterns: []string{"a/**", "c/**"},
			want:     true,
		},
		{
			name:     "no pattern matches",
			path:     "z/file.go",
			patterns: []string{"a/**", "b/**"},
			want:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := matchFileScope(tc.path, tc.patterns)
			assert.Equal(t, tc.want, got)
		})
	}
}
