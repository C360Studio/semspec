package planreviewer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// mockFixtureRoot resolves the e2e mock-responses dir relative to this package.
const mockFixtureRoot = "../../test/e2e/fixtures/mock-responses"

// mockToolCallEnvelope is the on-disk shape of a mock LLM response fixture: the
// real payload is the JSON-encoded submit_work arguments string.
type mockToolCallEnvelope struct {
	ToolCalls []struct {
		Function struct {
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

// decodeFixtureArgs reads <scenario>/<role>.json, pulls the first tool call's
// submit_work arguments, and unmarshals them into `into`. Returns false if the
// fixture file does not exist (so optional roles like the analyst sub-phase can
// be skipped).
func decodeFixtureArgs(t *testing.T, scenario, role string, into any) bool {
	t.Helper()
	p := filepath.Join(mockFixtureRoot, scenario, role+".json")
	raw, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return false
	}
	if err != nil {
		t.Fatalf("read fixture %s: %v", p, err)
	}
	var env mockToolCallEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope %s: %v", p, err)
	}
	if len(env.ToolCalls) == 0 {
		t.Fatalf("fixture %s has no tool_calls", p)
	}
	if err := json.Unmarshal([]byte(env.ToolCalls[0].Function.Arguments), into); err != nil {
		t.Fatalf("unmarshal args %s: %v", p, err)
	}
	return true
}

// TestMockFixturesConformToArchitectureRules is the offline half of ADR-049's
// gate (b): it loads the ACTUAL mock fixtures (plan-phase, execution-phase) and
// runs the deterministic plan-reviewer architecture rules over the architecture
// + scope they declare, asserting ZERO blocking (error) findings. This catches
// fixture rot (#162/#163 — the stale scope↔implementation_files mismatch that
// fired scoped_file_unowned, and the file-count overload shape) at `go test`
// time instead of only at a docker mock-ladder run, so the free pre-paid
// regression gate cannot silently rot again.
//
// Scenario evidence is synthesized (one scenario per requirement) to mirror the
// runtime where every requirement carries scenarios at R2 — this exercises the
// ADR-049 stub-risk path: a cohesive multi-capability component (plan-phase's
// `api` owns two capabilities and one file) must pass because each capability
// has scenario evidence, the exact shape the retired file-count rule wrongly
// rejected.
func TestMockFixturesConformToArchitectureRules(t *testing.T) {
	for _, scenario := range []string{"plan-phase", "execution-phase"} {
		t.Run(scenario, func(t *testing.T) {
			var arch workflow.ArchitectureDocument
			if !decodeFixtureArgs(t, scenario, "mock-architecture-generator", &arch) {
				t.Fatalf("%s has no mock-architecture-generator fixture", scenario)
			}

			var planner struct {
				Scope workflow.Scope `json:"scope"`
			}
			decodeFixtureArgs(t, scenario, "mock-planner", &planner)

			var reqGen struct {
				Requirements []struct {
					Title          string `json:"title"`
					CapabilityName string `json:"capability_name"`
				} `json:"requirements"`
			}
			decodeFixtureArgs(t, scenario, "mock-requirement-generator", &reqGen)

			plan := &workflow.Plan{
				Slug:         scenario,
				Scope:        planner.Scope,
				Architecture: &arch,
			}

			// Optional analyst sub-phase (plan-phase has it, execution-phase
			// does not) — populate Exploration so capability.unresolved is also
			// exercised against the real declared capabilities.
			var analyst struct {
				Capabilities []workflow.Capability `json:"capabilities"`
			}
			if decodeFixtureArgs(t, scenario, "mock-planner.1", &analyst) {
				plan.Exploration = &workflow.Exploration{Capabilities: analyst.Capabilities}
			}

			for i, r := range reqGen.Requirements {
				id := fmt.Sprintf("req-%d", i)
				plan.Requirements = append(plan.Requirements, workflow.Requirement{
					ID:             id,
					Title:          r.Title,
					CapabilityName: r.CapabilityName,
				})
				plan.Scenarios = append(plan.Scenarios, workflow.Scenario{
					ID:            fmt.Sprintf("sc-%d", i),
					RequirementID: id,
				})
			}

			result := &workflow.PlanReviewResult{Verdict: "approved"}
			mergeArchitectureFindings(plan, result)

			for _, f := range result.ErrorFindings() {
				t.Errorf("fixture %s fires blocking finding %s on %q: %s", scenario, f.SOPID, f.TargetID, f.Issue)
			}
			if result.Verdict != "approved" {
				t.Errorf("fixture %s: verdict = %q, want approved (no blocking findings)", scenario, result.Verdict)
			}
		})
	}
}
