package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixturesDir = "../test/e2e/fixtures"

// writeFixtureFile writes content to path under root, creating parent directories.
func writeFixtureFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", rel, err)
	}
}

// TestDetect_GoProject tests detection against the existing go-project fixture.
func TestDetect_GoProject(t *testing.T) {
	// The go-project fixture has: go.mod, main.go, README.md
	repoRoot := filepath.Join(fixturesDir, "go-project")

	detector := NewFileSystemDetector()
	result, err := detector.Detect(repoRoot)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	t.Run("detects Go language", func(t *testing.T) {
		if !hasLanguage(result, "Go") {
			t.Error("expected Go language detection")
		}
	})

	t.Run("Go is primary language", func(t *testing.T) {
		for _, l := range result.Languages {
			if l.Name == "Go" {
				if !l.Primary {
					t.Error("expected Go to be primary language")
				}
				return
			}
		}
		t.Error("Go language not found")
	})

	t.Run("Go version extracted from go.mod", func(t *testing.T) {
		for _, l := range result.Languages {
			if l.Name == "Go" {
				if l.Version == nil {
					t.Error("expected Go version to be extracted from go.mod")
					return
				}
				if *l.Version == "" {
					t.Error("expected non-empty Go version")
				}
				return
			}
		}
	})

	t.Run("Go marker is go.mod", func(t *testing.T) {
		for _, l := range result.Languages {
			if l.Name == "Go" && l.Marker != "go.mod" {
				t.Errorf("Go marker = %q, want %q", l.Marker, "go.mod")
			}
		}
	})

	t.Run("Go confidence is high", func(t *testing.T) {
		for _, l := range result.Languages {
			if l.Name == "Go" && l.Confidence != ConfidenceHigh {
				t.Errorf("Go confidence = %q, want %q", l.Confidence, ConfidenceHigh)
			}
		}
	})

	t.Run("proposed checklist includes go-build, go-vet, go-test", func(t *testing.T) {
		wantChecks := []string{"go-build", "go-vet", "go-test"}
		for _, want := range wantChecks {
			found := false
			for _, c := range result.ProposedChecklist {
				if c.Name == want {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected check %q in proposed checklist", want)
			}
		}
	})

	t.Run("detects README.md", func(t *testing.T) {
		found := false
		for _, d := range result.ExistingDocs {
			if d.Path == "README.md" && d.Type == DocTypeProjectDocs {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected README.md in existing docs")
		}
	})
}

// TestDetect_SvelteProject tests detection against the existing svelte-project fixture.
func TestDetect_SvelteProject(t *testing.T) {
	// The svelte-project fixture has: package.json with svelte and @sveltejs/kit deps.
	repoRoot := filepath.Join(fixturesDir, "svelte-project")

	// Add tsconfig.json so TypeScript is detected with high confidence.
	tmp := t.TempDir()
	copyDir(t, repoRoot, tmp)
	writeFixtureFile(t, tmp, "tsconfig.json", `{"compilerOptions": {"strict": true}}`)

	detector := NewFileSystemDetector()
	result, err := detector.Detect(tmp)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	t.Run("detects TypeScript language", func(t *testing.T) {
		if !hasLanguage(result, "TypeScript") {
			t.Errorf("expected TypeScript, got languages: %v", languageNames(result))
		}
	})

	t.Run("detects SvelteKit framework", func(t *testing.T) {
		if !hasFramework(result, "SvelteKit") {
			t.Errorf("expected SvelteKit framework, got: %v", frameworkNames(result))
		}
	})

	t.Run("SvelteKit is high confidence", func(t *testing.T) {
		for _, f := range result.Frameworks {
			if f.Name == "SvelteKit" && f.Confidence != ConfidenceHigh {
				t.Errorf("SvelteKit confidence = %q, want high", f.Confidence)
			}
		}
	})

	t.Run("proposed checklist includes svelte-check", func(t *testing.T) {
		found := false
		for _, c := range result.ProposedChecklist {
			if c.Name == "svelte-check" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected svelte-check in proposed checklist")
		}
	})
}

// TestDetect_TypeScriptProject tests detection against the ts-project fixture.
func TestDetect_TypeScriptProject(t *testing.T) {
	// The ts-project fixture has: package.json, tsconfig.json, README.md
	repoRoot := filepath.Join(fixturesDir, "ts-project")

	detector := NewFileSystemDetector()
	result, err := detector.Detect(repoRoot)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	t.Run("detects TypeScript language", func(t *testing.T) {
		if !hasLanguage(result, "TypeScript") {
			t.Errorf("expected TypeScript, got: %v", languageNames(result))
		}
	})

	t.Run("TypeScript marker is tsconfig.json", func(t *testing.T) {
		for _, l := range result.Languages {
			if l.Name == "TypeScript" && l.Marker != "tsconfig.json" {
				t.Errorf("TypeScript marker = %q, want tsconfig.json", l.Marker)
			}
		}
	})

	t.Run("TypeScript type checker detected", func(t *testing.T) {
		if !hasTool(result, "TypeScript") {
			t.Error("expected TypeScript type checker in tooling")
		}
	})

	t.Run("proposed checklist includes tsc", func(t *testing.T) {
		found := false
		for _, c := range result.ProposedChecklist {
			if c.Name == "tsc" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected tsc in proposed checklist")
		}
	})
}

// TestDetect_JavaScriptProject tests detection of a plain JS project (no tsconfig).
func TestDetect_JavaScriptProject(t *testing.T) {
	// The js-project fixture has: package.json without tsconfig or typescript dep.
	repoRoot := filepath.Join(fixturesDir, "js-project")

	detector := NewFileSystemDetector()
	result, err := detector.Detect(repoRoot)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	t.Run("detects JavaScript not TypeScript", func(t *testing.T) {
		if hasLanguage(result, "TypeScript") {
			t.Error("expected JavaScript, not TypeScript, when no tsconfig.json")
		}
		if !hasLanguage(result, "JavaScript") {
			t.Errorf("expected JavaScript, got: %v", languageNames(result))
		}
	})

	t.Run("JavaScript marker is package.json", func(t *testing.T) {
		for _, l := range result.Languages {
			if l.Name == "JavaScript" && l.Marker != "package.json" {
				t.Errorf("JavaScript marker = %q, want package.json", l.Marker)
			}
		}
	})
}

// TestDetect_PythonProject tests detection of a Python project created in a temp dir.
func TestDetect_PythonProject(t *testing.T) {
	tmp := t.TempDir()
	writeFixtureFile(t, tmp, "pyproject.toml", `
[build-system]
requires = ["setuptools"]

[project]
name = "myapp"
requires-python = ">=3.11"

[tool.ruff]
line-length = 88
`)
	writeFixtureFile(t, tmp, "src/main.py", `print("hello")`)
	writeFixtureFile(t, tmp, "conftest.py", `# pytest conftest`)

	detector := NewFileSystemDetector()
	result, err := detector.Detect(tmp)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	t.Run("detects Python language", func(t *testing.T) {
		if !hasLanguage(result, "Python") {
			t.Errorf("expected Python, got: %v", languageNames(result))
		}
	})

	t.Run("Python marker is pyproject.toml", func(t *testing.T) {
		for _, l := range result.Languages {
			if l.Name == "Python" && l.Marker != "pyproject.toml" {
				t.Errorf("Python marker = %q, want pyproject.toml", l.Marker)
			}
		}
	})

	t.Run("Python version extracted from requires-python", func(t *testing.T) {
		for _, l := range result.Languages {
			if l.Name == "Python" {
				if l.Version == nil {
					t.Error("expected Python version")
					return
				}
				if !strings.Contains(*l.Version, "3.11") {
					t.Errorf("Python version = %q, want to contain 3.11", *l.Version)
				}
				return
			}
		}
	})

	t.Run("detects Pytest tooling", func(t *testing.T) {
		if !hasTool(result, "Pytest") {
			t.Error("expected Pytest in tooling from conftest.py")
		}
	})

	t.Run("detects Ruff from pyproject.toml tool section", func(t *testing.T) {
		if !hasTool(result, "Ruff") {
			t.Error("expected Ruff in tooling from [tool.ruff] in pyproject.toml")
		}
	})

	t.Run("proposed checklist includes pytest", func(t *testing.T) {
		found := false
		for _, c := range result.ProposedChecklist {
			if c.Name == "pytest" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected pytest in proposed checklist")
		}
	})

	t.Run("proposed checklist includes ruff", func(t *testing.T) {
		found := false
		for _, c := range result.ProposedChecklist {
			if c.Name == "ruff" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected ruff in proposed checklist")
		}
	})
}

// TestDetect_JavaProject tests detection of a Maven Java project.
func TestDetect_JavaProject(t *testing.T) {
	tmp := t.TempDir()
	writeFixtureFile(t, tmp, "pom.xml", `<?xml version="1.0"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
</project>`)
	writeFixtureFile(t, tmp, "src/main/java/App.java", `public class App {}`)

	detector := NewFileSystemDetector()
	result, err := detector.Detect(tmp)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	t.Run("detects Java language", func(t *testing.T) {
		if !hasLanguage(result, "Java") {
			t.Errorf("expected Java, got: %v", languageNames(result))
		}
	})

	t.Run("Java marker is pom.xml", func(t *testing.T) {
		for _, l := range result.Languages {
			if l.Name == "Java" && l.Marker != "pom.xml" {
				t.Errorf("Java marker = %q, want pom.xml", l.Marker)
			}
		}
	})

	t.Run("proposed checklist includes mvn-test", func(t *testing.T) {
		found := false
		for _, c := range result.ProposedChecklist {
			if c.Name == "mvn-test" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected mvn-test in proposed checklist")
		}
	})
}

// TestDetect_RustProject tests detection of a Rust project.
func TestDetect_RustProject(t *testing.T) {
	tmp := t.TempDir()
	writeFixtureFile(t, tmp, "Cargo.toml", `[package]
name = "myapp"
version = "0.1.0"
edition = "2021"`)
	writeFixtureFile(t, tmp, "src/main.rs", `fn main() {}`)

	detector := NewFileSystemDetector()
	result, err := detector.Detect(tmp)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	t.Run("detects Rust language", func(t *testing.T) {
		if !hasLanguage(result, "Rust") {
			t.Errorf("expected Rust, got: %v", languageNames(result))
		}
	})

	t.Run("proposed checklist includes cargo-build and cargo-test", func(t *testing.T) {
		for _, name := range []string{"cargo-build", "cargo-test"} {
			found := false
			for _, c := range result.ProposedChecklist {
				if c.Name == name {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected check %q in proposed checklist", name)
			}
		}
	})
}

// TestDetect_Tooling tests individual tool detection with dedicated temp directories.
func TestDetect_Tooling(t *testing.T) {
	detector := NewFileSystemDetector()

	tests := []struct {
		name     string
		files    map[string]string
		wantTool string
		wantCat  ToolCategory
	}{
		{
			name:     "golangci-lint from .golangci.yml",
			files:    map[string]string{"go.mod": "module x\ngo 1.21\n", ".golangci.yml": "linters:\n  enable:\n    - govet\n"},
			wantTool: "golangci-lint",
			wantCat:  ToolCategoryLinter,
		},
		{
			name:     "Revive from revive.toml",
			files:    map[string]string{"go.mod": "module x\ngo 1.21\n", "revive.toml": "[rule.exported]\n"},
			wantTool: "Revive",
			wantCat:  ToolCategoryLinter,
		},
		{
			name:     "ESLint from eslint.config.js",
			files:    map[string]string{"package.json": `{}`, "eslint.config.js": "export default [];"},
			wantTool: "ESLint",
			wantCat:  ToolCategoryLinter,
		},
		{
			name:     "ESLint from .eslintrc.json",
			files:    map[string]string{"package.json": `{}`, ".eslintrc.json": `{}`},
			wantTool: "ESLint",
			wantCat:  ToolCategoryLinter,
		},
		{
			name:     "Prettier from .prettierrc",
			files:    map[string]string{"package.json": `{}`, ".prettierrc": `{}`},
			wantTool: "Prettier",
			wantCat:  ToolCategoryFormatter,
		},
		{
			name:     "Biome from biome.json (suppresses eslint)",
			files:    map[string]string{"package.json": `{}`, "biome.json": `{}`, "eslint.config.js": ""},
			wantTool: "Biome",
			wantCat:  ToolCategoryLinter,
		},
		{
			name:     "Taskfile from Taskfile.yml",
			files:    map[string]string{"Taskfile.yml": "version: '3'\ntasks:\n  test:\n    cmd: go test ./...\n"},
			wantTool: "Taskfile",
			wantCat:  ToolCategoryTaskRunner,
		},
		{
			name:     "Make from Makefile",
			files:    map[string]string{"Makefile": "test:\n\tgo test ./...\n"},
			wantTool: "Make",
			wantCat:  ToolCategoryTaskRunner,
		},
		{
			name:     "GitHub Actions from .github/workflows/",
			files:    map[string]string{".github/workflows/ci.yml": "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n"},
			wantTool: "GitHub Actions",
			wantCat:  ToolCategoryCI,
		},
		{
			name:     "Docker Compose from docker-compose.yml",
			files:    map[string]string{"docker-compose.yml": "services:\n  app:\n    image: alpine\n"},
			wantTool: "Docker Compose",
			wantCat:  ToolCategoryContainer,
		},
		{
			name:     "Vitest from vitest.config.ts",
			files:    map[string]string{"package.json": `{}`, "vitest.config.ts": ""},
			wantTool: "Vitest",
			wantCat:  ToolCategoryTestFramework,
		},
		{
			name:     "Jest from jest.config.js",
			files:    map[string]string{"package.json": `{}`, "jest.config.js": ""},
			wantTool: "Jest",
			wantCat:  ToolCategoryTestFramework,
		},
		{
			name:     "Ruff from ruff.toml",
			files:    map[string]string{"ruff.toml": "[lint]\n"},
			wantTool: "Ruff",
			wantCat:  ToolCategoryLinter,
		},
		{
			name: "Ruff from pyproject.toml tool section",
			files: map[string]string{
				"pyproject.toml": "[project]\nname = \"x\"\n\n[tool.ruff]\nline-length = 88\n",
			},
			wantTool: "Ruff",
			wantCat:  ToolCategoryLinter,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			for rel, content := range tt.files {
				writeFixtureFile(t, tmp, rel, content)
			}

			result, err := detector.Detect(tmp)
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}

			found := false
			for _, tool := range result.Tooling {
				if tool.Name == tt.wantTool {
					found = true
					if tool.Category != tt.wantCat {
						t.Errorf("tool %q category = %q, want %q", tt.wantTool, tool.Category, tt.wantCat)
					}
					break
				}
			}
			if !found {
				t.Errorf("expected tool %q, got: %v", tt.wantTool, toolNames(result))
			}
		})
	}
}

// TestDetect_BiomeSuppressesESLintAndPrettier verifies that Biome detection
// prevents ESLint and Prettier from also being proposed.
func TestDetect_BiomeSuppressesESLintAndPrettier(t *testing.T) {
	tmp := t.TempDir()
	writeFixtureFile(t, tmp, "package.json", `{}`)
	writeFixtureFile(t, tmp, "biome.json", `{}`)
	writeFixtureFile(t, tmp, "eslint.config.js", "")
	writeFixtureFile(t, tmp, ".prettierrc", `{}`)

	detector := NewFileSystemDetector()
	result, err := detector.Detect(tmp)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	if !hasTool(result, "Biome") {
		t.Error("expected Biome")
	}
	if hasTool(result, "ESLint") {
		t.Error("Biome is present: ESLint should not be detected")
	}
	if hasTool(result, "Prettier") {
		t.Error("Biome is present: Prettier should not be detected")
	}
}

// TestDetect_TaskfileIntegration verifies that a Taskfile with well-known targets
// produces task-* checks in the proposed checklist.
func TestDetect_TaskfileIntegration(t *testing.T) {
	tmp := t.TempDir()
	writeFixtureFile(t, tmp, "go.mod", "module example.com/app\ngo 1.21\n")
	writeFixtureFile(t, tmp, "Taskfile.yml", `version: '3'
tasks:
  test:
    cmds:
      - go test ./...
  lint:
    cmds:
      - golangci-lint run ./...
  build:
    cmds:
      - go build ./...
`)

	detector := NewFileSystemDetector()
	result, err := detector.Detect(tmp)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	t.Run("detects Taskfile", func(t *testing.T) {
		if !hasTool(result, "Taskfile") {
			t.Error("expected Taskfile tool")
		}
	})

	// When Taskfile has test/lint/build targets, those should appear in the checklist.
	wantChecks := []string{"task-test", "task-lint", "task-build"}
	for _, want := range wantChecks {
		found := false
		for _, c := range result.ProposedChecklist {
			if c.Name == want {
				found = true
				if !strings.HasPrefix(c.Command, "task ") {
					t.Errorf("task check %q command = %q, want 'task <target>'", want, c.Command)
				}
				break
			}
		}
		if !found {
			t.Errorf("expected check %q in proposed checklist, got: %v", want, checkNames(result))
		}
	}
}

// TestDetect_MakefileIntegration verifies that a Makefile with well-known targets
// produces make-* checks in the proposed checklist.
func TestDetect_MakefileIntegration(t *testing.T) {
	tmp := t.TempDir()
	writeFixtureFile(t, tmp, "go.mod", "module example.com/app\ngo 1.21\n")
	writeFixtureFile(t, tmp, "Makefile", `
.PHONY: test lint build

test:
	go test ./...

lint:
	go vet ./...

build:
	go build ./...
`)

	detector := NewFileSystemDetector()
	result, err := detector.Detect(tmp)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	wantChecks := []string{"make-test", "make-lint", "make-build"}
	for _, want := range wantChecks {
		found := false
		for _, c := range result.ProposedChecklist {
			if c.Name == want {
				found = true
				if !strings.HasPrefix(c.Command, "make ") {
					t.Errorf("make check %q command = %q, want 'make <target>'", want, c.Command)
				}
				break
			}
		}
		if !found {
			t.Errorf("expected check %q in proposed checklist, got: %v", want, checkNames(result))
		}
	}
}

// TestDetect_FrameworkDetection covers individual framework signals in package.json.
func TestDetect_FrameworkDetection(t *testing.T) {
	detector := NewFileSystemDetector()

	tests := []struct {
		name          string
		packageJSON   string
		extraFiles    map[string]string
		wantFramework string
	}{
		{
			name:          "SvelteKit from @sveltejs/kit dep",
			packageJSON:   `{"devDependencies": {"@sveltejs/kit": "^2.0", "svelte": "^5.0", "typescript": "^5.0"}}`,
			extraFiles:    map[string]string{"tsconfig.json": "{}"},
			wantFramework: "SvelteKit",
		},
		{
			name:          "React from react dep",
			packageJSON:   `{"dependencies": {"react": "^18.0", "react-dom": "^18.0"}}`,
			wantFramework: "React",
		},
		{
			name:          "Next.js from next dep",
			packageJSON:   `{"dependencies": {"next": "^14.0", "react": "^18.0"}}`,
			wantFramework: "Next.js",
		},
		{
			name:          "Vue from vue dep",
			packageJSON:   `{"dependencies": {"vue": "^3.0"}}`,
			wantFramework: "Vue",
		},
		{
			name:          "Express from express dep",
			packageJSON:   `{"dependencies": {"express": "^4.0"}}`,
			wantFramework: "Express",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			writeFixtureFile(t, tmp, "package.json", tt.packageJSON)
			for rel, content := range tt.extraFiles {
				writeFixtureFile(t, tmp, rel, content)
			}

			result, err := detector.Detect(tmp)
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}

			if !hasFramework(result, tt.wantFramework) {
				t.Errorf("expected framework %q, got: %v", tt.wantFramework, frameworkNames(result))
			}
		})
	}
}

