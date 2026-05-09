package planmanager

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestDefaultConfig_AutoRejectOnExhaustionFalse pins the production-safe
// default: with no explicit config, the plan-manager preserves human-in-the-
// loop behavior on requirement-failure convergence (stay in implementing,
// await operator decision). Flipping this default to true would change
// production semantics for everyone — must be opt-in per environment.
func TestDefaultConfig_AutoRejectOnExhaustionFalse(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.AutoRejectOnExhaustion {
		t.Errorf("DefaultConfig.AutoRejectOnExhaustion = true, want false (production must default to human-in-the-loop)")
	}
}

// TestConfigUnmarshal_AutoRejectOnExhaustion confirms the JSON field name
// matches what the e2e configs set. Renaming the JSON tag would silently
// disable the autonomous-fail-fast behavior in every E2E run — they'd
// fall back to default false and Playwright would time out at 40 minutes
// again. Pinning the on-the-wire name here catches that.
func TestConfigUnmarshal_AutoRejectOnExhaustion(t *testing.T) {
	tests := []struct {
		name string
		json string
		want bool
	}{
		{
			name: "explicit true (e2e config shape)",
			json: `{"execution_bucket_name":"X","event_stream_name":"Y","auto_reject_on_exhaustion":true}`,
			want: true,
		},
		{
			name: "explicit false (production opt-out)",
			json: `{"execution_bucket_name":"X","event_stream_name":"Y","auto_reject_on_exhaustion":false}`,
			want: false,
		},
		{
			name: "unset (default — human-in-the-loop)",
			json: `{"execution_bucket_name":"X","event_stream_name":"Y"}`,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			if err := json.Unmarshal([]byte(tt.json), &cfg); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if cfg.AutoRejectOnExhaustion != tt.want {
				t.Errorf("AutoRejectOnExhaustion = %v, want %v", cfg.AutoRejectOnExhaustion, tt.want)
			}
		})
	}
}

// TestCountBlockedByFailure pins the take-24 fix: when a requirement fails,
// every requirement that transitively depends_on it can never reach a
// successful terminal state (the orchestrator only dispatches reqs whose
// deps completed). Without counting these as terminal-equivalent, the plan
// hangs in implementing forever waiting for reqs that will never start.
func TestCountBlockedByFailure(t *testing.T) {
	tests := []struct {
		name      string
		plan      *workflow.Plan
		failedIDs map[string]bool
		want      int
	}{
		{
			name:      "nil plan returns 0",
			plan:      nil,
			failedIDs: map[string]bool{"req-1": true},
			want:      0,
		},
		{
			name: "no failures returns 0",
			plan: &workflow.Plan{
				Requirements: []workflow.Requirement{
					{ID: "req-1", DependsOn: nil},
					{ID: "req-2", DependsOn: []string{"req-1"}},
				},
			},
			failedIDs: map[string]bool{},
			want:      0,
		},
		{
			name: "take-24 shape: req-1 failed, req-2 depends on req-1",
			plan: &workflow.Plan{
				Requirements: []workflow.Requirement{
					{ID: "req-1", DependsOn: nil},
					{ID: "req-2", DependsOn: []string{"req-1"}},
				},
			},
			failedIDs: map[string]bool{"req-1": true},
			want:      1, // req-2 is blocked
		},
		{
			name: "transitive chain: req-1 → req-2 → req-3, req-1 fails",
			plan: &workflow.Plan{
				Requirements: []workflow.Requirement{
					{ID: "req-1", DependsOn: nil},
					{ID: "req-2", DependsOn: []string{"req-1"}},
					{ID: "req-3", DependsOn: []string{"req-2"}},
				},
			},
			failedIDs: map[string]bool{"req-1": true},
			want:      2, // req-2 + req-3 both blocked transitively
		},
		{
			name: "fan-out: req-1 fails, req-2 + req-3 + req-4 all depend on it",
			plan: &workflow.Plan{
				Requirements: []workflow.Requirement{
					{ID: "req-1"},
					{ID: "req-2", DependsOn: []string{"req-1"}},
					{ID: "req-3", DependsOn: []string{"req-1"}},
					{ID: "req-4", DependsOn: []string{"req-1"}},
				},
			},
			failedIDs: map[string]bool{"req-1": true},
			want:      3,
		},
		{
			name: "independent reqs are NOT blocked when peer fails",
			plan: &workflow.Plan{
				Requirements: []workflow.Requirement{
					{ID: "req-1"},
					{ID: "req-2"}, // no depends_on — independent
				},
			},
			failedIDs: map[string]bool{"req-1": true},
			want:      0,
		},
		{
			name: "multi-dep: req-3 depends on both req-1 (failed) and req-2 (running) — still blocked",
			plan: &workflow.Plan{
				Requirements: []workflow.Requirement{
					{ID: "req-1"},
					{ID: "req-2"},
					{ID: "req-3", DependsOn: []string{"req-1", "req-2"}},
				},
			},
			failedIDs: map[string]bool{"req-1": true},
			want:      1, // req-3 can never run because one of its deps failed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countBlockedByFailure(tt.plan, tt.failedIDs)
			if got != tt.want {
				t.Errorf("countBlockedByFailure = %d, want %d", got, tt.want)
			}
		})
	}
}
