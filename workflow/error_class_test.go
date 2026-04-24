package workflow

import "testing"

// TestClassifyErrorReason pins the cross-layer contract between
// execution-manager (which writes INFRASTRUCTURE:-prefixed reasons) and
// Phase 5 consumers (plan-manager's InfraHealth tracker, retry endpoint,
// UI). If either side drifts from the prefix, this test fails loudly.
func TestClassifyErrorReason(t *testing.T) {
	cases := []struct {
		reason string
		want   string
	}{
		{"", ErrorClassAgent},
		{"merge_failed: conflict on foo.go", ErrorClassAgent},
		{"validator rejected: 3 tests failed", ErrorClassAgent},
		{"INFRASTRUCTURE: merge_failed: sandbox needs reconciliation", ErrorClassInfrastructure},
		{"INFRASTRUCTURE:", ErrorClassInfrastructure},
		{"  INFRASTRUCTURE: leading whitespace is agent", ErrorClassAgent}, // prefix must be literal-first
		{"something INFRASTRUCTURE: embedded", ErrorClassAgent},
	}
	for _, tc := range cases {
		if got := ClassifyErrorReason(tc.reason); got != tc.want {
			t.Errorf("ClassifyErrorReason(%q) = %q, want %q", tc.reason, got, tc.want)
		}
	}
}
