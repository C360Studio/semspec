package planmanager

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestResolveAutoRejectOnExhaustion pins the override semantics. Per-plan
// override wins; nil falls back to component config; nil plan falls back to
// component config. Production code paths must keep their existing behaviour
// when no override is set, so each branch is asserted explicitly.
func TestResolveAutoRejectOnExhaustion(t *testing.T) {
	truePtr := func() *bool { v := true; return &v }
	falsePtr := func() *bool { v := false; return &v }

	tests := []struct {
		name string
		plan *workflow.Plan
		cfg  Config
		want bool
		why  string
	}{
		{
			name: "nil plan falls back to config true",
			plan: nil,
			cfg:  Config{AutoRejectOnExhaustion: true},
			want: true,
			why:  "GitHub-issue mutation path passes nil; must respect component config",
		},
		{
			name: "nil plan falls back to config false (production default)",
			plan: nil,
			cfg:  Config{AutoRejectOnExhaustion: false},
			want: false,
			why:  "production default: stall-and-await-operator preserved when no override",
		},
		{
			name: "plan override nil falls back to config true",
			plan: &workflow.Plan{},
			cfg:  Config{AutoRejectOnExhaustion: true},
			want: true,
			why:  "existing plans created before this field existed have nil override",
		},
		{
			name: "plan override nil falls back to config false",
			plan: &workflow.Plan{},
			cfg:  Config{AutoRejectOnExhaustion: false},
			want: false,
			why:  "production default preserved for existing plans",
		},
		{
			name: "plan override true beats config false",
			plan: &workflow.Plan{AutoRejectOnExhaustion: truePtr()},
			cfg:  Config{AutoRejectOnExhaustion: false},
			want: true,
			why:  "operator pinned this plan to autonomous fail-fast despite fleet config",
		},
		{
			name: "plan override false beats config true",
			plan: &workflow.Plan{AutoRejectOnExhaustion: falsePtr()},
			cfg:  Config{AutoRejectOnExhaustion: true},
			want: false,
			why:  "iteration-exhaustion test scenario opts back into stall path in autonomous fleet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveAutoRejectOnExhaustion(tt.plan, tt.cfg)
			if got != tt.want {
				t.Errorf("resolveAutoRejectOnExhaustion = %v, want %v (%s)", got, tt.want, tt.why)
			}
		})
	}
}
