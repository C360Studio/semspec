package changeproposalhandler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// allPlanDecisionKinds enumerates PlanDecisionKind for the per-kind disposition
// assertions below. It is NOT trusted as the source of truth on its own:
// TestRecoveryDisposition_WatchdogExhaustive parses workflow/types.go and fails
// if any constant declared there is missing from this list or from
// kindDisposition (and vice-versa). So a new PlanDecisionKind added to the
// source cannot pass CI without being given a deterministic disposition — that
// is the real forcing function for #221 invariant 1, independent of whether
// anyone remembers to update this hand-maintained list.
var allPlanDecisionKinds = []workflow.PlanDecisionKind{
	workflow.PlanDecisionKindRequirementChange,
	workflow.PlanDecisionKindExecutionExhausted,
	workflow.PlanDecisionKindStoryReprepare,
	workflow.PlanDecisionKindArchitectureRevise,
	workflow.PlanDecisionKindAssemblyConflict,
	workflow.PlanDecisionKindScopeIncomplete,
}

// disposition classifies the full-auto handling of a PlanDecisionKind.
type disposition int

const (
	// autoAcceptable: a well-formed recovery proposal of this kind can be
	// auto-accepted by shouldAutoAcceptRecovery (subject to per-kind caps).
	autoAcceptable disposition = iota
	// humanGatedOrTerminal: this kind is NEVER auto-accepted. It is a terminal
	// record (execution_exhausted, assembly_conflict) that must surface as a
	// visible proposed/terminal state, never silently auto-resolve.
	humanGatedOrTerminal
)

// kindDisposition is the #221 INV1 contract: every PlanDecisionKind has a
// deterministic full-auto disposition, so none can sit in proposed-limbo with
// no owner. proposer is the proposer under which an autoAcceptable kind fires —
// the auto-accept filter is proposer-scoped (recovery-agent for the recovery
// actions; plan-manager for the deterministic scope_incomplete gate).
var kindDisposition = map[workflow.PlanDecisionKind]struct {
	want     disposition
	proposer string
}{
	workflow.PlanDecisionKindRequirementChange: {autoAcceptable, "recovery-agent"},
	workflow.PlanDecisionKindStoryReprepare:    {autoAcceptable, "recovery-agent"},
	workflow.PlanDecisionKindScopeIncomplete:   {autoAcceptable, "plan-manager"},
	// #211: full-auto is full-auto — a scoped architecture_revise auto-accepts
	// (bounded by MaxAutoArchitectureRevises), no human gate.
	workflow.PlanDecisionKindArchitectureRevise: {autoAcceptable, "recovery-agent"},
	workflow.PlanDecisionKindExecutionExhausted: {humanGatedOrTerminal, ""},
	workflow.PlanDecisionKindAssemblyConflict:   {humanGatedOrTerminal, ""},
}

// TestPlanDecisionKind_AllKindsEnumerated guards the enumeration used by the
// watchdog: every kind in allPlanDecisionKinds is IsValid(), and IsValid()
// actually rejects unknown kinds. If you add a PlanDecisionKind to IsValid(),
// add it to allPlanDecisionKinds AND kindDisposition — the watchdog then forces
// you to declare its full-auto disposition (#221 INV1).
func TestPlanDecisionKind_AllKindsEnumerated(t *testing.T) {
	for _, k := range allPlanDecisionKinds {
		if !k.IsValid() {
			t.Errorf("allPlanDecisionKinds contains %q which is not IsValid()", k)
		}
	}
	if workflow.PlanDecisionKind("bogus-kind").IsValid() {
		t.Error("IsValid() accepted a bogus kind; the enumeration guard is meaningless")
	}
}