// TestDetect_DocDetection verifies existing documentation files are found.
func TestDetect_DocDetection(t *testing.T) {
	tmp := t.TempDir()
	writeFixtureFile(t, tmp, "README.md", "# My Project\n")
	writeFixtureFile(t, tmp, "CONTRIBUTING.md", "# Contributing\n")
	writeFixtureFile(t, tmp, "CLAUDE.md", "## Project Rules\n")
	writeFixtureFile(t, tmp, "docs/architecture.md", "# Architecture\n")
	writeFixtureFile(t, tmp, ".semspec/sources/docs/go-conventions.md", "# Go conventions\n")

	detector := NewFileSystemDetector()
	result, err := detector.Detect(tmp)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	type wantDoc struct {
		path    string
		docType DocType
	}
	wants := []wantDoc{
		{"README.md", DocTypeProjectDocs},
		{"CONTRIBUTING.md", DocTypeContributing},
		{"CLAUDE.md", DocTypeClaudeInstructions},
		{"docs/architecture.md", DocTypeArchitectureDocs},
		{filepath.Join(".semspec", "sources", "docs", "go-conventions.md"), DocTypeExistingSOP},
	}

	for _, w := range wants {
		found := false
		for _, d := range result.ExistingDocs {
			if d.Path == w.path && d.Type == w.docType {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected doc {path=%q, type=%q} not found in %v", w.path, w.docType, result.ExistingDocs)
		}
	}
}

// TestDetect_MultiLanguageProject tests a repo with both Go and TypeScript.
func TestDetect_MultiLanguageProject(t *testing.T) {
	tmp := t.TempDir()
	writeFixtureFile(t, tmp, "go.mod", "module example.com/app\ngo 1.22\n")
	writeFixtureFile(t, tmp, "main.go", "package main\nfunc main() {}")
	writeFixtureFile(t, tmp, "ui/package.json", `{"devDependencies": {"typescript": "^5.0"}}`)
	writeFixtureFile(t, tmp, "ui/tsconfig.json", `{}`)
	// tsconfig.json at root level for detection to find.
	writeFixtureFile(t, tmp, "tsconfig.json", `{}`)

	detector := NewFileSystemDetector()
	result, err := detector.Detect(tmp)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	t.Run("detects both Go and TypeScript", func(t *testing.T) {
		if !hasLanguage(result, "Go") {
			t.Error("expected Go")
		}
		if !hasLanguage(result, "TypeScript") {
			t.Error("expected TypeScript")
		}
	})

	t.Run("Go is primary (first detected)", func(t *testing.T) {
		if len(result.Languages) == 0 {
			t.Fatal("no languages detected")
		}
		if result.Languages[0].Name != "Go" {
			t.Errorf("first language = %q, want Go", result.Languages[0].Name)
		}
		if !result.Languages[0].Primary {
			t.Error("expected first language to be primary")
		}
	})

	t.Run("second language is not primary", func(t *testing.T) {
		for i := 1; i < len(result.Languages); i++ {
			if result.Languages[i].Primary {
				t.Errorf("language[%d] = %q should not be primary", i, result.Languages[i].Name)
			}
		}
	})
}

// TestDetect_EmptyDirectory verifies graceful handling of an empty repository.
func TestDetect_EmptyDirectory(t *testing.T) {
	tmp := t.TempDir()

	detector := NewFileSystemDetector()
	result, err := detector.Detect(tmp)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	if len(result.Languages) != 0 {
		t.Errorf("expected no languages, got: %v", languageNames(result))
	}
	if len(result.Frameworks) != 0 {
		t.Errorf("expected no frameworks, got: %v", frameworkNames(result))
	}
	if len(result.ProposedChecklist) != 0 {
		t.Errorf("expected empty checklist, got: %v", checkNames(result))
	}
}

// TestDetect_MalformedPackageJSON verifies graceful handling of malformed JSON.
func TestDetect_MalformedPackageJSON(t *testing.T) {
	tmp := t.TempDir()
	writeFixtureFile(t, tmp, "package.json", `{ this is not valid json `)

	detector := NewFileSystemDetector()
	result, err := detector.Detect(tmp)
	// Must not return an error — malformed files are gracefully skipped.
	if err != nil {
		t.Fatalf("Detect() must not fail on malformed package.json, got: %v", err)
	}
	// Should not crash but may detect JS with medium confidence.
	_ = result
}

// TestDetect_GolangciYamlVariant tests that .golangci.yaml (alternate extension) is detected.
func TestDetect_GolangciYamlVariant(t *testing.T) {
	tmp := t.TempDir()
	writeFixtureFile(t, tmp, "go.mod", "module example.com/app\ngo 1.21\n")
	writeFixtureFile(t, tmp, ".golangci.yaml", "linters:\n  enable: [govet]\n")

	detector := NewFileSystemDetector()
	result, err := detector.Detect(tmp)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	if !hasTool(result, "golangci-lint") {
		t.Error("expected golangci-lint detected from .golangci.yaml")
	}
}

// TestDetect_GolangciLintInChecklist verifies golangci-lint is added as a non-required check.
func TestDetect_GolangciLintInChecklist(t *testing.T) {
	tmp := t.TempDir()
	writeFixtureFile(t, tmp, "go.mod", "module example.com/app\ngo 1.21\n")
	writeFixtureFile(t, tmp, ".golangci.yml", "linters:\n  enable: [govet]\n")

	detector := NewFileSystemDetector()
	result, err := detector.Detect(tmp)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	for _, c := range result.ProposedChecklist {
		if c.Name == "golangci-lint" {
			if c.Required {
				t.Error("golangci-lint should be non-required (optional)")
			}
			return
		}
	}
	t.Error("expected golangci-lint in proposed checklist")
}

// TestDetect_RequiredVsOptionalChecks verifies that required flags are set correctly.
func TestDetect_RequiredVsOptionalChecks(t *testing.T) {
	tmp := t.TempDir()
	writeFixtureFile(t, tmp, "go.mod", "module example.com/app\ngo 1.21\n")
	writeFixtureFile(t, tmp, "revive.toml", "")

	detector := NewFileSystemDetector()
	result, err := detector.Detect(tmp)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	required := map[string]bool{}
	for _, c := range result.ProposedChecklist {
		required[c.Name] = c.Required
	}

	tests := []struct {
		check    string
		required bool
	}{
		{"go-build", true},
		{"go-vet", true},
		{"go-test", true},
		{"revive", false},
	}

	for _, tt := range tests {
		got, ok := required[tt.check]
		if !ok {
			t.Errorf("check %q not found in proposed checklist", tt.check)
			continue
		}
		if got != tt.required {
			t.Errorf("check %q required = %v, want %v", tt.check, got, tt.required)
		}
	}
}

// TestDetect_GoVersionExtraction tests version parsing from various go.mod formats.
func TestDetect_GoVersionExtraction(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"single line", "module x\n\ngo 1.21\n", "1.21"},
		{"with patch", "module x\n\ngo 1.21.5\n", "1.21.5"},
		{"toolchain directive", "module x\n\ngo 1.25.3\n\ntoolchain go1.25.3\n", "1.25.3"},
		{"leading spaces in go directive", "module x\n\n  go 1.22  \n", "1.22"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			writeFixtureFile(t, tmp, "go.mod", tt.content)

			ver := extractGoVersion(filepath.Join(tmp, "go.mod"))
			if ver == nil {
				t.Fatalf("extractGoVersion() = nil, want %q", tt.want)
			}
			if *ver != tt.want {
				t.Errorf("extractGoVersion() = %q, want %q", *ver, tt.want)
			}
		})
	}
}

