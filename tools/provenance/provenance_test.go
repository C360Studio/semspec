package provenance

import (
	"testing"

	codeAst "github.com/c360studio/semspec/processor/ast"
)

func TestParseDecisionType(t *testing.T) {
	tests := []struct {
		message string
		want    string
	}{
		// Standard conventional commit formats
		{"feat: add new feature", "feat"},
		{"fix: resolve bug", "fix"},
		{"docs: update readme", "docs"},
		{"style: format code", "style"},
		{"refactor: improve structure", "refactor"},
		{"test: add unit tests", "test"},
		{"chore: update dependencies", "chore"},
		{"perf: optimize performance", "perf"},
		{"ci: update workflow", "ci"},
		{"build: configure build", "build"},
		{"revert: undo change", "revert"},

		// With scope
		{"feat(auth): add login", "feat"},
		{"fix(api): handle errors", "fix"},
		{"refactor(core): improve performance", "refactor"},

		// Non-conventional commits
		{"Update README.md", "unknown"},
		{"Initial commit", "unknown"},
		{"WIP: work in progress", "unknown"},
		{"", "unknown"},
		{"  fix: leading space", "unknown"}, // Leading space is invalid

		// Edge cases
		{"feat:no space", "feat"},      // Still valid
		{"feat(scope):no space", "feat"}, // Still valid
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			got := ParseDecisionType(tt.message)
			if got != tt.want {
				t.Errorf("ParseDecisionType(%q) = %q, want %q", tt.message, got, tt.want)
			}
		})
	}
}

func TestDecisionTriples(t *testing.T) {
	ctx := &ProvenanceContext{
		LoopID:   "loop-123",
		AgentID:  "agent-456",
		CallID:   "call-789",
		ToolName: "git_commit",
	}

	info := FileDecisionInfo{
		EntityID:   "git.decision.abc1234.12345678",
		FilePath:   "path/to/file.go",
		Operation:  "modify",
		CommitHash: "abc1234",
		Message:    "feat: add new feature",
		Branch:     "main",
		Repository: "/path/to/repo",
	}

	triples := ctx.DecisionTriples(info)

	// Check that we have the expected triples
	predicateValues := make(map[string]any)
	for _, t := range triples {
		if t.Subject == info.EntityID {
			predicateValues[t.Predicate] = t.Object
		}
	}

	// Verify core predicates
	if predicateValues["source.git.decision.type"] != "feat" {
		t.Errorf("decision type mismatch: got %v", predicateValues["source.git.decision.type"])
	}
	if predicateValues["source.git.decision.file"] != "path/to/file.go" {
		t.Errorf("decision file mismatch: got %v", predicateValues["source.git.decision.file"])
	}
	if predicateValues["source.git.decision.commit"] != "abc1234" {
		t.Errorf("decision commit mismatch: got %v", predicateValues["source.git.decision.commit"])
	}
	if predicateValues["source.git.decision.message"] != "feat: add new feature" {
		t.Errorf("decision message mismatch: got %v", predicateValues["source.git.decision.message"])
	}
	if predicateValues["source.git.decision.operation"] != "modify" {
		t.Errorf("decision operation mismatch: got %v", predicateValues["source.git.decision.operation"])
	}
	if predicateValues["source.git.decision.branch"] != "main" {
		t.Errorf("decision branch mismatch: got %v", predicateValues["source.git.decision.branch"])
	}
	if predicateValues["source.git.decision.repository"] != "/path/to/repo" {
		t.Errorf("decision repository mismatch: got %v", predicateValues["source.git.decision.repository"])
	}
	if predicateValues["source.git.decision.agent"] != "agent-456" {
		t.Errorf("decision agent mismatch: got %v", predicateValues["source.git.decision.agent"])
	}
	if predicateValues["source.git.decision.loop"] != "loop-123" {
		t.Errorf("decision loop mismatch: got %v", predicateValues["source.git.decision.loop"])
	}

	// Verify timestamp is set
	if predicateValues["source.git.decision.timestamp"] == nil {
		t.Error("decision timestamp should be set")
	}

	// Verify provenance link
	if predicateValues[ProvGeneratedBy] != "call-789" {
		t.Errorf("provenance link mismatch: got %v", predicateValues[ProvGeneratedBy])
	}
}

