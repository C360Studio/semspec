package scenariogenerator

import (
	"reflect"
	"strings"
	"testing"

	"github.com/c360studio/semspec/tools/terminal"
)

// TestScenarioSchemaStructParity guards the schema↔struct drift class
// (#125/#126, #137) for the scenario-generator wire shape. The model's
// submit_work payload for the "scenarios" deliverable is parsed into the
// UNEXPORTED llmScenario struct before IDs are assigned. A field present in the
// schema but not the struct means the model emits a value nothing unmarshals; a
// field in the struct but not the schema means the model physically cannot emit
// it (and ValidateScenarioTags would then reject an unwinnable loop — e.g. the
// tags / harness_profile_ids ADR-041 fields the persona is required to emit).
//
// Lives in the scenario-generator package because llmScenario is unexported;
// tools/terminal exposes SchemaForDeliverable as the single source of truth.
func TestScenarioSchemaStructParity(t *testing.T) {
	props := arrayItemProps(t, terminal.SchemaForDeliverable("scenarios"), "scenarios")
	assertWireSchemaStructParity(t, "llmScenario", reflect.TypeOf(llmScenario{}), props)
}

// arrayItemProps navigates schema.properties[key].items.properties for an
// array-of-object top-level field.
func arrayItemProps(t *testing.T, schema map[string]any, key string) map[string]any {
	t.Helper()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties map")
	}
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

// assertWireSchemaStructParity fails when the struct's json field set and the
// schema property set diverge (both directions). Mirrors the helper in
// tools/terminal/schema_struct_parity_test.go; duplicated here because that
// helper is an unexported test symbol and this struct is unexported in a
// different package.
func assertWireSchemaStructParity(t *testing.T, label string, structType reflect.Type, props map[string]any) {
	t.Helper()
	structFields := jsonFieldSet(structType)
	schemaFields := make(map[string]bool, len(props))
	for k := range props {
		schemaFields[k] = true
	}
	for f := range structFields {
		if !schemaFields[f] {
			t.Errorf("%s: struct field %q (json) is MISSING from the submit_work schema — the model cannot emit it. Add it to the schema. (schema↔struct drift, #137.)", label, f)
		}
	}
	for f := range schemaFields {
		if !structFields[f] {
			t.Errorf("%s: schema property %q has NO matching struct field — the model emits a value nothing unmarshals. Remove it from the schema or add the struct field.", label, f)
		}
	}
}

// jsonFieldSet returns the set of json field names for a struct, stripping
// ",omitempty" and skipping json:"-". Anonymous embedded structs are flattened.
func jsonFieldSet(t reflect.Type) map[string]bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	out := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			for k := range jsonFieldSet(f.Type) {
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
