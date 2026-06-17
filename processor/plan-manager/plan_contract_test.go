package planmanager

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestPlanStoreCreateSeedsContractPacketBeforeFirstSave(t *testing.T) {
	ps, err := newPlanStore(context.Background(), nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("newPlanStore: %v", err)
	}

	plan, err := ps.create(
		context.Background(),
		"brownfield-plan",
		"Brownfield Plan",
		"Integrate into the existing baseline; do not create a standalone project.",
		workflow.QALevelSynthesis,
		nil,
	)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if plan.Contract == nil {
		t.Fatal("new plan Contract is nil")
	}
	if plan.Contract.ID != workflow.PlanContractID("brownfield-plan") {
		t.Fatalf("Contract.ID = %q", plan.Contract.ID)
	}
	if plan.Contract.Brief != "Integrate into the existing baseline; do not create a standalone project." {
		t.Fatalf("Contract.Brief = %q", plan.Contract.Brief)
	}

	stored, ok := ps.get("brownfield-plan")
	if !ok {
		t.Fatal("plan not found in store after create")
	}
	if stored.Contract == nil {
		t.Fatal("stored plan Contract is nil")
	}
	if stored.Contract.ID != workflow.PlanContractID("brownfield-plan") {
		t.Fatalf("stored Contract.ID = %q", stored.Contract.ID)
	}
}

func TestPlanStoreCreateHydratesRuntimeTopologyFacts(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "go.mod"), []byte("module example.test/topology\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	ps, err := newPlanStore(context.Background(), nil, nil, slog.Default(), repoRoot)
	if err != nil {
		t.Fatalf("newPlanStore: %v", err)
	}

	plan, err := ps.create(
		context.Background(),
		"topology-plan",
		"Topology Plan",
		"Extend the existing repository.",
		workflow.QALevelSynthesis,
		nil,
	)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if plan.Contract == nil {
		t.Fatal("new plan Contract is nil")
	}
	if !hasTopologyFact(plan.Contract.TopologyFacts, "build_root", "go.mod", "go_module") {
		t.Fatalf("TopologyFacts = %#v, want go module build_root", plan.Contract.TopologyFacts)
	}

	stored, ok := ps.get("topology-plan")
	if !ok {
		t.Fatal("plan not found in store after create")
	}
	if stored.Contract == nil || !hasTopologyFact(stored.Contract.TopologyFacts, "build_root", "go.mod", "go_module") {
		t.Fatalf("stored TopologyFacts = %#v, want go module build_root", stored.Contract)
	}
}

func TestPlanStoreCreateImportedSeedsContractWhenMissing(t *testing.T) {
	ps, err := newPlanStore(context.Background(), nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("newPlanStore: %v", err)
	}
	plan := &workflow.Plan{
		Slug:    "imported-plan",
		Title:   "Imported Plan",
		Context: "Imported OpenSpec change brief.",
		Scope: workflow.Scope{
			Include: []string{"src/existing.go"},
		},
	}

	if err := ps.createImported(context.Background(), plan, workflow.QALevelSynthesis, nil); err != nil {
		t.Fatalf("createImported: %v", err)
	}
	stored, ok := ps.get("imported-plan")
	if !ok {
		t.Fatal("imported plan not found")
	}
	if stored.Contract == nil {
		t.Fatal("imported plan Contract is nil")
	}
	if stored.Contract.Brief != "Imported OpenSpec change brief." {
		t.Fatalf("Contract.Brief = %q", stored.Contract.Brief)
	}
	if got := stored.Contract.Scope.Include; len(got) != 1 || got[0] != "src/existing.go" {
		t.Fatalf("Contract.Scope.Include = %v", got)
	}
}

func hasTopologyFact(facts []workflow.TopologyFact, kind, path, value string) bool {
	for _, fact := range facts {
		if fact.Kind == kind && fact.Path == path && fact.Value == value {
			return true
		}
	}
	return false
}
