// Package model provides capability-based model selection for workflow tasks.
// Instead of hardcoding model names, commands specify capabilities (planning, writing, coding)
// and the registry resolves them to available models with fallback chains.
package model

// Capability represents a semantic capability for model selection.
// Instead of specifying "claude-sonnet", users specify "writing" or "planning".
type Capability string

const (
	// CapabilityPlanning is for high-level reasoning, architecture decisions.
	CapabilityPlanning Capability = "planning"

	// CapabilityWriting is for documentation, proposals, specifications.
	CapabilityWriting Capability = "writing"

	// CapabilityCoding is for code generation, implementation.
	CapabilityCoding Capability = "coding"

	// CapabilityReviewing is for code review, quality analysis.
	CapabilityReviewing Capability = "reviewing"

	// CapabilityFast is for quick responses, simple tasks.
	CapabilityFast Capability = "fast"
)

// RoleCapabilities maps workflow roles to their default capability.
// Used when no explicit capability or model is specified.
var RoleCapabilities = map[string]Capability{
	"proposal-writer": CapabilityWriting,
	"design-writer":   CapabilityPlanning,
	"spec-writer":     CapabilityWriting,
	"tasks-writer":    CapabilityPlanning,
}

// CapabilityForRole returns the default capability for a given role.
// Returns CapabilityWriting as fallback for unknown roles.
func CapabilityForRole(role string) Capability {
	if cap, ok := RoleCapabilities[role]; ok {
		return cap
	}
	return CapabilityWriting
}

// IsValid checks if a capability string is a known capability.
func (c Capability) IsValid() bool {
	switch c {
	case CapabilityPlanning, CapabilityWriting, CapabilityCoding, CapabilityReviewing, CapabilityFast:
		return true
	}
	return false
}

// String returns the string representation of the capability.
func (c Capability) String() string {
	return string(c)
}

// ParseCapability converts a string to a Capability, returning empty for invalid values.
func ParseCapability(s string) Capability {
	cap := Capability(s)
	if cap.IsValid() {
		return cap
	}
	return ""
}
