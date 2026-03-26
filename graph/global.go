package graph

import "sync"

// globalRegistry holds the process-wide GraphRegistry singleton.
// Initialized once via SetGlobalRegistry (from main.go) before components start.
// Components access it via GlobalRegistry().
var (
	globalRegistry *Registry
	globalMu       sync.RWMutex
)

// SetGlobalRegistry sets the process-wide graph registry.
// Must be called before component initialization.
func SetGlobalRegistry(r *Registry) {
	globalMu.Lock()
	globalRegistry = r
	globalMu.Unlock()
}

// GlobalRegistry returns the process-wide graph registry.
// Returns nil if not initialized (local-only mode — components fall back to
// single-source Gatherer via their config's graph_gateway_url).
func GlobalRegistry() *Registry {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalRegistry
}