// TestDetect_ChecklistTimeouts verifies that all proposed checks have a non-empty timeout.
func TestDetect_ChecklistTimeouts(t *testing.T) {
	tmp := t.TempDir()
	// Set up a multi-language project.
	writeFixtureFile(t, tmp, "go.mod", "module example.com/app\ngo 1.21\n")
	writeFixtureFile(t, tmp, "tsconfig.json", "{}")
	writeFixtureFile(t, tmp, "package.json", `{"devDependencies": {"typescript": "^5.0"}}`)

	detector := NewFileSystemDetector()
	result, err := detector.Detect(tmp)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	for _, c := range result.ProposedChecklist {
		if c.Timeout == "" {
			t.Errorf("check %q has empty timeout", c.Name)
		}
	}
}

// TestDetect_ChecklistTriggerPatterns verifies that all checks have non-empty trigger patterns.
func TestDetect_ChecklistTriggerPatterns(t *testing.T) {
	tmp := t.TempDir()
	writeFixtureFile(t, tmp, "go.mod", "module example.com/app\ngo 1.21\n")

	detector := NewFileSystemDetector()
	result, err := detector.Detect(tmp)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	for _, c := range result.ProposedChecklist {
		if len(c.Trigger) == 0 {
			t.Errorf("check %q has empty trigger patterns", c.Name)
		}
	}
}

