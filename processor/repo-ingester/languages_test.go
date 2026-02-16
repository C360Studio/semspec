package repoingester

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectLanguages(t *testing.T) {
	// Create a temporary directory with various files
	dir := t.TempDir()

	// Create test files
	files := map[string]string{
		"main.go":           "package main",
		"app.ts":            "const x = 1;",
		"style.css":         "body {}",
		"README.md":         "# README",
		"config.json":       "{}",
		"script.py":         "print('hello')",
		"nested/lib.go":     "package lib",
		".hidden/x.go":      "package hidden", // hidden dir, should be skipped
		"node_modules/x.js": "// npm",         // vendor dir, should be skipped
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0755)
		os.WriteFile(path, []byte(content), 0644)
	}

	languages, err := DetectLanguages(dir)
	if err != nil {
		t.Fatalf("DetectLanguages failed: %v", err)
	}

	// Should detect go, typescript, css, markdown, json, python
	langSet := make(map[string]bool)
	for _, l := range languages {
		langSet[l] = true
	}

	expected := []string{"go", "typescript", "css", "markdown", "json", "python"}
	for _, lang := range expected {
		if !langSet[lang] {
			t.Errorf("expected to detect %q, got: %v", lang, languages)
		}
	}

	// Should NOT detect javascript from node_modules
	if langSet["javascript"] {
		t.Error("should not detect javascript from node_modules")
	}
}

func TestDetectLanguages_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	languages, err := DetectLanguages(dir)
	if err != nil {
		t.Fatalf("DetectLanguages failed: %v", err)
	}

	if len(languages) != 0 {
		t.Errorf("expected empty languages for empty dir, got: %v", languages)
	}
}

func TestDetectLanguages_NonexistentDir(t *testing.T) {
	langs, err := DetectLanguages("/nonexistent/path")
	// filepath.Walk returns nil error for non-existent path but visits nothing
	// So we just check it doesn't panic and returns empty
	if err != nil {
		// If it errors, that's also acceptable
		return
	}
	if len(langs) != 0 {
		t.Errorf("expected empty languages for nonexistent dir, got: %v", langs)
	}
}

func TestFilterASTLanguages(t *testing.T) {
	// FilterASTLanguages only keeps go, typescript, javascript (per GetSupportedASTLanguages)
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "mixed languages",
			input:    []string{"go", "typescript", "python", "markdown", "json"},
			expected: []string{"go", "typescript"},
		},
		{
			name:     "all supported",
			input:    []string{"go", "typescript", "javascript"},
			expected: []string{"go", "typescript", "javascript"},
		},
		{
			name:     "no supported languages",
			input:    []string{"python", "rust", "java"},
			expected: []string{},
		},
		{
			name:     "empty input",
			input:    []string{},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterASTLanguages(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("FilterASTLanguages(%v) = %v, want %v", tt.input, result, tt.expected)
				return
			}

			resultSet := make(map[string]bool)
			for _, l := range result {
				resultSet[l] = true
			}

			for _, exp := range tt.expected {
				if !resultSet[exp] {
					t.Errorf("FilterASTLanguages(%v) missing %q, got %v", tt.input, exp, result)
				}
			}
		})
	}
}

func TestGetSupportedASTLanguages(t *testing.T) {
	supported := GetSupportedASTLanguages()

	expected := []string{"go", "typescript", "javascript"}

	if len(supported) != len(expected) {
		t.Errorf("GetSupportedASTLanguages() = %v, want %v", supported, expected)
	}

	supportedSet := make(map[string]bool)
	for _, l := range supported {
		supportedSet[l] = true
	}

	for _, exp := range expected {
		if !supportedSet[exp] {
			t.Errorf("GetSupportedASTLanguages() missing %q", exp)
		}
	}
}

func TestKnownLanguages(t *testing.T) {
	// Verify key languages exist in KnownLanguages
	expected := []string{"go", "typescript", "javascript", "python", "rust", "java"}

	for _, lang := range expected {
		if _, ok := KnownLanguages[lang]; !ok {
			t.Errorf("expected %q to be in KnownLanguages", lang)
		}
	}
}

func TestDetectLanguages_SkipsVendorDirs(t *testing.T) {
	dir := t.TempDir()

	// Create files in vendor directories that should be skipped
	vendorDirs := []string{"node_modules", "vendor", ".git"}

	for _, vd := range vendorDirs {
		path := filepath.Join(dir, vd, "test.go")
		os.MkdirAll(filepath.Dir(path), 0755)
		os.WriteFile(path, []byte("package test"), 0644)
	}

	// Create one file that should be detected
	os.WriteFile(filepath.Join(dir, "main.py"), []byte("print('hello')"), 0644)

	languages, err := DetectLanguages(dir)
	if err != nil {
		t.Fatalf("DetectLanguages failed: %v", err)
	}

	// Should only detect python, not go from vendor dirs
	langSet := make(map[string]bool)
	for _, l := range languages {
		langSet[l] = true
	}

	if !langSet["python"] {
		t.Error("expected to detect python")
	}
	if langSet["go"] {
		t.Error("should not detect go from vendor directories")
	}
}
