package terminal

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestSchemaForDeliverable_HasNamedProperties(t *testing.T) {
	tests := []struct {
		deliverableType string
		wantRequired    []string
	}{
		{"exploration", []string{"capabilities", "open_questions"}},
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
// requireFieldsPresent asserts every name in want appears in got.
func requireFieldsPresent(t *testing.T, location string, want, got []string) {
	t.Helper()
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s missing %q (got %v)", location, w, got)
		}
	}
}

// TestQAReviewSchema_ADR044CapabilityEvidenceDimension pins the ADR-044
// QA dimension addition: the qa-reviewer submit_work schema MUST require
// a capability_evidence field on dimensions so the persona has a schema
// slot for the M:N coverage gap diagnosis surfaced in the user prompt.
// A regression that drops the field would silently force the persona to
// shoehorn capability gaps into requirement_fulfillment or summary text.
func TestQAReviewSchema_ADR044CapabilityEvidenceDimension(t *testing.T) {
	schema := qaReviewSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema missing properties")
	}
	dims, ok := props["dimensions"].(map[string]any)
	if !ok {
		t.Fatal("schema missing dimensions")
	}
	dimProps, ok := dims["properties"].(map[string]any)
	if !ok {
		t.Fatal("dimensions properties missing")
	}
	if _, ok := dimProps["capability_evidence"]; !ok {
		t.Error("dimensions.capability_evidence missing — ADR-044 release-readiness gate has no schema slot")
	}
	required, ok := dims["required"].([]string)
	if !ok {
		t.Fatal("dimensions.required missing")
	}
	found := false
	for _, r := range required {
		if r == "capability_evidence" {
			found = true
			break
		}
	}
	if !found {
		t.Error("dimensions.required[] missing capability_evidence — strict mode rejects responses without it; persona will be schema-blocked from emitting the dimension")
	}
}

// TestQAReviewSchema_VerdictEnumMatchesConstants pins the #200 fix AND prevents
// its whole bug class. The LLM-facing verdict enum MUST equal the
// workflow.QAVerdict* constant set. #200 was exactly a drift here: the Go enum,
// the qa-reviewer parser/reconcileSkipVerdict guard, and plan-manager's terminal
// path all handled conditionally_approved, but the schema enum didn't — so strict
// mode schema-blocked Murat from emitting it, approved+skips coerced to
// needs_changes, and the 2026-06-16 hybrid-gpt5 plan false-rejected. Binding this
// test to the constants makes a future verdict added to workflow/subjects.go
// without the schema (or vice versa) fail the build instead of shipping.
func TestQAReviewSchema_VerdictEnumMatchesConstants(t *testing.T) {
	schema := qaReviewSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema missing properties")
	}
	verdict, ok := props["verdict"].(map[string]any)
	if !ok {
		t.Fatal("schema missing verdict")
	}
	enum, ok := verdict["enum"].([]string)
	if !ok {
		t.Fatalf("verdict.enum missing or not []string: %T", verdict["enum"])
	}

	// Source of truth: the QAVerdict constants. Adding a verdict here forces a
	// schema update (and vice versa) or this parity check fails.
	want := map[string]bool{
		string(workflow.QAVerdictApproved):              false,
		string(workflow.QAVerdictConditionallyApproved): false,
		string(workflow.QAVerdictNeedsChanges):          false,
		string(workflow.QAVerdictRejected):              false,
	}
	for _, v := range enum {
		if _, known := want[v]; !known {
			t.Errorf("verdict.enum has %q, not a workflow.QAVerdict* constant — schema/constant drift", v)
			continue
		}
		want[v] = true
	}
	for v, seen := range want {
		if !seen {
			t.Errorf("verdict.enum missing %q — schema drifted from workflow.QAVerdict* constants (the #200 bug class)", v)
		}
	}

	// The description must name conditionally_approved, or the model has no
	// guidance on when to select it over approved.
	if desc, _ := verdict["description"].(string); !strings.Contains(desc, "conditionally_approved") {
		t.Errorf("verdict.description does not mention conditionally_approved; model has no selection guidance: %q", desc)
	}
}

