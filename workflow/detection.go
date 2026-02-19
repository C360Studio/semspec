package workflow

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// StackDetector scans a repository root directory and returns a DetectionResult
// without making any LLM calls. All detection is deterministic file-presence logic.
type StackDetector interface {
	// Detect scans repoRoot and returns the detected stack configuration.
	// Missing or malformed files are handled gracefully — they produce no
	// detection output rather than errors.
	Detect(repoRoot string) (*DetectionResult, error)
}

// FileSystemDetector is the production StackDetector implementation.
// It operates entirely on the local filesystem.
type FileSystemDetector struct{}

// NewFileSystemDetector constructs a FileSystemDetector.
func NewFileSystemDetector() *FileSystemDetector {
	return &FileSystemDetector{}
}

// Detect implements StackDetector.
func (d *FileSystemDetector) Detect(repoRoot string) (*DetectionResult, error) {
	result := &DetectionResult{
		Languages:  []DetectedLanguage{},
		Frameworks: []DetectedFramework{},
		Tooling:    []DetectedTool{},
		ExistingDocs: []DetectedDoc{},
	}

	d.detectLanguages(repoRoot, result)
	d.detectFrameworks(repoRoot, result)
	d.detectTooling(repoRoot, result)
	d.detectDocs(repoRoot, result)
	result.ProposedChecklist = d.buildProposedChecklist(repoRoot, result)

	return result, nil
}

// excludedSubdirs lists directory names to skip when scanning subdirectories for
// language markers. These are either non-source directories (generated output,
// dependency caches) or internal semspec directories.
var excludedSubdirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	".git":         true,
	".semspec":     true,
	"dist":         true,
	"build":        true,
	"__pycache__":  true,
	".cache":       true,
}

// detectLanguages populates result.Languages by checking for primary marker files
// at the repository root, then scanning immediate subdirectories for additional
// language markers not found at the root.
func (d *FileSystemDetector) detectLanguages(repoRoot string, result *DetectionResult) {
	// Go: go.mod is the canonical marker.
	if goMod := filepath.Join(repoRoot, "go.mod"); fileExists(goMod) {
		ver := extractGoVersion(goMod)
		result.Languages = append(result.Languages, DetectedLanguage{
			Name:       "Go",
			Version:    ver,
			Marker:     "go.mod",
			Confidence: ConfidenceHigh,
		})
	}

	// TypeScript: tsconfig.json is the canonical marker.
	if fileExists(filepath.Join(repoRoot, "tsconfig.json")) {
		result.Languages = append(result.Languages, DetectedLanguage{
			Name:       "TypeScript",
			Version:    nil,
			Marker:     "tsconfig.json",
			Confidence: ConfidenceHigh,
		})
	} else if fileExists(filepath.Join(repoRoot, "package.json")) {
		// package.json without tsconfig → Node.js / JavaScript.
		// Check if typescript is listed as a dependency before claiming TypeScript.
		if hasPackageJSONDep(repoRoot, "typescript") {
			result.Languages = append(result.Languages, DetectedLanguage{
				Name:       "TypeScript",
				Version:    nil,
				Marker:     "package.json",
				Confidence: ConfidenceMedium,
			})
		} else {
			result.Languages = append(result.Languages, DetectedLanguage{
				Name:       "JavaScript",
				Version:    extractNodeVersion(repoRoot),
				Marker:     "package.json",
				Confidence: ConfidenceHigh,
			})
		}
	}

	// Python: pyproject.toml, requirements.txt, setup.py, or Pipfile.
	switch {
	case fileExists(filepath.Join(repoRoot, "pyproject.toml")):
		ver := extractPythonVersion(repoRoot)
		result.Languages = append(result.Languages, DetectedLanguage{
			Name:       "Python",
			Version:    ver,
			Marker:     "pyproject.toml",
			Confidence: ConfidenceHigh,
		})
	case fileExists(filepath.Join(repoRoot, "requirements.txt")):
		result.Languages = append(result.Languages, DetectedLanguage{
			Name:       "Python",
			Version:    nil,
			Marker:     "requirements.txt",
			Confidence: ConfidenceMedium,
		})
	case fileExists(filepath.Join(repoRoot, "setup.py")):
		result.Languages = append(result.Languages, DetectedLanguage{
			Name:       "Python",
			Version:    nil,
			Marker:     "setup.py",
			Confidence: ConfidenceMedium,
		})
	case fileExists(filepath.Join(repoRoot, "Pipfile")):
		result.Languages = append(result.Languages, DetectedLanguage{
			Name:       "Python",
			Version:    nil,
			Marker:     "Pipfile",
			Confidence: ConfidenceMedium,
		})
	}

	// Rust: Cargo.toml is the canonical marker.
	if fileExists(filepath.Join(repoRoot, "Cargo.toml")) {
		result.Languages = append(result.Languages, DetectedLanguage{
			Name:       "Rust",
			Version:    nil,
			Marker:     "Cargo.toml",
			Confidence: ConfidenceHigh,
		})
	}

	// Java: pom.xml (Maven) or build.gradle (Gradle).
	switch {
	case fileExists(filepath.Join(repoRoot, "pom.xml")):
		result.Languages = append(result.Languages, DetectedLanguage{
			Name:       "Java",
			Version:    nil,
			Marker:     "pom.xml",
			Confidence: ConfidenceHigh,
		})
	case fileExists(filepath.Join(repoRoot, "build.gradle")):
		result.Languages = append(result.Languages, DetectedLanguage{
			Name:       "Java",
			Version:    nil,
			Marker:     "build.gradle",
			Confidence: ConfidenceHigh,
		})
	case fileExists(filepath.Join(repoRoot, "build.gradle.kts")):
		result.Languages = append(result.Languages, DetectedLanguage{
			Name:       "Java",
			Version:    nil,
			Marker:     "build.gradle.kts",
			Confidence: ConfidenceHigh,
		})
	}

	// PHP: composer.json.
	if fileExists(filepath.Join(repoRoot, "composer.json")) {
		result.Languages = append(result.Languages, DetectedLanguage{
			Name:       "PHP",
			Version:    nil,
			Marker:     "composer.json",
			Confidence: ConfidenceHigh,
		})
	}

	// Ruby: Gemfile.
	if fileExists(filepath.Join(repoRoot, "Gemfile")) {
		result.Languages = append(result.Languages, DetectedLanguage{
			Name:       "Ruby",
			Version:    nil,
			Marker:     "Gemfile",
			Confidence: ConfidenceHigh,
		})
	}

	// C# / .NET: any *.csproj file.
	if globExists(repoRoot, "*.csproj") {
		result.Languages = append(result.Languages, DetectedLanguage{
			Name:       "C#",
			Version:    nil,
			Marker:     "*.csproj",
			Confidence: ConfidenceHigh,
		})
	}

	// Scan immediate subdirectories for language markers not found at root.
	// This handles monorepo/polyglot layouts (e.g., api/go.mod, ui/package.json).
	d.detectSubdirectoryLanguages(repoRoot, result)

	// Mark the first detected language as primary.
	if len(result.Languages) > 0 {
		result.Languages[0].Primary = true
	}
}