func TestDecisionTriplesMinimalContext(t *testing.T) {
	// Test with minimal context (no loop/agent IDs)
	ctx := &ProvenanceContext{
		CallID:   "call-123",
		ToolName: "git_commit",
	}

	info := FileDecisionInfo{
		EntityID:   "git.decision.xyz.abcdef",
		FilePath:   "file.go",
		CommitHash: "xyz",
		Message:    "fix: bug fix",
	}

	triples := ctx.DecisionTriples(info)

	// Should still have core triples
	predicateValues := make(map[string]any)
	for _, t := range triples {
		if t.Subject == info.EntityID {
			predicateValues[t.Predicate] = t.Object
		}
	}

	if predicateValues["source.git.decision.type"] != "fix" {
		t.Errorf("decision type mismatch: got %v", predicateValues["source.git.decision.type"])
	}

	// Should NOT have agent/loop if not set
	if _, ok := predicateValues["source.git.decision.agent"]; ok {
		t.Error("agent should not be set when AgentID is empty")
	}
	if _, ok := predicateValues["source.git.decision.loop"]; ok {
		t.Error("loop should not be set when LoopID is empty")
	}
}

func TestDecisionTriplesOptionalFields(t *testing.T) {
	ctx := &ProvenanceContext{CallID: "call-123"}

	// Test with empty optional fields
	info := FileDecisionInfo{
		EntityID:   "git.decision.abc.123",
		FilePath:   "file.go",
		CommitHash: "abc",
		Message:    "chore: cleanup",
		// Branch, Repository, Operation all empty
	}

	triples := ctx.DecisionTriples(info)

	predicateValues := make(map[string]any)
	for _, t := range triples {
		if t.Subject == info.EntityID {
			predicateValues[t.Predicate] = t.Object
		}
	}

	// Should not have optional predicates
	if _, ok := predicateValues["source.git.decision.branch"]; ok {
		t.Error("branch should not be set when empty")
	}
	if _, ok := predicateValues["source.git.decision.repository"]; ok {
		t.Error("repository should not be set when empty")
	}
	if _, ok := predicateValues["source.git.decision.operation"]; ok {
		t.Error("operation should not be set when empty")
	}
}

func TestCommitTriples(t *testing.T) {
	ctx := NewProvenanceContext("loop-1", "agent-1", "call-1", "git_commit")

	files := []string{"file1.go", "file2.go"}
	triples := ctx.CommitTriples("abc123", "feat: add feature", files)

	// Should have commit triples
	commitID := "git.commit.abc123"
	predicateValues := make(map[string]any)
	for _, tr := range triples {
		if tr.Subject == commitID {
			predicateValues[tr.Predicate] = tr.Object
		}
	}

	if predicateValues[codeAst.DcTitle] != "feat: add feature" {
		t.Errorf("commit message mismatch: got %v", predicateValues[codeAst.DcTitle])
	}
	if predicateValues[ProvGeneratedBy] != "call-1" {
		t.Errorf("commit provenance mismatch: got %v", predicateValues[ProvGeneratedBy])
	}
}

func TestActionTriples(t *testing.T) {
	ctx := NewProvenanceContext("loop-1", "agent-1", "call-1", "git_commit")

	// Successful action
	triples := ctx.ActionTriples(true, "")
	predicateValues := make(map[string]any)
	for _, tr := range triples {
		if tr.Subject == "call-1" {
			predicateValues[tr.Predicate] = tr.Object
		}
	}

	if predicateValues[AgenticActionType] != string(ActionTypeToolCall) {
		t.Errorf("action type mismatch: got %v", predicateValues[AgenticActionType])
	}
	if predicateValues[AgenticActionSuccess] != true {
		t.Errorf("action success mismatch: got %v", predicateValues[AgenticActionSuccess])
	}

	// Failed action
	triples = ctx.ActionTriples(false, "error message")
	predicateValues = make(map[string]any)
	for _, tr := range triples {
		if tr.Subject == "call-1" {
			predicateValues[tr.Predicate] = tr.Object
		}
	}

	if predicateValues[AgenticActionSuccess] != false {
		t.Errorf("failed action success should be false: got %v", predicateValues[AgenticActionSuccess])
	}
	if predicateValues[AgenticActionError] != "error message" {
		t.Errorf("action error mismatch: got %v", predicateValues[AgenticActionError])
	}
}