// TestStoriesSchema_ADR044Shape pins the ADR-044 M:N wire shape on the
// submit_work tool definition. Sarah's positional input struct in
// processor/story-preparer/component.go expects component_name (singular
// string), requirement_indices (plural ints), capability_indices (plural
// ints), and NO story-level files_owned / depends_on_labels. The strict
// JSON schema MUST match — pre-ADR-044 the schema was the old
// requirement_index / components / files_owned / depends_on_labels shape,
// which (with strict response_format) actively forced every Sarah
// dispatch into the OLD wire shape and burned the full retry budget on
// every paid run. Caught 2026-06-03 by go-reviewer on commit 9dee2057.
func TestStoriesSchema_ADR044Shape(t *testing.T) {
	schema := storiesSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema must have properties")
	}
	stories, ok := props["stories"].(map[string]any)
	if !ok {
		t.Fatal("schema missing stories")
	}
	items, ok := stories["items"].(map[string]any)
	if !ok {
		t.Fatal("stories items missing")
	}
	itemProps, ok := items["properties"].(map[string]any)
	if !ok {
		t.Fatal("story item properties missing")
	}

	// Fields that MUST be present (ADR-044 wire shape).
	mustHave := []string{"label", "component_name", "requirement_indices", "capability_indices", "title", "intent", "tasks"}
	for _, f := range mustHave {
		if _, ok := itemProps[f]; !ok {
			t.Errorf("schema missing ADR-044 property %q", f)
		}
	}

	// Fields that MUST be absent (retired by ADR-044).
	mustNotHave := []string{"requirement_index", "components", "files_owned", "depends_on_labels"}
	for _, f := range mustNotHave {
		if _, ok := itemProps[f]; ok {
			t.Errorf("schema still carries retired property %q — ADR-044 dropped it", f)
		}
	}

	// `required` list must list every ADR-044 field exactly (strict mode
	// rejects responses missing any required field).
	required, ok := items["required"].([]string)
	if !ok {
		t.Fatal("story item required[] missing")
	}
	requireFieldsPresent(t, "story.required", mustHave, required)
	reqSet := map[string]bool{}
	for _, r := range required {
		reqSet[r] = true
	}
	for _, f := range mustNotHave {
		if reqSet[f] {
			t.Errorf("required[] still lists retired %q — ADR-044 dropped it", f)
		}
	}

	// `component_name` is a string (singular anchor), not an array.
	cn, ok := itemProps["component_name"].(map[string]any)
	if !ok || cn["type"] != "string" {
		t.Errorf("component_name must be string (ADR-044 1:1 anchor), got %v", itemProps["component_name"])
	}

	// `requirement_indices` and `capability_indices` are integer arrays.
	for _, name := range []string{"requirement_indices", "capability_indices"} {
		field, ok := itemProps[name].(map[string]any)
		if !ok || field["type"] != "array" {
			t.Errorf("%s must be array, got %v", name, itemProps[name])
			continue
		}
		it, _ := field["items"].(map[string]any)
		if it == nil || it["type"] != "integer" {
			t.Errorf("%s.items must be integer, got %v", name, field["items"])
		}
	}
}

func TestArchitectureSchema_UpstreamResolutionsShape(t *testing.T) {
	schema := schemaForDeliverable("architecture")
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("architecture schema must have properties")
	}

	ur, ok := props["upstream_resolutions"].(map[string]any)
	if !ok {
		t.Fatal("architecture schema missing upstream_resolutions property — wiring bug regressed")
	}
	if ur["type"] != "array" {
		t.Errorf("upstream_resolutions.type = %v, want array", ur["type"])
	}

	urItems, ok := ur["items"].(map[string]any)
	if !ok {
		t.Fatal("upstream_resolutions.items missing")
	}
	urItemProps, _ := urItems["properties"].(map[string]any)
	urRequired, _ := urItems["required"].([]string)
	requireFieldsPresent(t, "upstream_resolutions.items.required",
		[]string{"name", "coordinate", "source_ref", "apis", "used_by"}, urRequired)

	// apis[].items shape.
	apis, _ := urItemProps["apis"].(map[string]any)
	apisItems, _ := apis["items"].(map[string]any)
	apisRequired, _ := apisItems["required"].([]string)
	requireFieldsPresent(t, "upstream_resolutions.items.apis.items.required",
		[]string{"symbol", "kind", "signature", "citation"}, apisRequired)

	// component_boundaries.items must require upstream_refs (bidirectional partner).
	cb, _ := props["component_boundaries"].(map[string]any)
	cbItems, _ := cb["items"].(map[string]any)
	cbRequired, _ := cbItems["required"].([]string)
	requireFieldsPresent(t, "component_boundaries.items.required",
		[]string{"upstream_refs"}, cbRequired)

	// Integration-target role remains on upstream_resolutions; runnable harness
	// topology moved to architecture.harness_profiles.
	requireFieldsPresent(t, "upstream_resolutions.items.required",
		[]string{"role"}, urRequired)
	if _, ok := urItemProps["test"+"_harness"]; ok {
		t.Fatal("upstream_resolutions.items must not expose legacy harness field")
	}

	assertRoleEnum(t, urItemProps)
	assertHarnessProfileShape(t, props)
}