// TestRecoveryDisposition_Watchdog is the #221 INV1 no-wedge invariant: in
// full-auto mode no proposed PlanDecision may wait forever without an owner.
// Every PlanDecisionKind must either auto-accept (a deterministic owner drives
// it) or be a visible human-gated/terminal decision. For each enumerated kind
// it asserts shouldAutoAcceptRecovery agrees with the declared disposition, so a
// regression that drops a kind into proposed-limbo fails deterministically
// instead of wedging a paid run.
func TestRecoveryDisposition_Watchdog(t *testing.T) {
	// Exhaustiveness: every enumerated kind has a declared disposition.
	for _, k := range allPlanDecisionKinds {
		if _, ok := kindDisposition[k]; !ok {
			t.Errorf("PlanDecisionKind %q has no declared full-auto disposition — "+
				"#221 INV1 requires every kind to auto-accept, human-gate, or terminate", k)
		}
	}

	for _, k := range allPlanDecisionKinds {
		d, ok := kindDisposition[k]
		if !ok {
			continue // already reported above
		}
		t.Run(string(k), func(t *testing.T) {
			switch d.want {
			case autoAcceptable:
				// A well-formed, non-contract-changing proposal of this kind from
				// its expected proposer MUST be auto-acceptable (it has an owner).
				dec := &workflow.PlanDecision{
					ProposedBy:     d.proposer,
					Status:         workflow.PlanDecisionStatusProposed,
					Kind:           k,
					AffectedReqIDs: []string{"req.demo.1"},
					ContractImpact: recoveryImpact(workflow.ContractImpactPreserve),
				}
				if !shouldAutoAcceptRecovery(dec) {
					t.Errorf("kind %q is declared autoAcceptable but shouldAutoAcceptRecovery=false "+
						"for a well-formed %s proposal — it has no auto owner and would sit proposed",
						k, d.proposer)
				}
			case humanGatedOrTerminal:
				// NO well-formed proposal of this kind may auto-accept, under any
				// proposer or contract impact — it must surface for a human/terminate.
				for _, proposer := range []string{"recovery-agent", "plan-manager", "qa-reviewer"} {
					for _, impact := range []workflow.ContractImpactKind{
						workflow.ContractImpactPreserve,
						workflow.ContractImpactRefine,
						workflow.ContractImpactChange,
					} {
						dec := &workflow.PlanDecision{
							ProposedBy:     proposer,
							Status:         workflow.PlanDecisionStatusProposed,
							Kind:           k,
							AffectedReqIDs: []string{"req.demo.1"},
							ContractImpact: recoveryImpact(impact),
						}
						if shouldAutoAcceptRecovery(dec) {
							t.Errorf("kind %q is declared human-gated/terminal but auto-accepted "+
								"(proposer=%s impact=%s) — would silently bypass the operator",
								k, proposer, impact)
						}
					}
				}
			}
		})
	}
}

// declaredPlanDecisionKinds parses the workflow package source and returns the
// string value of every constant declared with type PlanDecisionKind. This is
// the genuinely-exhaustive enumeration: it discovers kinds from source rather
// than a hand-maintained list, so a newly-added constant is caught even if a
// developer forgets to update allPlanDecisionKinds / kindDisposition.
func declaredPlanDecisionKinds(t *testing.T) []string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate workflow source")
	}
	workflowDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "workflow")
	entries, err := os.ReadDir(workflowDir)
	if err != nil {
		t.Fatalf("read workflow dir %q: %v", workflowDir, err)
	}

	fset := token.NewFileSet()
	var kinds []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(workflowDir, e.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Values) == 0 {
					// No explicit value → iota-style inheritance, not a
					// string kind declaration we care about.
					continue
				}
				id, ok := vs.Type.(*ast.Ident)
				if !ok || id.Name != "PlanDecisionKind" {
					continue
				}
				for _, val := range vs.Values {
					bl, ok := val.(*ast.BasicLit)
					if !ok || bl.Kind != token.STRING {
						continue
					}
					s, err := strconv.Unquote(bl.Value)
					if err != nil {
						continue
					}
					kinds = append(kinds, s)
				}
			}
		}
	}
	return kinds
}

// TestRecoveryDisposition_WatchdogExhaustive is the genuine #221 INV1 forcing
// function. It parses the workflow source for every declared PlanDecisionKind
// and asserts a two-way correspondence with the watchdog's kindDisposition map
// (and allPlanDecisionKinds): every declared kind has a disposition, and every
// disposition/list entry is a real declared kind. Adding a PlanDecisionKind to
// workflow/types.go without giving it a disposition fails here — the
// hand-maintained list can no longer silently drift out of coverage.
func TestRecoveryDisposition_WatchdogExhaustive(t *testing.T) {
	declared := declaredPlanDecisionKinds(t)
	if len(declared) == 0 {
		t.Fatal("discovered zero PlanDecisionKind constants in workflow source — " +
			"the AST parse is broken, so the exhaustiveness guarantee is void")
	}

	declaredSet := make(map[string]bool, len(declared))
	listSet := make(map[string]bool, len(allPlanDecisionKinds))
	for _, k := range allPlanDecisionKinds {
		listSet[string(k)] = true
	}

	for _, k := range declared {
		declaredSet[k] = true
		if _, ok := kindDisposition[workflow.PlanDecisionKind(k)]; !ok {
			t.Errorf("PlanDecisionKind %q is declared in workflow/types.go but has no entry in "+
				"kindDisposition — #221 INV1 requires every kind to auto-accept, human-gate, or terminate", k)
		}
		if !listSet[k] {
			t.Errorf("PlanDecisionKind %q is declared in workflow/types.go but missing from "+
				"allPlanDecisionKinds", k)
		}
	}

	// Reverse direction: no stale entries that no longer correspond to a kind.
	for k := range kindDisposition {
		if !declaredSet[string(k)] {
			t.Errorf("kindDisposition lists %q which is not a declared PlanDecisionKind (stale entry)", k)
		}
	}
	for k := range listSet {
		if !declaredSet[k] {
			t.Errorf("allPlanDecisionKinds lists %q which is not a declared PlanDecisionKind (stale entry)", k)
		}
	}
}