// detectSubdirectoryLanguages scans immediate subdirectories of repoRoot for
// language marker files. Only languages not already detected at root level are
// added, and they receive ConfidenceMedium since they represent a module within
// a larger workspace rather than the primary language of the repository.
//
// Only one level deep is scanned (direct children of repoRoot). Hidden directories
// and entries in excludedSubdirs are skipped.
func (d *FileSystemDetector) detectSubdirectoryLanguages(repoRoot string, result *DetectionResult) {
	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || excludedSubdirs[name] {
			continue
		}
		subDir := filepath.Join(repoRoot, name)

		// Go: go.mod in subdirectory.
		if !hasLanguage(result, "Go") {
			if goMod := filepath.Join(subDir, "go.mod"); fileExists(goMod) {
				ver := extractGoVersion(goMod)
				result.Languages = append(result.Languages, DetectedLanguage{
					Name:       "Go",
					Version:    ver,
					Marker:     filepath.Join(name, "go.mod"),
					Confidence: ConfidenceMedium,
				})
			}
		}

		// TypeScript / JavaScript: tsconfig.json or package.json in subdirectory.
		tsAlreadyDetected := hasLanguage(result, "TypeScript")
		jsAlreadyDetected := hasLanguage(result, "JavaScript")
		if !tsAlreadyDetected {
			if fileExists(filepath.Join(subDir, "tsconfig.json")) {
				result.Languages = append(result.Languages, DetectedLanguage{
					Name:       "TypeScript",
					Version:    nil,
					Marker:     filepath.Join(name, "tsconfig.json"),
					Confidence: ConfidenceMedium,
				})
				// Re-read flag so the package.json branch below is skipped.
				tsAlreadyDetected = true
			} else if fileExists(filepath.Join(subDir, "package.json")) {
				if hasPackageJSONDep(subDir, "typescript") {
					result.Languages = append(result.Languages, DetectedLanguage{
						Name:       "TypeScript",
						Version:    nil,
						Marker:     filepath.Join(name, "package.json"),
						Confidence: ConfidenceMedium,
					})
					tsAlreadyDetected = true
				} else if !jsAlreadyDetected {
					result.Languages = append(result.Languages, DetectedLanguage{
						Name:       "JavaScript",
						Version:    extractNodeVersion(subDir),
						Marker:     filepath.Join(name, "package.json"),
						Confidence: ConfidenceMedium,
					})
					jsAlreadyDetected = true
				}
			}
		}
		// Silence unused variable warnings — these flags guard the subdirectory loop.
		_ = tsAlreadyDetected
		_ = jsAlreadyDetected

		// Python: pyproject.toml, requirements.txt, setup.py, or Pipfile in subdirectory.
		if !hasLanguage(result, "Python") {
			switch {
			case fileExists(filepath.Join(subDir, "pyproject.toml")):
				ver := extractPythonVersion(subDir)
				result.Languages = append(result.Languages, DetectedLanguage{
					Name:       "Python",
					Version:    ver,
					Marker:     filepath.Join(name, "pyproject.toml"),
					Confidence: ConfidenceMedium,
				})
			case fileExists(filepath.Join(subDir, "requirements.txt")):
				result.Languages = append(result.Languages, DetectedLanguage{
					Name:       "Python",
					Version:    nil,
					Marker:     filepath.Join(name, "requirements.txt"),
					Confidence: ConfidenceMedium,
				})
			case fileExists(filepath.Join(subDir, "setup.py")):
				result.Languages = append(result.Languages, DetectedLanguage{
					Name:       "Python",
					Version:    nil,
					Marker:     filepath.Join(name, "setup.py"),
					Confidence: ConfidenceMedium,
				})
			case fileExists(filepath.Join(subDir, "Pipfile")):
				result.Languages = append(result.Languages, DetectedLanguage{
					Name:       "Python",
					Version:    nil,
					Marker:     filepath.Join(name, "Pipfile"),
					Confidence: ConfidenceMedium,
				})
			}
		}

		// Rust: Cargo.toml in subdirectory.
		if !hasLanguage(result, "Rust") {
			if fileExists(filepath.Join(subDir, "Cargo.toml")) {
				result.Languages = append(result.Languages, DetectedLanguage{
					Name:       "Rust",
					Version:    nil,
					Marker:     filepath.Join(name, "Cargo.toml"),
					Confidence: ConfidenceMedium,
				})
			}
		}

		// Java: pom.xml or build.gradle in subdirectory.
		if !hasLanguage(result, "Java") {
			switch {
			case fileExists(filepath.Join(subDir, "pom.xml")):
				result.Languages = append(result.Languages, DetectedLanguage{
					Name:       "Java",
					Version:    nil,
					Marker:     filepath.Join(name, "pom.xml"),
					Confidence: ConfidenceMedium,
				})
			case fileExists(filepath.Join(subDir, "build.gradle")):
				result.Languages = append(result.Languages, DetectedLanguage{
					Name:       "Java",
					Version:    nil,
					Marker:     filepath.Join(name, "build.gradle"),
					Confidence: ConfidenceMedium,
				})
			case fileExists(filepath.Join(subDir, "build.gradle.kts")):
				result.Languages = append(result.Languages, DetectedLanguage{
					Name:       "Java",
					Version:    nil,
					Marker:     filepath.Join(name, "build.gradle.kts"),
					Confidence: ConfidenceMedium,
				})
			}
		}

		// PHP: composer.json in subdirectory.
		if !hasLanguage(result, "PHP") {
			if fileExists(filepath.Join(subDir, "composer.json")) {
				result.Languages = append(result.Languages, DetectedLanguage{
					Name:       "PHP",
					Version:    nil,
					Marker:     filepath.Join(name, "composer.json"),
					Confidence: ConfidenceMedium,
				})
			}
		}

		// Ruby: Gemfile in subdirectory.
		if !hasLanguage(result, "Ruby") {
			if fileExists(filepath.Join(subDir, "Gemfile")) {
				result.Languages = append(result.Languages, DetectedLanguage{
					Name:       "Ruby",
					Version:    nil,
					Marker:     filepath.Join(name, "Gemfile"),
					Confidence: ConfidenceMedium,
				})
			}
		}
	}
}

