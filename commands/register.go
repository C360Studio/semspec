// Package commands provides slash commands for the Semspec agent.
// Commands are registered globally via init() for use by agentic-dispatch.
package commands

import agenticdispatch "github.com/c360/semstreams/processor/agentic-dispatch"

func init() {
	agenticdispatch.RegisterCommand("spec", &SpecCommand{})
	agenticdispatch.RegisterCommand("propose", &ProposeCommand{})
	agenticdispatch.RegisterCommand("tasks", &TasksCommand{})
}
