package planmanager

import (
	"encoding/json"
	"testing"
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
