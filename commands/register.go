// Package commands provides slash commands for the Semspec agent.
// Commands are registered globally via init() for use by agentic-dispatch.
package commands

import (
	"sync"

	"github.com/c360studio/semspec/model"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
)

var (
	// modelRegistry is the global model registry for capability-based model selection.
	modelRegistry     *model.Registry
	modelRegistryOnce sync.Once
)

// GetModelRegistry returns the global model registry, creating a default one if needed.
func GetModelRegistry() *model.Registry {
	modelRegistryOnce.Do(func() {
		if modelRegistry == nil {
			modelRegistry = model.NewDefaultRegistry()
		}
	})
	return modelRegistry
}

// SetModelRegistry sets the global model registry.
// Should be called early in application startup before commands execute.
func SetModelRegistry(r *model.Registry) {
	modelRegistry = r
}

func init() {
	// Workflow commands (legacy)
	agenticdispatch.RegisterCommand("propose", &ProposeCommand{})
	agenticdispatch.RegisterCommand("design", &DesignCommand{})
	agenticdispatch.RegisterCommand("spec", &SpecCommand{})
	agenticdispatch.RegisterCommand("tasks", &TasksCommand{})

	// Workflow commands (new - ADR-003)
	agenticdispatch.RegisterCommand("plan", &PlanCommand{})
	agenticdispatch.RegisterCommand("explore", &ExploreCommand{})
	agenticdispatch.RegisterCommand("promote", &PromoteCommand{})

	// Validation commands
	agenticdispatch.RegisterCommand("check", &CheckCommand{})
	agenticdispatch.RegisterCommand("approve", &ApproveCommand{})

	// Lifecycle commands
	agenticdispatch.RegisterCommand("archive", &ArchiveCommand{})
	agenticdispatch.RegisterCommand("changes", &StatusCommand{})

	// Integration commands
	agenticdispatch.RegisterCommand("github", &GitHubCommand{})

	// Utility commands
	agenticdispatch.RegisterCommand("help", &HelpCommand{})
	agenticdispatch.RegisterCommand("context", &ContextCommand{})

	// Coordination commands (Knowledge Gap Resolution)
	agenticdispatch.RegisterCommand("ask", &AskCommand{})
	agenticdispatch.RegisterCommand("questions", &QuestionsCommand{})
	agenticdispatch.RegisterCommand("answer", &AnswerCommand{})

	// Debug commands (Observability)
	agenticdispatch.RegisterCommand("debug", &DebugCommand{})
}
