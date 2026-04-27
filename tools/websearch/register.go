package websearch

import (
	"os"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// Register registers the web_search tool on the supplied registry if
// BRAVE_SEARCH_API_KEY is set. Returns nil when the env var is missing
// (the tool is intentionally disabled, not an error).
func Register(reg *agentictools.ExecutorRegistry) error {
	apiKey := os.Getenv("BRAVE_SEARCH_API_KEY")
	if apiKey == "" {
		return nil
	}
	provider := NewBraveProvider(apiKey)
	exec := NewExecutor(provider)
	return reg.RegisterTool("web_search", exec)
}