// frameworkCandidate describes a single framework detection signal.
// Candidates are listed in priority order — more specific frameworks first.
type frameworkCandidate struct {
	dep        string // dependency name to search for in package.json
	name       string // human-readable framework name
	markerFile string // preferred config file for the marker field
}

// defaultFrameworkCandidates is the ordered list of framework signals checked
// in every package.json. SvelteKit precedes plain Svelte so that a project
// with @sveltejs/kit does not also report plain Svelte.
var defaultFrameworkCandidates = []frameworkCandidate{
	{"@sveltejs/kit", "SvelteKit", "svelte.config.js"},
	{"svelte", "Svelte", "package.json"},
	{"next", "Next.js", "next.config.js"},
	{"react", "React", "package.json"},
	{"vue", "Vue", "package.json"},
	{"@angular/core", "Angular", "angular.json"},
	{"express", "Express", "package.json"},
}

// detectFrameworks inspects package.json dependencies for known framework signals.
// Scans the root directory first, then immediate subdirectories.
func (d *FileSystemDetector) detectFrameworks(repoRoot string, result *DetectionResult) {
	// Detect frameworks in the root package.json first.
	if fileExists(filepath.Join(repoRoot, "package.json")) {
		d.detectFrameworksInDir(repoRoot, "", result)
	}

	// Then scan immediate subdirectories that have their own package.json.
	// This handles monorepo layouts where the UI lives in a subdirectory.
	d.detectSubdirectoryFrameworks(repoRoot, result)
}

