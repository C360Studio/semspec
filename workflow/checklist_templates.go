package workflow

// checkTemplate is an internal blueprint for a quality gate check.
// Templates are compiled into the binary and keyed to detected languages
// and tooling. Users never edit templates directly — they edit the
// proposed checks that templates seed.
type checkTemplate struct {
	Name        string
	Command     string
	Trigger     []string
	Category    CheckCategory
	Required    bool
	Timeout     string
	Description string
	WorkingDir  string
}

// toCheck converts a template to a Check, applying any override for WorkingDir.
func (t checkTemplate) toCheck(workingDir string) Check {
	wd := t.WorkingDir
	if workingDir != "" {
		wd = workingDir
	}
	return Check{
		Name:        t.Name,
		Command:     t.Command,
		Trigger:     t.Trigger,
		Category:    t.Category,
		Required:    t.Required,
		Timeout:     t.Timeout,
		Description: t.Description,
		WorkingDir:  wd,
	}
}

// --- Go templates -----------------------------------------------------------

// goBaseTemplates are the minimum checks for any Go project.
var goBaseTemplates = []checkTemplate{
	{
		Name:        "go-build",
		Command:     "go build ./...",
		Trigger:     []string{"*.go", "go.mod", "go.sum"},
		Category:    CheckCategoryCompile,
		Required:    true,
		Timeout:     "120s",
		Description: "Compile all Go packages",
	},
	{
		Name:        "go-vet",
		Command:     "go vet ./...",
		Trigger:     []string{"*.go"},
		Category:    CheckCategoryLint,
		Required:    true,
		Timeout:     "60s",
		Description: "Run Go static analysis",
	},
	{
		Name:        "go-test",
		Command:     "go test ./...",
		Trigger:     []string{"*.go"},
		Category:    CheckCategoryTest,
		Required:    true,
		Timeout:     "300s",
		Description: "Run Go test suite",
	},
}

// goGolangciLintTemplate is proposed when .golangci.yml is detected.
var goGolangciLintTemplate = checkTemplate{
	Name:        "golangci-lint",
	Command:     "golangci-lint run ./...",
	Trigger:     []string{"*.go"},
	Category:    CheckCategoryLint,
	Required:    false,
	Timeout:     "120s",
	Description: "Run golangci-lint with project config",
}

// goReviveTemplate is proposed when revive.toml is detected.
var goReviveTemplate = checkTemplate{
	Name:        "revive",
	Command:     "revive -config revive.toml ./...",
	Trigger:     []string{"*.go"},
	Category:    CheckCategoryLint,
	Required:    false,
	Timeout:     "60s",
	Description: "Run Revive linter with project config",
}

// --- Node.js / TypeScript templates -----------------------------------------

// nodeBaseTemplates are the minimum checks for any Node.js project.
var nodeBaseTemplates = []checkTemplate{
	{
		Name:        "npm-test",
		Command:     "npm test",
		Trigger:     []string{"*.js", "*.ts", "*.jsx", "*.tsx", "*.mjs", "*.cjs"},
		Category:    CheckCategoryTest,
		Required:    true,
		Timeout:     "300s",
		Description: "Run Node.js test suite",
	},
}

// nodeTSCTemplate is proposed when tsconfig.json is detected.
var nodeTSCTemplate = checkTemplate{
	Name:        "tsc",
	Command:     "npx tsc --noEmit",
	Trigger:     []string{"*.ts", "*.tsx"},
	Category:    CheckCategoryTypecheck,
	Required:    true,
	Timeout:     "120s",
	Description: "Run TypeScript type checking",
}

// nodeSvelteCheckTemplate is proposed when svelte is detected.
var nodeSvelteCheckTemplate = checkTemplate{
	Name:        "svelte-check",
	Command:     "npm run check",
	Trigger:     []string{"*.svelte", "*.ts"},
	Category:    CheckCategoryTypecheck,
	Required:    true,
	Timeout:     "120s",
	Description: "Svelte/TypeScript type checking",
}

// nodeESLintTemplate is proposed when an ESLint config is detected.
var nodeESLintTemplate = checkTemplate{
	Name:        "eslint",
	Command:     "npx eslint .",
	Trigger:     []string{"*.js", "*.ts", "*.jsx", "*.tsx", "*.svelte", "*.mjs"},
	Category:    CheckCategoryLint,
	Required:    false,
	Timeout:     "60s",
	Description: "Run ESLint",
}

// nodePrettierTemplate is proposed when a Prettier config is detected.
var nodePrettierTemplate = checkTemplate{
	Name:        "prettier",
	Command:     "npx prettier --check .",
	Trigger:     []string{"*.js", "*.ts", "*.jsx", "*.tsx", "*.svelte", "*.json", "*.md"},
	Category:    CheckCategoryFormat,
	Required:    false,
	Timeout:     "30s",
	Description: "Check code formatting with Prettier",
}

// nodeBiomeTemplate is proposed when biome.json is detected.
// Biome replaces both ESLint and Prettier, so it is mutually exclusive with those.
var nodeBiomeTemplate = checkTemplate{
	Name:        "biome",
	Command:     "npx biome check .",
	Trigger:     []string{"*.js", "*.ts", "*.jsx", "*.tsx", "*.json"},
	Category:    CheckCategoryLint,
	Required:    false,
	Timeout:     "60s",
	Description: "Run Biome linter and formatter",
}

// nodeVitestTemplate is proposed when vitest.config.* is detected.
var nodeVitestTemplate = checkTemplate{
	Name:        "vitest",
	Command:     "npx vitest run",
	Trigger:     []string{"*.ts", "*.js", "*.tsx", "*.jsx"},
	Category:    CheckCategoryTest,
	Required:    true,
	Timeout:     "300s",
	Description: "Run Vitest test suite",
}

