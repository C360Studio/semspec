package terminal

import (
	"reflect"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestArchitectureSchemaStructParity is the guard against the recurring class of
// bug that kept biting us: a field exists in the Go struct / validator / prompt
// but NOT in the submit_work tool schema the model actually calls (or vice
// versa). The model follows the function signature over prompt prose, so a field
// missing from the schema is a field the model cannot emit — and if a validator
// then requires it, the loop is unwinnable.
//
// Concrete history: #125 added apis[].import + apis[].artifact and #126 added
// upstream_resolutions[].resolution_kind to the struct, prompt, validator, and
// openapi — but NOT to this schema. The architect physically could not emit
// imports; ValidateUpstreamImports rejected the missing field; arch-gen wedged
// for multiple paid runs (2026-06). The existing strict-mode tests
// (TestSchemasNoAdditionalProperties / TestSchemasRequiredCompleteness) check the
// schema against ITSELF and so could never catch schema-vs-struct drift. This
// test checks the schema against the STRUCT.
//
// When this fails, the fix is one of:
//   - add the missing field to the schema (the model needs to emit it), or
//   - add it to the systemOwned allowlist with a comment (the SYSTEM fills it; it
//     is intentionally not part of the model's submit_work contract).
func TestArchitectureSchemaStructParity(t *testing.T) {
	schema := schemaForDeliverable("architecture")
	props := schemaProps(t, schema)

	// ArchitectureDocument top-level. integration_flows/e2e_flows live UNDER
	// test_surface in the schema, not at top level; name is system-set. These are
	// the only legitimately-system-owned top-level fields.
	assertSchemaStructParity(t, "ArchitectureDocument",
		reflect.TypeOf(workflow.ArchitectureDocument{}), props,
		"integration_flows", "e2e_flows", "name")

	// component_boundaries[].items — capabilities is SYSTEM-DERIVED from
	// capability_indices (ResolveCapabilityIndices), so it is intentionally not in
	// the model's schema.
	assertSchemaStructParity(t, "ComponentDef",
		reflect.TypeOf(workflow.ComponentDef{}), itemsProps(t, props, "component_boundaries"),
		"capabilities")

	// upstream_resolutions[].items — every field is architect-authored; no
	// system-owned exceptions. This is the subtree that kept regressing.
	urProps := itemsProps(t, props, "upstream_resolutions")
	assertSchemaStructParity(t, "UpstreamResolution",
		reflect.TypeOf(workflow.UpstreamResolution{}), urProps)

	// upstream_resolutions[].apis[].items — APISurface. import/artifact MUST be
	// here (the bug). No system-owned exceptions.
	assertSchemaStructParity(t, "APISurface",
		reflect.TypeOf(workflow.APISurface{}), itemsProps(t, urProps, "apis"))
}

// TestExplorationSchemaStructParity extends the same schema↔struct drift guard
// to the analyst sub-phase deliverable (ADR-040 Move 1). The exploration schema
// is parsed straight into workflow.Exploration, so a field that exists in the
// schema but not the struct (or vice versa) is the exact #125/#126 class of bug
// for the capability wire shape. capabilities[] maps to workflow.Capability.
func TestExplorationSchemaStructParity(t *testing.T) {
	schema := schemaForDeliverable("exploration")
	props := schemaProps(t, schema)

	// Top level: capabilities + open_questions. Every field is analyst-authored;
	// no system-owned exceptions.
	assertSchemaStructParity(t, "Exploration",
		reflect.TypeOf(workflow.Exploration{}), props)

	// capabilities[].items — workflow.Capability. name/lifecycle/description/
	// depends_on/surfaces are all model-emitted; no system-owned exceptions.
	assertSchemaStructParity(t, "Capability",
		reflect.TypeOf(workflow.Capability{}), itemsProps(t, props, "capabilities"))
}

// TestPlanSchemaStructParity guards the planner sub-phase deliverable. Unlike
// the parse-into-one-struct deliverables above, workflow.Plan is the durable
// entity and carries many SYSTEM-owned fields (id, status, review_*, github,
// qa_*, …) that the model never emits. So the top level is a one-directional
// SUBSET check — every planSchema property must map to a Plan json field (a
// schema prop with no struct field is a value nothing unmarshals) — while the
// reverse direction (struct fields absent from the schema) is intentionally not
// asserted. The scope sub-object IS isomorphic to workflow.Scope, so it gets the
// full bidirectional parity check.
func TestPlanSchemaStructParity(t *testing.T) {
	schema := schemaForDeliverable("plan")
	props := schemaProps(t, schema)

	assertSchemaPropsSubsetOfStruct(t, "Plan",
		reflect.TypeOf(workflow.Plan{}), props)

	assertSchemaStructParity(t, "Scope",
		reflect.TypeOf(workflow.Scope{}), objectProps(t, props, "scope"))
}

// assertSchemaPropsSubsetOfStruct fails when a schema property has no matching
// struct json field — the model emits a value nothing unmarshals. It does NOT
// assert the reverse (struct fields absent from the schema), which is the right
// shape for deliverables whose consuming struct is a durable entity with
// system-owned fields the model never sets.
func assertSchemaPropsSubsetOfStruct(t *testing.T, label string, structType reflect.Type, props map[string]any) {
	t.Helper()
	structFields := structJSONFields(structType)
	for f := range props {
		if !structFields[f] {
			t.Errorf("%s: schema property %q has NO matching struct field — the model emits a value nothing unmarshals. Remove it from the schema or add the struct field.", label, f)
		}
	}
}

// objectProps navigates props[key].properties for an object (non-array) field.
func objectProps(t *testing.T, props map[string]any, key string) map[string]any {
	t.Helper()
	field, ok := props[key].(map[string]any)
	if !ok {
		t.Fatalf("schema missing object property %q", key)
	}
	p, ok := field["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema property %q has no properties", key)
	}
	return p
}