// TestDetect_MonorepoSubdirectories verifies that language markers found in
// immediate subdirectories are detected when no root-level markers exist.
func TestDetect_MonorepoSubdirectories(t *testing.T) {
	tmp := t.TempDir()
	// Go API in api/ subdirectory.
	writeFixtureFile(t, tmp, "api/go.mod", "module todo-app\n\ngo 1.22\n")
	writeFixtureFile(t, tmp, "api/main.go", "package main\nfunc main() {}")
	// Svelte UI in ui/ subdirectory.
	writeFixtureFile(t, tmp, "ui/package.json", `{"devDependencies": {"@sveltejs/kit": "^2.0", "svelte": "^5.0", "typescript": "^5.0"}}`)
	writeFixtureFile(t, tmp, "ui/tsconfig.json", `{}`)
	// README at root.
	writeFixtureFile(t, tmp, "README.md", "# My Monorepo\n")

	detector := NewFileSystemDetector()
	result, err := detector.Detect(tmp)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	t.Run("detects Go from api/go.mod", func(t *testing.T) {
		if !hasLanguage(result, "Go") {
			t.Errorf("expected Go detected from api/go.mod, got: %v", languageNames(result))
		}
		for _, l := range result.Languages {
			if l.Name == "Go" {
				if l.Marker != filepath.Join("api", "go.mod") {
					t.Errorf("Go marker = %q, want %q", l.Marker, filepath.Join("api", "go.mod"))
				}
				if l.Confidence != ConfidenceMedium {
					t.Errorf("Go confidence = %q, want medium (subdirectory detection)", l.Confidence)
				}
			}
		}
	})

	t.Run("detects TypeScript from ui/tsconfig.json", func(t *testing.T) {
		if !hasLanguage(result, "TypeScript") {
			t.Errorf("expected TypeScript from ui/tsconfig.json, got: %v", languageNames(result))
		}
		for _, l := range result.Languages {
			if l.Name == "TypeScript" {
				if l.Marker != filepath.Join("ui", "tsconfig.json") {
					t.Errorf("TypeScript marker = %q, want %q", l.Marker, filepath.Join("ui", "tsconfig.json"))
				}
				if l.Confidence != ConfidenceMedium {
					t.Errorf("TypeScript confidence = %q, want medium (subdirectory detection)", l.Confidence)
				}
			}
		}
	})

	t.Run("Go is primary (first detected)", func(t *testing.T) {
		if len(result.Languages) == 0 {
			t.Fatal("no languages detected")
		}
		if result.Languages[0].Name != "Go" {
			t.Errorf("first language = %q, want Go", result.Languages[0].Name)
		}
		if !result.Languages[0].Primary {
			t.Error("expected first language to be primary")
		}
	})

	t.Run("detects SvelteKit framework from ui/package.json", func(t *testing.T) {
		if !hasFramework(result, "SvelteKit") {
			t.Errorf("expected SvelteKit framework from ui/package.json, got: %v", frameworkNames(result))
		}
	})

	t.Run("detects README at root", func(t *testing.T) {
		found := false
		for _, d := range result.ExistingDocs {
			if d.Path == "README.md" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected README.md in existing docs")
		}
	})
}