// nodeJestTemplate is proposed when jest.config.* is detected.
var nodeJestTemplate = checkTemplate{
	Name:        "jest",
	Command:     "npx jest",
	Trigger:     []string{"*.ts", "*.js", "*.tsx", "*.jsx"},
	Category:    CheckCategoryTest,
	Required:    true,
	Timeout:     "300s",
	Description: "Run Jest test suite",
}

// --- Python templates --------------------------------------------------------

// pythonBaseTemplates are the minimum checks for any Python project.
var pythonBaseTemplates = []checkTemplate{
	{
		Name:        "pytest",
		Command:     "python -m pytest .",
		Trigger:     []string{"*.py"},
		Category:    CheckCategoryTest,
		Required:    true,
		Timeout:     "300s",
		Description: "Run Python test suite with pytest",
	},
}

// pythonRuffTemplate is proposed when ruff.toml or [tool.ruff] in pyproject.toml is detected.
var pythonRuffTemplate = checkTemplate{
	Name:        "ruff",
	Command:     "ruff check .",
	Trigger:     []string{"*.py"},
	Category:    CheckCategoryLint,
	Required:    false,
	Timeout:     "60s",
	Description: "Run Ruff linter",
}

// pythonMypyTemplate is proposed for Python projects (mypy is widely used).
var pythonMypyTemplate = checkTemplate{
	Name:        "mypy",
	Command:     "mypy .",
	Trigger:     []string{"*.py"},
	Category:    CheckCategoryTypecheck,
	Required:    false,
	Timeout:     "120s",
	Description: "Run mypy type checking",
}

// --- Rust templates ----------------------------------------------------------

// rustBaseTemplates are the minimum checks for any Rust project.
var rustBaseTemplates = []checkTemplate{
	{
		Name:        "cargo-build",
		Command:     "cargo build",
		Trigger:     []string{"*.rs", "Cargo.toml", "Cargo.lock"},
		Category:    CheckCategoryCompile,
		Required:    true,
		Timeout:     "300s",
		Description: "Compile Rust project",
	},
	{
		Name:        "cargo-test",
		Command:     "cargo test",
		Trigger:     []string{"*.rs"},
		Category:    CheckCategoryTest,
		Required:    true,
		Timeout:     "300s",
		Description: "Run Rust test suite",
	},
	{
		Name:        "cargo-clippy",
		Command:     "cargo clippy -- -D warnings",
		Trigger:     []string{"*.rs"},
		Category:    CheckCategoryLint,
		Required:    false,
		Timeout:     "120s",
		Description: "Run Clippy linter",
	},
}

// --- Java templates ----------------------------------------------------------

// javaBaseTemplates are the minimum checks for a Maven project.
var javaMavenTemplates = []checkTemplate{
	{
		Name:        "mvn-test",
		Command:     "mvn test",
		Trigger:     []string{"*.java", "pom.xml"},
		Category:    CheckCategoryTest,
		Required:    true,
		Timeout:     "600s",
		Description: "Run Java test suite with Maven",
	},
}

// javaGradleTemplates are the minimum checks for a Gradle project.
var javaGradleTemplates = []checkTemplate{
	{
		Name:        "gradle-test",
		Command:     "./gradlew test",
		Trigger:     []string{"*.java", "build.gradle", "build.gradle.kts"},
		Category:    CheckCategoryTest,
		Required:    true,
		Timeout:     "600s",
		Description: "Run Java test suite with Gradle",
	},
}

// --- Taskfile / Make integration --------------------------------------------

// wellKnownTaskfileTargets are Taskfile task names checked during detection.
// When found, they replace raw tool commands with the task invocation.
var wellKnownTaskfileTargets = []string{"test", "lint", "check", "build", "fmt", "format"}

// wellKnownMakeTargets are Makefile target names checked during detection.
var wellKnownMakeTargets = []string{"test", "lint", "check", "build", "fmt", "format"}

// taskfileCheckTemplate builds a Check that runs a Taskfile target.
func taskfileCheckTemplate(target string, category CheckCategory, trigger []string, timeout string) Check {
	return Check{
		Name:        "task-" + target,
		Command:     "task " + target,
		Trigger:     trigger,
		Category:    category,
		Required:    target == "build" || target == "test" || target == "check",
		Timeout:     timeout,
		Description: "Run 'task " + target + "' from Taskfile",
	}
}

// makeCheckTemplate builds a Check that runs a Makefile target.
func makeCheckTemplate(target string, category CheckCategory, trigger []string, timeout string) Check {
	return Check{
		Name:        "make-" + target,
		Command:     "make " + target,
		Trigger:     trigger,
		Category:    category,
		Required:    target == "build" || target == "test" || target == "check",
		Timeout:     timeout,
		Description: "Run 'make " + target + "' from Makefile",
	}
}

// taskTargetCategory maps a well-known task name to its check category.
var taskTargetCategory = map[string]CheckCategory{
	"test":   CheckCategoryTest,
	"lint":   CheckCategoryLint,
	"check":  CheckCategoryTypecheck,
	"build":  CheckCategoryCompile,
	"fmt":    CheckCategoryFormat,
	"format": CheckCategoryFormat,
}

// taskTargetTimeout maps a well-known task name to its default timeout.
var taskTargetTimeout = map[string]string{
	"test":   "300s",
	"lint":   "120s",
	"check":  "120s",
	"build":  "120s",
	"fmt":    "30s",
	"format": "30s",
}
