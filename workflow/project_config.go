package workflow

import "time"

// ProjectConfig is the top-level structure written to .semspec/project.json.
// It records the detected stack, repository metadata, and initialization timestamp.
// Components read this file directly — it is not stored in the graph.
type ProjectConfig struct {
	// Name is the human-readable project name.
	Name string `json:"name"`

	// Description is a brief description of what the project does.
	Description string `json:"description,omitempty"`

	// Version is the project configuration schema version.
	Version string `json:"version"`

	// InitializedAt is when the project was first initialized.
	InitializedAt time.Time `json:"initialized_at"`

	// Languages contains the detected programming languages.
	Languages []LanguageInfo `json:"languages"`

	// Frameworks contains the detected frameworks.
	Frameworks []FrameworkInfo `json:"frameworks,omitempty"`

	// Tooling describes detected tools grouped by category.
	Tooling ProjectTooling `json:"tooling"`

	// Repository contains VCS metadata.
	Repository RepositoryInfo `json:"repository,omitempty"`
}

// LanguageInfo describes a detected programming language.
type LanguageInfo struct {
	// Name is the language name (e.g., "Go", "TypeScript", "Python").
	Name string `json:"name"`

	// Version is the detected language version (nil when not detectable).
	Version *string `json:"version"`

	// Primary indicates whether this is the primary project language.
	Primary bool `json:"primary"`
}

// FrameworkInfo describes a detected framework.
type FrameworkInfo struct {
	// Name is the framework name (e.g., "SvelteKit", "React", "Express").
	Name string `json:"name"`

	// Language is the language this framework belongs to.
	Language string `json:"language"`
}

// ProjectTooling groups detected tooling by functional category.
type ProjectTooling struct {
	// TaskRunner is the detected task runner (e.g., "Taskfile", "Make", "Just").
	TaskRunner string `json:"task_runner,omitempty"`

	// Linters lists the detected linting tools (e.g., ["revive", "eslint"]).
	Linters []string `json:"linters,omitempty"`

	// TestFrameworks lists the detected testing frameworks.
	TestFrameworks []string `json:"test_frameworks,omitempty"`

	// CI is the detected CI system (e.g., "GitHub Actions").
	CI string `json:"ci,omitempty"`

	// Container is the detected container tooling (e.g., "Docker Compose").
	Container string `json:"container,omitempty"`
}

// RepositoryInfo contains version control metadata.
type RepositoryInfo struct {
	// URL is the remote repository URL (e.g., "github.com/org/repo").
	URL string `json:"url,omitempty"`

	// DefaultBranch is the primary branch name (e.g., "main").
	DefaultBranch string `json:"default_branch,omitempty"`
}

// Checklist is the top-level structure written to .semspec/checklist.json.
// It defines deterministic quality gates that run after developer agent output.
type Checklist struct {
	// Version is the checklist schema version.
	Version string `json:"version"`

	// CreatedAt is when the checklist was created.
	CreatedAt time.Time `json:"created_at"`

	// Checks is the ordered list of quality gate checks.
	Checks []Check `json:"checks"`
}

// Check defines a single deterministic quality gate.
type Check struct {
	// Name is the unique identifier for this check (e.g., "go-build").
	Name string `json:"name"`

	// Command is the shell command to execute (e.g., "go build ./...").
	Command string `json:"command"`

	// Trigger is a list of glob patterns — the check runs only when a
	// modified file matches at least one pattern.
	Trigger []string `json:"trigger"`

	// Category classifies the check type.
	Category CheckCategory `json:"category"`

	// Required indicates whether failure blocks progression to the reviewer.
	// Non-required checks produce warnings only.
	Required bool `json:"required"`

	// Timeout is the maximum execution time in Go duration format (e.g., "120s").
	Timeout string `json:"timeout"`

	// Description is a human-readable explanation of what the check validates.
	Description string `json:"description"`

	// WorkingDir is the directory in which to run the command, relative to
	// the repository root. Defaults to "." when empty.
	WorkingDir string `json:"working_dir,omitempty"`
}

// CheckCategory classifies the functional role of a quality gate check.
type CheckCategory string

