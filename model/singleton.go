package model

import "sync"

// Global registry instance and initialization guard.
//
// Deprecated: callers should consume model.RegistryReader from
// component.Dependencies.ModelRegistry instead. The global is parsed once
// at startup from config and is not kept in sync with the semstreams
// config.Manager (which subscribes to NATS KV updates), so the two can
// diverge at runtime — manifesting as routing bugs where the dispatched
// model name doesn't match what agentic-loop sends to the LLM endpoint.
// Only web-ingester still relies on this singleton, because llm/client.go
// and llm/governor.go consume the semspec/model.Registry concrete type for
// endpoint health tracking which the semstreams registry does not expose.
var (
	globalRegistry *Registry
	globalOnce     sync.Once
)

// Global returns the singleton registry instance.
// Creates a default registry on first call if not already initialized.
//
// Deprecated: see package-level note. Use deps.ModelRegistry.
func Global() *Registry {
	globalOnce.Do(func() {
		globalRegistry = NewDefaultRegistry()
	})
	return globalRegistry
}

// InitGlobal initializes the global registry with a custom instance.
// Must be called before any call to Global() to take effect.
// Safe for concurrent use but only the first call has any effect.
func InitGlobal(r *Registry) {
	globalOnce.Do(func() {
		globalRegistry = r
	})
}

// ResetGlobal resets the global registry for testing purposes.
// This is NOT thread-safe and should only be used in tests.
func ResetGlobal() {
	globalOnce = sync.Once{}
	globalRegistry = nil
}
