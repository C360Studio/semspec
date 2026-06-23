// Package schemaparity provides reflection-based helpers for asserting that a
// submit_work JSON schema and the Go struct it unmarshals into carry the same
// field set. It is the shared engine behind the per-deliverable parity guards
// that catch the schema↔struct drift class (#125/#126/#267, and the review
// action/target_field gap): a field in the struct but not the schema is a field
// the model cannot emit on strict providers (and silently omits on advisory
// ones); a field in the schema but not the struct is model output nothing reads.
//
// The functions are pure (no testing import) and return violation strings, so
// they can be shared across the per-processor parity tests whose parse DTOs are
// unexported and must be reflected on from inside their own package. Each caller
// does `for _, v := range schemaparity.Bidirectional(...) { t.Error(v) }`.
package schemaparity

import (
	"fmt"
	"reflect"
	"strings"
)

// JSONFields returns the json field-name set of a struct, stripping
// ",omitempty", skipping json:"-", and flattening anonymous embedded structs.
func JSONFields(t reflect.Type) map[string]bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	out := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			for k := range JSONFields(f.Type) {
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

// Bidirectional returns a violation for every field that exists on one side but
// not the other, modulo systemOwned (fields the system fills post-parse, which
// the model is not expected to emit and so are legitimately absent from the
// schema). Use for deliverables whose parse target is a pure DTO of model output.
func Bidirectional(label string, structType reflect.Type, props map[string]any, systemOwned ...string) []string {
	owned := make(map[string]bool, len(systemOwned))
	for _, f := range systemOwned {
		owned[f] = true
	}
	structFields := JSONFields(structType)
	schemaFields := make(map[string]bool, len(props))
	for k := range props {
		schemaFields[k] = true
	}
	var out []string
	for f := range structFields {
		if owned[f] || schemaFields[f] {
			continue
		}
		out = append(out, fmt.Sprintf("%s: struct field %q (json) is MISSING from the submit_work schema — the model cannot emit it. Add it to the schema, or to systemOwned if the system fills it.", label, f))
	}
	for f := range schemaFields {
		if !structFields[f] {
			out = append(out, fmt.Sprintf("%s: schema property %q has NO matching struct field — the model emits a value nothing unmarshals. Remove it from the schema or add the struct field.", label, f))
		}
	}
	for f := range owned {
		if schemaFields[f] {
			out = append(out, fmt.Sprintf("%s: %q is in systemOwned but also present in the schema — remove it from the allowlist.", label, f))
		}
	}
	return out
}

// SchemaSubsetOfStruct returns a violation for every schema property with no
// matching struct field (model emits a value nothing unmarshals). It does NOT
// assert the reverse — correct for durable-entity parse targets that carry
// system-owned fields the model never sets.
func SchemaSubsetOfStruct(label string, structType reflect.Type, props map[string]any) []string {
	structFields := JSONFields(structType)
	var out []string
	for f := range props {
		if !structFields[f] {
			out = append(out, fmt.Sprintf("%s: schema property %q has NO matching struct field — the model emits a value nothing unmarshals. Remove it from the schema or add the struct field.", label, f))
		}
	}
	return out
}

// Props returns schema["properties"] or an error if absent.
func Props(schema map[string]any) (map[string]any, error) {
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema has no properties map")
	}
	return props, nil
}

// ItemsProps navigates props[key].items.properties for an array-of-object field.
func ItemsProps(props map[string]any, key string) (map[string]any, error) {
	field, ok := props[key].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema missing array property %q", key)
	}
	items, ok := field["items"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema property %q has no items", key)
	}
	p, ok := items["properties"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema property %q items has no properties", key)
	}
	return p, nil
}

// ObjectProps navigates props[key].properties for an object (non-array) field.
func ObjectProps(props map[string]any, key string) (map[string]any, error) {
	field, ok := props[key].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema missing object property %q", key)
	}
	p, ok := field["properties"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema property %q has no properties", key)
	}
	return p, nil
}
