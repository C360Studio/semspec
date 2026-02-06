package validation

import (
	"testing"
	"time"
)

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	if config.MaxAttempts != 3 {
		t.Errorf("expected MaxAttempts 3, got %d", config.MaxAttempts)
	}
	if config.BackoffBase != 5*time.Second {
		t.Errorf("expected BackoffBase 5s, got %v", config.BackoffBase)
	}
	if config.BackoffMultiplier != 2.0 {
		t.Errorf("expected BackoffMultiplier 2.0, got %f", config.BackoffMultiplier)
	}
}

func TestRetryManagerRecordAttempt(t *testing.T) {
	rm := NewRetryManager(DefaultRetryConfig())

	// First attempt
	attempt := rm.RecordAttempt("test-slug", "propose")
	if attempt != 1 {
		t.Errorf("expected attempt 1, got %d", attempt)
	}

	// Second attempt
	attempt = rm.RecordAttempt("test-slug", "propose")
	if attempt != 2 {
		t.Errorf("expected attempt 2, got %d", attempt)
	}

	// Different step resets
	attempt = rm.RecordAttempt("test-slug", "design")
	if attempt != 1 {
		t.Errorf("expected attempt 1 for new step, got %d", attempt)
	}
}

func TestRetryManagerCanRetry(t *testing.T) {
	config := RetryConfig{
		MaxAttempts:       3,
		BackoffBase:       time.Second,
		BackoffMultiplier: 2.0,
	}
	rm := NewRetryManager(config)

	// First check - no attempts yet
	if !rm.CanRetry("slug", "step") {
		t.Error("expected CanRetry true before any attempts")
	}

	// Record attempts
	rm.RecordAttempt("slug", "step")
	rm.RecordAttempt("slug", "step")

	// Still can retry (2 < 3)
	if !rm.CanRetry("slug", "step") {
		t.Error("expected CanRetry true after 2 attempts")
	}

	// Third attempt
	rm.RecordAttempt("slug", "step")

	// Cannot retry (3 >= 3)
	if rm.CanRetry("slug", "step") {
		t.Error("expected CanRetry false after 3 attempts")
	}
}

func TestRetryManagerGetBackoffDuration(t *testing.T) {
	config := RetryConfig{
		MaxAttempts:       5,
		BackoffBase:       time.Second,
		BackoffMultiplier: 2.0,
	}
	rm := NewRetryManager(config)

	// No attempts - no backoff
	if rm.GetBackoffDuration("slug", "step") != 0 {
		t.Error("expected 0 backoff before any attempts")
	}

	// After 1 attempt: base * 2^0 = 1s
	rm.RecordAttempt("slug", "step")
	backoff := rm.GetBackoffDuration("slug", "step")
	if backoff != time.Second {
		t.Errorf("expected 1s backoff, got %v", backoff)
	}

	// After 2 attempts: base * 2^1 = 2s
	rm.RecordAttempt("slug", "step")
	backoff = rm.GetBackoffDuration("slug", "step")
	if backoff != 2*time.Second {
		t.Errorf("expected 2s backoff, got %v", backoff)
	}

	// After 3 attempts: base * 2^2 = 4s
	rm.RecordAttempt("slug", "step")
	backoff = rm.GetBackoffDuration("slug", "step")
	if backoff != 4*time.Second {
		t.Errorf("expected 4s backoff, got %v", backoff)
	}
}

func TestRetryManagerClearState(t *testing.T) {
	rm := NewRetryManager(DefaultRetryConfig())

	rm.RecordAttempt("slug", "step")
	rm.RecordAttempt("slug", "step")

	if rm.GetAttemptCount("slug", "step") != 2 {
		t.Error("expected 2 attempts before clear")
	}

	rm.ClearState("slug", "step")

	if rm.GetAttemptCount("slug", "step") != 0 {
		t.Error("expected 0 attempts after clear")
	}
}

