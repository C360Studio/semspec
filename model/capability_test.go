package model

import "testing"

func TestCapabilityForRole(t *testing.T) {
	tests := []struct {
		role     string
		expected Capability
	}{
		// Core 5 roles (ADR-003)
		{"general", CapabilityFast},
		{"planner", CapabilityPlanning},
		{"developer", CapabilityCoding},
		{"reviewer", CapabilityReviewing},
		{"writer", CapabilityWriting},
		// Fallback
		{"unknown-role", CapabilityWriting},
		{"", CapabilityWriting},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			got := CapabilityForRole(tt.role)
			if got != tt.expected {
				t.Errorf("CapabilityForRole(%q) = %q, want %q", tt.role, got, tt.expected)
			}
		})
	}
}

func TestCapabilityIsValid(t *testing.T) {
	tests := []struct {
		cap      Capability
		expected bool
	}{
		{CapabilityPlanning, true},
		{CapabilityWriting, true},
		{CapabilityCoding, true},
		{CapabilityReviewing, true},
		{CapabilityFast, true},
		{Capability("invalid"), false},
		{Capability(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.cap), func(t *testing.T) {
			got := tt.cap.IsValid()
			if got != tt.expected {
				t.Errorf("Capability(%q).IsValid() = %v, want %v", tt.cap, got, tt.expected)
			}
		})
	}
}

func TestParseCapability(t *testing.T) {
	tests := []struct {
		input    string
		expected Capability
	}{
		{"planning", CapabilityPlanning},
		{"writing", CapabilityWriting},
		{"coding", CapabilityCoding},
		{"reviewing", CapabilityReviewing},
		{"fast", CapabilityFast},
		{"invalid", ""},
		{"", ""},
		{"PLANNING", ""}, // case-sensitive
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseCapability(tt.input)
			if got != tt.expected {
				t.Errorf("ParseCapability(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestCapabilityString(t *testing.T) {
	tests := []struct {
		cap      Capability
		expected string
	}{
		{CapabilityPlanning, "planning"},
		{CapabilityWriting, "writing"},
		{CapabilityCoding, "coding"},
		{CapabilityReviewing, "reviewing"},
		{CapabilityFast, "fast"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := tt.cap.String()
			if got != tt.expected {
				t.Errorf("Capability.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}
