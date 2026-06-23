package lessondecomposer

import (
	"reflect"
	"testing"

	"github.com/c360studio/semspec/tools/schemaparity"
	"github.com/c360studio/semspec/tools/terminal"
)

// TestLessonSchemaStructParity guards the lesson-decomposer (ADR-033 Phase 2b)
// deliverable against schema↔struct drift. Output parses into the unexported
// decomposerResult, with named evidence_steps/evidence_files item structs.
func TestLessonSchemaStructParity(t *testing.T) {
	props, err := schemaparity.Props(terminal.SchemaForDeliverable("lesson"))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range schemaparity.Bidirectional("decomposerResult", reflect.TypeOf(decomposerResult{}), props) {
		t.Error(v)
	}
	stepItems, err := schemaparity.ItemsProps(props, "evidence_steps")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range schemaparity.Bidirectional("decomposerStepRef", reflect.TypeOf(decomposerStepRef{}), stepItems) {
		t.Error(v)
	}
	fileItems, err := schemaparity.ItemsProps(props, "evidence_files")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range schemaparity.Bidirectional("decomposerFileRef", reflect.TypeOf(decomposerFileRef{}), fileItems) {
		t.Error(v)
	}
}
