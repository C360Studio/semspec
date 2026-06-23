package terminal

import (
	"reflect"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestReviewSchemaStructParity guards the plan-reviewer deliverable. The
// findings[] subtree silently lost the take-24 remediation directive: action,
// target_field, and target_value live on workflow.PlanReviewFinding, are
// demanded by the reviewer prompt (software.go:946), and are rendered by
// writeViolationFinding/formatActionDirective — but were ABSENT from this
// schema with additionalProperties:false, so strict-mode reviewers could not
// emit them and the directive came back empty. Same class as #267 /
// arch-gen #125/#126. This test pins the findings item shape so it cannot
// regress.
func TestReviewSchemaStructParity(t *testing.T) {
	schema := schemaForDeliverable("review")
	props := schemaProps(t, schema)

	// findings[].items ↔ workflow.PlanReviewFinding — full bidirectional parity.
	// Every finding field is reviewer-authored; no system-owned exceptions.
	assertSchemaStructParity(t, "PlanReviewFinding",
		reflect.TypeOf(workflow.PlanReviewFinding{}), itemsProps(t, props, "findings"))

	// Top level: parseReviewFromResult unmarshals into workflow.PlanReviewResult
	// (verdict/summary/findings only). The schema also emits feedback,
	// rejection_type, and scenario_verdicts, which the deterministic
	// ValidateReviewDeliverable (validators.go) and plan-manager read from the
	// RAW deliverable map, not this struct. So assert only the #267 direction —
	// every struct field must be emittable (struct ⊆ schema) — and do not flag
	// the raw-map-read schema fields.
	schemaFields := make(map[string]bool, len(props))
	for k := range props {
		schemaFields[k] = true
	}
	for f := range structJSONFields(reflect.TypeOf(workflow.PlanReviewResult{})) {
		if !schemaFields[f] {
			t.Errorf("PlanReviewResult: struct field %q (json) is MISSING from the review schema — the model cannot emit it. Add it to the schema. (schema↔struct drift class.)", f)
		}
	}
}
