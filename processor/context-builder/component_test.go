package contextbuilder

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semspec/processor/context-builder/gatherers"
	"github.com/c360studio/semspec/processor/context-builder/strategies"
)

func TestTaskTypeIsValid(t *testing.T) {
	tests := []struct {
		name     string
		taskType TaskType
		want     bool
	}{
		{"review is valid", TaskTypeReview, true},
		{"implementation is valid", TaskTypeImplementation, true},
		{"exploration is valid", TaskTypeExploration, true},
		{"empty is invalid", TaskType(""), false},
		{"unknown is invalid", TaskType("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.taskType.IsValid()
			if got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContextBuildRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     *ContextBuildRequest
		wantErr bool
	}{
		{
			name: "valid review request",
			req: &ContextBuildRequest{
				RequestID: "req-1",
				TaskType:  TaskTypeReview,
				Files:     []string{"main.go"},
			},
			wantErr: false,
		},
		{
			name: "valid review request with git ref",
			req: &ContextBuildRequest{
				RequestID: "req-2",
				TaskType:  TaskTypeReview,
				GitRef:    "HEAD~1..HEAD",
			},
			wantErr: false,
		},
		{
			name: "invalid review without files or git ref",
			req: &ContextBuildRequest{
				RequestID: "req-3",
				TaskType:  TaskTypeReview,
			},
			wantErr: true,
		},
		{
			name: "valid implementation request",
			req: &ContextBuildRequest{
				RequestID:    "req-4",
				TaskType:     TaskTypeImplementation,
				SpecEntityID: "spec-1",
			},
			wantErr: false,
		},
		{
			name: "valid exploration request",
			req: &ContextBuildRequest{
				RequestID: "req-5",
				TaskType:  TaskTypeExploration,
				Topic:     "authentication",
			},
			wantErr: false,
		},
		{
			name: "missing request id",
			req: &ContextBuildRequest{
				TaskType: TaskTypeReview,
				Files:    []string{"main.go"},
			},
			wantErr: true,
		},
		{
			name: "invalid task type",
			req: &ContextBuildRequest{
				RequestID: "req-6",
				TaskType:  TaskType("invalid"),
			},
			wantErr: true,
		},
		{
			name: "negative token budget",
			req: &ContextBuildRequest{
				RequestID:   "req-7",
				TaskType:    TaskTypeExploration,
				TokenBudget: -100,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestContextBuildRequestPayloadValidate(t *testing.T) {
	tests := []struct {
		name    string
		req     *ContextBuildRequest
		wantErr bool
	}{
		{
			name: "valid request passes Validate",
			req: &ContextBuildRequest{
				RequestID: "req-1",
				TaskType:  TaskTypeReview,
				Files:     []string{"main.go"},
			},
			wantErr: false,
		},
		{
			name: "missing request_id fails Validate",
			req: &ContextBuildRequest{
				TaskType: TaskTypeReview,
				Files:    []string{"main.go"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBudgetCalculator(t *testing.T) {
	calc := NewBudgetCalculator(32000, 6400)

	tests := []struct {
		name       string
		req        *ContextBuildRequest
		maxTokens  map[string]int
		wantBudget int
	}{
		{
			name: "explicit budget takes precedence",
			req: &ContextBuildRequest{
				TokenBudget: 10000,
				Model:       "test-model",
			},
			maxTokens:  map[string]int{"test-model": 128000},
			wantBudget: 10000,
		},
		{
			name: "model-based budget calculation",
			req: &ContextBuildRequest{
				Model: "test-model",
			},
			maxTokens:  map[string]int{"test-model": 128000},
			wantBudget: 121600, // 128000 - 6400
		},
		{
			name: "fallback to default",
			req: &ContextBuildRequest{
				Model: "unknown-model",
			},
			maxTokens:  map[string]int{},
			wantBudget: 32000,
		},
		{
			name:       "no model specified, fallback to default",
			req:        &ContextBuildRequest{},
			maxTokens:  map[string]int{},
			wantBudget: 32000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getMaxTokens := func(modelName string) int {
				return tt.maxTokens[modelName]
			}

			got := calc.Calculate(tt.req, getMaxTokens)
			if got != tt.wantBudget {
				t.Errorf("Calculate() = %v, want %v", got, tt.wantBudget)
			}
		})
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "default config is valid",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "missing stream name",
			config: Config{
				ConsumerName:        "test",
				InputSubjectPattern: "context.build.>",
				OutputSubjectPrefix: "context.built",
				DefaultTokenBudget:  32000,
				GraphGatewayURL:     "http://localhost:8082",
			},
			wantErr: true,
		},
		{
			name: "missing consumer name",
			config: Config{
				StreamName:          "AGENT",
				InputSubjectPattern: "context.build.>",
				OutputSubjectPrefix: "context.built",
				DefaultTokenBudget:  32000,
				GraphGatewayURL:     "http://localhost:8082",
			},
			wantErr: true,
		},
		{
			name: "invalid default token budget",
			config: Config{
				StreamName:          "AGENT",
				ConsumerName:        "test",
				InputSubjectPattern: "context.build.>",
				OutputSubjectPrefix: "context.built",
				DefaultTokenBudget:  0,
				GraphGatewayURL:     "http://localhost:8082",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestContextBuildRequestJSON(t *testing.T) {
	req := &ContextBuildRequest{
		RequestID:    "req-123",
		TaskType:     TaskTypeReview,
		WorkflowID:   "wf-456",
		Files:        []string{"main.go", "util.go"},
		GitRef:       "HEAD~1..HEAD",
		Topic:        "authentication",
		SpecEntityID: "spec-789",
		Capability:   "reviewing",
		Model:        "claude-sonnet",
		TokenBudget:  50000,
	}

	// Marshal
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal
	var decoded ContextBuildRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify fields
	if decoded.RequestID != req.RequestID {
		t.Errorf("RequestID = %v, want %v", decoded.RequestID, req.RequestID)
	}
	if decoded.TaskType != req.TaskType {
		t.Errorf("TaskType = %v, want %v", decoded.TaskType, req.TaskType)
	}
	if len(decoded.Files) != len(req.Files) {
		t.Errorf("Files length = %v, want %v", len(decoded.Files), len(req.Files))
	}
}

func TestContextBuildResponseJSON(t *testing.T) {
	resp := &ContextBuildResponse{
		RequestID:    "req-123",
		TaskType:     TaskTypeReview,
		TokenCount:   25000,
		Entities:     []EntityRef{{ID: "entity-1", Type: "function"}},
		Documents:    map[string]string{"main.go": "package main"},
		Diffs:        "diff content here",
		SOPIDs:       []string{"sop-1", "sop-2"},
		TokensUsed:   25000,
		TokensBudget: 32000,
		Truncated:    false,
	}

	// Marshal
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal
	var decoded ContextBuildResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify fields
	if decoded.RequestID != resp.RequestID {
		t.Errorf("RequestID = %v, want %v", decoded.RequestID, resp.RequestID)
	}
	if decoded.TokensUsed != resp.TokensUsed {
		t.Errorf("TokensUsed = %v, want %v", decoded.TokensUsed, resp.TokensUsed)
	}
	if len(decoded.SOPIDs) != len(resp.SOPIDs) {
		t.Errorf("SOPIDs length = %v, want %v", len(decoded.SOPIDs), len(resp.SOPIDs))
	}
}

func TestBudgetAllocation(t *testing.T) {
	t.Run("basic allocation", func(t *testing.T) {
		budget := strategies.NewBudgetAllocation(10000)

		err := budget.Allocate("sops", 3000)
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}

		if budget.Remaining() != 7000 {
			t.Errorf("Remaining() = %v, want 7000", budget.Remaining())
		}

		if budget.Allocated != 3000 {
			t.Errorf("Allocated = %v, want 3000", budget.Allocated)
		}
	})

	t.Run("allocation exceeds budget", func(t *testing.T) {
		budget := strategies.NewBudgetAllocation(10000)

		err := budget.Allocate("too_big", 15000)
		if err == nil {
			t.Error("Expected error for exceeding budget")
		}

		if budget.Allocated != 0 {
			t.Errorf("Allocated = %v, want 0 after failed allocation", budget.Allocated)
		}
	})

	t.Run("try allocate partial", func(t *testing.T) {
		budget := strategies.NewBudgetAllocation(10000)
		budget.Allocate("first", 8000)

		// Try to allocate 5000 but only 2000 remains
		actual := budget.TryAllocate("second", 5000)
		if actual != 2000 {
			t.Errorf("TryAllocate() = %v, want 2000", actual)
		}

		if budget.Remaining() != 0 {
			t.Errorf("Remaining() = %v, want 0", budget.Remaining())
		}
	})

	t.Run("can fit check", func(t *testing.T) {
		budget := strategies.NewBudgetAllocation(10000)
		budget.Allocate("first", 6000)

		if !budget.CanFit(4000) {
			t.Error("CanFit(4000) should be true")
		}

		if budget.CanFit(5000) {
			t.Error("CanFit(5000) should be false")
		}
	})
}

func TestTokenEstimator(t *testing.T) {
	estimator := strategies.NewTokenEstimator()

	t.Run("estimate tokens", func(t *testing.T) {
		content := "Hello, world!" // 13 chars
		tokens := estimator.Estimate(content)

		// With 4 chars per token, expect ~3 tokens
		if tokens < 2 || tokens > 5 {
			t.Errorf("Estimate() = %v, expected 2-5 for 13 char string", tokens)
		}
	})

	t.Run("estimate empty string", func(t *testing.T) {
		tokens := estimator.Estimate("")
		if tokens != 0 {
			t.Errorf("Estimate(\"\") = %v, want 0", tokens)
		}
	})

	t.Run("truncate to fit", func(t *testing.T) {
		content := "This is a test string that should be truncated to fit within the token limit."
		truncated, wasTruncated := estimator.TruncateToTokens(content, 5) // Very small limit

		if !wasTruncated {
			t.Error("Expected truncation for small token limit")
		}

		if len(truncated) >= len(content) {
			t.Errorf("Truncated content should be shorter: len=%d vs original=%d", len(truncated), len(content))
		}
	})

	t.Run("no truncation needed", func(t *testing.T) {
		content := "Short"
		result, wasTruncated := estimator.TruncateToTokens(content, 100)

		if wasTruncated {
			t.Error("Should not truncate short content")
		}

		if result != content {
			t.Errorf("Content should be unchanged: %q vs %q", result, content)
		}
	})
}

func TestGitRefValidation(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		wantErr bool
	}{
		{"empty ref is valid", "", false},
		{"HEAD is valid", "HEAD", false},
		{"HEAD~1 is valid", "HEAD~1", false},
		{"branch name is valid", "main", false},
		{"branch with slash is valid", "feature/auth", false},
		{"commit hash is valid", "abc123def", false},
		{"ref range is valid", "HEAD~1..HEAD", false},
		{"ref range with branches", "main..feature/auth", false},
		{"contains null byte", "main\x00evil", true},
		{"contains newline", "main\nevil", true},
		{"starts with dash", "-evil", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gatherers.ValidateGitRef(tt.ref)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateGitRef(%q) error = %v, wantErr %v", tt.ref, err, tt.wantErr)
			}
		})
	}
}

func TestStrategyConstants(t *testing.T) {
	// Verify constants are defined and have sensible values
	if strategies.MinTokensForTests <= 0 {
		t.Error("MinTokensForTests should be positive")
	}
	if strategies.MinTokensForConventions <= 0 {
		t.Error("MinTokensForConventions should be positive")
	}
	if strategies.MinTokensForDocs <= 0 {
		t.Error("MinTokensForDocs should be positive")
	}
	if strategies.MinTokensForPartial <= 0 {
		t.Error("MinTokensForPartial should be positive")
	}
	if strategies.MaxRelatedPatterns <= 0 {
		t.Error("MaxRelatedPatterns should be positive")
	}
	if strategies.MaxMatchingEntities <= 0 {
		t.Error("MaxMatchingEntities should be positive")
	}

	// Verify ordering makes sense
	if strategies.MinTokensForPartial >= strategies.MinTokensForDocs {
		t.Error("MinTokensForPartial should be less than MinTokensForDocs")
	}
	if strategies.MinTokensForDocs >= strategies.MinTokensForConventions {
		t.Error("MinTokensForDocs should be less than MinTokensForConventions")
	}
}
