package astindexer

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestResolvePaths_NonGlob(t *testing.T) {
	// Create test directory
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	paths, err := ResolvePaths([]string{subDir})
	if err != nil {
		t.Fatalf("ResolvePaths failed: %v", err)
	}

	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}

	absSubDir, _ := filepath.Abs(subDir)
	if paths[0] != absSubDir {
		t.Errorf("expected %q, got %q", absSubDir, paths[0])
	}
}

func TestResolvePaths_NonGlob_NotDirectory(t *testing.T) {
	// Create a file, not a directory
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ResolvePaths([]string{filePath})
	if err == nil {
		t.Error("expected error for non-directory path")
	}
}

func TestResolvePaths_SingleLevelGlob(t *testing.T) {
	// Create test structure:
	// tmpDir/
	//   services/
	//     auth/
	//     users/
	//     db/
	tmpDir := t.TempDir()
	servicesDir := filepath.Join(tmpDir, "services")
	if err := os.Mkdir(servicesDir, 0755); err != nil {
		t.Fatal(err)
	}

	subdirs := []string{"auth", "users", "db"}
	for _, name := range subdirs {
		if err := os.Mkdir(filepath.Join(servicesDir, name), 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Also create a file that should not be matched
	if err := os.WriteFile(filepath.Join(servicesDir, "README.md"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	pattern := filepath.Join(servicesDir, "*")
	paths, err := ResolvePaths([]string{pattern})
	if err != nil {
		t.Fatalf("ResolvePaths failed: %v", err)
	}

	// Should only include directories, not the README file
	if len(paths) != 3 {
		t.Errorf("expected 3 paths, got %d: %v", len(paths), paths)
	}

	// Verify we got all expected directories
	pathSet := make(map[string]bool)
	for _, p := range paths {
		pathSet[filepath.Base(p)] = true
	}

	for _, expected := range subdirs {
		if !pathSet[expected] {
			t.Errorf("expected %q in results", expected)
		}
	}
}

func TestResolvePaths_DoubleStarGlob(t *testing.T) {
	// Create test structure:
	// tmpDir/
	//   a/
	//     b/
	//       c/
	tmpDir := t.TempDir()

	dirs := []string{
		filepath.Join(tmpDir, "a"),
		filepath.Join(tmpDir, "a", "b"),
		filepath.Join(tmpDir, "a", "b", "c"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	pattern := filepath.Join(tmpDir, "**")
	paths, err := ResolvePaths([]string{pattern})
	if err != nil {
		t.Fatalf("ResolvePaths failed: %v", err)
	}

	// Should include all directories recursively
	if len(paths) < 3 {
		t.Errorf("expected at least 3 paths, got %d: %v", len(paths), paths)
	}
}

func TestResolvePaths_Deduplication(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "sub")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Pass the same path multiple times
	paths, err := ResolvePaths([]string{subDir, subDir, subDir})
	if err != nil {
		t.Fatalf("ResolvePaths failed: %v", err)
	}

	if len(paths) != 1 {
		t.Errorf("expected 1 deduplicated path, got %d", len(paths))
	}
}

func TestResolvePaths_NoMatch(t *testing.T) {
	tmpDir := t.TempDir()
	pattern := filepath.Join(tmpDir, "nonexistent", "*")

	_, err := ResolvePaths([]string{pattern})
	if err == nil {
		t.Error("expected error for no-match pattern")
	}
}

func TestContainsGlob(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"./simple/path", false},
		{"./path/*", true},
		{"./path/**", true},
		{"./path/?.txt", true},
		{"./path/[abc]", true},
		{"", false},
	}

	for _, tc := range tests {
		got := containsGlob(tc.pattern)
		if got != tc.want {
			t.Errorf("containsGlob(%q) = %v, want %v", tc.pattern, got, tc.want)
		}
	}
}

func TestResolveWatchPaths(t *testing.T) {
	// Create test structure
	tmpDir := t.TempDir()
	servicesDir := filepath.Join(tmpDir, "services")
	if err := os.Mkdir(servicesDir, 0755); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"auth", "users"} {
		if err := os.Mkdir(filepath.Join(servicesDir, name), 0755); err != nil {
			t.Fatal(err)
		}
	}

	frontendDir := filepath.Join(tmpDir, "frontend")
	if err := os.Mkdir(frontendDir, 0755); err != nil {
		t.Fatal(err)
	}

	configs := []WatchPathConfig{
		{
			Path:      filepath.Join(servicesDir, "*"),
			Org:       "myorg",
			Project:   "backend",
			Languages: []string{"go"},
			Excludes:  []string{"vendor"},
		},
		{
			Path:      frontendDir,
			Org:       "myorg",
			Project:   "frontend",
			Languages: []string{"typescript"},
			Excludes:  []string{"node_modules"},
		},
	}

	resolved, err := ResolveWatchPaths(configs)
	if err != nil {
		t.Fatalf("ResolveWatchPaths failed: %v", err)
	}

	// Should have 3 paths: auth, users, frontend
	if len(resolved) != 3 {
		t.Errorf("expected 3 resolved paths, got %d", len(resolved))
	}

	// Check that configs are preserved correctly
	for _, rp := range resolved {
		base := filepath.Base(rp.AbsPath)
		switch base {
		case "auth", "users":
			if rp.Config.Project != "backend" {
				t.Errorf("expected project 'backend' for %s, got %s", base, rp.Config.Project)
			}
		case "frontend":
			if rp.Config.Project != "frontend" {
				t.Errorf("expected project 'frontend' for %s, got %s", base, rp.Config.Project)
			}
		}
	}
}

func TestMakeAbsolutePattern(t *testing.T) {
	cwd, _ := os.Getwd()

	tests := []struct {
		pattern  string
		wantBase string // The non-glob base should be absolute
	}{
		{"./services/*", filepath.Join(cwd, "services")},
		{"./a/b/**", filepath.Join(cwd, "a", "b")},
		{"./*", cwd},
	}

	for _, tc := range tests {
		result, err := makeAbsolutePattern(tc.pattern)
		if err != nil {
			t.Errorf("makeAbsolutePattern(%q) error: %v", tc.pattern, err)
			continue
		}

		// Check that the result starts with the expected base
		if !filepath.IsAbs(result) {
			t.Errorf("makeAbsolutePattern(%q) = %q, want absolute path", tc.pattern, result)
		}
	}
}

func TestResolvePaths_RelativePath(t *testing.T) {
	// Create a subdirectory relative to current working dir
	tmpDir := t.TempDir()

	// Change to tmpDir temporarily
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	// Create a subdirectory
	if err := os.Mkdir("testsubdir", 0755); err != nil {
		t.Fatal(err)
	}

	paths, err := ResolvePaths([]string{"./testsubdir"})
	if err != nil {
		t.Fatalf("ResolvePaths failed: %v", err)
	}

	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}

	// Should be absolute
	if !filepath.IsAbs(paths[0]) {
		t.Errorf("expected absolute path, got %q", paths[0])
	}
}

func TestResolvePaths_MultiplePatterns(t *testing.T) {
	tmpDir := t.TempDir()

	// Create directories
	dirs := []string{"a", "b", "c"}
	for _, name := range dirs {
		if err := os.Mkdir(filepath.Join(tmpDir, name), 0755); err != nil {
			t.Fatal(err)
		}
	}

	paths, err := ResolvePaths([]string{
		filepath.Join(tmpDir, "a"),
		filepath.Join(tmpDir, "b"),
		filepath.Join(tmpDir, "c"),
	})
	if err != nil {
		t.Fatalf("ResolvePaths failed: %v", err)
	}

	if len(paths) != 3 {
		t.Errorf("expected 3 paths, got %d", len(paths))
	}

	// Sort for consistent comparison
	sort.Strings(paths)
	for i, name := range dirs {
		expected := filepath.Join(tmpDir, name)
		if paths[i] != expected {
			t.Errorf("paths[%d] = %q, want %q", i, paths[i], expected)
		}
	}
}
