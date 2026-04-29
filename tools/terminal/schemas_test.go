package terminal

import (
	"testing"
)

func TestSchemaForDeliverable_HasNamedProperties(t *testing.T) {
	tests := []struct {
		deliverableType string
		wantRequired    []string
	}{
		{"plan", []string{"goal", "context"}},
		{"requirements", []string{"requirements"}},
		{"scenarios", []string{"scenarios"}},
		{"architecture", []string{"technology_choices", "component_boundaries", "data_flow", "decisions", "actors", "integrations"}},
		{"review", []string{"verdict", "feedback"}},
		{"developer", []string{"summary", "files_modified"}},
		{"lesson", []string{"summary", "detail", "injection_form", "root_cause_role"}},
		{"", []string{"summary", "files_modified"}}, // default
	}

	for _, tt := range tests {
		name := tt.deliverableType
		if name == "" {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			schema := schemaForDeliverable(tt.deliverableType)

			props, ok := schema["properties"].(map[string]any)
			if !ok {
				t.Fatal("schema must have properties")
			}

			for _, field := range tt.wantRequired {
				if _, exists := props[field]; !exists {
					t.Errorf("schema missing property %q", field)
				}
			}

			required, ok := schema["required"].([]string)
			if !ok {
				t.Fatal("schema must have required array")
			}

			reqSet := map[string]bool{}
			for _, r := range required {
				reqSet[r] = true
			}
			for _, field := range tt.wantRequired {
				if !reqSet[field] {
					t.Errorf("%q should be required", field)
				}
			}
		})
	}
}

func TestToolsForDeliverable_SwapsSubmitWork(t *testing.T) {
	// ToolsForDeliverable requires global tool registration which happens
	// at component startup. Test the schema swap logic directly.
	planSchema := schemaForDeliverable("plan")
	reviewSchema := schemaForDeliverable("review")

	// Plan schema should have goal, not verdict.
	planProps := planSchema["properties"].(map[string]any)
	if _, ok := planProps["goal"]; !ok {
		t.Error("plan schema should have goal")
	}
	if _, ok := planProps["verdict"]; ok {
		t.Error("plan schema should NOT have verdict")
	}

	// Review schema should have verdict, not goal.
	reviewProps := reviewSchema["properties"].(map[string]any)
	if _, ok := reviewProps["verdict"]; !ok {
		t.Error("review schema should have verdict")
	}
	if _, ok := reviewProps["goal"]; ok {
		t.Error("review schema should NOT have goal")
	}
}
