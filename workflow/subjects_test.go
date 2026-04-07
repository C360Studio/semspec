package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTypedSubjectPatterns(t *testing.T) {
	assert.Equal(t, "workflow.events.requirements.generated", RequirementsGenerated.Pattern)
	assert.Equal(t, "workflow.events.scenarios.requirement_generated", ScenariosForRequirementGenerated.Pattern)
	assert.Equal(t, "workflow.events.scenarios.generated", ScenariosGenerated.Pattern)
	assert.Equal(t, "workflow.events.generation.failed", GenerationFailed.Pattern)
	assert.Equal(t, "workflow.events.requirement.execution_complete", RequirementExecutionComplete.Pattern)
	assert.Equal(t, "user.signal.escalate", UserEscalation.Pattern)
	assert.Equal(t, "user.signal.error", UserSignalError.Pattern)
}