// detectFrameworksInDir checks for framework signals in the package.json under dir.
// dirPrefix is prepended to marker file paths; use an empty string for the root directory.
func (d *FileSystemDetector) detectFrameworksInDir(dir, dirPrefix string, result *DetectionResult) {
	lang := "TypeScript"
	if !hasLanguage(result, "TypeScript") {
		lang = "JavaScript"
	}

	seen := make(map[string]bool)
	// Pre-seed seen with already-detected frameworks to avoid duplicates.
	for _, f := range result.Frameworks {
		seen[f.Name] = true
	}

	for _, c := range defaultFrameworkCandidates {
		if seen[c.name] {
			continue
		}
		if !hasPackageJSONDep(dir, c.dep) {
			continue
		}

		// Resolve the best available marker file, prefixed with the subdirectory.
		markerBase := c.markerFile
		if !fileExists(filepath.Join(dir, markerBase)) {
			markerBase = "package.json"
		}
		marker := markerBase
		if dirPrefix != "" {
			marker = filepath.Join(dirPrefix, markerBase)
		}

		result.Frameworks = append(result.Frameworks, DetectedFramework{
			Name:       c.name,
			Language:   lang,
			Marker:     marker,
			Confidence: ConfidenceHigh,
		})

		seen[c.name] = true
		// If we matched SvelteKit, skip plain Svelte.
		if c.dep == "@sveltejs/kit" {
			seen["Svelte"] = true
		}
		break // Only report the highest-priority framework per dependency group.
	}
}

// detectSubdirectoryFrameworks scans immediate subdirectories of repoRoot for
// package.json files and checks them for framework dependencies not already
// detected at root level.
func (d *FileSystemDetector) detectSubdirectoryFrameworks(repoRoot string, result *DetectionResult) {
	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || excludedSubdirs[name] {
			continue
		}
		subDir := filepath.Join(repoRoot, name)
		if !fileExists(filepath.Join(subDir, "package.json")) {
			continue
		}
		d.detectFrameworksInDir(subDir, name, result)
	}
}

