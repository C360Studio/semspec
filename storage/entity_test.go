package storage

import (
	"testing"
)

func TestEntityID(t *testing.T) {
	t.Run("NewEntityID generates valid ID", func(t *testing.T) {
		id := NewEntityID(EntityTypeProposal)
		if id.Type != EntityTypeProposal {
			t.Errorf("expected type %s, got %s", EntityTypeProposal, id.Type)
		}
		if id.ID == "" {
			t.Error("expected non-empty ID")
		}
	})

	t.Run("String returns correct format", func(t *testing.T) {
		id := EntityID{Type: EntityTypeTask, ID: "abc123"}
		expected := "task:abc123"
		if id.String() != expected {
			t.Errorf("expected %s, got %s", expected, id.String())
		}
	})

	t.Run("ParseEntityID parses valid ID", func(t *testing.T) {
		id, err := ParseEntityID("proposal:abc123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id.Type != EntityTypeProposal {
			t.Errorf("expected type %s, got %s", EntityTypeProposal, id.Type)
		}
		if id.ID != "abc123" {
			t.Errorf("expected ID abc123, got %s", id.ID)
		}
	})

	t.Run("ParseEntityID handles all types", func(t *testing.T) {
		tests := []struct {
			input    string
			expected EntityType
		}{
			{"proposal:123", EntityTypeProposal},
			{"task:456", EntityTypeTask},
			{"result:789", EntityTypeResult},
		}

		for _, tc := range tests {
			id, err := ParseEntityID(tc.input)
			if err != nil {
				t.Errorf("unexpected error for %s: %v", tc.input, err)
				continue
			}
			if id.Type != tc.expected {
				t.Errorf("for %s: expected type %s, got %s", tc.input, tc.expected, id.Type)
			}
		}
	})

	t.Run("ParseEntityID rejects invalid format", func(t *testing.T) {
		invalidIDs := []string{
			"invalid",
			"no-colon",
			"",
			"unknown:123",
		}

		for _, input := range invalidIDs {
			_, err := ParseEntityID(input)
			if err == nil {
				t.Errorf("expected error for %q, got nil", input)
			}
		}
	})

	t.Run("Round trip ID conversion", func(t *testing.T) {
		original := NewEntityID(EntityTypeResult)
		str := original.String()
		parsed, err := ParseEntityID(str)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if parsed.Type != original.Type {
			t.Errorf("type mismatch: expected %s, got %s", original.Type, parsed.Type)
		}
		if parsed.ID != original.ID {
			t.Errorf("ID mismatch: expected %s, got %s", original.ID, parsed.ID)
		}
	})
}

func TestTaskStatus(t *testing.T) {
	t.Run("Valid status values", func(t *testing.T) {
		statuses := []TaskStatus{
			TaskStatusPending,
			TaskStatusInProgress,
			TaskStatusComplete,
			TaskStatusFailed,
		}

		for _, s := range statuses {
			if s == "" {
				t.Errorf("empty status value")
			}
		}
	})
}

func TestProposal(t *testing.T) {
	t.Run("Proposal fields", func(t *testing.T) {
		p := Proposal{
			ID:          "proposal:123",
			Title:       "Test Proposal",
			Description: "A test proposal",
		}

		if p.ID != "proposal:123" {
			t.Errorf("unexpected ID: %s", p.ID)
		}
		if p.Title != "Test Proposal" {
			t.Errorf("unexpected title: %s", p.Title)
		}
	})
}

func TestTask(t *testing.T) {
	t.Run("Task fields", func(t *testing.T) {
		task := Task{
			ID:          "task:456",
			ProposalID:  "proposal:123",
			Title:       "Test Task",
			Description: "A test task",
			Status:      TaskStatusPending,
		}

		if task.ID != "task:456" {
			t.Errorf("unexpected ID: %s", task.ID)
		}
		if task.ProposalID != "proposal:123" {
			t.Errorf("unexpected proposal ID: %s", task.ProposalID)
		}
		if task.Status != TaskStatusPending {
			t.Errorf("unexpected status: %s", task.Status)
		}
	})

	t.Run("StatusChange tracking", func(t *testing.T) {
		task := Task{
			ID:     "task:789",
			Status: TaskStatusPending,
		}

		// Simulate status change
		task.StatusChange = append(task.StatusChange, StatusChange{
			From: TaskStatusPending,
			To:   TaskStatusInProgress,
		})

		if len(task.StatusChange) != 1 {
			t.Errorf("expected 1 status change, got %d", len(task.StatusChange))
		}
		if task.StatusChange[0].From != TaskStatusPending {
			t.Errorf("unexpected from status: %s", task.StatusChange[0].From)
		}
		if task.StatusChange[0].To != TaskStatusInProgress {
			t.Errorf("unexpected to status: %s", task.StatusChange[0].To)
		}
	})
}

func TestResult(t *testing.T) {
	t.Run("Result with artifacts", func(t *testing.T) {
		r := Result{
			ID:      "result:abc",
			TaskID:  "task:456",
			Success: true,
			Output:  "Task completed successfully",
			Artifacts: []Artifact{
				{Path: "file.go", Action: "created", Hash: "abc123"},
				{Path: "test.go", Action: "modified", Hash: "def456"},
			},
		}

		if r.ID != "result:abc" {
			t.Errorf("unexpected ID: %s", r.ID)
		}
		if !r.Success {
			t.Error("expected success to be true")
		}
		if len(r.Artifacts) != 2 {
			t.Errorf("expected 2 artifacts, got %d", len(r.Artifacts))
		}
		if r.Artifacts[0].Path != "file.go" {
			t.Errorf("unexpected artifact path: %s", r.Artifacts[0].Path)
		}
		if r.Artifacts[0].Action != "created" {
			t.Errorf("unexpected artifact action: %s", r.Artifacts[0].Action)
		}
	})

	t.Run("Result with error", func(t *testing.T) {
		r := Result{
			ID:      "result:xyz",
			TaskID:  "task:789",
			Success: false,
			Error:   "compilation failed",
		}

		if r.Success {
			t.Error("expected success to be false")
		}
		if r.Error != "compilation failed" {
			t.Errorf("unexpected error: %s", r.Error)
		}
	})
}

func TestArtifact(t *testing.T) {
	t.Run("Artifact actions", func(t *testing.T) {
		actions := []string{"created", "modified", "deleted"}
		for _, action := range actions {
			a := Artifact{
				Path:   "test.go",
				Action: action,
			}
			if a.Action != action {
				t.Errorf("expected action %s, got %s", action, a.Action)
			}
		}
	})
}

func TestBucketNames(t *testing.T) {
	t.Run("Bucket names are set", func(t *testing.T) {
		if BucketProposals != "SEMSPEC_PROPOSALS" {
			t.Errorf("unexpected proposals bucket: %s", BucketProposals)
		}
		if BucketTasks != "SEMSPEC_TASKS" {
			t.Errorf("unexpected tasks bucket: %s", BucketTasks)
		}
		if BucketResults != "SEMSPEC_RESULTS" {
			t.Errorf("unexpected results bucket: %s", BucketResults)
		}
	})
}
