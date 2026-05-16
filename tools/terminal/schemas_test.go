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
		{"architecture", []string{"technology_choices", "component_boundaries", "data_flow", "decisions", "actors", "integrations", "upstream_resolutions", "test_surface"}},
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

// TestArchitectureSchema_UpstreamResolutionsShape locks in the strict-
// schema additions from take-28's wiring-bug fix. The architect's
// submit_work response_format is sent to the model with Strict: true
// (tools/terminal/response_format.go:64), which means the model CANNOT
// emit fields the schema doesn't include. Take-28 wedged because we
// added upstream_resolutions to the Go struct + persona but missed the
// strict JSON schema — gemini-pro silently dropped the field across two
// revision iters even with explicit reviewer feedback. Pinning the
// shape here catches the same wiring miss recurring (mirror of the
// take-22 write_todos-not-in-palette pattern).
func TestArchitectureSchema_UpstreamResolutionsShape(t *testing.T) {
	schema := schemaForDeliverable("architecture")
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("architecture schema must have properties")
	}

	// upstream_resolutions must be a top-level array.
	ur, ok := props["upstream_resolutions"].(map[string]any)
	if !ok {
		t.Fatal("architecture schema missing upstream_resolutions property — wiring bug regressed")
	}
	if ur["type"] != "array" {
		t.Errorf("upstream_resolutions.type = %v, want array", ur["type"])
	}

	// Each item must require name + coordinate + source_ref + apis + used_by.
	urItems, ok := ur["items"].(map[string]any)
	if !ok {
		t.Fatal("upstream_resolutions.items missing")
	}
	urRequired, _ := urItems["required"].([]string)
	for _, want := range []string{"name", "coordinate", "source_ref", "apis", "used_by"} {
		found := false
		for _, r := range urRequired {
			if r == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("upstream_resolutions.items.required missing %q (got %v)", want, urRequired)
		}
	}

	// apis[].items must require symbol/kind/signature/lifecycle/notes/citation.
	urItemProps, _ := urItems["properties"].(map[string]any)
	apis, _ := urItemProps["apis"].(map[string]any)
	apisItems, _ := apis["items"].(map[string]any)
	apisRequired, _ := apisItems["required"].([]string)
	for _, want := range []string{"symbol", "kind", "signature", "citation"} {
		found := false
		for _, r := range apisRequired {
			if r == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("upstream_resolutions.items.apis.items.required missing %q (got %v)", want, apisRequired)
		}
	}

	// component_boundaries.items must require upstream_refs (bidirectional partner).
	cb, _ := props["component_boundaries"].(map[string]any)
	cbItems, _ := cb["items"].(map[string]any)
	cbRequired, _ := cbItems["required"].([]string)
	foundUR := false
	for _, r := range cbRequired {
		if r == "upstream_refs" {
			foundUR = true
			break
		}
	}
	if !foundUR {
		t.Errorf("component_boundaries.items.required missing 'upstream_refs' (bidirectional partner regressed)")
	}

	// upstream_resolutions.items must require role + test_harness (the
	// Testcontainers-led integration tier additions — 2026-05-15). Mirror
	// of the take-28 wedge prevention: Go struct + persona without the
	// strict schema = silent field drop on strict-mode endpoints.
	for _, want := range []string{"role", "test_harness"} {
		found := false
		for _, r := range urRequired {
			if r == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("upstream_resolutions.items.required missing %q (Testcontainers-tier wiring regressed)", want)
		}
	}

	// role must be an enum constraining the LLM to known values.
	role, _ := urItemProps["role"].(map[string]any)
	if role == nil {
		t.Fatal("upstream_resolutions.items.role property missing")
	}
	roleEnum, _ := role["enum"].([]string)
	if len(roleEnum) == 0 {
		t.Error("upstream_resolutions.items.role.enum missing — strict mode requires constrained values to prevent free-form drift")
	}
	wantRoles := map[string]bool{"build_dep": true, "runtime_dep": true, "integration_target": true}
	for _, r := range roleEnum {
		if !wantRoles[r] {
			t.Errorf("unexpected role enum value %q", r)
		}
		delete(wantRoles, r)
	}
	if len(wantRoles) > 0 {
		t.Errorf("role enum missing values: %v", wantRoles)
	}

	// test_harness must be a nullable object so resolutions with role !=
	// integration_target can satisfy "required" with null.
	th, _ := urItemProps["test_harness"].(map[string]any)
	if th == nil {
		t.Fatal("upstream_resolutions.items.test_harness property missing")
	}
	thType, _ := th["type"].([]any)
	if len(thType) != 2 || thType[0] != "object" || thType[1] != "null" {
		t.Errorf("test_harness.type = %v, want [object, null] for nullable strict-mode shape", th["type"])
	}
	thRequired, _ := th["required"].([]string)
	for _, want := range []string{"library", "image", "access_method"} {
		found := false
		for _, r := range thRequired {
			if r == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("test_harness.required missing %q", want)
		}
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
