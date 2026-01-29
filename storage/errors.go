package storage

import "errors"

// Common storage errors.
var (
	// ErrNotFound is returned when an entity is not found.
	ErrNotFound = errors.New("entity not found")
)