// assertSchemaStructParity fails when the struct's JSON field set and the schema
// property set diverge, modulo systemOwned (fields the system fills, not the
// model). Both directions are checked: a struct field absent from the schema (the
// model cannot emit it) and a schema property with no struct field (the model
// emits something nothing reads).
func assertSchemaStructParity(t *testing.T, label string, structType reflect.Type, props map[string]any, systemOwned ...string) {
	t.Helper()
	owned := make(map[string]bool, len(systemOwned))
	for _, f := range systemOwned {
		owned[f] = true
	}
	structFields := structJSONFields(structType)
	schemaFields := make(map[string]bool, len(props))
	for k := range props {
		schemaFields[k] = true
	}

	for f := range structFields {
		if owned[f] {
			continue
		}
		if !schemaFields[f] {
			t.Errorf("%s: struct field %q (json) is MISSING from the submit_work schema — the model cannot emit it. Add it to the schema, or to systemOwned if the system fills it. (This is the schema↔struct drift class that wedged arch-gen.)", label, f)
		}
	}
	for f := range schemaFields {
		if !structFields[f] {
			t.Errorf("%s: schema property %q has NO matching struct field — the model emits a value nothing unmarshals. Remove it from the schema or add the struct field.", label, f)
		}
	}
	// A systemOwned entry that IS in the schema is a stale allowlist entry.
	for f := range owned {
		if schemaFields[f] {
			t.Errorf("%s: %q is in systemOwned but also present in the schema — remove it from the allowlist.", label, f)
		}
	}
}

// structJSONFields returns the set of json field names for a struct, stripping
// ",omitempty" and skipping json:"-". Anonymous embedded structs are flattened.
func structJSONFields(t reflect.Type) map[string]bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	out := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			for k := range structJSONFields(f.Type) {
				out[k] = true
			}
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" {
			continue
		}
		out[name] = true
	}
	return out
}

func schemaProps(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties map")
	}
	return props
}

// itemsProps navigates props[key].items.properties for an array-of-object field.
func itemsProps(t *testing.T, props map[string]any, key string) map[string]any {
	t.Helper()
	field, ok := props[key].(map[string]any)
	if !ok {
		t.Fatalf("schema missing array property %q", key)
	}
	items, ok := field["items"].(map[string]any)
	if !ok {
		t.Fatalf("schema property %q has no items", key)
	}
	p, ok := items["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema property %q items has no properties", key)
	}
	return p
}
