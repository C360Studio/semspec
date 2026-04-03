package graph

import "sync"

// globalSources holds the process-wide SourceRegistry singleton.
// Initialized once via SetGlobalSources (from main.go) before components start.
// Components access it via GlobalSources().
var (
	globalSources *SourceRegistry
	globalMu      sync.RWMutex
)

// SetGlobalSources stores the process-wide graph source registry.
// Called once during application startup before components start.
func SetGlobalSources(r *SourceRegistry) {
	globalMu.Lock()
	globalSources = r
	globalMu.Unlock()
}

// GlobalSources returns the process-wide graph source registry, or nil
// when graph sources are not configured.
func GlobalSources() *SourceRegistry {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalSources
}
