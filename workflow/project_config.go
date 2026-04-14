package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"
)

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

	// Org is the organization segment for entity IDs (default: "semspec").
	// Used as the first part of the 6-part entity ID: org.platform.domain.system.type.instance.
	Org string `json:"org,omitempty"`

	// Platform is the project identifier segment for entity IDs (default: "local").
	// Should be a short slug unique within your org to avoid collisions when
	// federating entities across semspec instances (e.g., "dragon", "semspec").
	// Auto-derived from project Name if not set explicitly.
	Platform string `json:"platform,omitempty"`

	// InitializedAt is when the project was first initialized.
	InitializedAt time.Time `json:"initialized_at"`

	// ApprovedAt is when the human approved this config. Nil means pending review.
	ApprovedAt *time.Time `json:"approved_at,omitempty"`

	// UpdatedAt is the last mutation timestamp, used for graph vs file reconciliation.
	UpdatedAt time.Time `json:"updated_at,omitempty"`

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

	// ApprovedAt is when the human approved this checklist. Nil means pending review.
	ApprovedAt *time.Time `json:"approved_at,omitempty"`

	// UpdatedAt is the last mutation timestamp, used for graph vs file reconciliation.
	UpdatedAt time.Time `json:"updated_at,omitempty"`

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

	// CheckCategorySetup installs dependencies before other checks run.
	CheckCategorySetup CheckCategory = "setup"
)

// Standards is the top-level structure written to .semspec/standards.json.
// Items are injected into agent context as a preamble before
// strategy-specific content. Standards grow over time as SOPs are authored.
type Standards struct {
	// Version is the standards schema version.
	Version string `json:"version"`

	// GeneratedAt is when the standards were last generated or regenerated.
	GeneratedAt time.Time `json:"generated_at"`

	// ApprovedAt is when the human approved these standards. Nil means pending review.
	ApprovedAt *time.Time `json:"approved_at,omitempty"`

	// UpdatedAt is the last mutation timestamp, used for graph vs file reconciliation.
	UpdatedAt time.Time `json:"updated_at,omitempty"`

	// TokenEstimate is the approximate token count for all items combined.
	// Used by the context-builder to account for standards in the token budget.
	TokenEstimate int `json:"token_estimate"`

	// Items is the ordered list of project standards.
	Items []Standard `json:"items"`
}

// Standard is a single project standard injected into agent context.
type Standard struct {
	// ID is the unique, stable identifier for this standard (e.g., "test-coverage").
	ID string `json:"id"`

	// Text is the standard statement in plain English. It should be a single
	// concrete sentence that an agent can follow without ambiguity.
	Text string `json:"text"`

	// Severity controls how violations are treated by the reviewer.
	Severity StandardSeverity `json:"severity"`

	// Category groups related standards (e.g., "testing", "code-quality").
	Category string `json:"category"`

	// AppliesTo is a list of glob patterns for files this standard governs.
	// An empty list means the standard applies to all files.
	AppliesTo []string `json:"applies_to,omitempty"`

	// Roles lists which agent roles this standard applies to (e.g., "developer", "reviewer").
	// An empty list means the standard applies to all roles.
	Roles []string `json:"roles,omitempty"`

	// Origin tracks where this standard came from, enabling the future flywheel.
	Origin string `json:"origin"`
}

// StandardSeverity controls how a standard violation is treated.
type StandardSeverity string

const (
	// StandardSeverityMust means a violation should block reviewer approval.
	StandardSeverityMust StandardSeverity = "must"

	// StandardSeverityShould means a violation is surfaced but does not block.
	StandardSeverityShould StandardSeverity = "should"

	// StandardSeverityMay means a violation is informational only.
	StandardSeverityMay StandardSeverity = "may"
)

// StandardOrigin constants describe the source of a standard.
const (
	// StandardOriginInit marks standards generated during project initialization.
	StandardOriginInit = "init"

	// StandardOriginManual marks standards added directly by the user.
	StandardOriginManual = "manual"

	// StandardOriginReviewPattern marks standards promoted from recurring review feedback.
	StandardOriginReviewPattern = "review-pattern"
)

// StandardOriginSOP returns the origin string for a standard derived from an SOP file.
// Format: "sop:<filename>" (e.g., "sop:go-conventions.md").
func StandardOriginSOP(filename string) string {
	return "sop:" + filename
}