func TestRetryManagerClearWorkflow(t *testing.T) {
	rm := NewRetryManager(DefaultRetryConfig())

	rm.RecordAttempt("workflow-1", "propose")
	rm.RecordAttempt("workflow-1", "design")
	rm.RecordAttempt("workflow-2", "propose")

	rm.ClearWorkflow("workflow-1")

	if rm.GetAttemptCount("workflow-1", "propose") != 0 {
		t.Error("expected workflow-1:propose cleared")
	}
	if rm.GetAttemptCount("workflow-1", "design") != 0 {
		t.Error("expected workflow-1:design cleared")
	}
	if rm.GetAttemptCount("workflow-2", "propose") != 1 {
		t.Error("expected workflow-2:propose preserved")
	}
}

func TestRetryManagerGetState(t *testing.T) {
	rm := NewRetryManager(DefaultRetryConfig())

	// No state yet
	if rm.GetState("slug", "step") != nil {
		t.Error("expected nil state before any attempts")
	}

	rm.RecordAttempt("slug", "step")
	rm.RecordFailure("slug", "step", "test error", &ValidationResult{
		Valid:           false,
		MissingSections: []string{"Why"},
	})

	state := rm.GetState("slug", "step")
	if state == nil {
		t.Fatal("expected non-nil state")
	}

	if state.WorkflowSlug != "slug" {
		t.Errorf("expected WorkflowSlug 'slug', got %q", state.WorkflowSlug)
	}
	if state.WorkflowStep != "step" {
		t.Errorf("expected WorkflowStep 'step', got %q", state.WorkflowStep)
	}
	if state.LastError != "test error" {
		t.Errorf("expected LastError 'test error', got %q", state.LastError)
	}
	if state.ValidationError == nil {
		t.Error("expected ValidationError to be set")
	}
}

func TestShouldRetry(t *testing.T) {
	config := RetryConfig{
		MaxAttempts:       3,
		BackoffBase:       time.Second,
		BackoffMultiplier: 2.0,
	}
	rm := NewRetryManager(config)

	t.Run("valid result - no retry needed", func(t *testing.T) {
		rm.RecordAttempt("valid-slug", "step")
		decision := rm.ShouldRetry("valid-slug", "step", &ValidationResult{Valid: true})

		if decision.ShouldRetry {
			t.Error("expected no retry for valid result")
		}
		if decision.IsFinalFailure {
			t.Error("valid result should not be final failure")
		}
	})

	t.Run("invalid result - retry allowed", func(t *testing.T) {
		rm.RecordAttempt("retry-slug", "step")
		decision := rm.ShouldRetry("retry-slug", "step", &ValidationResult{
			Valid:           false,
			MissingSections: []string{"Why"},
		})

		if !decision.ShouldRetry {
			t.Error("expected retry for invalid result")
		}
		if decision.IsFinalFailure {
			t.Error("should not be final failure on first attempt")
		}
		if decision.Feedback == "" {
			t.Error("expected feedback for retry")
		}
	})

	t.Run("max retries exceeded", func(t *testing.T) {
		// Record 3 attempts
		rm.RecordAttempt("maxed-slug", "step")
		rm.RecordAttempt("maxed-slug", "step")
		rm.RecordAttempt("maxed-slug", "step")

		decision := rm.ShouldRetry("maxed-slug", "step", &ValidationResult{
			Valid:           false,
			MissingSections: []string{"Why"},
		})

		if decision.ShouldRetry {
			t.Error("expected no retry after max attempts")
		}
		if !decision.IsFinalFailure {
			t.Error("expected final failure after max attempts")
		}
		if decision.Feedback == "" {
			t.Error("expected feedback for final failure")
		}
	})
}