const (
	// CheckCategoryCompile validates that code compiles without errors.
	CheckCategoryCompile CheckCategory = "compile"

	// CheckCategoryLint validates code style and static analysis.
	CheckCategoryLint CheckCategory = "lint"

	// CheckCategoryTypecheck validates type correctness.
	CheckCategoryTypecheck CheckCategory = "typecheck"

	// CheckCategoryTest validates behaviour through test execution.
	CheckCategoryTest CheckCategory = "test"

	// CheckCategoryFormat validates code formatting.
	CheckCategoryFormat CheckCategory = "format"
)

// Standards is the top-level structure written to .semspec/standards.json.
// Rules are injected into every agent context as a preamble before
// strategy-specific content. Standards grow over time as SOPs are authored.
type Standards struct {
	// Version is the standards schema version.
	Version string `json:"version"`

	// GeneratedAt is when the standards were last generated or regenerated.
	GeneratedAt time.Time `json:"generated_at"`

	// TokenEstimate is the approximate token count for all rules combined.
	// Used by the context-builder to account for standards in the token budget.
	TokenEstimate int `json:"token_estimate"`

	// Rules is the ordered list of project standards.
	Rules []Rule `json:"rules"`
}

// Rule is a single project standard injected into agent context.
type Rule struct {
	// ID is the unique, stable identifier for this rule (e.g., "test-coverage").
	ID string `json:"id"`

	// Text is the rule statement in plain English. It should be a single
	// concrete sentence that an agent can follow without ambiguity.
	Text string `json:"text"`

	// Severity controls how violations are treated by the reviewer.
	Severity RuleSeverity `json:"severity"`

	// Category groups related rules (e.g., "testing", "code-quality").
	Category string `json:"category"`

	// AppliesTo is a list of glob patterns for files this rule governs.
	// An empty list means the rule applies to all files.
	AppliesTo []string `json:"applies_to,omitempty"`

	// Origin tracks where this rule came from, enabling the future flywheel.
	Origin string `json:"origin"`
}

// RuleSeverity controls how a rule violation is treated.
type RuleSeverity string

const (
	// RuleSeverityError means a violation should block reviewer approval.
	RuleSeverityError RuleSeverity = "error"

	// RuleSeverityWarning means a violation is surfaced but does not block.
	RuleSeverityWarning RuleSeverity = "warning"

	// RuleSeverityInfo means a violation is informational only.
	RuleSeverityInfo RuleSeverity = "info"
)

// RuleOrigin constants describe the source of a standards rule.
const (
	// RuleOriginInit marks rules generated during project initialization.
	RuleOriginInit = "init"

	// RuleOriginManual marks rules added directly by the user.
	RuleOriginManual = "manual"

	// RuleOriginReviewPattern marks rules promoted from recurring review feedback.
	// Format: "review-pattern" (used by the future flywheel).
	RuleOriginReviewPattern = "review-pattern"
)

// RuleOriginSOP returns the origin string for a rule derived from an SOP file.
// Format: "sop:<filename>" (e.g., "sop:go-conventions.md").
func RuleOriginSOP(filename string) string {
	return "sop:" + filename
}

// InitStatus describes the current initialization state of the project.
// Returned by GET /api/project/status to let the UI decide whether to show
// the setup wizard or the normal activity view.
type InitStatus struct {
	// Initialized is true only when all three config files exist.
	Initialized bool `json:"initialized"`

	// HasProjectJSON is true when .semspec/project.json exists.
	HasProjectJSON bool `json:"has_project_json"`

	// HasChecklist is true when .semspec/checklist.json exists.
	HasChecklist bool `json:"has_checklist"`

	// HasStandards is true when .semspec/standards.json exists.
	HasStandards bool `json:"has_standards"`

	// SOPCount is the number of .md files in .semspec/sources/docs/.
	SOPCount int `json:"sop_count"`

	// WorkspacePath is the absolute path to the repository root.
	WorkspacePath string `json:"workspace_path"`
}

// File path constants for project initialization artifacts.
const (
	// ProjectConfigFile is the file name for project metadata.
	ProjectConfigFile = "project.json"

	// ChecklistFile is the file name for the quality gate checklist.
	ChecklistFile = "checklist.json"

	// StandardsFile is the file name for project standards.
	StandardsFile = "standards.json"
)

