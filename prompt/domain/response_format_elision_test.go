package domain

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/prompt"
)

// TestOutputFormatElidesSchemaProse exercises elideSchemaProse: when
// HasResponseFormat is true the schema-prose prelude (intro line +
// "Required: X (string)..." listing) drops out, while semantic guidance
// and behavioral directives stay. Frontier providers (HasResponseFormat
// false) keep the prompt verbatim.
func TestOutputFormatElidesSchemaProse(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Software()...)
	a := prompt.NewAssembler(r)

	tests := []struct {
		role            prompt.Role
		dropWhenSet     []string // substrings that MUST be elided when HasResponseFormat=true
		keepAlways      []string // substrings that MUST appear in both modes
	}{
		{
			role: prompt.RoleDeveloper,
			dropWhenSet: []string{
				"When your changes are complete, call the submit_work tool with these JSON fields:",
				`"summary": "Implemented /goodbye endpoint with tests"`,
				"Required: summary (string)",
			},
			keepAlways: []string{
				"Respond ONLY via the submit_work tool call",
			},
		},
		{
			role: prompt.RolePlanner,
			dropWhenSet: []string{
				"When your plan is ready, call the submit_work tool with these JSON fields:",
				`"goal": "Add /goodbye endpoint with JSON response and tests"`,
				"Required: goal (string), context (string)",
			},
			keepAlways: []string{
				"CRITICAL — scope.include is for files that ALREADY EXIST",
				"Respond ONLY via the submit_work tool call",
			},
		},
		{
			role: prompt.RolePlanReviewer,
			dropWhenSet: []string{
				"When your review is complete, call the submit_work tool with these JSON fields:",
				`"sop_id": "api-testing"`,
			},
			keepAlways: []string{
				"CRITICAL: findings drive the verdict",
				"Respond ONLY via the submit_work tool call",
			},
		},
		{
			role: prompt.RoleScenarioGenerator,
			dropWhenSet: []string{
				"When your scenarios are ready, call the submit_work tool with these JSON fields:",
				`"title": "Goodbye endpoint returns correct JSON"`,
				"Required: scenarios",
			},
			keepAlways: []string{
				"Respond ONLY via the submit_work tool call",
			},
		},
		{
			role: prompt.RoleArchitect,
			dropWhenSet: []string{
				"When your architecture analysis is ready, call the submit_work tool with these JSON fields:",
				`"category": "web_framework"`,
				"Required: technology_choices, component_boundaries",
			},
			keepAlways: []string{
				"Respond ONLY via the submit_work tool call",
			},
		},
		{
			role: prompt.RoleReviewer,
			dropWhenSet: []string{
				"When your review is complete, call the submit_work tool with these JSON fields.",
				`"feedback": "Implementation correctly adds /goodbye endpoint`,
			},
			keepAlways: []string{
				"REQUIRED fields:",
				"rejection_type: MUST be \"fixable\" or \"restructure\"",
				"Respond ONLY via the submit_work tool call",
			},
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			withRF := a.Assemble(&prompt.AssemblyContext{
				Role:              tt.role,
				HasResponseFormat: true,
			}).SystemMessage
			withoutRF := a.Assemble(&prompt.AssemblyContext{
				Role:              tt.role,
				HasResponseFormat: false,
			}).SystemMessage

			for _, snippet := range tt.dropWhenSet {
				if strings.Contains(withRF, snippet) {
					t.Errorf("HasResponseFormat=true: should NOT contain %q", snippet)
				}
				if !strings.Contains(withoutRF, snippet) {
					t.Errorf("HasResponseFormat=false: should contain %q (regression in default prompt)", snippet)
				}
			}
			for _, snippet := range tt.keepAlways {
				if !strings.Contains(withRF, snippet) {
					t.Errorf("HasResponseFormat=true: lost %q (semantic content must survive elision)", snippet)
				}
				if !strings.Contains(withoutRF, snippet) {
					t.Errorf("HasResponseFormat=false: lost %q", snippet)
				}
			}

			if len(withRF) >= len(withoutRF) {
				t.Errorf("HasResponseFormat=true prompt (%d chars) should be shorter than HasResponseFormat=false (%d chars)",
					len(withRF), len(withoutRF))
			}
		})
	}
}
