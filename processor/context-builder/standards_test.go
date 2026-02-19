package contextbuilder

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// writeStandardsFile writes a workflow.Standards value to a temp file and
// returns the directory and file path so tests can configure the builder.
func writeStandardsFile(t *testing.T, dir string, s workflow.Standards) string {
	t.Helper()

	semspecDir := filepath.Join(dir, ".semspec")
	if err := os.MkdirAll(semspecDir, 0o755); err != nil {
		t.Fatalf("create .semspec dir: %v", err)
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal standards: %v", err)
	}

	path := filepath.Join(semspecDir, "standards.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write standards file: %v", err)
	}

	return path
}

// newTestBuilder creates a Builder configured against a temp directory.
// The caller controls StandardsPath and StandardsMaxTokens via the config.
func newTestBuilder(t *testing.T, repoPath string, standardsPath string, maxTokens int) *Builder {
	t.Helper()

	cfg := DefaultConfig()
	cfg.RepoPath = repoPath
	cfg.StandardsPath = standardsPath
	cfg.StandardsMaxTokens = maxTokens

	return &Builder{
		logger: slog.Default(),
		config: cfg,
	}
}

// --- loadStandardsPreamble ---

func TestLoadStandardsPreamble_MissingFile(t *testing.T) {
	dir := t.TempDir()
	b := newTestBuilder(t, dir, ".semspec/standards.json", 1000)

	preamble, tokens := b.loadStandardsPreamble()

	if preamble != "" {
		t.Errorf("expected empty preamble for missing file, got %q", preamble)
	}
	if tokens != 0 {
		t.Errorf("expected 0 tokens for missing file, got %d", tokens)
	}
}

