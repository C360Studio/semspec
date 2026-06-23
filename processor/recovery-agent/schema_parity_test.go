package recoveryagent

import (
	"reflect"
	"testing"

	"github.com/c360studio/semspec/tools/schemaparity"
	"github.com/c360studio/semspec/tools/terminal"
	"github.com/c360studio/semspec/workflow"
)

// TestRecoverySchemaStructParity guards the recovery-agent (ADR-037) deliverable
// against schema↔struct drift. Output parses into the unexported
// rawRecoveryResult. `feedback` is a DELIBERATE non-schema salvage field —
// mid-tier models recurringly misfile the fix prose under "feedback" instead of
// "refined_prompt", and the action-inference net adopts it rather than
// terminal-failing a recoverable wedge — so it is allowlisted here.
func TestRecoverySchemaStructParity(t *testing.T) {
	props, err := schemaparity.Props(terminal.SchemaForDeliverable("recovery"))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range schemaparity.Bidirectional("rawRecoveryResult", reflect.TypeOf(rawRecoveryResult{}), props, "feedback") {
		t.Error(v)
	}
	// contract_impact ↔ workflow.ContractImpact (the nested obligation summary).
	ciProps, err := schemaparity.ObjectProps(props, "contract_impact")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range schemaparity.Bidirectional("ContractImpact", reflect.TypeOf(workflow.ContractImpact{}), ciProps) {
		t.Error(v)
	}
}
