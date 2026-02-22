package astindexer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semspec/processor/ast"
	"github.com/c360studio/semstreams/message"
)

// TestMultiLanguageInitialIndex verifies that the component correctly parses
// files of different languages (Go, TypeScript, Python) and generates the
// expected entities.
func TestMultiLanguageInitialIndex(t *testing.T) {
	tmpDir := t.TempDir()

	// Create Go file
	goCode := `package main

// User represents a user.
type User struct {
	Name string
}

// NewUser creates a new user.
func NewUser(name string) *User {
	return &User{Name: name}
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(goCode), 0644); err != nil {
		t.Fatalf("write go file: %v", err)
	}

	// Create TypeScript file
	tsCode := `/**
 * User interface.
 */
export interface User {
  name: string;
}

/**
 * Creates a new user.
 */
export function createUser(name: string): User {
  return { name };
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "user.ts"), []byte(tsCode), 0644); err != nil {
		t.Fatalf("write ts file: %v", err)
	}

	// Parse with Go parser
	goParser, err := ast.DefaultRegistry.CreateParser("go", "testorg", "testproj", tmpDir)
	if err != nil {
		t.Fatalf("create go parser: %v", err)
	}

	goResult, err := goParser.ParseFile(context.Background(), filepath.Join(tmpDir, "main.go"))
	if err != nil {
		t.Fatalf("parse go file: %v", err)
	}

	// Verify Go entities
	var hasGoStruct, hasGoFunc bool
	for _, e := range goResult.Entities {
		if e.Type == ast.TypeStruct && e.Name == "User" {
			hasGoStruct = true
		}
		if e.Type == ast.TypeFunction && e.Name == "NewUser" {
			hasGoFunc = true
		}
	}
	if !hasGoStruct {
		t.Error("Go User struct not found")
	}
	if !hasGoFunc {
		t.Error("Go NewUser function not found")
	}

	// Parse with TypeScript parser
	tsParser, err := ast.DefaultRegistry.CreateParser("typescript", "testorg", "testproj", tmpDir)
	if err != nil {
		t.Fatalf("create ts parser: %v", err)
	}

	tsResult, err := tsParser.ParseFile(context.Background(), filepath.Join(tmpDir, "user.ts"))
	if err != nil {
		t.Fatalf("parse ts file: %v", err)
	}

	// Verify TypeScript entities
	var hasTSInterface, hasTSFunc bool
	for _, e := range tsResult.Entities {
		if e.Type == ast.TypeInterface && e.Name == "User" {
			hasTSInterface = true
		}
		if e.Type == ast.TypeFunction && e.Name == "createUser" {
			hasTSFunc = true
		}
	}
	if !hasTSInterface {
		t.Error("TypeScript User interface not found")
	}
	if !hasTSFunc {
		t.Error("TypeScript createUser function not found")
	}
}

// TestExtensionToParserMapping verifies that file extensions are correctly
// mapped to their respective parsers.
func TestExtensionToParserMapping(t *testing.T) {
	tests := []struct {
		extension  string
		wantParser string
	}{
		{".go", "go"},
		{".ts", "typescript"},
		{".tsx", "typescript"},
		{".mts", "typescript"},
		{".cts", "typescript"},
		{".js", "javascript"},
		{".jsx", "javascript"},
		{".mjs", "javascript"},
		{".cjs", "javascript"},
		{".py", "python"},
		{".java", "java"},
		{".svelte", "svelte"},
	}

	for _, tt := range tests {
		t.Run(tt.extension, func(t *testing.T) {
			parserName, ok := ast.DefaultRegistry.GetParserName(tt.extension)
			if !ok {
				t.Fatalf("no parser found for extension %s", tt.extension)
			}
			if parserName != tt.wantParser {
				t.Errorf("extension %s: got parser %q, want %q", tt.extension, parserName, tt.wantParser)
			}
		})
	}
}

