package contextbuilder

import "github.com/c360studio/semspec/model"

// CapabilityResolver looks up the model for a capability.
type CapabilityResolver interface {
	Resolve(cap model.Capability) string
}

// BudgetCalculator calculates token budgets based on model capabilities.
type BudgetCalculator struct {
	defaultBudget      int
	headroomTokens     int
	capabilityResolver CapabilityResolver
}

// NewBudgetCalculator creates a new budget calculator.
func NewBudgetCalculator(defaultBudget, headroomTokens int) *BudgetCalculator {
	return &BudgetCalculator{
		defaultBudget:  defaultBudget,
		headroomTokens: headroomTokens,
	}
}

// SetCapabilityResolver sets the capability resolver for model lookup.
func (c *BudgetCalculator) SetCapabilityResolver(resolver CapabilityResolver) {
	c.capabilityResolver = resolver
}

// Calculate determines the token budget for a request.
// Priority order:
// 1. Explicit TokenBudget in request
// 2. Capability-based calculation (lookup model for capability, then get max tokens)
// 3. Model-based calculation from registry
// 4. Default budget
func (c *BudgetCalculator) Calculate(req *ContextBuildRequest, getModelMaxTokens func(modelName string) int) int {
	// 1. Explicit override takes precedence
	if req.TokenBudget > 0 {
		return req.TokenBudget
	}

	// 2. Try to get from capability (resolves capability -> model -> max_tokens)
	if req.Capability != "" && c.capabilityResolver != nil {
		cap := model.ParseCapability(req.Capability)
		if cap != "" {
			modelName := c.capabilityResolver.Resolve(cap)
			if maxTokens := getModelMaxTokens(modelName); maxTokens > 0 {
				return maxTokens - c.headroomTokens
			}
		}
	}

	// 3. Try to get from explicit model
	if req.Model != "" {
		if maxTokens := getModelMaxTokens(req.Model); maxTokens > 0 {
			return maxTokens - c.headroomTokens
		}
	}

	// 4. Fall back to default
	return c.defaultBudget
}
