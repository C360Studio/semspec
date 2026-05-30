package specimport

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// StructuralResult is the outcome of a Layer-1 structural pre-flight.
// OK is true when the change directory is well-formed enough to attempt
// translation. Findings always contains diagnostic detail; non-empty
// Findings with severity=error means OK is false.
type StructuralResult struct {
	OK          bool                 `json:"ok"`
	ChangeName  string               `json:"change_name"`
	ChangePath  string               `json:"change_path"`
	Findings    []StructuralFinding  `json:"findings,omitempty"`
	Proposal    StructuralProposal   `json:"proposal,omitempty"`
	Specs       map[string]SpecCheck `json:"specs,omitempty"`
	HasDesign   bool                 `json:"has_design,omitempty"`
	HasTasks    bool                 `json:"has_tasks,omitempty"`
	HasOpenYAML bool                 `json:"has_openspec_yaml,omitempty"`
	Schema      string               `json:"schema,omitempty"` // value from .openspec.yaml
}

// StructuralFinding describes one issue with the change layout. Mirrors
// the shape of PlanReviewFinding deliberately so plan-manager's existing
// finding-formatting can render structural import errors back to the
// operator without bespoke shaping code.
type StructuralFinding struct {
	Code       string `json:"code"`     // e.g. "missing_proposal"
	Severity   string `json:"severity"` // "error" | "warning"
	Path       string `json:"path,omitempty"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

// StructuralProposal records proposal.md presence + the capability names
// extracted from its ## headers. The translator uses CapabilityNames as
// the expected-capability set when reading from the graph.
type StructuralProposal struct {
	Exists           bool     `json:"exists"`
	Path             string   `json:"path,omitempty"`
	CapabilityNames  []string `json:"capability_names,omitempty"` // kebab-case
	HasWhySection    bool     `json:"has_why_section,omitempty"`
	HasChangeSection bool     `json:"has_change_section,omitempty"`
}

// SpecCheck records per-capability spec.md presence + headline shape.
type SpecCheck struct {
	Exists bool   `json:"exists"`
	Path   string `json:"path,omitempty"`
	// HasRequirements is true when at least one ### Requirement: header
	// appears in spec.md. Coarse heuristic — the deeper validation runs
	// post-graph-ingest in the translator.
	HasRequirements bool `json:"has_requirements,omitempty"`
}

// StructuralCheck validates an `openspec/changes/<name>/` directory for
// importability per ADR-040 Move 4. Returns OK=true when the layout is
// sufficient to attempt graph-driven translation; OK=false with one or
// more error-severity findings otherwise.
//
// Filesystem-only — no LLM calls, no graph reads, no network.
func StructuralCheck(changePath string) (*StructuralResult, error) {
	if changePath == "" {
		return nil, errors.New("change path is required")
	}
	info, err := os.Stat(changePath)
	if err != nil {
		return nil, fmt.Errorf("stat change path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("change path %q is not a directory", changePath)
	}

	res := &StructuralResult{
		ChangeName: filepath.Base(changePath),
		ChangePath: changePath,
		Specs:      make(map[string]SpecCheck),
	}

	checkProposal(changePath, res)
	checkSpecs(changePath, res)
	checkOptionalFiles(changePath, res)
	checkOpenSpecYAML(changePath, res)

	res.OK = !hasErrorFinding(res.Findings)
	return res, nil
}

func checkProposal(changePath string, res *StructuralResult) {
	proposalPath := filepath.Join(changePath, "proposal.md")
	res.Proposal.Path = proposalPath
	data, err := os.ReadFile(proposalPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			res.Findings = append(res.Findings, StructuralFinding{
				Code:       "missing_proposal",
				Severity:   "error",
				Path:       proposalPath,
				Message:    "proposal.md is required for OpenSpec import",
				Suggestion: "Create proposal.md with at least one ## Capability section.",
			})
		} else {
			res.Findings = append(res.Findings, StructuralFinding{
				Code:     "proposal_read_error",
				Severity: "error",
				Path:     proposalPath,
				Message:  err.Error(),
			})
		}
		return
	}
	res.Proposal.Exists = true
	body := string(data)
	res.Proposal.CapabilityNames = extractCapabilityNamesFromProposal(body)
	res.Proposal.HasWhySection = headerExists(body, "## why")
	res.Proposal.HasChangeSection = headerExists(body, "## what changes")
	if len(res.Proposal.CapabilityNames) == 0 {
		res.Findings = append(res.Findings, StructuralFinding{
			Code:       "proposal_no_capabilities",
			Severity:   "error",
			Path:       proposalPath,
			Message:    "proposal.md does not declare any capabilities",
			Suggestion: "Add one or more `- \\`cap-name\\` — description` entries under a ## What Changes section.",
		})
	}
}

func checkSpecs(changePath string, res *StructuralResult) {
	if len(res.Proposal.CapabilityNames) == 0 {
		return // already reported via proposal_no_capabilities
	}
	specsRoot := filepath.Join(changePath, "specs")
	for _, capName := range res.Proposal.CapabilityNames {
		specPath := filepath.Join(specsRoot, capName, "spec.md")
		check := SpecCheck{Path: specPath}
		data, err := os.ReadFile(specPath)
		if err != nil {
			res.Findings = append(res.Findings, StructuralFinding{
				Code:       "missing_spec",
				Severity:   "error",
				Path:       specPath,
				Message:    fmt.Sprintf("Capability %q declared in proposal but specs/%s/spec.md is missing", capName, capName),
				Suggestion: "Create the spec.md for this capability or remove the capability from proposal.md.",
			})
			res.Specs[capName] = check
			continue
		}
		check.Exists = true
		body := string(data)
		check.HasRequirements = strings.Contains(strings.ToLower(body), "### requirement:")
		if !check.HasRequirements {
			res.Findings = append(res.Findings, StructuralFinding{
				Code:       "spec_no_requirements",
				Severity:   "warning",
				Path:       specPath,
				Message:    fmt.Sprintf("spec.md for %q has no ### Requirement: headers", capName),
				Suggestion: "Add one or more ### Requirement: blocks. (Warning only — the translator may still find requirement entities in the graph.)",
			})
		}
		res.Specs[capName] = check
	}
}

func checkOptionalFiles(changePath string, res *StructuralResult) {
	_, err := os.Stat(filepath.Join(changePath, "design.md"))
	res.HasDesign = err == nil
	_, err = os.Stat(filepath.Join(changePath, "tasks.md"))
	res.HasTasks = err == nil
}

func checkOpenSpecYAML(changePath string, res *StructuralResult) {
	yamlPath := filepath.Join(changePath, ".openspec.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		// Missing .openspec.yaml is a warning, not a hard reject —
		// imports work with the default schema.
		res.Findings = append(res.Findings, StructuralFinding{
			Code:       "missing_openspec_yaml",
			Severity:   "warning",
			Path:       yamlPath,
			Message:    ".openspec.yaml is missing; assuming schema=spec-driven default.",
			Suggestion: "Add .openspec.yaml with `schema: spec-driven` for explicit adopter tooling compatibility.",
		})
		res.Schema = "spec-driven"
		return
	}
	res.HasOpenYAML = true
	// Minimal parse: look for `schema:` line. Full YAML parse would
	// pull a dependency; the only field we care about is schema. Strip
	// inline `# comment` suffix so `schema: spec-driven  # default`
	// parses to `spec-driven` not `spec-driven  # default`. Per
	// go-reviewer PR 4 audit #6.
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "schema:") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "schema:"))
			if hashIdx := strings.Index(value, "#"); hashIdx >= 0 {
				value = strings.TrimSpace(value[:hashIdx])
			}
			res.Schema = value
			break
		}
	}
	if res.Schema == "" {
		res.Schema = "spec-driven"
	}
	if res.Schema != "spec-driven" {
		res.Findings = append(res.Findings, StructuralFinding{
			Code:       "unsupported_schema",
			Severity:   "error",
			Path:       yamlPath,
			Message:    fmt.Sprintf(".openspec.yaml declares schema=%q but semspec only imports schema=spec-driven", res.Schema),
			Suggestion: "Convert the change to spec-driven schema, or wait for semspec to add the variant.",
		})
	}
}

