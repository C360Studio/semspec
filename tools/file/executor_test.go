package file

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/c360studio/semstreams/agentic"
)

func TestFileRead(t *testing.T) {
	tmpDir := t.TempDir()
	executor := NewExecutor(tmpDir)

	// Create test file
	testContent := "hello world"
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	tests := []struct {
		name      string
		path      string
		wantErr   bool
		wantMatch string
	}{
		{
			name:      "read existing file",
			path:      "test.txt",
			wantErr:   false,
			wantMatch: testContent,
		},
		{
			name:    "read non-existent file",
			path:    "nonexistent.txt",
			wantErr: true,
		},
		{
			name:    "read outside repo root",
			path:    "../../../etc/passwd",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := agentic.ToolCall{
				ID:        "test-call",
				Name:      "file_read",
				Arguments: map[string]any{"path": tt.path},
			}

			result, _ := executor.Execute(context.Background(), call)

			if tt.wantErr {
				if result.Error == "" {
					t.Error("expected error, got none")
				}
			} else {
				if result.Error != "" {
					t.Errorf("unexpected error: %s", result.Error)
				}
				if result.Content != tt.wantMatch {
					t.Errorf("content mismatch: got %q, want %q", result.Content, tt.wantMatch)
				}
			}
		})
	}
}

func TestFileWrite(t *testing.T) {
	tmpDir := t.TempDir()
	executor := NewExecutor(tmpDir)

	tests := []struct {
		name    string
		path    string
		content string
		wantErr bool
	}{
		{
			name:    "write new file",
			path:    "new.txt",
			content: "new content",
			wantErr: false,
		},
		{
			name:    "write with parent dirs",
			path:    "subdir/deep/file.txt",
			content: "nested content",
			wantErr: false,
		},
		{
			name:    "write outside repo root",
			path:    "../../../tmp/evil.txt",
			content: "evil",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := agentic.ToolCall{
				ID:   "test-call",
				Name: "file_write",
				Arguments: map[string]any{
					"path":    tt.path,
					"content": tt.content,
				},
			}

			result, _ := executor.Execute(context.Background(), call)

			if tt.wantErr {
				if result.Error == "" {
					t.Error("expected error, got none")
				}
			} else {
				if result.Error != "" {
					t.Errorf("unexpected error: %s", result.Error)
				}

				// Verify file was created
				fullPath := filepath.Join(tmpDir, tt.path)
				content, err := os.ReadFile(fullPath)
				if err != nil {
					t.Errorf("failed to read written file: %v", err)
				}
				if string(content) != tt.content {
					t.Errorf("content mismatch: got %q, want %q", string(content), tt.content)
				}
			}
		})
	}
}

func TestFileList(t *testing.T) {
	tmpDir := t.TempDir()
	executor := NewExecutor(tmpDir)

	// Create test files
	os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("1"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("2"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "file3.go"), []byte("3"), 0644)
	os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)

	tests := []struct {
		name        string
		path        string
		pattern     string
		wantErr     bool
		wantMinLen  int
		wantContain string
	}{
		{
			name:        "list directory",
			path:        ".",
			wantErr:     false,
			wantMinLen:  4, // 3 files + 1 dir
			wantContain: "file1.txt",
		},
		{
			name:        "list with pattern",
			path:        ".",
			pattern:     "*.txt",
			wantErr:     false,
			wantMinLen:  2,
			wantContain: "file1.txt",
		},
		{
			name:    "list non-existent",
			path:    "nonexistent",
			wantErr: true,
		},
		{
			name:    "list outside repo",
			path:    "../../../",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{"path": tt.path}
			if tt.pattern != "" {
				args["pattern"] = tt.pattern
			}

			call := agentic.ToolCall{
				ID:        "test-call",
				Name:      "file_list",
				Arguments: args,
			}

			result, _ := executor.Execute(context.Background(), call)

			if tt.wantErr {
				if result.Error == "" {
					t.Error("expected error, got none")
				}
			} else {
				if result.Error != "" {
					t.Errorf("unexpected error: %s", result.Error)
				}
				if len(result.Content) < tt.wantMinLen {
					t.Errorf("content too short: got %d, want at least %d", len(result.Content), tt.wantMinLen)
				}
			}
		})
	}
}

func TestListTools(t *testing.T) {
	executor := NewExecutor("/tmp")
	tools := executor.ListTools()

	if len(tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(tools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	expected := []string{"file_read", "file_write", "file_list"}
	for _, name := range expected {
		if !toolNames[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestValidatePath(t *testing.T) {
	tmpDir := t.TempDir()
	executor := NewExecutor(tmpDir)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "relative path within repo",
			path:    "subdir/file.txt",
			wantErr: false,
		},
		{
			name:    "absolute path within repo",
			path:    filepath.Join(tmpDir, "file.txt"),
			wantErr: false,
		},
		{
			name:    "path traversal attack",
			path:    "../../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "absolute path outside repo",
			path:    "/etc/passwd",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.validatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
