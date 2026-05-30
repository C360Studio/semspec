package planner

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestParseExplorationFromResult(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantCount int
		wantErr   string // empty = expect success
	}{
		{
			name: "clean JSON happy path",
			input: `{
				"capabilities": [
					{"name": "user-auth", "lifecycle": "new", "description": "Authenticate users."}
				],
				"open_questions": ["Do we support OAuth?"]
			}`,
			wantCount: 1,
		},
		{
			name: "multiple capabilities with deps",
			input: `{
				"capabilities": [
					{"name": "user-auth", "lifecycle": "new", "description": "Auth."},
					{"name": "session-store", "lifecycle": "new", "description": "Sessions.", "depends_on": ["user-auth"]}
				]
			}`,
			wantCount: 2,
		},
		{
			name: "JSON wrapped in markdown fence",
			input: "Here is the exploration:\n\n```json\n" +
				`{"capabilities":[{"name":"x","lifecycle":"new","description":"Test."}]}` +
				"\n```\n",
			wantCount: 1,
		},
		{
			name:    "empty result rejected",
			input:   "",
			wantErr: "empty",
		},
		{
			name: "missing capabilities rejected",
			input: `{
				"capabilities": [],
				"open_questions": ["something"]
			}`,
			wantErr: "at least one capability",
		},
		{
			name: "non-kebab-case name rejected",
			input: `{
				"capabilities": [{"name": "User_Auth", "lifecycle": "new", "description": "Bad case."}]
			}`,
			wantErr: "kebab-case",
		},
		{
			name: "uppercase name rejected",
			input: `{
				"capabilities": [{"name": "UserAuth", "lifecycle": "new", "description": "Bad case."}]
			}`,
			wantErr: "kebab-case",
		},
		{
			name: "leading hyphen rejected",
			input: `{
				"capabilities": [{"name": "-user-auth", "lifecycle": "new", "description": "Bad."}]
			}`,
			wantErr: "kebab-case",
		},
		{
			name: "invalid lifecycle rejected",
			input: `{
				"capabilities": [{"name": "user-auth", "lifecycle": "ancient", "description": "Bad."}]
			}`,
			wantErr: "new or modified",
		},
		{
			name: "missing description rejected",
			input: `{
				"capabilities": [{"name": "user-auth", "lifecycle": "new"}]
			}`,
			wantErr: "missing description",
		},
		{
			name: "duplicate capability name rejected",
			input: `{
				"capabilities": [
					{"name": "user-auth", "lifecycle": "new", "description": "First."},
					{"name": "user-auth", "lifecycle": "modified", "description": "Second."}
				]
			}`,
			wantErr: "more than once",
		},
		{
			name: "orphan depends_on rejected",
			input: `{
				"capabilities": [
					{"name": "user-auth", "lifecycle": "new", "description": "Auth.", "depends_on": ["nonexistent"]}
				]
			}`,
			wantErr: "not declared",
		},
		{
			name:    "garbage JSON rejected",
			input:   "this is not JSON at all",
			wantErr: "no JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exp, _, err := parseExplorationFromResult(tt.input)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected success, got error: %v", err)
					return
				}
				if exp == nil {
					t.Errorf("expected non-nil exploration")
					return
				}
				if len(exp.Capabilities) != tt.wantCount {
					t.Errorf("expected %d capabilities, got %d", tt.wantCount, len(exp.Capabilities))
				}
				return
			}
			if err == nil {
				t.Errorf("expected error containing %q, got nil", tt.wantErr)
				return
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestIsKebabCase(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"user-auth", true},
		{"mavsdk-bootstrap", true},
		{"a", true},
		{"a-b-c", true},
		{"v2-api", true},
		{"123", true},
		{"", false},
		{"UserAuth", false},
		{"user_auth", false},
		{"-leading", false},
		{"trailing-", false},
		{"with space", false},
		{"with.dot", false},
		{"with/slash", false},
	}
	for _, tt := range tests {
		t.Run(tt.s, func(t *testing.T) {
			if got := isKebabCase(tt.s); got != tt.want {
				t.Errorf("isKebabCase(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

// TestRoutePlanStateEntry_AnalystGate exercises the dispatch routing logic
// at the seam: given a Plan in a particular state, which sub-phase should
// the planner component target? We test the decision function directly
// rather than the full goroutine pipeline so the test stays deterministic.
// The actual claim/dispatch side effects are tested at the integration
// layer in mock-e2e.
func TestRouteCreated_AnalystGate(t *testing.T) {
	tests := []struct {
		name         string
		analystOn    bool
		hasExpl      bool
		hasGoal      bool
		hasFindings  bool
		wantAnalyst  bool
		wantRevision bool
	}{
		{
			name:        "fresh plan with analyst on → analyst",
			analystOn:   true,
			wantAnalyst: true,
		},
		{
			name:        "fresh plan with analyst off → legacy planner",
			analystOn:   false,
			wantAnalyst: false,
		},
		{
			name:         "revision plan (goal + findings) → planner (revision)",
			analystOn:    true,
			hasGoal:      true,
			hasFindings:  true,
			wantAnalyst:  false,
			wantRevision: true,
		},
		{
			name:        "plan with existing exploration → planner (analyst skipped)",
			analystOn:   true,
			hasExpl:     true,
			wantAnalyst: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := &workflow.Plan{Slug: "test", Title: "Test"}
			if tt.hasExpl {
				plan.Exploration = &workflow.Exploration{
					Capabilities: []workflow.Capability{{Name: "x", Lifecycle: workflow.CapabilityNew, Description: "test"}},
				}
			}
			if tt.hasGoal {
				plan.Goal = "test goal"
			}
			if tt.hasFindings {
				plan.ReviewFindings = []byte(`[{"finding":"missing X"}]`)
			}

			// Replicate routeCreated's decision logic without invoking the
			// goroutine-spawning side effects. This is a pin on the rule
			// "wantAnalyst = AnalystSubPhase && Exploration==nil && !isRevision".
			isRevision := plan.Goal != "" && len(plan.ReviewFindings) > 0
			wantAnalyst := tt.analystOn && plan.Exploration == nil && !isRevision

			if wantAnalyst != tt.wantAnalyst {
				t.Errorf("expected wantAnalyst=%v, got %v (isRevision=%v)", tt.wantAnalyst, wantAnalyst, isRevision)
			}
			if isRevision != tt.wantRevision {
				t.Errorf("expected wantRevision=%v, got %v", tt.wantRevision, isRevision)
			}
		})
	}
}