// extractCapabilityNamesFromProposal scans proposal.md for capability
// identifiers. Recognises two shapes:
//
//   - `cap-name` — description  (preferred: matches PR 3 emitter output)
//   - ### `cap-name`            (alternate header form)
//
// Names are returned in document order, deduplicated.
func extractCapabilityNamesFromProposal(body string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, line := range strings.Split(body, "\n") {
		name := extractTickedName(strings.TrimSpace(line))
		if name == "" {
			continue
		}
		if !looksKebabCase(name) {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// extractTickedName pulls the first backtick-wrapped token from a line
// when the line starts with a markdown bullet ("- ", "* ", "+ ") or a
// "### " header. Returns "" when neither shape matches. Per go-reviewer
// PR 4 audit #8 — `*` and `+` are also legal markdown bullets and some
// adopter conventions prefer them over `-`.
func extractTickedName(line string) string {
	var content string
	switch {
	case strings.HasPrefix(line, "- "):
		content = strings.TrimPrefix(line, "- ")
	case strings.HasPrefix(line, "* "):
		content = strings.TrimPrefix(line, "* ")
	case strings.HasPrefix(line, "+ "):
		content = strings.TrimPrefix(line, "+ ")
	case strings.HasPrefix(line, "### "):
		content = strings.TrimPrefix(line, "### ")
	default:
		return ""
	}
	start := strings.Index(content, "`")
	if start < 0 {
		return ""
	}
	end := strings.Index(content[start+1:], "`")
	if end < 0 {
		return ""
	}
	return content[start+1 : start+1+end]
}

func looksKebabCase(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return false
		}
	}
	return true
}

func headerExists(body, header string) bool {
	low := strings.ToLower(body)
	for _, line := range strings.Split(low, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), header) {
			return true
		}
	}
	return false
}

func hasErrorFinding(findings []StructuralFinding) bool {
	for _, f := range findings {
		if f.Severity == "error" {
			return true
		}
	}
	return false
}
