package qareviewer

import (
	"reflect"
	"testing"

	"github.com/c360studio/semspec/tools/schemaparity"
	"github.com/c360studio/semspec/tools/terminal"
)

// TestQAReviewSchemaStructParity guards the qa-reviewer (Murat) deliverable
// against schema↔struct drift. Output parses into the unexported qaReviewOutput /
// qaPlanDecision, which carry anonymous inline structs for `dimensions` and
// `artifact_refs`; this descends into both so a drift at any level fails.
func TestQAReviewSchemaStructParity(t *testing.T) {
	props, err := schemaparity.Props(terminal.SchemaForDeliverable("qa-review"))
	if err != nil {
		t.Fatal(err)
	}
	outT := reflect.TypeOf(qaReviewOutput{})
	for _, v := range schemaparity.Bidirectional("qaReviewOutput", outT, props) {
		t.Error(v)
	}

	// dimensions — anonymous inline struct on qaReviewOutput.
	dimField, ok := outT.FieldByName("Dimensions")
	if !ok {
		t.Fatal("qaReviewOutput has no Dimensions field")
	}
	dimProps, err := schemaparity.ObjectProps(props, "dimensions")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range schemaparity.Bidirectional("qaReviewOutput.Dimensions", dimField.Type, dimProps) {
		t.Error(v)
	}

	// plan_decisions[].items ↔ qaPlanDecision.
	pdItems, err := schemaparity.ItemsProps(props, "plan_decisions")
	if err != nil {
		t.Fatal(err)
	}
	pdT := reflect.TypeOf(qaPlanDecision{})
	for _, v := range schemaparity.Bidirectional("qaPlanDecision", pdT, pdItems) {
		t.Error(v)
	}

	// artifact_refs[].items — anonymous inline struct on qaPlanDecision.
	arField, ok := pdT.FieldByName("ArtifactRefs")
	if !ok {
		t.Fatal("qaPlanDecision has no ArtifactRefs field")
	}
	arItems, err := schemaparity.ItemsProps(pdItems, "artifact_refs")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range schemaparity.Bidirectional("qaPlanDecision.ArtifactRefs", arField.Type.Elem(), arItems) {
		t.Error(v)
	}
}
