package contextbuilder

import (
	"testing"

	"github.com/c360studio/semspec/model"
)

// mockCapabilityResolver implements CapabilityResolver for testing.
type mockCapabilityResolver struct {
	capabilityToModel map[model.Capability]string
}

func (m *mockCapabilityResolver) Resolve(cap model.Capability) string {
	if m.capabilityToModel == nil {
		return ""
	}
	return m.capabilityToModel[cap]
}

func TestBudgetCalculator_Calculate(t *testing.T) {
	// Setup: model registry maps models to their max tokens
	modelTokens := map[string]int{
		"planning-model":  200000, // 200K context
		"reviewing-model": 128000, // 128K context
		"fast-model":      8000,   // 8K context
		"default-model":   32000,  // 32K context
	}
	getModelMaxTokens := func(modelName string) int {
		return modelTokens[modelName]
	}

	// Setup: capability resolver maps capabilities to models
	resolver := &mockCapabilityResolver{
		capabilityToModel: map[model.Capability]string{
			model.CapabilityPlanning:  "planning-model",
			model.CapabilityReviewing: "reviewing-model",
			model.CapabilityFast:      "fast-model",
		},
	}

	tests := []struct {
		name           string
		defaultBudget  int
		headroomTokens int
		req            *ContextBuildRequest
		useResolver    bool
		wantBudget     int
	}{
		{
			name:           "explicit token budget takes precedence",
			defaultBudget:  32000,
			headroomTokens: 4000,
			req: &ContextBuildRequest{
				TokenBudget: 50000,
				Capability:  "planning",
			},
			useResolver: true,
			wantBudget:  50000, // Explicit budget used, ignoring capability
		},
		{
			name:           "capability resolves to model budget",
			defaultBudget:  32000,
			headroomTokens: 4000,
			req: &ContextBuildRequest{
				Capability: "planning",
			},
			useResolver: true,
			wantBudget:  196000, // 200000 - 4000 headroom
		},
		{
			name:           "reviewing capability resolves to model budget",
			defaultBudget:  32000,
			headroomTokens: 4000,
			req: &ContextBuildRequest{
				Capability: "reviewing",
			},
			useResolver: true,
			wantBudget:  124000, // 128000 - 4000 headroom
		},
		{
			name:           "fast capability resolves to model budget",
			defaultBudget:  32000,
			headroomTokens: 2000,
			req: &ContextBuildRequest{
				Capability: "fast",
			},
			useResolver: true,
			wantBudget:  6000, // 8000 - 2000 headroom
		},
		{
			name:           "explicit model overrides capability when no resolver",
			defaultBudget:  32000,
			headroomTokens: 4000,
			req: &ContextBuildRequest{
				Model:      "default-model",
				Capability: "planning", // Would give 200K but resolver not set
			},
			useResolver: false, // No resolver, so capability is ignored
			wantBudget:  28000, // 32000 - 4000 headroom
		},
		{
			name:           "explicit model used when capability is empty",
			defaultBudget:  32000,
			headroomTokens: 4000,
			req: &ContextBuildRequest{
				Model: "default-model",
			},
			useResolver: true,
			wantBudget:  28000, // 32000 - 4000 headroom
		},
		{
			name:           "falls back to default when no capability or model",
			defaultBudget:  32000,
			headroomTokens: 4000,
			req:            &ContextBuildRequest{},
			useResolver:    true,
			wantBudget:     32000, // Default budget (headroom not subtracted)
		},
		{
			name:           "invalid capability falls through to model",
			defaultBudget:  32000,
			headroomTokens: 4000,
			req: &ContextBuildRequest{
				Capability: "invalid-capability",
				Model:      "default-model",
			},
			useResolver: true,
			wantBudget:  28000, // Falls through to model: 32000 - 4000
		},
		{
			name:           "invalid capability and no model uses default",
			defaultBudget:  32000,
			headroomTokens: 4000,
			req: &ContextBuildRequest{
				Capability: "invalid-capability",
			},
			useResolver: true,
			wantBudget:  32000, // Falls through to default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calc := NewBudgetCalculator(tt.defaultBudget, tt.headroomTokens)
			if tt.useResolver {
				calc.SetCapabilityResolver(resolver)
			}

			got := calc.Calculate(tt.req, getModelMaxTokens)
			if got != tt.wantBudget {
				t.Errorf("Calculate() = %d, want %d", got, tt.wantBudget)
			}
		})
	}
}

func TestBudgetCalculator_CapabilityPriority(t *testing.T) {
	// This test verifies that capability-based lookup happens BEFORE
	// explicit model lookup, ensuring the token budget matches the
	// model that will actually be used for LLM calls.

	modelTokens := map[string]int{
		"planning-model": 200000,
		"other-model":    50000,
	}
	getModelMaxTokens := func(modelName string) int {
		return modelTokens[modelName]
	}

	resolver := &mockCapabilityResolver{
		capabilityToModel: map[model.Capability]string{
			model.CapabilityPlanning: "planning-model",
		},
	}

	calc := NewBudgetCalculator(32000, 4000)
	calc.SetCapabilityResolver(resolver)

	// When both capability and model are set, capability takes precedence
	// because the LLM client will use capability to select the model.
	req := &ContextBuildRequest{
		Capability: "planning",
		Model:      "other-model", // This would give 46000, but capability wins
	}

	got := calc.Calculate(req, getModelMaxTokens)
	want := 196000 // planning-model: 200000 - 4000 headroom

	if got != want {
		t.Errorf("Calculate() with both capability and model = %d, want %d (capability should win)", got, want)
	}
}
