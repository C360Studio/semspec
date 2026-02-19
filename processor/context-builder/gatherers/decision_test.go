package gatherers

import (
	"strings"
	"testing"
	"time"
)

func TestEntityToDecision(t *testing.T) {
	entity := Entity{
		ID: "git.decision.abc1234.12345678",
		Triples: []Triple{
			{Predicate: "source.git.decision.type", Object: "feat"},
			{Predicate: "source.git.decision.file", Object: "path/to/file.go"},
			{Predicate: "source.git.decision.commit", Object: "abc1234"},
			{Predicate: "source.git.decision.message", Object: "feat: add feature"},
			{Predicate: "source.git.decision.branch", Object: "main"},
			{Predicate: "source.git.decision.agent", Object: "agent-123"},
			{Predicate: "source.git.decision.loop", Object: "loop-456"},
			{Predicate: "source.git.decision.operation", Object: "modify"},
			{Predicate: "source.git.decision.timestamp", Object: "2024-01-15T10:30:00Z"},
		},
	}

	decision := entityToDecision(entity)

	if decision.ID != "git.decision.abc1234.12345678" {
		t.Errorf("ID mismatch: got %s", decision.ID)
	}
	if decision.Type != "feat" {
		t.Errorf("Type mismatch: got %s", decision.Type)
	}
	if decision.File != "path/to/file.go" {
		t.Errorf("File mismatch: got %s", decision.File)
	}
	if decision.Commit != "abc1234" {
		t.Errorf("Commit mismatch: got %s", decision.Commit)
	}
	if decision.Message != "feat: add feature" {
		t.Errorf("Message mismatch: got %s", decision.Message)
	}
	if decision.Branch != "main" {
		t.Errorf("Branch mismatch: got %s", decision.Branch)
	}
	if decision.Agent != "agent-123" {
		t.Errorf("Agent mismatch: got %s", decision.Agent)
	}
	if decision.Loop != "loop-456" {
		t.Errorf("Loop mismatch: got %s", decision.Loop)
	}
	if decision.Operation != "modify" {
		t.Errorf("Operation mismatch: got %s", decision.Operation)
	}
	if decision.Timestamp.IsZero() {
		t.Error("Timestamp should be parsed")
	}
}

func TestGetPredicateString(t *testing.T) {
	entity := Entity{
		ID: "test",
		Triples: []Triple{
			{Predicate: "foo.bar", Object: "value1"},
			{Predicate: "baz.qux", Object: "value2"},
			{Predicate: "num.val", Object: 123}, // Non-string
		},
	}

	tests := []struct {
		predicate string
		want      string
	}{
		{"foo.bar", "value1"},
		{"baz.qux", "value2"},
		{"num.val", ""}, // Non-string returns empty
		{"missing", ""}, // Missing returns empty
	}

	for _, tt := range tests {
		t.Run(tt.predicate, func(t *testing.T) {
			got := getPredicateString(entity, tt.predicate)
			if got != tt.want {
				t.Errorf("getPredicateString(%q) = %q, want %q", tt.predicate, got, tt.want)
			}
		})
	}
}

func TestSortDecisionsByTimestamp(t *testing.T) {
	now := time.Now()
	decisions := []Decision{
		{ID: "old", Timestamp: now.Add(-2 * time.Hour)},
		{ID: "newest", Timestamp: now},
		{ID: "middle", Timestamp: now.Add(-1 * time.Hour)},
	}

	sortDecisionsByTimestamp(decisions)

	// Should be newest first
	if decisions[0].ID != "newest" {
		t.Errorf("First should be newest, got %s", decisions[0].ID)
	}
	if decisions[1].ID != "middle" {
		t.Errorf("Second should be middle, got %s", decisions[1].ID)
	}
	if decisions[2].ID != "old" {
		t.Errorf("Third should be old, got %s", decisions[2].ID)
	}
}

func TestFormatDecisionsAsContext(t *testing.T) {
	t.Run("empty decisions", func(t *testing.T) {
		result := FormatDecisionsAsContext(nil, 10)
		if result != "" {
			t.Errorf("empty decisions should return empty string, got %q", result)
		}
	})

	t.Run("formats decisions", func(t *testing.T) {
		decisions := []Decision{
			{
				ID:        "git.decision.abc.123",
				Type:      "feat",
				File:      "file.go",
				Commit:    "abc123",
				Message:   "feat: add feature",
				Operation: "add",
				Timestamp: time.Now(),
			},
		}

		result := FormatDecisionsAsContext(decisions, 10)

		if result == "" {
			t.Error("should return formatted string")
		}
		if !strings.Contains(result, "Previous Decisions") {
			t.Error("should contain header")
		}
		if !strings.Contains(result, "file.go") {
			t.Error("should contain file path")
		}
		if !strings.Contains(result, "feat") {
			t.Error("should contain decision type")
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		decisions := []Decision{
			{ID: "1", Message: "msg1", Commit: "a"},
			{ID: "2", Message: "msg2", Commit: "b"},
			{ID: "3", Message: "msg3", Commit: "c"},
		}

		result := FormatDecisionsAsContext(decisions, 2)

		// Should only contain 2 decision headers
		if !strings.Contains(result, "msg1") {
			t.Error("should contain first decision")
		}
		if !strings.Contains(result, "msg2") {
			t.Error("should contain second decision")
		}
		if strings.Contains(result, "msg3") {
			t.Error("should NOT contain third decision (limited)")
		}
	})
}

func TestDecisionGathererCreation(t *testing.T) {
	graph := NewGraphGatherer("http://localhost:8080")
	gatherer := NewDecisionGatherer(graph)

	if gatherer == nil {
		t.Error("gatherer should not be nil")
	}
	if gatherer.graph != graph {
		t.Error("graph reference mismatch")
	}
}