// BaselineStandards returns the default standards seeded during project
// initialization. Covers OWASP security fundamentals and language-agnostic
// engineering hygiene. Scoped to developer + reviewer roles so they appear
// in both behavioral gates and structural checklists.
func BaselineStandards() []Standard {
	return []Standard{
		// Security baseline (universal)
		{
			ID:       "sec-no-secrets",
			Text:     "Source code must not contain hardcoded credentials, API keys, passwords, or tokens. Use environment variables or a secrets manager.",
			Severity: StandardSeverityMust,
			Category: "security",
			Roles:    []string{"developer", "reviewer"},
			Origin:   StandardOriginInit,
		},
		{
			ID:       "sec-input-validation",
			Text:     "All external input (HTTP parameters, file paths, user strings) must be validated at the trust boundary before reaching business logic.",
			Severity: StandardSeverityMust,
			Category: "security",
			Roles:    []string{"developer", "reviewer"},
			Origin:   StandardOriginInit,
		},
		{
			ID:       "sec-safe-errors",
			Text:     "Error responses to external consumers must not expose stack traces, file paths, or internal implementation details. Log details server-side.",
			Severity: StandardSeverityShould,
			Category: "security",
			Roles:    []string{"developer", "reviewer"},
			Origin:   StandardOriginInit,
		},
		{
			ID:        "sec-parameterized-queries",
			Text:      "Database queries must use parameterized statements. Never concatenate user input into SQL or shell commands.",
			Severity:  StandardSeverityMust,
			Category:  "security",
			AppliesTo: []string{"**/*repo*", "**/*store*", "**/*query*", "**/*db*"},
			Roles:     []string{"developer", "reviewer"},
			Origin:    StandardOriginInit,
		},
		{
			ID:        "sec-auth-checks",
			Text:      "Every authenticated endpoint must verify authorization before processing the request.",
			Severity:  StandardSeverityShould,
			Category:  "security",
			AppliesTo: []string{"**/*handler*", "**/*controller*", "**/*route*", "**/*middleware*"},
			Roles:     []string{"developer", "reviewer", "scenario-reviewer"},
			Origin:    StandardOriginInit,
		},
		// Engineering baseline (language-agnostic)
		{
			ID:       "eng-test-coverage",
			Text:     "All new or modified behavior must have corresponding tests, and each test must trace back to a specific scenario or requirement (referenced by ID in a comment, test name, or description). Untested code is unfinished code; untraceable tests are unverifiable claims.",
			Severity: StandardSeverityMust,
			Category: "engineering",
			Roles:    []string{"developer", "reviewer"},
			Origin:   StandardOriginInit,
		},
		{
			ID:       "eng-clean-diffs",
			Text:     "Changes must be limited to what the task requires. Do not refactor, add features, or \"improve\" code outside the task scope. Report problems you notice in other code, but do not fix them in this changeset.",
			Severity: StandardSeverityShould,
			Category: "engineering",
			Roles:    []string{"developer"},
			Origin:   StandardOriginInit,
		},
		{
			ID:       "eng-control-flow-depth",
			Text:     "Functions must not exceed two levels of nesting. Flatten conditional chains — if you need a diagram to follow the logic, it has failed.",
			Severity: StandardSeverityShould,
			Category: "engineering",
			Roles:    []string{"developer", "reviewer"},
			Origin:   StandardOriginInit,
		},
		{
			ID:       "eng-loop-bounds",
			Text:     "Every loop must have an explicit, enforced upper bound. Unbounded retry loops, polling loops, and recursive crawlers are not permitted.",
			Severity: StandardSeverityMust,
			Category: "engineering",
			Roles:    []string{"developer", "reviewer"},
			Origin:   StandardOriginInit,
		},
		{
			ID:       "eng-function-single-responsibility",
			Text:     "Each function must do exactly one thing, describable in a single sentence without the word 'and'. If a function requires the word 'and' to describe, decompose it.",
			Severity: StandardSeverityShould,
			Category: "engineering",
			Roles:    []string{"developer", "reviewer"},
			Origin:   StandardOriginInit,
		},
		{
			ID:       "eng-assertion-density",
			Text:     "Non-trivial functions must validate their preconditions explicitly at the entry point. Implicit assumptions are not acceptable — make them checkable.",
			Severity: StandardSeverityShould,
			Category: "engineering",
			Roles:    []string{"developer", "reviewer"},
			Origin:   StandardOriginInit,
		},
		{
			ID:       "eng-error-handling",
			Text:     "All errors must be handled, logged, or explicitly propagated. Empty catch blocks, ignored return values, and bare error discards are not permitted.",
			Severity: StandardSeverityMust,
			Category: "engineering",
			Roles:    []string{"developer", "reviewer"},
			Origin:   StandardOriginInit,
		},
		{
			ID:       "eng-state-scope",
			Text:     "Declare all variables at the narrowest possible scope. Package-level, module-level, and class-level state requires architectural justification. Pass dependencies explicitly at call sites.",
			Severity: StandardSeverityShould,
			Category: "engineering",
			Roles:    []string{"developer", "reviewer"},
			Origin:   StandardOriginInit,
		},
		{
			ID:       "eng-side-effect-visibility",
			Text:     "Functions that perform I/O, mutations, or network calls must make this obvious from their name and call site. Pure computation must be structurally separate from side-effectful operations. Never bury writes or network calls inside helpers with innocent-looking names.",
			Severity: StandardSeverityMust,
			Category: "engineering",
			Roles:    []string{"developer", "reviewer"},
			Origin:   StandardOriginInit,
		},
		{
			ID:       "eng-abstraction-depth",
			Text:     "Prefer direct, linearly readable code over elegant abstractions that require decoding. Every layer of indirection must be justified. If something breaks, you must be able to trace execution by reading the code.",
			Severity: StandardSeverityShould,
			Category: "engineering",
			Roles:    []string{"developer", "reviewer"},
			Origin:   StandardOriginInit,
		},
		{
			ID:       "eng-resource-lifecycle",
			Text:     "Every resource acquired (file handle, lock, connection, allocation, subscription) must be explicitly released, including on all error paths. Use the language's idiomatic scoped-cleanup pattern (defer, with, try-with-resources, RAII, using). Resource lifetime must be declared, not assumed.",
			Severity: StandardSeverityMust,
			Category: "engineering",
			Roles:    []string{"developer", "reviewer"},
			Origin:   StandardOriginInit,
		},
		{
			ID:       "eng-warnings-as-errors",
			Text:     "All compiler warnings and linter violations are treated as errors. Zero-warning policy with no exceptions.",
			Severity: StandardSeverityMust,
			Category: "engineering",
			Roles:    []string{"developer", "reviewer"},
			Origin:   StandardOriginInit,
		},
	}
}

