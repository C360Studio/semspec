package httptool

import (
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// Register registers the http_request tool on the supplied registry with an
// optional NATS client. Pass nil for nc to register without graph persistence.
func Register(reg *agentictools.ExecutorRegistry, nc NATSClient, opts ...Option) error {
	exec := NewExecutor(nc, opts...)
	return reg.RegisterTool("http_request", exec)
}
