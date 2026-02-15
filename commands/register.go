// Package commands provides slash commands for the Semspec agent.
// Commands are registered globally via init() for use by agentic-dispatch.
package commands

import (
	"github.com/c360studio/semspec/model"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
)

// GetModelRegistry returns the global model registry.
// Delegates to model.Global() for centralized singleton management.
func GetModelRegistry() *model.Registry {
	return model.Global()
}

// SetModelRegistry sets the global model registry.
// Should be called early in application startup before commands execute.
// Delegates to model.InitGlobal() for centralized singleton management.
func SetModelRegistry(r *model.Registry) {
	model.InitGlobal(r)
}

func init() {
	// Workflow commands (ADR-003)
	agenticdispatch.RegisterCommand("plan", &PlanCommand{})
	agenticdispatch.RegisterCommand("approve", &ApproveCommand{})
	agenticdispatch.RegisterCommand("tasks", &TasksCommand{})
	agenticdispatch.RegisterCommand("execute", &ExecuteCommand{})

	// Validation commands
	agenticdispatch.RegisterCommand("check", &CheckCommand{})

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