// detectTooling checks for marker files corresponding to known development tools.
func (d *FileSystemDetector) detectTooling(repoRoot string, result *DetectionResult) {
	// Task runners — detected in priority order (Taskfile > Make > Just).
	switch {
	case fileExists(filepath.Join(repoRoot, "Taskfile.yml")):
		result.Tooling = append(result.Tooling, DetectedTool{
			Name:     "Taskfile",
			Category: ToolCategoryTaskRunner,
			Marker:   "Taskfile.yml",
		})
	case fileExists(filepath.Join(repoRoot, "Makefile")):
		result.Tooling = append(result.Tooling, DetectedTool{
			Name:     "Make",
			Category: ToolCategoryTaskRunner,
			Marker:   "Makefile",
		})
	case fileExists(filepath.Join(repoRoot, "Justfile")):
		result.Tooling = append(result.Tooling, DetectedTool{
			Name:     "Just",
			Category: ToolCategoryTaskRunner,
			Marker:   "Justfile",
		})
	}

	// Go linters.
	if fileExists(filepath.Join(repoRoot, ".golangci.yml")) || fileExists(filepath.Join(repoRoot, ".golangci.yaml")) {
		result.Tooling = append(result.Tooling, DetectedTool{
			Name:     "golangci-lint",
			Category: ToolCategoryLinter,
			Language: "Go",
			Marker:   ".golangci.yml",
		})
	}
	if fileExists(filepath.Join(repoRoot, "revive.toml")) {
		result.Tooling = append(result.Tooling, DetectedTool{
			Name:     "Revive",
			Category: ToolCategoryLinter,
			Language: "Go",
			Marker:   "revive.toml",
		})
	}

	// Biome — replaces ESLint + Prettier for Node projects.
	hasBiome := fileExists(filepath.Join(repoRoot, "biome.json"))
	if hasBiome {
		result.Tooling = append(result.Tooling, DetectedTool{
			Name:     "Biome",
			Category: ToolCategoryLinter,
			Marker:   "biome.json",
		})
	}

	// ESLint — skip if Biome is present, as they are mutually exclusive.
	if !hasBiome {
		eslintMarker := findFirstFile(repoRoot, []string{
			".eslintrc.js", ".eslintrc.cjs", ".eslintrc.yaml", ".eslintrc.yml",
			".eslintrc.json", ".eslintrc", "eslint.config.js", "eslint.config.cjs",
			"eslint.config.mjs",
		})
		if eslintMarker != "" {
			result.Tooling = append(result.Tooling, DetectedTool{
				Name:     "ESLint",
				Category: ToolCategoryLinter,
				Marker:   eslintMarker,
			})
		}
	}

	// Prettier — skip if Biome is present.
	if !hasBiome {
		prettierMarker := findFirstFile(repoRoot, []string{
			".prettierrc", ".prettierrc.js", ".prettierrc.cjs", ".prettierrc.yaml",
			".prettierrc.yml", ".prettierrc.json", ".prettierrc.toml",
			"prettier.config.js", "prettier.config.cjs",
		})
		if prettierMarker != "" {
			result.Tooling = append(result.Tooling, DetectedTool{
				Name:     "Prettier",
				Category: ToolCategoryFormatter,
				Marker:   prettierMarker,
			})
		}
	}

	// TypeScript type checker.
	if fileExists(filepath.Join(repoRoot, "tsconfig.json")) {
		result.Tooling = append(result.Tooling, DetectedTool{
			Name:     "TypeScript",
			Category: ToolCategoryTypeChecker,
			Marker:   "tsconfig.json",
		})
	}

	// Python test framework.
	if fileExists(filepath.Join(repoRoot, "pytest.ini")) || fileExists(filepath.Join(repoRoot, "conftest.py")) {
		result.Tooling = append(result.Tooling, DetectedTool{
			Name:     "Pytest",
			Category: ToolCategoryTestFramework,
			Language: "Python",
			Marker:   "pytest.ini",
		})
	}

	// Python linter: Ruff.
	if fileExists(filepath.Join(repoRoot, "ruff.toml")) || hasPyprojectToolSection(repoRoot, "ruff") {
		result.Tooling = append(result.Tooling, DetectedTool{
			Name:     "Ruff",
			Category: ToolCategoryLinter,
			Language: "Python",
			Marker:   "ruff.toml",
		})
	}

	// Jest test framework.
	jestMarker := findFirstFile(repoRoot, []string{
		"jest.config.js", "jest.config.cjs", "jest.config.mjs",
		"jest.config.ts", "jest.config.json",
	})
	if jestMarker != "" {
		result.Tooling = append(result.Tooling, DetectedTool{
			Name:     "Jest",
			Category: ToolCategoryTestFramework,
			Marker:   jestMarker,
		})
	}

	// Vitest test framework.
	vitestMarker := findFirstFile(repoRoot, []string{
		"vitest.config.js", "vitest.config.cjs", "vitest.config.mjs",
		"vitest.config.ts",
	})
	if vitestMarker != "" {
		result.Tooling = append(result.Tooling, DetectedTool{
			Name:     "Vitest",
			Category: ToolCategoryTestFramework,
			Marker:   vitestMarker,
		})
	}

	// CI: GitHub Actions.
	if dirExists(filepath.Join(repoRoot, ".github", "workflows")) {
		result.Tooling = append(result.Tooling, DetectedTool{
			Name:     "GitHub Actions",
			Category: ToolCategoryCI,
			Marker:   ".github/workflows/",
		})
	}

	// Container tooling.
	if fileExists(filepath.Join(repoRoot, "docker-compose.yml")) || fileExists(filepath.Join(repoRoot, "docker-compose.yaml")) {
		result.Tooling = append(result.Tooling, DetectedTool{
			Name:     "Docker Compose",
			Category: ToolCategoryContainer,
			Marker:   "docker-compose.yml",
		})
	} else if fileExists(filepath.Join(repoRoot, "Dockerfile")) {
		result.Tooling = append(result.Tooling, DetectedTool{
			Name:     "Docker",
			Category: ToolCategoryContainer,
			Marker:   "Dockerfile",
		})
	}
}

