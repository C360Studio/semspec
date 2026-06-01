package terminal

import (
	"fmt"
	"sort"
	"testing"
)

// allDeliverableSchemas returns every schema used by ResponseFormat /
// submit_work tool definitions. Keep in sync with schemaForDeliverable.
func allDeliverableSchemas() map[string]map[string]any {
	return map[string]map[string]any{
		"plan":         planSchema(),
		"requirements": requirementsSchema(),
		"scenarios":    scenariosSchema(),
		"stories":      storiesSchema(),
		"architecture": architectureSchema(),
		"review":       reviewSchema(),
		"developer":    developerSchema(),
		"lesson":       lessonSchema(),
		"qa-review":    qaReviewSchema(),
	}
}

// allowedStrictKeys is the OpenAI Structured Outputs subset (per
// semstreams docs/operations/13-structured-output.md):
// type, properties, items, required, enum, description, additionalProperties,
// minimum, maximum.
func allowedStrictKeys() map[string]bool {
	return map[string]bool{
		"type":                 true,
		"properties":           true,
		"items":                true,
		"required":             true,
		"enum":                 true,
		"description":          true,
		"additionalProperties": true,
		"minimum":              true,
		"maximum":              true,
	}
}

// TestSchemasNoAdditionalProperties guards the mechanical half of the
// strict-mode subset:
//
//   - additionalProperties: false at every object level
//   - no $ref or anyOf
//   - only allowed keywords
//
// These prevent the model from hallucinating extra keys and are safe to
// enforce regardless of Strict:true/false on ResponseFormat. Adopting them
// is purely defensive — no semantic change to the deliverable shape.
func TestSchemasNoAdditionalProperties(t *testing.T) {
	allowed := allowedStrictKeys()
	for name, schema := range allDeliverableSchemas() {
		t.Run(name, func(t *testing.T) {
			var violations []string
			walkAdditionalProperties(schema, name, allowed, &violations)
			if len(violations) > 0 {
				sort.Strings(violations)
				for _, v := range violations {
					t.Error(v)
				}
			}
		})
	}
}

// TestSchemasRequiredCompleteness enforces the second half of the OpenAI
// strict-mode subset: every declared property must appear in the parent's
// required list. The contract is "every field is required; nullable-type
// encodes optional semantics" — `"type": ["string", "null"]` for fields
// the model may legitimately leave unset, with the prompt directing it to
// set null/empty when not applicable.
//
// Migrated 2026-05-07. Schemas previously used the absence-as-optional
// convention; required-completeness landed alongside the Strict:true flip
// on ResponseFormat and ToolDefinition.Strict.
func TestSchemasRequiredCompleteness(t *testing.T) {
	for name, schema := range allDeliverableSchemas() {
		t.Run(name, func(t *testing.T) {
			var violations []string
			walkRequiredCompleteness(schema, name, &violations)
			if len(violations) > 0 {
				sort.Strings(violations)
				for _, v := range violations {
					t.Log(v)
				}
				t.Fatalf("%d required-completeness violations in %s schema", len(violations), name)
			}
		})
	}
}

func walkAdditionalProperties(node any, path string, allowed map[string]bool, out *[]string) {
	obj, ok := node.(map[string]any)
	if !ok {
		return
	}
	if _, hasRef := obj["$ref"]; hasRef {
		*out = append(*out, fmt.Sprintf("%s: $ref not allowed in strict-mode subset", path))
	}
	if _, hasAnyOf := obj["anyOf"]; hasAnyOf {
		*out = append(*out, fmt.Sprintf("%s: anyOf not allowed", path))
	}
	for k := range obj {
		if !allowed[k] && k != "$ref" && k != "anyOf" {
			*out = append(*out, fmt.Sprintf("%s: unsupported keyword %q", path, k))
		}
	}
	if obj["type"] != "object" {
		if items, ok := obj["items"].(map[string]any); ok {
			walkAdditionalProperties(items, path+".items", allowed, out)
		}
		return
	}
	ap, hasAP := obj["additionalProperties"]
	if !hasAP {
		*out = append(*out, fmt.Sprintf("%s: missing additionalProperties:false", path))
	} else if ap != false {
		*out = append(*out, fmt.Sprintf("%s: additionalProperties must be false, got %v", path, ap))
	}
	props, _ := obj["properties"].(map[string]any)
	for k, v := range props {
		walkAdditionalProperties(v, path+"."+k, allowed, out)
	}
}

func walkRequiredCompleteness(node any, path string, out *[]string) {
	obj, ok := node.(map[string]any)
	if !ok {
		return
	}
	if obj["type"] != "object" {
		if items, ok := obj["items"].(map[string]any); ok {
			walkRequiredCompleteness(items, path+".items", out)
		}
		return
	}
	props, _ := obj["properties"].(map[string]any)
	requiredSet := map[string]bool{}
	switch r := obj["required"].(type) {
	case []string:
		for _, s := range r {
			requiredSet[s] = true
		}
	case []any:
		for _, s := range r {
			if str, ok := s.(string); ok {
				requiredSet[str] = true
			}
		}
	}
	for k, v := range props {
		if !requiredSet[k] {
			*out = append(*out, fmt.Sprintf("%s: property %q not in required", path, k))
		}
		walkRequiredCompleteness(v, path+"."+k, out)
	}
}