func TestStateCount(t *testing.T) {
	rm := NewRetryManager(DefaultRetryConfig())

	if rm.StateCount() != 0 {
		t.Error("expected 0 states initially")
	}

	rm.RecordAttempt("slug1", "step1")
	rm.RecordAttempt("slug2", "step1")

	if rm.StateCount() != 2 {
		t.Errorf("expected 2 states, got %d", rm.StateCount())
	}

	rm.ClearState("slug1", "step1")
	if rm.StateCount() != 1 {
		t.Errorf("expected 1 state after clear, got %d", rm.StateCount())
	}
}

func TestPruneOld(t *testing.T) {
	rm := NewRetryManager(DefaultRetryConfig())

	// Record some attempts
	rm.RecordAttempt("old-slug", "step")
	rm.RecordAttempt("new-slug", "step")

	// Manually set one state to be old
	rm.mu.Lock()
	if state, exists := rm.states["old-slug:step"]; exists {
		state.CreatedAt = time.Now().Add(-2 * time.Hour)
	}
	rm.mu.Unlock()

	// Prune states older than 1 hour
	pruned := rm.PruneOld(1 * time.Hour)

	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}

	if rm.StateCount() != 1 {
		t.Errorf("expected 1 state remaining, got %d", rm.StateCount())
	}

	// The new-slug should still exist
	if rm.GetAttemptCount("new-slug", "step") == 0 {
		t.Error("expected new-slug to still exist")
	}

	// The old-slug should be gone
	if rm.GetAttemptCount("old-slug", "step") != 0 {
		t.Error("expected old-slug to be pruned")
	}
}

func TestDeepCopy(t *testing.T) {
	original := &RetryState{
		WorkflowSlug: "slug",
		WorkflowStep: "step",
		Attempts:     2,
		ValidationError: &ValidationResult{
			Valid:           false,
			MissingSections: []string{"Why", "What"},
			Warnings:        []string{"TODO found"},
			SectionDetails:  map[string]string{"Title": "OK"},
		},
	}

	copied := original.DeepCopy()

	// Verify deep copy
	if copied == original {
		t.Error("DeepCopy returned same pointer")
	}
	if copied.ValidationError == original.ValidationError {
		t.Error("ValidationError not deep copied")
	}

	// Modify the copy and verify original is unchanged
	copied.ValidationError.MissingSections[0] = "Modified"
	if original.ValidationError.MissingSections[0] == "Modified" {
		t.Error("MissingSections not deep copied - original was modified")
	}

	copied.ValidationError.SectionDetails["Title"] = "Modified"
	if original.ValidationError.SectionDetails["Title"] == "Modified" {
		t.Error("SectionDetails not deep copied - original was modified")
	}
}

func TestDeepCopyNil(t *testing.T) {
	var state *RetryState
	copied := state.DeepCopy()
	if copied != nil {
		t.Error("expected nil for nil input")
	}

	// State without ValidationError
	state = &RetryState{WorkflowSlug: "slug"}
	copied = state.DeepCopy()
	if copied.ValidationError != nil {
		t.Error("expected nil ValidationError in copy")
	}
}

func TestRetryDecisionFields(t *testing.T) {
	config := RetryConfig{
		MaxAttempts:       5,
		BackoffBase:       2 * time.Second,
		BackoffMultiplier: 2.0,
	}
	rm := NewRetryManager(config)

	rm.RecordAttempt("slug", "step")
	rm.RecordAttempt("slug", "step")

	decision := rm.ShouldRetry("slug", "step", &ValidationResult{
		Valid:           false,
		MissingSections: []string{"Why"},
	})

	if decision.AttemptNumber != 2 {
		t.Errorf("expected AttemptNumber 2, got %d", decision.AttemptNumber)
	}
	if decision.MaxAttempts != 5 {
		t.Errorf("expected MaxAttempts 5, got %d", decision.MaxAttempts)
	}
	// After 2 attempts: 2s * 2^1 = 4s
	if decision.BackoffSeconds != 4.0 {
		t.Errorf("expected BackoffSeconds 4.0, got %f", decision.BackoffSeconds)
	}
}