// TestDetect_RootTakesPriorityOverSubdir verifies that a root-level language marker
// takes priority over the same marker in a subdirectory, and the language is
// detected exactly once with high confidence.
func TestDetect_RootTakesPriorityOverSubdir(t *testing.T) {
	tmp := t.TempDir()
	// Go at root AND in subdirectory.
	writeFixtureFile(t, tmp, "go.mod", "module root-app\n\ngo 1.22\n")
	writeFixtureFile(t, tmp, "api/go.mod", "module api-app\n\ngo 1.21\n")

	detector := NewFileSystemDetector()
	result, err := detector.Detect(tmp)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	// Should detect Go only once (from root), with high confidence.
	goCount := 0
	for _, l := range result.Languages {
		if l.Name == "Go" {
			goCount++
			if l.Marker != "go.mod" {
				t.Errorf("Go marker = %q, want root-level go.mod", l.Marker)
			}
			if l.Confidence != ConfidenceHigh {
				t.Errorf("Go confidence = %q, want high (root detection)", l.Confidence)
			}
		}
	}
	if goCount != 1 {
		t.Errorf("expected exactly 1 Go detection, got %d", goCount)
	}
}

// TestDetect_SubdirSkipsExcludedDirs verifies that excluded directories
// (node_modules, vendor, .git, .semspec, dist, build, __pycache__, .cache)
// are not scanned for language markers.
func TestDetect_SubdirSkipsExcludedDirs(t *testing.T) {
	tmp := t.TempDir()
	// Place go.mod in directories that must be skipped.
	excludedDirs := []string{"node_modules", "vendor", "dist", "build", "__pycache__", ".cache"}
	for _, dir := range excludedDirs {
		writeFixtureFile(t, tmp, dir+"/go.mod", "module fake\n\ngo 1.22\n")
	}

	detector := NewFileSystemDetector()
	result, err := detector.Detect(tmp)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	if hasLanguage(result, "Go") {
		t.Errorf("expected no Go detection from excluded dirs, got: %v", languageNames(result))
	}
}

