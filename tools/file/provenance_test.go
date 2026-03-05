package file

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"

	"github.com/c360studio/semspec/tools/provenance"
)

// captureEmitter collects emitted triples for assertion in tests.
type captureEmitter struct {
	triples []message.Triple
}

func (c *captureEmitter) emit(triples []message.Triple) {
	c.triples = append(c.triples, triples...)
}

func (c *captureEmitter) predicates() map[string]struct{} {
	m := make(map[string]struct{}, len(c.triples))
	for _, t := range c.triples {
		m[t.Predicate] = struct{}{}
	}
	return m
}

func (c *captureEmitter) hasPredicateForSubject(subject, predicate string) bool {
	for _, t := range c.triples {
		if t.Subject == subject && t.Predicate == predicate {
			return true
		}
	}
	return false
}

// newProvenanceExecutor creates an Executor wired with a capture emitter for testing.
func newProvenanceExecutor(t *testing.T, repoRoot string) (*Executor, *captureEmitter) {
	t.Helper()
	emitter := &captureEmitter{}
	provCtx := provenance.NewContext("loop-test", "agent-test", "", "")
	executor := NewExecutor(repoRoot).WithProvenance(provCtx, emitter.emit)
	return executor, emitter
}

func TestFileWriteEmitsGenerationAndActionTriples(t *testing.T) {
	tmpDir := t.TempDir()
	executor, emitter := newProvenanceExecutor(t, tmpDir)

	call := agentic.ToolCall{
		ID:   "call-write-001",
		Name: "file_write",
		Arguments: map[string]any{
			"path":    "hello.txt",
			"content": "hello world",
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

	// GenerationTriples must be present
	if _, ok := preds[provenance.ProvGeneratedBy]; !ok {
		t.Errorf("missing generation predicate %q in emitted triples", provenance.ProvGeneratedBy)
	}
	if _, ok := preds[provenance.ProvGeneratedAt]; !ok {
		t.Errorf("missing generation timestamp predicate %q in emitted triples", provenance.ProvGeneratedAt)
	}

	// ActionTriples must be present
	if _, ok := preds[provenance.AgenticActionType]; !ok {
		t.Errorf("missing action type predicate %q in emitted triples", provenance.AgenticActionType)
	}
	if _, ok := preds[provenance.AgenticActionSuccess]; !ok {
		t.Errorf("missing action success predicate %q in emitted triples", provenance.AgenticActionSuccess)
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

func TestFileReadEmitsUsageAndActionTriples(t *testing.T) {
	tmpDir := t.TempDir()

	// Write a file to read back
	testFile := filepath.Join(tmpDir, "readable.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	executor, emitter := newProvenanceExecutor(t, tmpDir)

	call := agentic.ToolCall{
		ID:   "call-read-001",
		Name: "file_read",
		Arguments: map[string]any{
			"path": "readable.txt",
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

	// UsageTriples must be present
	if _, ok := preds[provenance.ProvUsed]; !ok {
		t.Errorf("missing usage predicate %q in emitted triples", provenance.ProvUsed)
	}
	if _, ok := preds[provenance.AgenticActionInput]; !ok {
		t.Errorf("missing action input predicate %q in emitted triples", provenance.AgenticActionInput)
	}

	// ActionTriples must be present
	if _, ok := preds[provenance.AgenticActionType]; !ok {
		t.Errorf("missing action type predicate %q in emitted triples", provenance.AgenticActionType)
	}
	if _, ok := preds[provenance.AgenticActionSuccess]; !ok {
		t.Errorf("missing action success predicate %q in emitted triples", provenance.AgenticActionSuccess)
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

func TestFileWriteEntityIDEncodesPath(t *testing.T) {
	tmpDir := t.TempDir()
	executor, emitter := newProvenanceExecutor(t, tmpDir)

	call := agentic.ToolCall{
		ID:   "call-write-002",
		Name: "file_write",
		Arguments: map[string]any{
			"path":    "sub/dir/file.go",
			"content": "package main",
		},
	}

	if _, err := executor.Execute(context.Background(), call); err != nil {
		t.Fatalf("unexpected Execute error: %v", err)
	}

	// The entity ID for "sub/dir/file.go" must be "code.file.sub-dir-file.go"
	expectedEntityID := "code.file.sub-dir-file.go"
	found := false
	for _, tr := range emitter.triples {
		if tr.Subject == expectedEntityID && tr.Predicate == provenance.ProvGeneratedBy {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected triple with subject %q and predicate %q", expectedEntityID, provenance.ProvGeneratedBy)
	}
}

func TestNoProvenanceEmittedWithoutContext(t *testing.T) {
	tmpDir := t.TempDir()
	// Executor created without WithProvenance — must not panic or emit anything
	executor := NewExecutor(tmpDir)

	call := agentic.ToolCall{
		ID:   "call-no-prov",
		Name: "file_write",
		Arguments: map[string]any{
			"path":    "safe.txt",
			"content": "data",
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