// detectDocs scans for well-known documentation files.
func (d *FileSystemDetector) detectDocs(repoRoot string, result *DetectionResult) {
	type docCandidate struct {
		path string
		typ  DocType
	}
	candidates := []docCandidate{
		{"README.md", DocTypeProjectDocs},
		{"CONTRIBUTING.md", DocTypeContributing},
		{"CLAUDE.md", DocTypeClaudeInstructions},
	}

	for _, c := range candidates {
		fullPath := filepath.Join(repoRoot, c.path)
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}
		result.ExistingDocs = append(result.ExistingDocs, DetectedDoc{
			Path:      c.path,
			Type:      c.typ,
			SizeBytes: info.Size(),
		})
	}

	// Scan docs/ directory for architecture/convention documents.
	docsDir := filepath.Join(repoRoot, "docs")
	entries, err := os.ReadDir(docsDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			relPath := filepath.Join("docs", entry.Name())
			info, err := entry.Info()
			if err != nil {
				continue
			}
			result.ExistingDocs = append(result.ExistingDocs, DetectedDoc{
				Path:      relPath,
				Type:      DocTypeArchitectureDocs,
				SizeBytes: info.Size(),
			})
		}
	}

	// Scan .semspec/sources/docs/ for pre-existing SOPs.
	sopDir := filepath.Join(repoRoot, ".semspec", "sources", "docs")
	sopEntries, err := os.ReadDir(sopDir)
	if err == nil {
		for _, entry := range sopEntries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			relPath := filepath.Join(".semspec", "sources", "docs", entry.Name())
			info, err := entry.Info()
			if err != nil {
				continue
			}
			result.ExistingDocs = append(result.ExistingDocs, DetectedDoc{
				Path:      relPath,
				Type:      DocTypeExistingSOP,
				SizeBytes: info.Size(),
			})
		}
	}
}

// buildProposedChecklist generates the deterministic checklist from detected languages
// and tooling. When a Taskfile or Makefile is found, well-known targets are preferred
// over raw tool commands to respect the project's existing conventions.
func (d *FileSystemDetector) buildProposedChecklist(repoRoot string, result *DetectionResult) []Check {
	var checks []Check

	hasTaskRunner, runnerName := detectTaskRunner(result)

	// Go checks.
	if hasLanguage(result, "Go") {
		if hasTaskRunner {
			taskChecks := buildTaskRunnerChecks(repoRoot, runnerName, []string{"*.go", "go.mod", "go.sum"})
			if len(taskChecks) > 0 {
				checks = append(checks, taskChecks...)
			} else {
				// Fall back to raw Go commands when the task runner has no matching targets.
				for _, tmpl := range goBaseTemplates {
					checks = append(checks, tmpl.toCheck(""))
				}
			}
		} else {
			for _, tmpl := range goBaseTemplates {
				checks = append(checks, tmpl.toCheck(""))
			}
		}

		// Additional Go tooling checks are always raw commands (not wrapped in task runner).
		if hasTool(result, "golangci-lint") {
			checks = append(checks, goGolangciLintTemplate.toCheck(""))
		}
		if hasTool(result, "Revive") {
			checks = append(checks, goReviveTemplate.toCheck(""))
		}
	}

	// Node.js / TypeScript checks.
	if hasLanguage(result, "TypeScript") || hasLanguage(result, "JavaScript") {
		// TypeScript type checking is separate from test running.
		if hasTool(result, "TypeScript") {
			if hasFramework(result, "SvelteKit") || hasFramework(result, "Svelte") {
				checks = append(checks, nodeSvelteCheckTemplate.toCheck(""))
			} else {
				checks = append(checks, nodeTSCTemplate.toCheck(""))
			}
		}

		// Test framework — prefer specific framework if detected.
		switch {
		case hasTool(result, "Vitest"):
			checks = append(checks, nodeVitestTemplate.toCheck(""))
		case hasTool(result, "Jest"):
			checks = append(checks, nodeJestTemplate.toCheck(""))
		default:
			if hasTaskRunner {
				taskChecks := buildTaskRunnerChecks(repoRoot, runnerName, []string{"*.ts", "*.js", "*.svelte"})
				if len(taskChecks) > 0 {
					checks = append(checks, taskChecks...)
				} else {
					checks = append(checks, nodeBaseTemplates[0].toCheck(""))
				}
			} else {
				checks = append(checks, nodeBaseTemplates[0].toCheck(""))
			}
		}

		// Linting / formatting.
		if hasTool(result, "Biome") {
			checks = append(checks, nodeBiomeTemplate.toCheck(""))
		} else {
			if hasTool(result, "ESLint") {
				checks = append(checks, nodeESLintTemplate.toCheck(""))
			}
			if hasTool(result, "Prettier") {
				checks = append(checks, nodePrettierTemplate.toCheck(""))
			}
		}
	}

	// Python checks.
	if hasLanguage(result, "Python") {
		for _, tmpl := range pythonBaseTemplates {
			checks = append(checks, tmpl.toCheck(""))
		}
		if hasTool(result, "Ruff") {
			checks = append(checks, pythonRuffTemplate.toCheck(""))
		}
		// Propose mypy for typed Python projects.
		if fileExists(filepath.Join(repoRoot, "mypy.ini")) || hasPyprojectToolSection(repoRoot, "mypy") {
			checks = append(checks, pythonMypyTemplate.toCheck(""))
		}
	}

	// Rust checks.
	if hasLanguage(result, "Rust") {
		for _, tmpl := range rustBaseTemplates {
			checks = append(checks, tmpl.toCheck(""))
		}
	}

	// Java checks.
	if hasLanguage(result, "Java") {
		if fileExists(filepath.Join(repoRoot, "pom.xml")) {
			for _, tmpl := range javaMavenTemplates {
				checks = append(checks, tmpl.toCheck(""))
			}
		} else if fileExists(filepath.Join(repoRoot, "build.gradle")) || fileExists(filepath.Join(repoRoot, "build.gradle.kts")) {
			for _, tmpl := range javaGradleTemplates {
				checks = append(checks, tmpl.toCheck(""))
			}
		}
	}

	return checks
}

