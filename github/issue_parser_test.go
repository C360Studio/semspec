package github

import "testing"

func TestParseIssueBody(t *testing.T) {
	tests := []struct {
		name string
		body string
		want ParsedIssue
	}{
		{
			name: "full issue form body",
			body: `### Description

Add OAuth2 support for enterprise SSO.
Users should be able to log in with their corporate identity.

### Scope

src/auth/**, src/middleware/**

### Constraints

Must not break existing session-based auth.

### Priority

High`,
			want: ParsedIssue{
				Description: "Add OAuth2 support for enterprise SSO.\nUsers should be able to log in with their corporate identity.",
				Scope:       "src/auth/**, src/middleware/**",
				Constraints: "Must not break existing session-based auth.",
				Priority:    "high",
			},
		},
		{
			name: "description only",
			body: `### Description

Fix the login page timeout.`,
			want: ParsedIssue{
				Description: "Fix the login page timeout.",
				Priority:    "normal",
			},
		},
		{
			name: "empty sections",
			body: `### Description

Fix bug

### Scope

### Constraints

### Priority
`,
			want: ParsedIssue{
				Description: "Fix bug",
				Priority:    "normal",
			},
		},
		{
			name: "no headings (plain text body)",
			body: "Please fix the authentication timeout issue when using SSO.",
			want: ParsedIssue{
				Priority: "normal",
			},
		},
		{
			name: "priority normalization",
			body: `### Description

Test

### Priority

LOW`,
			want: ParsedIssue{
				Description: "Test",
				Priority:    "low",
			},
		},
		{
			name: "unknown priority defaults to normal",
			body: `### Description

Test

### Priority

Critical`,
			want: ParsedIssue{
				Description: "Test",
				Priority:    "normal",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseIssueBody(tt.body)
			if got.Description != tt.want.Description {
				t.Errorf("Description = %q, want %q", got.Description, tt.want.Description)
			}
			if got.Scope != tt.want.Scope {
				t.Errorf("Scope = %q, want %q", got.Scope, tt.want.Scope)
			}
			if got.Constraints != tt.want.Constraints {
				t.Errorf("Constraints = %q, want %q", got.Constraints, tt.want.Constraints)
			}
			if got.Priority != tt.want.Priority {
				t.Errorf("Priority = %q, want %q", got.Priority, tt.want.Priority)
			}
		})
	}
}

func TestIssue_HasLabel(t *testing.T) {
	issue := Issue{
		Labels: []Label{{Name: "semspec"}, {Name: "bug"}},
	}
	if !issue.HasLabel("semspec") {
		t.Error("HasLabel(semspec) = false, want true")
	}
	if issue.HasLabel("feature") {
		t.Error("HasLabel(feature) = true, want false")
	}
}