// assertRoleEnum validates the role property is a constrained enum.
func assertRoleEnum(t *testing.T, urItemProps map[string]any) {
	t.Helper()
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
}

func assertHarnessProfileShape(t *testing.T, props map[string]any) {
	t.Helper()
	hp, _ := props["harness_profiles"].(map[string]any)
	if hp == nil {
		t.Fatal("architecture schema missing harness_profiles property")
	}
	if hp["type"] != "array" {
		t.Errorf("harness_profiles.type = %v, want array", hp["type"])
	}
	items, _ := hp["items"].(map[string]any)
	required, _ := items["required"].([]string)
	requireFieldsPresent(t, "harness_profiles.items.required",
		[]string{"profile_id", "used_by", "purpose", "covers"}, required)
	itemProps, _ := items["properties"].(map[string]any)
	for _, key := range []string{"profile_id", "used_by", "purpose", "covers"} {
		if itemProps[key] == nil {
			t.Errorf("harness_profiles.items.properties missing %q", key)
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

// TestExplorationSchema_HasCapabilitiesNotGoal pins the ADR-040 Move 1
// fix from real-LLM run #3 (2026-05-30): the analyst sub-phase dispatch
// must route submit_work's parameter schema to explorationSchema (with
// capabilities + open_questions) NOT planSchema (with goal/context/scope).
// Runs #1 and #2 both wedged because dispatchAnalyst passed
// deliverableType="plan" to ToolsForEndpoint, so the LLM saw the planner
// schema as the literal function signature and emitted goal/context/scope
// on every retry — completely overriding the analyst persona instruction.
//
// Fix shipped here ensures the deliverableType="exploration" path
// returns a fundamentally different shape so the model has no way to
// produce planner-shape output without a schema validation failure.
func TestExplorationSchema_HasCapabilitiesNotGoal(t *testing.T) {
	schema := schemaForDeliverable("exploration")
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("exploration schema must have properties map")
	}
	// capabilities + open_questions MUST be present.
	for _, field := range []string{"capabilities", "open_questions"} {
		if _, exists := props[field]; !exists {
			t.Errorf("exploration schema missing required property %q", field)
		}
	}
	// goal / context / scope MUST NOT be present — the contamination
	// surface that wedged runs #1 + #2.
	for _, field := range []string{"goal", "context", "scope"} {
		if _, exists := props[field]; exists {
			t.Errorf("exploration schema contains planner-shape property %q — this is the run-#1/#2 wedge surface", field)
		}
	}

	// Capability item shape: name (kebab-case), lifecycle (new|modified),
	// description, depends_on must all be required.
	caps, ok := props["capabilities"].(map[string]any)
	if !ok {
		t.Fatal("capabilities property must be a map")
	}
	items, ok := caps["items"].(map[string]any)
	if !ok {
		t.Fatal("capabilities.items must be a map")
	}
	itemProps, ok := items["properties"].(map[string]any)
	if !ok {
		t.Fatal("capability item must have properties")
	}
	for _, field := range []string{"name", "lifecycle", "description", "depends_on"} {
		if _, exists := itemProps[field]; !exists {
			t.Errorf("capability item missing required property %q", field)
		}
	}
	// Lifecycle must be enumerated to new|modified.
	lifecycle, ok := itemProps["lifecycle"].(map[string]any)
	if !ok {
		t.Fatal("lifecycle property must be a map")
	}
	enum, ok := lifecycle["enum"].([]string)
	if !ok || len(enum) != 2 {
		t.Fatalf("lifecycle enum must be [new, modified], got %v", lifecycle["enum"])
	}
	if !(enum[0] == "new" && enum[1] == "modified") {
		t.Errorf("lifecycle enum mismatch: got %v, want [new, modified]", enum)
	}
}
