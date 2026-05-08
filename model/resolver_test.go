package model

import "testing"

func TestResolveModel(t *testing.T) {
	reg := NewRegistry(map[Capability]*CapabilityConfig{
		CapabilityCoding: {
			Preferred: []string{"dense-coder"},
		},
		CapabilityReviewing: {
			Preferred: []string{"reviewer-model"},
		},
		// CapabilityPlanning intentionally absent so the "registry has
		// no entry" branch falls through to defaults.Model.
	}, map[string]*EndpointConfig{
		"dense-coder":    {Provider: "openai", Model: "dense-coder-1"},
		"reviewer-model": {Provider: "openai", Model: "rev-1"},
		"default":        {Provider: "openai", Model: "default-1"},
	})

	resolver := AsCapabilityResolver(reg)

	tests := []struct {
		name       string
		resolver   CapabilityResolver
		override   string
		capability Capability
		want       string
	}{
		{
			name:       "override wins over registry",
			resolver:   resolver,
			override:   "moe-model",
			capability: CapabilityCoding,
			want:       "moe-model",
		},
		{
			name:       "registry resolves capability when no override",
			resolver:   resolver,
			override:   "",
			capability: CapabilityCoding,
			want:       "dense-coder",
		},
		{
			name:       "different capability resolves to different endpoint",
			resolver:   resolver,
			override:   "",
			capability: CapabilityReviewing,
			want:       "reviewer-model",
		},
		{
			name:       "missing capability falls back to registry default",
			resolver:   resolver,
			override:   "",
			capability: CapabilityPlanning,
			want:       "default", // Registry.Resolve returns defaults.Model
		},
		{
			name:       "nil resolver with override returns override",
			resolver:   nil,
			override:   "pinned",
			capability: CapabilityCoding,
			want:       "pinned",
		},
		{
			name:       "nil resolver with no override returns empty",
			resolver:   nil,
			override:   "",
			capability: CapabilityCoding,
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveModel(tt.resolver, tt.override, tt.capability)
			if got != tt.want {
				t.Errorf("ResolveModel(reg, %q, %q) = %q, want %q",
					tt.override, tt.capability, got, tt.want)
			}
		})
	}
}

// TestAsCapabilityResolver_NilSafe verifies the adapter handles nil registries.
func TestAsCapabilityResolver_NilSafe(t *testing.T) {
	if got := AsCapabilityResolver(nil); got != nil {
		t.Errorf("AsCapabilityResolver(nil) = %v, want nil", got)
	}
}
