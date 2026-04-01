package workflow

import (
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// manifestClient is the package-level singleton for graph manifest fetching.
var manifestClient *ManifestClient

// GetManifestClient returns the package-level manifest client singleton.
// Returns nil if the graph gateway URL is not configured.
func GetManifestClient() *ManifestClient {
	return manifestClient
}

// Register initializes the manifest client and registers graph tools
// (graph_search, graph_query, graph_summary).
func Register() {
	manifestClient = NewManifestClient(getGatewayURL(), nil)
	graphExec := NewGraphExecutor()
	_ = agentictools.RegisterTool("graph_summary", graphExec)
	_ = agentictools.RegisterTool("graph_search", graphExec)
	_ = agentictools.RegisterTool("graph_query", graphExec)
}
