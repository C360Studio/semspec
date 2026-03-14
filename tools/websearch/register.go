package websearch

import (
	"os"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

func init() {
	apiKey := os.Getenv("BRAVE_SEARCH_API_KEY")
	if apiKey == "" {
		// Don't register if no API key is configured.
		return
	}

	provider := NewBraveProvider(apiKey)
	exec := NewExecutor(provider)
	for _, tool := range exec.ListTools() {
		_ = agentictools.RegisterTool(tool.Name, exec)
	}
}
