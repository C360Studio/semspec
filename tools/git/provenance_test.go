package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"

	"github.com/c360studio/semspec/tools/provenance"
)

// gitCaptureEmitter collects emitted triples for assertion in tests.
type gitCaptureEmitter struct {
	triples []message.Triple
}

func (c *gitCaptureEmitter) emit(triples []message.Triple) {
	c.triples = append(c.triples, triples...)
}

func (c *gitCaptureEmitter) predicates() map[string]struct{} {
	m := make(map[string]struct{}, len(c.triples))
	for _, t := range c.triples {
		m[t.Predicate] = struct{}{}
	}
	return m
}

// newProvenanceGitExecutor creates a git Executor wired with a capture emitter for testing.
func newProvenanceGitExecutor(t *testing.T, repoRoot string) (*Executor, *gitCaptureEmitter) {
	t.Helper()
	emitter := &gitCaptureEmitter{}
	provCtx := provenance.NewContext("loop-git-test", "agent-git-test", "", "")
	executor := NewExecutor(repoRoot).WithProvenance(provCtx, emitter.emit)
	return executor, emitter
}

func TestGitCommitEmitsCommitAndActionTriples(t *testing.T) {
	repoDir := setupTestRepo(t)
	executor, emitter := newProvenanceGitExecutor(t, repoDir)

	// Stage a new file
	testFile := filepath.Join(repoDir, "provenance-test.txt")
	if err := os.WriteFile(testFile, []byte("provenance data"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	if _, err := executor.runGit(context.Background(), "add", "provenance-test.txt"); err != nil {
		t.Fatalf("failed to stage file: %v", err)
	}

	call := agentic.ToolCall{
		ID:   "call-commit-001",
		Name: "git_commit",
		Arguments: map[string]any{
			"message": "feat: add provenance test file",
		},
	}

	result, err := executor.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected Execute error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected tool error: %s", result.Error)
	}

	if len(emitter.triples) == 0 {
		t.Fatal("expected provenance triples to be emitted, got none")
	}

	preds := emitter.predicates()

	// CommitTriples — commit entity must reference the call via ProvGeneratedBy
	if _, ok := preds[provenance.ProvGeneratedBy]; !ok {
		t.Errorf("missing commit generation predicate %q", provenance.ProvGeneratedBy)
	}
	if _, ok := preds[provenance.ProvGeneratedAt]; !ok {
		t.Errorf("missing commit generation timestamp predicate %q", provenance.ProvGeneratedAt)
	}

	// ActionTriples must be present
	if _, ok := preds[provenance.AgenticActionType]; !ok {
		t.Errorf("missing action type predicate %q", provenance.AgenticActionType)
	}
	if _, ok := preds[provenance.AgenticActionSuccess]; !ok {
		t.Errorf("missing action success predicate %q", provenance.AgenticActionSuccess)
	}

	// Action must be marked successful
	var successValue any
	for _, tr := range emitter.triples {
		if tr.Predicate == provenance.AgenticActionSuccess {
			successValue = tr.Object
		}
	}
	if successValue != true {
		t.Errorf("expected action success = true, got %v", successValue)
	}
}

func TestGitCommitEmitsCommitEntityID(t *testing.T) {
	repoDir := setupTestRepo(t)
	executor, emitter := newProvenanceGitExecutor(t, repoDir)

	// Stage a file
	testFile := filepath.Join(repoDir, "entity-id-test.txt")
	if err := os.WriteFile(testFile, []byte("data"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	if _, err := executor.runGit(context.Background(), "add", "entity-id-test.txt"); err != nil {
		t.Fatalf("failed to stage file: %v", err)
	}

	call := agentic.ToolCall{
		ID:   "call-commit-002",
		Name: "git_commit",
		Arguments: map[string]any{
			"message": "chore: add entity id test file",
		},
	}

	if _, err := executor.Execute(context.Background(), call); err != nil {
		t.Fatalf("unexpected Execute error: %v", err)
	}

	// The commit entity subject must follow the "git.commit.{hash}" pattern
	found := false
	for _, tr := range emitter.triples {
		if len(tr.Subject) > len("git.commit.") &&
			tr.Subject[:len("git.commit.")] == "git.commit." &&
			tr.Predicate == provenance.ProvGeneratedBy {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a triple with subject starting with 'git.commit.' and predicate ProvGeneratedBy")
	}
}

func TestGitCommitAgentAttributionInTriples(t *testing.T) {
	repoDir := setupTestRepo(t)
	executor, emitter := newProvenanceGitExecutor(t, repoDir)

	// Stage a file
	testFile := filepath.Join(repoDir, "attr-test.txt")
	if err := os.WriteFile(testFile, []byte("attr"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	if _, err := executor.runGit(context.Background(), "add", "attr-test.txt"); err != nil {
		t.Fatalf("failed to stage file: %v", err)
	}

	call := agentic.ToolCall{
		ID:   "call-commit-003",
		Name: "git_commit",
		Arguments: map[string]any{
			"message": "docs: add attribution test",
		},
	}

	if _, err := executor.Execute(context.Background(), call); err != nil {
		t.Fatalf("unexpected Execute error: %v", err)
	}

	// Agent attribution must appear in commit triples
	foundAttribution := false
	for _, tr := range emitter.triples {
		if tr.Predicate == provenance.ProvAttributedTo && tr.Object == "agent-git-test" {
			foundAttribution = true
			break
		}
	}
	if !foundAttribution {
		t.Error("expected attribution triple with agent-git-test as object")
	}

	// Loop association must appear in action triples
	foundAssociation := false
	for _, tr := range emitter.triples {
		if tr.Predicate == provenance.ProvAssociatedWith && tr.Object == "loop-git-test" {
			foundAssociation = true
			break
		}
	}
	if !foundAssociation {
		t.Error("expected association triple with loop-git-test as object")
	}
}

func TestNoProvenanceEmittedWithoutGitContext(t *testing.T) {
	repoDir := setupTestRepo(t)
	// Executor created without WithProvenance — must not panic
	executor := NewExecutor(repoDir)

	// Stage a file
	testFile := filepath.Join(repoDir, "no-prov.txt")
	if err := os.WriteFile(testFile, []byte("no prov"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	if _, err := executor.runGit(context.Background(), "add", "no-prov.txt"); err != nil {
		t.Fatalf("failed to stage file: %v", err)
	}

	call := agentic.ToolCall{
		ID:   "call-commit-noprov",
		Name: "git_commit",
		Arguments: map[string]any{
			"message": "test: commit without provenance",
		},
	}

	result, err := executor.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Errorf("unexpected tool error: %s", result.Error)
	}
}
