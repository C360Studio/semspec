package git

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
)

func TestGenerateDecisionEntityID(t *testing.T) {
	tests := []struct {
		name       string
		commitHash string
		filePath   string
		wantPrefix string
	}{
		{
			name:       "short commit hash",
			commitHash: "abc1234",
			filePath:   "path/to/file.go",
			wantPrefix: "git.decision.abc1234.",
		},
		{
			name:       "full commit hash truncated",
			commitHash: "abc1234567890abcdef",
			filePath:   "path/to/file.go",
			wantPrefix: "git.decision.abc1234.",
		},
		{
			name:       "same commit different file",
			commitHash: "abc1234",
			filePath:   "different/file.ts",
			wantPrefix: "git.decision.abc1234.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := GenerateDecisionEntityID(tt.commitHash, tt.filePath)
			if len(id) < len(tt.wantPrefix)+8 {
				t.Errorf("ID too short: %s", id)
			}
			if id[:len(tt.wantPrefix)] != tt.wantPrefix {
				t.Errorf("ID prefix mismatch: got %s, want prefix %s", id, tt.wantPrefix)
			}
		})
	}

	// Test that same inputs produce same output (deterministic)
	t.Run("deterministic", func(t *testing.T) {
		id1 := GenerateDecisionEntityID("abc1234", "file.go")
		id2 := GenerateDecisionEntityID("abc1234", "file.go")
		if id1 != id2 {
			t.Errorf("same inputs should produce same ID: %s != %s", id1, id2)
		}
	})

	// Test that different files produce different IDs
	t.Run("different files different IDs", func(t *testing.T) {
		id1 := GenerateDecisionEntityID("abc1234", "file1.go")
		id2 := GenerateDecisionEntityID("abc1234", "file2.go")
		if id1 == id2 {
			t.Errorf("different files should produce different IDs: %s == %s", id1, id2)
		}
	})
}

func TestNewDecisionEntityPayload(t *testing.T) {
	triples := []message.Triple{
		{Subject: "test", Predicate: "source.git.decision.type", Object: "feat"},
		{Subject: "test", Predicate: "source.git.decision.file", Object: "file.go"},
	}

	payload := NewDecisionEntityPayload("abc1234", "file.go", triples)

	if payload.ID == "" {
		t.Error("EntityID should be set")
	}
	if payload.FilePath != "file.go" {
		t.Errorf("FilePath mismatch: got %s, want file.go", payload.FilePath)
	}
	if payload.CommitHash != "abc1234" {
		t.Errorf("CommitHash mismatch: got %s, want abc1234", payload.CommitHash)
	}
	if len(payload.TripleData) != 2 {
		t.Errorf("TripleData length mismatch: got %d, want 2", len(payload.TripleData))
	}
	if payload.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set")
	}
}

func TestDecisionEntityPayloadInterface(t *testing.T) {
	payload := &DecisionEntityPayload{
		ID:         "git.decision.abc1234.12345678",
		CommitHash: "abc1234",
		FilePath:   "test.go",
		UpdatedAt:  time.Now(),
		TripleData: []message.Triple{
			{Subject: "test", Predicate: "test.pred", Object: "value"},
		},
	}

	// Test EntityID
	if payload.EntityID() != "git.decision.abc1234.12345678" {
		t.Errorf("EntityID() mismatch: got %s", payload.EntityID())
	}

	// Test Triples
	if len(payload.Triples()) != 1 {
		t.Errorf("Triples() length mismatch: got %d", len(payload.Triples()))
	}

	// Test Schema
	schema := payload.Schema()
	if schema.Domain != "git" || schema.Category != "decision" || schema.Version != "v1" {
		t.Errorf("Schema mismatch: got %+v", schema)
	}

	// Test Validate
	if err := payload.Validate(); err != nil {
		t.Errorf("Validate() should pass: %v", err)
	}

	// Test Validate with empty ID
	emptyPayload := &DecisionEntityPayload{}
	if err := emptyPayload.Validate(); err == nil {
		t.Error("Validate() should fail with empty EntityID")
	}
}

func TestDecisionEntityPayloadJSON(t *testing.T) {
	original := &DecisionEntityPayload{
		ID:         "git.decision.abc1234.12345678",
		CommitHash: "abc1234",
		FilePath:   "test.go",
		UpdatedAt:  time.Now().Truncate(time.Millisecond), // Truncate for comparison
		TripleData: []message.Triple{
			{Subject: "test", Predicate: "source.git.decision.type", Object: "feat"},
		},
	}

	// Marshal
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Verify JSON structure
	var jsonMap map[string]any
	if err := json.Unmarshal(data, &jsonMap); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}
	if jsonMap["id"] != "git.decision.abc1234.12345678" {
		t.Errorf("JSON id field mismatch: got %v", jsonMap["id"])
	}
	if jsonMap["file_path"] != "test.go" {
		t.Errorf("JSON file_path field mismatch: got %v", jsonMap["file_path"])
	}

	// Unmarshal back
	var decoded DecisionEntityPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("EntityID mismatch after round-trip: got %s, want %s", decoded.ID, original.ID)
	}
	if decoded.FilePath != original.FilePath {
		t.Errorf("FilePath mismatch after round-trip: got %s, want %s", decoded.FilePath, original.FilePath)
	}
	if decoded.CommitHash != original.CommitHash {
		t.Errorf("CommitHash mismatch after round-trip: got %s, want %s", decoded.CommitHash, original.CommitHash)
	}
}

func TestParseFileOperation(t *testing.T) {
	tests := []struct {
		statusCode string
		want       string
	}{
		{"A", "add"},
		{"A\t", "add"},
		{"M", "modify"},
		{"D", "delete"},
		{"R100", "rename"},
		{"", "modify"},  // Default
		{"X", "modify"}, // Unknown defaults to modify
	}

	for _, tt := range tests {
		t.Run(tt.statusCode, func(t *testing.T) {
			got := ParseFileOperation(tt.statusCode)
			if got != tt.want {
				t.Errorf("ParseFileOperation(%q) = %s, want %s", tt.statusCode, got, tt.want)
			}
		})
	}
}

func TestFileChangeInfo(t *testing.T) {
	info := FileChangeInfo{
		Path:      "path/to/file.go",
		Operation: "modify",
	}

	if info.Path != "path/to/file.go" {
		t.Errorf("Path mismatch: got %s", info.Path)
	}
	if info.Operation != "modify" {
		t.Errorf("Operation mismatch: got %s", info.Operation)
	}
}

func TestDecisionEntityTypeConstant(t *testing.T) {
	if DecisionEntityType.Domain != "git" {
		t.Errorf("Domain mismatch: got %s, want git", DecisionEntityType.Domain)
	}
	if DecisionEntityType.Category != "decision" {
		t.Errorf("Category mismatch: got %s, want decision", DecisionEntityType.Category)
	}
	if DecisionEntityType.Version != "v1" {
		t.Errorf("Version mismatch: got %s, want v1", DecisionEntityType.Version)
	}
}