// TestDetect_SubdirHiddenDirsSkipped verifies that hidden directories
// (those starting with ".") are not scanned.
func TestDetect_SubdirHiddenDirsSkipped(t *testing.T) {
	tmp := t.TempDir()
	// Place go.mod in a hidden directory — should not trigger detection.
	writeFixtureFile(t, tmp, ".hidden/go.mod", "module hidden\n\ngo 1.22\n")

	detector := NewFileSystemDetector()
	result, err := detector.Detect(tmp)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	if hasLanguage(result, "Go") {
		t.Errorf("expected no Go detection from hidden directories, got: %v", languageNames(result))
	}
}

// --- Helper utilities for tests ---------------------------------------------

// copyDir copies all files from src to dst shallowly (one level only).
// Used to extend E2E fixtures without modifying them.
func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", src, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0644); err != nil {
			t.Fatalf("WriteFile(%s): %v", e.Name(), err)
		}
	}
}

func languageNames(r *DetectionResult) []string {
	names := make([]string, len(r.Languages))
	for i, l := range r.Languages {
		names[i] = l.Name
	}
	return names
}

func frameworkNames(r *DetectionResult) []string {
	names := make([]string, len(r.Frameworks))
	for i, f := range r.Frameworks {
		names[i] = f.Name
	}
	return names
}

func toolNames(r *DetectionResult) []string {
	names := make([]string, len(r.Tooling))
	for i, t := range r.Tooling {
		names[i] = t.Name
	}
	return names
}

func checkNames(r *DetectionResult) []string {
	names := make([]string, len(r.ProposedChecklist))
	for i, c := range r.ProposedChecklist {
		names[i] = c.Name
	}
	return names
}