// --- Helper functions -------------------------------------------------------

// extractGoVersion parses the `go X.Y` directive from go.mod.
// Returns nil when the version cannot be determined.
func extractGoVersion(goModPath string) *string {
	f, err := os.Open(goModPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "go ") {
			ver := strings.TrimPrefix(line, "go ")
			ver = strings.TrimSpace(ver)
			if ver != "" {
				return &ver
			}
		}
	}
	return nil
}

// packageJSON is a minimal representation of package.json for dependency inspection.
type packageJSON struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	PeerDependencies map[string]string `json:"peerDependencies"`
	Engines         struct {
		Node string `json:"node"`
	} `json:"engines"`
}

// readPackageJSON reads and parses package.json from repoRoot.
// Returns nil when the file is absent or malformed.
func readPackageJSON(repoRoot string) *packageJSON {
	data, err := os.ReadFile(filepath.Join(repoRoot, "package.json"))
	if err != nil {
		return nil
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}
	return &pkg
}

// hasPackageJSONDep returns true if the named package appears in any of the
// three dependency maps in package.json. The match is prefix-based so that
// "svelte" matches both "svelte" and "@sveltejs/kit".
func hasPackageJSONDep(repoRoot, dep string) bool {
	pkg := readPackageJSON(repoRoot)
	if pkg == nil {
		return false
	}
	for _, deps := range []map[string]string{
		pkg.Dependencies,
		pkg.DevDependencies,
		pkg.PeerDependencies,
	} {
		for name := range deps {
			if name == dep || strings.HasPrefix(name, dep+"/") {
				return true
			}
		}
	}
	return false
}

// extractNodeVersion reads the engines.node field from package.json.
// Returns nil when absent.
func extractNodeVersion(repoRoot string) *string {
	pkg := readPackageJSON(repoRoot)
	if pkg == nil || pkg.Engines.Node == "" {
		return nil
	}
	v := pkg.Engines.Node
	return &v
}

// extractPythonVersion reads requires-python from pyproject.toml via simple line scan.
// Returns nil when absent or unparseable.
func extractPythonVersion(repoRoot string) *string {
	data, err := os.ReadFile(filepath.Join(repoRoot, "pyproject.toml"))
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "requires-python") {
			// e.g.  requires-python = ">=3.11"
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				ver := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
				if ver != "" {
					return &ver
				}
			}
		}
	}
	return nil
}

// hasPyprojectToolSection returns true when pyproject.toml contains a [tool.<name>] section.
func hasPyprojectToolSection(repoRoot, toolName string) bool {
	data, err := os.ReadFile(filepath.Join(repoRoot, "pyproject.toml"))
	if err != nil {
		return false
	}
	target := "[tool." + toolName + "]"
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == target {
			return true
		}
	}
	return false
}

// findFirstFile returns the first filename from candidates that exists under repoRoot.
// Returns an empty string when none are found.
func findFirstFile(repoRoot string, candidates []string) string {
	for _, name := range candidates {
		if fileExists(filepath.Join(repoRoot, name)) {
			return name
		}
	}
	return ""
}

// globExists returns true if any file matching the pattern exists directly under repoRoot.
// Only supports simple "*.ext" patterns (single-directory, no recursion).
func globExists(repoRoot, pattern string) bool {
	matches, err := filepath.Glob(filepath.Join(repoRoot, pattern))
	return err == nil && len(matches) > 0
}

