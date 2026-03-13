package workflow

import (
	"encoding/json"
	"fmt"
	"os"
)

// ErrorCategory is a stable identifier for an error category.
// Values are defined in configs/error_categories.json and loaded at startup via
// LoadErrorCategories. Use the registry's IsValid method to validate a category ID
// before recording it on an agent.
type ErrorCategory = string

// ErrorCategoryDef defines an error category as a graph entity.
// Categories are seeded from configs/error_categories.json on startup and stored as
// graph entities so they can be referenced by agent triples.
type ErrorCategoryDef struct {
	// ID is the stable machine-readable identifier, e.g. "missing_tests".
	ID string `json:"id"`

	// Label is the short human-readable name, e.g. "Missing Tests".
	Label string `json:"label"`

	// Description explains what this category covers.
	Description string `json:"description"`

	// Signals lists observable patterns that indicate this category of error.
	// Used by the trend-based prompt injection to surface relevant guidance.
	Signals []string `json:"signals"`

	// Guidance is injected into the agent prompt when the error trend threshold
	// is reached. Should be actionable and specific to the category.
	Guidance string `json:"guidance"`
}

// errorCategoriesFile is the on-disk JSON envelope for the categories list.
type errorCategoriesFile struct {
	Categories []*ErrorCategoryDef `json:"categories"`
}

// ErrorCategoryRegistry loads and validates error categories from the JSON config.
// It provides O(1) lookup by ID and is safe for concurrent read access after construction.
type ErrorCategoryRegistry struct {
	categories map[string]*ErrorCategoryDef
}

// LoadErrorCategories reads and validates error categories from a JSON file at the given path.
// Returns an error if the file cannot be read, the JSON is malformed, any category is missing
// an ID, or any IDs are duplicated.
func LoadErrorCategories(path string) (*ErrorCategoryRegistry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read error categories: %w", err)
	}
	return LoadErrorCategoriesFromBytes(data)
}

// LoadErrorCategoriesFromBytes parses and validates error categories from JSON bytes.
// This variant is useful in tests where categories are provided inline rather than from disk.
func LoadErrorCategoriesFromBytes(data []byte) (*ErrorCategoryRegistry, error) {
	var file errorCategoriesFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse error categories: %w", err)
	}

	registry := &ErrorCategoryRegistry{
		categories: make(map[string]*ErrorCategoryDef, len(file.Categories)),
	}

	for _, cat := range file.Categories {
		if cat.ID == "" {
			return nil, fmt.Errorf("error category missing id")
		}
		if _, exists := registry.categories[cat.ID]; exists {
			return nil, fmt.Errorf("duplicate error category id: %q", cat.ID)
		}
		registry.categories[cat.ID] = cat
	}

	return registry, nil
}

// Get returns the category definition for the given ID.
// The second return value is false if the ID is not registered.
func (r *ErrorCategoryRegistry) Get(id string) (*ErrorCategoryDef, bool) {
	cat, ok := r.categories[id]
	return cat, ok
}

// All returns all registered category definitions.
// Order is not guaranteed; callers that need a stable order should sort the result.
func (r *ErrorCategoryRegistry) All() []*ErrorCategoryDef {
	result := make([]*ErrorCategoryDef, 0, len(r.categories))
	for _, cat := range r.categories {
		result = append(result, cat)
	}
	return result
}

// IsValid returns true if the given ID corresponds to a registered error category.
func (r *ErrorCategoryRegistry) IsValid(id string) bool {
	_, ok := r.categories[id]
	return ok
}