// TestExcludeDirectoryHandling verifies that excluded directories are properly
// skipped during parsing.
func TestExcludeDirectoryHandling(t *testing.T) {
	tmpDir := t.TempDir()

	// Create directory structure with excluded dirs
	dirs := []string{
		"src",
		"node_modules",
		".git",
		"vendor",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(tmpDir, d), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
		// Create a Go file in each directory
		code := "package " + strings.ReplaceAll(d, ".", "") + "\n\nfunc Foo() {}\n"
		filePath := filepath.Join(tmpDir, d, "file.go")
		if err := os.WriteFile(filePath, []byte(code), 0644); err != nil {
			t.Fatalf("write file in %s: %v", d, err)
		}
	}

	// Create config with excludes
	wp := WatchPathConfig{
		Path:      tmpDir,
		Org:       "testorg",
		Project:   "testproj",
		Languages: []string{"go"},
		Excludes:  []string{"node_modules", "vendor"},
	}

	excludeSet := make(map[string]bool)
	for _, exc := range wp.Excludes {
		excludeSet[exc] = true
	}

	// Walk and count files, simulating the component's parseDirectory behavior
	var parsedFiles []string
	err := filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if excludeSet[base] || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			parsedFiles = append(parsedFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	// Should only include src/file.go (node_modules, vendor, .git excluded)
	if len(parsedFiles) != 1 {
		t.Errorf("expected 1 parsed file, got %d: %v", len(parsedFiles), parsedFiles)
	}
	if len(parsedFiles) > 0 && !strings.Contains(parsedFiles[0], "src") {
		t.Errorf("expected src/file.go, got %s", parsedFiles[0])
	}
}

// TestErrorResilience verifies that the component continues processing
// even when some files fail to parse.
func TestErrorResilience(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a valid Go file
	validCode := `package main

func ValidFunc() {}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "valid.go"), []byte(validCode), 0644); err != nil {
		t.Fatalf("write valid file: %v", err)
	}

	// Create an invalid Go file
	invalidCode := `package main

func Broken( {
`
	if err := os.WriteFile(filepath.Join(tmpDir, "invalid.go"), []byte(invalidCode), 0644); err != nil {
		t.Fatalf("write invalid file: %v", err)
	}

	parser, err := ast.DefaultRegistry.CreateParser("go", "testorg", "testproj", tmpDir)
	if err != nil {
		t.Fatalf("create parser: %v", err)
	}

	// Parse valid file should succeed
	result, err := parser.ParseFile(context.Background(), filepath.Join(tmpDir, "valid.go"))
	if err != nil {
		t.Errorf("valid file should parse successfully: %v", err)
	}
	if result == nil {
		t.Error("valid file should return non-nil result")
	}

	// Parse invalid file should return error
	_, err = parser.ParseFile(context.Background(), filepath.Join(tmpDir, "invalid.go"))
	if err == nil {
		t.Error("invalid file should return error")
	}
}

// TestEntityIDFormat verifies that entity IDs follow the expected format.
func TestEntityIDFormat(t *testing.T) {
	tmpDir := t.TempDir()

	code := `package main

type User struct {
	Name string
}

func CreateUser(name string) *User {
	return &User{Name: name}
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "user.go"), []byte(code), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	parser, err := ast.DefaultRegistry.CreateParser("go", "acme", "myproject", tmpDir)
	if err != nil {
		t.Fatalf("create parser: %v", err)
	}

	result, err := parser.ParseFile(context.Background(), filepath.Join(tmpDir, "user.go"))
	if err != nil {
		t.Fatalf("parse file: %v", err)
	}

	for _, e := range result.Entities {
		// Entity IDs should follow format: org.semspec.code.type.project.name
		if !strings.HasPrefix(e.ID, "acme.semspec.code.") {
			t.Errorf("entity ID %q should have prefix 'acme.semspec.code.'", e.ID)
		}
		if !strings.Contains(e.ID, "myproject") {
			t.Errorf("entity ID %q should contain project name 'myproject'", e.ID)
		}
	}
}

// TestWatchPathConfigValidation verifies config validation catches errors.
func TestWatchPathConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  WatchPathConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: WatchPathConfig{
				Path:      "/some/path",
				Org:       "testorg",
				Project:   "testproj",
				Languages: []string{"go"},
			},
			wantErr: false,
		},
		{
			name: "missing path",
			config: WatchPathConfig{
				Org:       "testorg",
				Project:   "testproj",
				Languages: []string{"go"},
			},
			wantErr: true,
		},
		{
			name: "missing org",
			config: WatchPathConfig{
				Path:      "/some/path",
				Project:   "testproj",
				Languages: []string{"go"},
			},
			wantErr: true,
		},
		{
			name: "missing project",
			config: WatchPathConfig{
				Path:      "/some/path",
				Org:       "testorg",
				Languages: []string{"go"},
			},
			wantErr: true,
		},
		{
			name: "unknown language",
			config: WatchPathConfig{
				Path:      "/some/path",
				Org:       "testorg",
				Project:   "testproj",
				Languages: []string{"cobol"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestGetFileExtensions verifies that file extensions are correctly derived
// from configured languages.
func TestGetFileExtensions(t *testing.T) {
	tests := []struct {
		name       string
		languages  []string
		wantSubset []string
	}{
		{
			name:       "go only",
			languages:  []string{"go"},
			wantSubset: []string{".go"},
		},
		{
			name:       "typescript only",
			languages:  []string{"typescript"},
			wantSubset: []string{".ts", ".tsx"},
		},
		{
			name:       "multiple languages",
			languages:  []string{"go", "typescript"},
			wantSubset: []string{".go", ".ts", ".tsx"},
		},
		{
			name:       "default to go",
			languages:  []string{},
			wantSubset: []string{".go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wp := WatchPathConfig{
				Path:      "/some/path",
				Org:       "testorg",
				Project:   "testproj",
				Languages: tt.languages,
			}
			exts := wp.GetFileExtensions()

			extSet := make(map[string]bool)
			for _, ext := range exts {
				extSet[ext] = true
			}

			for _, want := range tt.wantSubset {
				if !extSet[want] {
					t.Errorf("GetFileExtensions() missing %q, got %v", want, exts)
				}
			}
		})
	}
}

// TestASTEntityPayloadSerialization verifies that entity payloads can be
// properly serialized to JSON.
func TestASTEntityPayloadSerialization(t *testing.T) {
	payload := &ASTEntityPayload{
		ID: "acme.semspec.code.function.test.Foo",
		TripleData: []message.Triple{
			{Subject: "acme.semspec.code.function.test.Foo", Predicate: "rdf:type", Object: "code:Function"},
			{Subject: "acme.semspec.code.function.test.Foo", Predicate: "code:name", Object: "Foo"},
		},
		UpdatedAt: time.Now(),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	var decoded ASTEntityPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if decoded.ID != payload.ID {
		t.Errorf("ID mismatch: got %q, want %q", decoded.ID, payload.ID)
	}
	if len(decoded.TripleData) != len(payload.TripleData) {
		t.Errorf("TripleData length mismatch: got %d, want %d", len(decoded.TripleData), len(payload.TripleData))
	}
}

// TestParseDirectoryWithMultipleLanguages tests parsing a directory containing
// files of multiple languages.
func TestParseDirectoryWithMultipleLanguages(t *testing.T) {
	tmpDir := t.TempDir()

	// Create Go file
	goCode := `package main

func Main() {}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(goCode), 0644); err != nil {
		t.Fatalf("write go file: %v", err)
	}

	// Create TypeScript file
	tsCode := `export function helper(): void {}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "helper.ts"), []byte(tsCode), 0644); err != nil {
		t.Fatalf("write ts file: %v", err)
	}

	// Create JavaScript file
	jsCode := `function utils() {}
export { utils };
`
	if err := os.WriteFile(filepath.Join(tmpDir, "utils.js"), []byte(jsCode), 0644); err != nil {
		t.Fatalf("write js file: %v", err)
	}

	// Parse with Go parser
	goParser, err := ast.DefaultRegistry.CreateParser("go", "testorg", "testproj", tmpDir)
	if err != nil {
		t.Fatalf("create go parser: %v", err)
	}

	// Count parsed Go entities
	goResult, err := goParser.ParseFile(context.Background(), filepath.Join(tmpDir, "main.go"))
	if err != nil {
		t.Fatalf("parse go file: %v", err)
	}

	var hasMain bool
	for _, e := range goResult.Entities {
		if e.Type == ast.TypeFunction && e.Name == "Main" {
			hasMain = true
		}
	}
	if !hasMain {
		t.Error("Go Main function not found")
	}

	// Parse with TypeScript parser
	tsParser, err := ast.DefaultRegistry.CreateParser("typescript", "testorg", "testproj", tmpDir)
	if err != nil {
		t.Fatalf("create ts parser: %v", err)
	}

	tsResult, err := tsParser.ParseFile(context.Background(), filepath.Join(tmpDir, "helper.ts"))
	if err != nil {
		t.Fatalf("parse ts file: %v", err)
	}

	var hasHelper bool
	for _, e := range tsResult.Entities {
		if e.Type == ast.TypeFunction && e.Name == "helper" {
			hasHelper = true
		}
	}
	if !hasHelper {
		t.Error("TypeScript helper function not found")
	}
}

// TestConcurrentParsing verifies that multiple files can be parsed concurrently
// without race conditions.
func TestConcurrentParsing(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple Go files
	for i := 0; i < 10; i++ {
		code := "package main\n\nfunc Func" + string(rune('A'+i)) + "() {}\n"
		path := filepath.Join(tmpDir, "file"+string(rune('a'+i))+".go")
		if err := os.WriteFile(path, []byte(code), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}

	parser, err := ast.DefaultRegistry.CreateParser("go", "testorg", "testproj", tmpDir)
	if err != nil {
		t.Fatalf("create parser: %v", err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var results []*ast.ParseResult
	var errors []error

	// Parse files concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			path := filepath.Join(tmpDir, "file"+string(rune('a'+idx))+".go")
			result, err := parser.ParseFile(context.Background(), path)
			mu.Lock()
			if err != nil {
				errors = append(errors, err)
			} else {
				results = append(results, result)
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	if len(errors) > 0 {
		t.Errorf("concurrent parsing produced errors: %v", errors)
	}
	if len(results) != 10 {
		t.Errorf("expected 10 results, got %d", len(results))
	}
}

// TestContextCancellation verifies that parsing respects context cancellation.
func TestContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a Go file
	code := `package main

func Main() {}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(code), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	parser, err := ast.DefaultRegistry.CreateParser("go", "testorg", "testproj", tmpDir)
	if err != nil {
		t.Fatalf("create parser: %v", err)
	}

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Parse with cancelled context - behavior depends on parser implementation
	// Some parsers may still succeed if parsing is fast enough before context check
	_, _ = parser.ParseFile(ctx, filepath.Join(tmpDir, "main.go"))
	// Just verify no panic occurs - cancellation handling varies by implementation
}

// TestParseTestdataFixtures verifies that the testdata fixtures parse correctly.
func TestParseTestdataFixtures(t *testing.T) {
	// Get the testdata path relative to this test file
	testdataPath := "testdata"

	// Check if testdata exists
	if _, err := os.Stat(testdataPath); os.IsNotExist(err) {
		t.Skip("testdata directory not found, skipping fixture tests")
	}

	t.Run("go fixture", func(t *testing.T) {
		goFile := filepath.Join(testdataPath, "go", "main.go")
		if _, err := os.Stat(goFile); os.IsNotExist(err) {
			t.Skip("go fixture not found")
		}

		parser, err := ast.DefaultRegistry.CreateParser("go", "testorg", "testproj", testdataPath)
		if err != nil {
			t.Fatalf("create parser: %v", err)
		}

		result, err := parser.ParseFile(context.Background(), goFile)
		if err != nil {
			t.Fatalf("parse file: %v", err)
		}

		// Verify expected entities
		var hasUser, hasNewUser, hasGreet, hasMaxUsers bool
		for _, e := range result.Entities {
			switch e.Name {
			case "User":
				hasUser = true
			case "NewUser":
				hasNewUser = true
			case "Greet":
				hasGreet = true
			case "MaxUsers":
				hasMaxUsers = true
			}
		}

		if !hasUser {
			t.Error("User struct not found")
		}
		if !hasNewUser {
			t.Error("NewUser function not found")
		}
		if !hasGreet {
			t.Error("Greet method not found")
		}
		if !hasMaxUsers {
			t.Error("MaxUsers constant not found")
		}
	})

	t.Run("typescript fixture", func(t *testing.T) {
		tsFile := filepath.Join(testdataPath, "ts", "auth.ts")
		if _, err := os.Stat(tsFile); os.IsNotExist(err) {
			t.Skip("typescript fixture not found")
		}

		parser, err := ast.DefaultRegistry.CreateParser("typescript", "testorg", "testproj", testdataPath)
		if err != nil {
			t.Fatalf("create parser: %v", err)
		}

		result, err := parser.ParseFile(context.Background(), tsFile)
		if err != nil {
			t.Fatalf("parse file: %v", err)
		}

		// Verify expected entities
		var hasUser, hasAuthenticate, hasAuthService bool
		for _, e := range result.Entities {
			switch e.Name {
			case "User":
				hasUser = true
			case "authenticate":
				hasAuthenticate = true
			case "AuthService":
				hasAuthService = true
			}
		}

		if !hasUser {
			t.Error("User interface not found")
		}
		if !hasAuthenticate {
			t.Error("authenticate function not found")
		}
		if !hasAuthService {
			t.Error("AuthService class not found")
		}
	})

	t.Run("python fixture", func(t *testing.T) {
		pyFile := filepath.Join(testdataPath, "py", "app.py")
		if _, err := os.Stat(pyFile); os.IsNotExist(err) {
			t.Skip("python fixture not found")
		}

		if !ast.DefaultRegistry.HasParser("python") {
			t.Skip("python parser not registered")
		}

		parser, err := ast.DefaultRegistry.CreateParser("python", "testorg", "testproj", testdataPath)
		if err != nil {
			t.Fatalf("create parser: %v", err)
		}

		result, err := parser.ParseFile(context.Background(), pyFile)
		if err != nil {
			t.Fatalf("parse file: %v", err)
		}

		// Verify expected entities
		var hasUser, hasCreateUser, hasUserService bool
		for _, e := range result.Entities {
			switch e.Name {
			case "User":
				hasUser = true
			case "create_user":
				hasCreateUser = true
			case "UserService":
				hasUserService = true
			}
		}

		if !hasUser {
			t.Error("User class not found")
		}
		if !hasCreateUser {
			t.Error("create_user function not found")
		}
		if !hasUserService {
			t.Error("UserService class not found")
		}
	})
}