// dirExists returns true if the path exists and is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// hasLanguage returns true if the named language appears in result.Languages.
func hasLanguage(result *DetectionResult, name string) bool {
	for _, l := range result.Languages {
		if l.Name == name {
			return true
		}
	}
	return false
}

// hasFramework returns true if the named framework appears in result.Frameworks.
func hasFramework(result *DetectionResult, name string) bool {
	for _, f := range result.Frameworks {
		if f.Name == name {
			return true
		}
	}
	return false
}

// hasTool returns true if the named tool appears in result.Tooling.
func hasTool(result *DetectionResult, name string) bool {
	for _, t := range result.Tooling {
		if t.Name == name {
			return true
		}
	}
	return false
}

// detectTaskRunner returns the runner name when a task runner is detected.
func detectTaskRunner(result *DetectionResult) (bool, string) {
	for _, t := range result.Tooling {
		if t.Category == ToolCategoryTaskRunner {
			return true, t.Name
		}
	}
	return false, ""
}

// buildTaskRunnerChecks generates checks for well-known task targets found in the
// task runner file. Returns an empty slice when no matching targets are present.
func buildTaskRunnerChecks(repoRoot, runnerName string, defaultTrigger []string) []Check {
	switch runnerName {
	case "Taskfile":
		return buildTaskfileChecks(repoRoot, defaultTrigger)
	case "Make":
		return buildMakefileChecks(repoRoot, defaultTrigger)
	}
	return nil
}

// buildTaskfileChecks parses Taskfile.yml for well-known target names.
func buildTaskfileChecks(repoRoot string, trigger []string) []Check {
	targets := parseTaskfileTargets(filepath.Join(repoRoot, "Taskfile.yml"))
	return buildRunnerChecks(targets, trigger, func(target string, category CheckCategory, t []string, timeout string) Check {
		return taskfileCheckTemplate(target, category, t, timeout)
	})
}

// buildMakefileChecks parses Makefile for well-known target names.
func buildMakefileChecks(repoRoot string, trigger []string) []Check {
	targets := parseMakefileTargets(filepath.Join(repoRoot, "Makefile"))
	return buildRunnerChecks(targets, trigger, func(target string, category CheckCategory, t []string, timeout string) Check {
		return makeCheckTemplate(target, category, t, timeout)
	})
}

// buildRunnerChecks matches discovered targets against well-known names and builds checks.
func buildRunnerChecks(targets map[string]bool, trigger []string, builder func(string, CheckCategory, []string, string) Check) []Check {
	var checks []Check
	// Preserve insertion order by iterating over well-known names.
	for _, name := range wellKnownTaskfileTargets {
		if !targets[name] {
			continue
		}
		category := taskTargetCategory[name]
		timeout := taskTargetTimeout[name]
		if category == "" {
			category = CheckCategoryTest
		}
		if timeout == "" {
			timeout = "120s"
		}
		checks = append(checks, builder(name, category, trigger, timeout))
	}
	return checks
}

// parseTaskfileTargets extracts task names from a Taskfile.yml via simple line scanning.
// Returns a set of task names found. Malformed files return an empty set.
func parseTaskfileTargets(path string) map[string]bool {
	found := make(map[string]bool)
	f, err := os.Open(path)
	if err != nil {
		return found
	}
	defer f.Close()

	// Taskfile task names appear as top-level keys under the `tasks:` section.
	// Pattern: lines starting without whitespace that end with `:` after the tasks: header.
	inTasks := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "tasks:" {
			inTasks = true
			continue
		}
		if inTasks {
			// Another top-level key ends the tasks section.
			if len(line) > 0 && line[0] != ' ' && line[0] != '\t' && trimmed != "" {
				if !strings.HasSuffix(trimmed, ":") {
					inTasks = false
					continue
				}
			}
			// Task name: single-level indented key ending with ':'
			if strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t") {
				if strings.HasSuffix(trimmed, ":") {
					name := strings.TrimSuffix(trimmed, ":")
					found[name] = true
				}
			}
		}
	}
	return found
}

// parseMakefileTargets extracts target names from a Makefile via simple line scanning.
// Returns a set of target names found. Only plain targets (no %) are included.
func parseMakefileTargets(path string) map[string]bool {
	found := make(map[string]bool)
	f, err := os.Open(path)
	if err != nil {
		return found
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		// Skip comments, blank lines, and lines starting with whitespace (recipes).
		if len(line) == 0 || line[0] == '#' || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		// Target line format: "target: [deps]"
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		target := strings.TrimSpace(line[:idx])
		// Skip pattern rules and special targets.
		if strings.Contains(target, "%") || strings.HasPrefix(target, ".") {
			continue
		}
		found[target] = true
	}
	return found
}
