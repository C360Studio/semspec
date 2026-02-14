package contextbuilder

// BudgetCalculator calculates token budgets based on model capabilities.
type BudgetCalculator struct {
	defaultBudget  int
	headroomTokens int
}

// NewBudgetCalculator creates a new budget calculator.
func NewBudgetCalculator(defaultBudget, headroomTokens int) *BudgetCalculator {
	return &BudgetCalculator{
		defaultBudget:  defaultBudget,
		headroomTokens: headroomTokens,
	}
}

// Calculate determines the token budget for a request.
// Priority order:
// 1. Explicit TokenBudget in request
// 2. Model-based calculation from registry
// 3. Default budget
func (c *BudgetCalculator) Calculate(req *ContextBuildRequest, getModelMaxTokens func(modelName string) int) int {
	// 1. Explicit override takes precedence
	if req.TokenBudget > 0 {
		return req.TokenBudget
	}

	// 2. Try to get from model
	modelName := req.Model
	if modelName != "" {
		if maxTokens := getModelMaxTokens(modelName); maxTokens > 0 {
			return maxTokens - c.headroomTokens
		}
	}

	// 3. Fall back to default
	return c.defaultBudget
}