// DetectionResult is the output of the stack detector.
// It is returned by the /api/project/detect endpoint and used to seed
// the checklist and project config during wizard confirmation.
type DetectionResult struct {
	// Languages are the detected programming languages.
	Languages []DetectedLanguage `json:"languages"`

	// Frameworks are the detected frameworks.
	Frameworks []DetectedFramework `json:"frameworks"`

	// Tooling are the detected development tools.
	Tooling []DetectedTool `json:"tooling"`

	// ExistingDocs are documentation files found in the repository.
	ExistingDocs []DetectedDoc `json:"existing_docs"`

	// ProposedChecklist is the deterministic set of quality gate checks
	// derived from the detected languages and tooling.
	ProposedChecklist []Check `json:"proposed_checklist"`
}

// DetectedLanguage describes a language found in the repository.
type DetectedLanguage struct {
	// Name is the language name (e.g., "Go", "TypeScript").
	Name string `json:"name"`

	// Version is the detected language version (nil when not detectable).
	Version *string `json:"version"`

	// Marker is the file that triggered detection (e.g., "go.mod").
	Marker string `json:"marker"`

	// Confidence is the detection confidence level.
	Confidence DetectionConfidence `json:"confidence"`

	// Primary indicates whether this is the primary language of the repository.
	// The first detected language is marked primary.
	Primary bool `json:"primary,omitempty"`
}

// DetectedFramework describes a framework found in the repository.
type DetectedFramework struct {
	// Name is the framework name (e.g., "SvelteKit", "React").
	Name string `json:"name"`

	// Language is the language the framework belongs to.
	Language string `json:"language"`

	// Marker is the file or signal that triggered detection.
	Marker string `json:"marker"`

	// Confidence is the detection confidence level.
	Confidence DetectionConfidence `json:"confidence"`
}

// DetectedTool describes a development tool found in the repository.
type DetectedTool struct {
	// Name is the tool name (e.g., "ESLint", "golangci-lint").
	Name string `json:"name"`

	// Category groups the tool by function.
	Category ToolCategory `json:"category"`

	// Language is the language the tool targets (empty for language-agnostic tools).
	Language string `json:"language,omitempty"`

	// Marker is the file that triggered detection.
	Marker string `json:"marker"`
}

// DetectedDoc describes a documentation file found in the repository.
type DetectedDoc struct {
	// Path is the file path relative to the repository root.
	Path string `json:"path"`

	// Type classifies the document's purpose.
	Type DocType `json:"type"`

	// SizeBytes is the file size.
	SizeBytes int64 `json:"size_bytes"`
}

// DetectionConfidence represents how certain the detector is about a result.
type DetectionConfidence string

const (
	// ConfidenceHigh means a definitive primary marker file was found.
	ConfidenceHigh DetectionConfidence = "high"

	// ConfidenceMedium means only secondary signals were found.
	ConfidenceMedium DetectionConfidence = "medium"
)

// ToolCategory classifies a development tool by its function.
type ToolCategory string

const (
	// ToolCategoryLinter classifies linting tools.
	ToolCategoryLinter ToolCategory = "linter"

	// ToolCategoryFormatter classifies code formatting tools.
	ToolCategoryFormatter ToolCategory = "formatter"

	// ToolCategoryTaskRunner classifies task runner tools.
	ToolCategoryTaskRunner ToolCategory = "task_runner"

	// ToolCategoryCI classifies continuous integration tools.
	ToolCategoryCI ToolCategory = "ci"

	// ToolCategoryContainer classifies container tooling.
	ToolCategoryContainer ToolCategory = "container"

	// ToolCategoryTestFramework classifies test framework tools.
	ToolCategoryTestFramework ToolCategory = "test_framework"

	// ToolCategoryTypeChecker classifies type checking tools.
	ToolCategoryTypeChecker ToolCategory = "type_checker"
)

// DocType classifies a documentation file by its purpose.
type DocType string

const (
	// DocTypeProjectDocs is a general project documentation file (e.g., README.md).
	DocTypeProjectDocs DocType = "project_docs"

	// DocTypeContributing is a contribution guide (e.g., CONTRIBUTING.md).
	DocTypeContributing DocType = "contributing"

	// DocTypeClaudeInstructions is a Claude-specific instructions file (CLAUDE.md).
	DocTypeClaudeInstructions DocType = "claude_instructions"

	// DocTypeExistingSOP is an SOP file already ingested under .semspec/sources/docs/.
	DocTypeExistingSOP DocType = "existing_sop"

	// DocTypeArchitectureDocs is an architecture or convention document.
	DocTypeArchitectureDocs DocType = "architecture_docs"
)
