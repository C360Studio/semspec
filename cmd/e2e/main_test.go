package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/c360studio/semspec/test/e2e/config"
)

// TestMockFixtureDirsMapToRegisteredScenarios guards the runnable-name ↔
// fixture-dir bijection. Every directory under test/e2e/fixtures/mock-responses
// must correspond to a registered scenario Name() (modulo the documented
// fixture-alias targets), or it is an ORPHAN: well-formed (so the offline
// conformance test in processor/plan-reviewer happily passes over it) yet
// unrunnable — `e2e <name>` and `task e2e:mock -- <name>` both fail with
// "unknown scenario". That is exactly how hello-world-double-rejection rotted
// undetected: a complete, conformant fixture set with no scenario behind it.
//
// This runs inside `go test ./...` (the only CI gate over these fixtures), so a
// future orphan fails closed instead of lurking until someone runs the manual
// docker mock-ladder.
func TestMockFixtureDirsMapToRegisteredScenarios(t *testing.T) {
	registered := map[string]struct{}{}
	for _, s := range buildScenarioList(&config.Config{}) {
		registered[s.Name()] = struct{}{}
	}

	// Fixture dirs that intentionally back a differently-named scenario via the
	// mock-llm fixtureScenarioAliases map (cmd/mock-llm/main.go). The dir is the
	// alias TARGET and is itself a registered name, so it is covered by the
	// registered set above; this map documents the relationship for readers and
	// future-proofs the check if an alias target ever stops being registered
	// under its own name.
	fixtureAliasTargets := map[string]struct{}{
		"qa-cycle": {}, // qa-cycle-integration reuses qa-cycle's fixtures
	}

	const mockRoot = "../../test/e2e/fixtures/mock-responses"
	entries, err := os.ReadDir(mockRoot)
	if err != nil {
		t.Fatalf("read mock fixture root %s: %v", mockRoot, err)
	}

	sawDir := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sawDir = true
		dir := e.Name()
		if _, ok := registered[dir]; ok {
			continue
		}
		if _, ok := fixtureAliasTargets[dir]; ok {
			continue
		}
		t.Errorf("orphan fixture dir %q under %s: no registered scenario has Name()==%q and it is not a known fixture-alias target. Register a scenario for it (cmd/e2e buildScenarioList), record it in fixtureAliasTargets, or delete the directory.",
			dir, filepath.Clean(mockRoot), dir)
	}
	if !sawDir {
		t.Fatalf("no fixture directories found under %s", filepath.Clean(mockRoot))
	}
}
