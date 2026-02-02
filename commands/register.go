// Package commands provides slash commands for the Semspec agent.
// Commands are registered globally via init() for use by agentic-dispatch.
package commands

import agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"

func init() {
	// Workflow commands
	agenticdispatch.RegisterCommand("propose", &ProposeCommand{})
	agenticdispatch.RegisterCommand("design", &DesignCommand{})
	agenticdispatch.RegisterCommand("spec", &SpecCommand{})
	agenticdispatch.RegisterCommand("tasks", &TasksCommand{})

	// Validation commands
	agenticdispatch.RegisterCommand("check", &CheckCommand{})
	agenticdispatch.RegisterCommand("approve", &ApproveCommand{})

	// Lifecycle commands
	agenticdispatch.RegisterCommand("archive", &ArchiveCommand{})
	agenticdispatch.RegisterCommand("changes", &StatusCommand{})

	// Integration commands
	agenticdispatch.RegisterCommand("github", &GitHubCommand{})
}