func TestLoadStandardsPreamble_MalformedFile(t *testing.T) {
	dir := t.TempDir()
	semspecDir := filepath.Join(dir, ".semspec")
	if err := os.MkdirAll(semspecDir, 0o755); err != nil {
		t.Fatalf("create .semspec dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(semspecDir, "standards.json"), []byte("not valid json {{{"), 0o644); err != nil {
		t.Fatalf("write malformed file: %v", err)
	}

	b := newTestBuilder(t, dir, ".semspec/standards.json", 1000)
	preamble, tokens := b.loadStandardsPreamble()

	if preamble != "" {
		t.Errorf("expected empty preamble for malformed file, got %q", preamble)
	}
	if tokens != 0 {
		t.Errorf("expected 0 tokens for malformed file, got %d", tokens)
	}
}

func TestLoadStandardsPreamble_EmptyRules(t *testing.T) {
	dir := t.TempDir()
	writeStandardsFile(t, dir, workflow.Standards{
		Version:     "1.0.0",
		GeneratedAt: time.Now(),
		Rules:       []workflow.Rule{},
	})

	b := newTestBuilder(t, dir, ".semspec/standards.json", 1000)
	preamble, tokens := b.loadStandardsPreamble()

	if preamble != "" {
		t.Errorf("expected empty preamble for empty rules, got %q", preamble)
	}
	if tokens != 0 {
		t.Errorf("expected 0 tokens for empty rules, got %d", tokens)
	}
}

func TestLoadStandardsPreamble_FormatsSingleRule(t *testing.T) {
	dir := t.TempDir()
	writeStandardsFile(t, dir, workflow.Standards{
		Version:     "1.0.0",
		GeneratedAt: time.Now(),
		Rules: []workflow.Rule{
			{
				ID:       "test-coverage",
				Text:     "All new code must include tests.",
				Severity: workflow.RuleSeverityError,
				Category: "testing",
				Origin:   workflow.RuleOriginInit,
			},
		},
	})

	b := newTestBuilder(t, dir, ".semspec/standards.json", 1000)
	preamble, tokens := b.loadStandardsPreamble()

	if preamble == "" {
		t.Fatal("expected non-empty preamble")
	}
	if tokens <= 0 {
		t.Errorf("expected positive token estimate, got %d", tokens)
	}

	// Verify required preamble sections are present.
	if !strings.Contains(preamble, "## Project Standards (Always Active)") {
		t.Errorf("preamble missing header section:\n%s", preamble)
	}
	if !strings.Contains(preamble, "[ERROR]") {
		t.Errorf("preamble missing [ERROR] severity tag:\n%s", preamble)
	}
	if !strings.Contains(preamble, "All new code must include tests.") {
		t.Errorf("preamble missing rule text:\n%s", preamble)
	}
}

func TestLoadStandardsPreamble_SeverityOrdering(t *testing.T) {
	dir := t.TempDir()
	writeStandardsFile(t, dir, workflow.Standards{
		Version:     "1.0.0",
		GeneratedAt: time.Now(),
		// Intentionally out of order: info before error before warning.
		Rules: []workflow.Rule{
			{
				ID:       "info-rule",
				Text:     "This is informational.",
				Severity: workflow.RuleSeverityInfo,
				Origin:   workflow.RuleOriginInit,
			},
			{
				ID:       "error-rule",
				Text:     "This is an error rule.",
				Severity: workflow.RuleSeverityError,
				Origin:   workflow.RuleOriginInit,
			},
			{
				ID:       "warning-rule",
				Text:     "This is a warning rule.",
				Severity: workflow.RuleSeverityWarning,
				Origin:   workflow.RuleOriginInit,
			},
		},
	})

	b := newTestBuilder(t, dir, ".semspec/standards.json", 5000)
	preamble, _ := b.loadStandardsPreamble()

	if preamble == "" {
		t.Fatal("expected non-empty preamble")
	}

	errorPos := strings.Index(preamble, "This is an error rule.")
	warningPos := strings.Index(preamble, "This is a warning rule.")
	infoPos := strings.Index(preamble, "This is informational.")

	if errorPos < 0 || warningPos < 0 || infoPos < 0 {
		t.Fatalf("one or more rule texts not found in preamble:\n%s", preamble)
	}
	if errorPos > warningPos {
		t.Errorf("error rule should appear before warning rule (error=%d, warning=%d)", errorPos, warningPos)
	}
	if warningPos > infoPos {
		t.Errorf("warning rule should appear before info rule (warning=%d, info=%d)", warningPos, infoPos)
	}
}

func TestLoadStandardsPreamble_SeverityUpperCase(t *testing.T) {
	dir := t.TempDir()
	writeStandardsFile(t, dir, workflow.Standards{
		Version:     "1.0.0",
		GeneratedAt: time.Now(),
		Rules: []workflow.Rule{
			{ID: "e", Text: "Error rule.", Severity: workflow.RuleSeverityError, Origin: workflow.RuleOriginInit},
			{ID: "w", Text: "Warning rule.", Severity: workflow.RuleSeverityWarning, Origin: workflow.RuleOriginInit},
			{ID: "i", Text: "Info rule.", Severity: workflow.RuleSeverityInfo, Origin: workflow.RuleOriginInit},
		},
	})

	b := newTestBuilder(t, dir, ".semspec/standards.json", 5000)
	preamble, _ := b.loadStandardsPreamble()

	for _, tag := range []string{"[ERROR]", "[WARNING]", "[INFO]"} {
		if !strings.Contains(preamble, tag) {
			t.Errorf("preamble missing severity tag %q:\n%s", tag, preamble)
		}
	}
	// Lowercase versions must not appear as standalone tags.
	for _, tag := range []string{"[error]", "[warning]", "[info]"} {
		if strings.Contains(preamble, tag) {
			t.Errorf("preamble should not contain lowercase tag %q:\n%s", tag, preamble)
		}
	}
}

func TestLoadStandardsPreamble_TokenBudgetTruncation(t *testing.T) {
	dir := t.TempDir()

	// Create many rules that will exceed a tight token budget.
	rules := make([]workflow.Rule, 20)
	for i := range rules {
		rules[i] = workflow.Rule{
			ID:       "rule",
			Text:     "This is a relatively long rule text that consumes a moderate number of tokens per entry.",
			Severity: workflow.RuleSeverityError,
			Origin:   workflow.RuleOriginInit,
		}
	}
	writeStandardsFile(t, dir, workflow.Standards{
		Version:     "1.0.0",
		GeneratedAt: time.Now(),
		Rules:       rules,
	})

	// Set a very tight token budget (100 tokens) to force truncation.
	b := newTestBuilder(t, dir, ".semspec/standards.json", 100)
	preamble, tokens := b.loadStandardsPreamble()

	if preamble == "" {
		t.Fatal("expected non-empty preamble even with tight budget")
	}

	// The returned token count must not exceed the configured max.
	if tokens > 100 {
		t.Errorf("token count %d exceeds max %d", tokens, 100)
	}

	// A truncation notice should appear.
	if !strings.Contains(preamble, "truncated") {
		t.Errorf("expected truncation notice in preamble:\n%s", preamble)
	}
}

func TestLoadStandardsPreamble_AbsoluteStandardsPath(t *testing.T) {
	dir := t.TempDir()
	// Write the file at an absolute path outside the repo root.
	absPath := filepath.Join(dir, "custom-standards.json")

	data, err := json.Marshal(workflow.Standards{
		Version:     "1.0.0",
		GeneratedAt: time.Now(),
		Rules: []workflow.Rule{
			{
				ID:       "custom",
				Text:     "Custom absolute path rule.",
				Severity: workflow.RuleSeverityWarning,
				Origin:   workflow.RuleOriginManual,
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(absPath, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Use an absolute standards path; repo_path should be ignored for path resolution.
	b := newTestBuilder(t, "/some/other/repo", absPath, 1000)
	preamble, tokens := b.loadStandardsPreamble()

	if preamble == "" {
		t.Fatal("expected non-empty preamble for absolute path")
	}
	if tokens <= 0 {
		t.Errorf("expected positive token count, got %d", tokens)
	}
	if !strings.Contains(preamble, "Custom absolute path rule.") {
		t.Errorf("rule text missing from preamble:\n%s", preamble)
	}
}

// --- Build() integration: standards injected into Documents ---

func TestBuild_StandardsInjectedIntoDocuments(t *testing.T) {
	dir := t.TempDir()
	writeStandardsFile(t, dir, workflow.Standards{
		Version:     "1.0.0",
		GeneratedAt: time.Now(),
		Rules: []workflow.Rule{
			{
				ID:       "test-coverage",
				Text:     "All new code must include tests.",
				Severity: workflow.RuleSeverityError,
				Origin:   workflow.RuleOriginInit,
			},
		},
	})

	// Build a minimal builder that will exercise the standards injection path.
	// We use a nil model registry and stub gatherers to avoid NATS/graph
	// dependencies — the exploration strategy gracefully handles empty graph
	// responses, so this is safe for unit testing purposes.
	cfg := DefaultConfig()
	cfg.RepoPath = dir
	cfg.StandardsPath = ".semspec/standards.json"
	cfg.StandardsMaxTokens = 1000

	b := NewBuilder(cfg, nil, slog.Default())

	req := &ContextBuildRequest{
		RequestID: "req-standards-test",
		TaskType:  TaskTypeExploration,
		Topic:     "testing",
	}

	ctx := t.Context()
	resp, err := b.Build(ctx, req)
	if err != nil {
		t.Fatalf("Build() returned unexpected error: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("Build() response contained error: %s", resp.Error)
	}

	if resp.Documents == nil {
		t.Fatal("expected non-nil Documents map")
	}
	standards, ok := resp.Documents["__standards__"]
	if !ok {
		t.Fatalf("expected __standards__ key in Documents, got keys: %v", documentKeys(resp.Documents))
	}
	if !strings.Contains(standards, "All new code must include tests.") {
		t.Errorf("__standards__ missing expected rule text:\n%s", standards)
	}
}

func TestBuild_NoStandardsFileDoesNotFail(t *testing.T) {
	dir := t.TempDir()
	// No standards.json written — graceful degradation expected.

	cfg := DefaultConfig()
	cfg.RepoPath = dir
	cfg.StandardsPath = ".semspec/standards.json"
	cfg.StandardsMaxTokens = 1000

	b := NewBuilder(cfg, nil, slog.Default())

	req := &ContextBuildRequest{
		RequestID: "req-no-standards",
		TaskType:  TaskTypeExploration,
		Topic:     "testing",
	}

	ctx := t.Context()
	resp, err := b.Build(ctx, req)
	if err != nil {
		t.Fatalf("Build() should not error when standards file is missing: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("Build() response should not error when standards file is missing: %s", resp.Error)
	}
	if resp.Documents != nil {
		if _, ok := resp.Documents["__standards__"]; ok {
			t.Error("__standards__ key should not be present when no standards file exists")
		}
	}
}

// documentKeys returns the keys of a map for use in test error messages.
func documentKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
