package model

import "sync"

// Global registry instance and initialization guard.
var (
	globalRegistry *Registry
	globalOnce     sync.Once
)

// Global returns the singleton registry instance.
// Creates a default registry on first call if not already initialized.
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