// ForRole returns items that apply to the given role.
// Standards with an empty Roles list apply to all roles.
func (s Standards) ForRole(role string) []Standard {
	var filtered []Standard
	for _, item := range s.Items {
		if len(item.Roles) == 0 || slices.Contains(item.Roles, role) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// LoadStandardsFromDisk reads and parses .semspec/standards.json from the given repo root.
// Returns nil if the file doesn't exist or can't be parsed.
func LoadStandardsFromDisk(repoRoot string) *Standards {
	data, err := os.ReadFile(filepath.Join(repoRoot, ".semspec", StandardsFile))
	if err != nil {
		return nil
	}
	var standards Standards
	if err := json.Unmarshal(data, &standards); err != nil {
		return nil
	}
	return &standards
}

// InitStatus describes the current initialization state of the project.
// Returned by GET /api/project/status to let the UI decide whether to show
// the setup wizard or the normal activity view.
type InitStatus struct {
	// Initialized is true only when all three config files exist.
	Initialized bool `json:"initialized"`

	// ProjectName is the human-readable project name from project.json.
	ProjectName string `json:"project_name,omitempty"`

	// ProjectDescription is the project description from project.json.
	ProjectDescription string `json:"project_description,omitempty"`

	// ProjectOrg is the organization segment from project.json (default: "semspec").
	ProjectOrg string `json:"project_org,omitempty"`

	// ProjectPlatform is the project identifier segment from project.json (default: "local").
	ProjectPlatform string `json:"project_platform,omitempty"`

	// EntityPrefix is the computed "org.platform" prefix used for all entity IDs.
	EntityPrefix string `json:"entity_prefix"`

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

	// Scaffold state — tracks whether scaffold has been called and what was requested.

	// Scaffolded is true when .semspec/scaffold.json exists.
	Scaffolded bool `json:"scaffolded"`

	// ScaffoldedAt is when the scaffold was created.
	ScaffoldedAt *time.Time `json:"scaffolded_at,omitempty"`

	// ScaffoldedLanguages lists the languages requested during scaffold.
	ScaffoldedLanguages []string `json:"scaffolded_languages,omitempty"`

	// ScaffoldedFiles lists the marker files created during scaffold.
	ScaffoldedFiles []string `json:"scaffolded_files,omitempty"`

	// Per-file approval timestamps.

	// ProjectApprovedAt is when project.json was approved.
	ProjectApprovedAt *time.Time `json:"project_approved_at,omitempty"`

	// ChecklistApprovedAt is when checklist.json was approved.
	ChecklistApprovedAt *time.Time `json:"checklist_approved_at,omitempty"`

	// StandardsApprovedAt is when standards.json was approved.
	StandardsApprovedAt *time.Time `json:"standards_approved_at,omitempty"`

	// AllApproved is true when all three config files have been approved.
	AllApproved bool `json:"all_approved"`
}

// ScaffoldState is persisted to .semspec/scaffold.json to track what was scaffolded.
// The status handler reads this to populate scaffold fields in InitStatus.
type ScaffoldState struct {
	// ScaffoldedAt is when the scaffold was created.
	ScaffoldedAt time.Time `json:"scaffolded_at"`

	// Languages lists the languages requested during scaffold.
	Languages []string `json:"languages"`

	// Frameworks lists the frameworks requested during scaffold.
	Frameworks []string `json:"frameworks"`

	// FilesCreated lists the marker files created during scaffold.
	FilesCreated []string `json:"files_created"`
}

// ScaffoldFile is the file name for scaffold state.
const ScaffoldFile = "scaffold.json"

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
