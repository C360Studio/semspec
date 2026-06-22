package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c360studio/semspec/test/e2e/config"
	"github.com/c360studio/semspec/tools/terminal"
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
		"qa-unit":     {}, // qa-integration reuses qa-unit's fixtures
		"stall-retry": {}, // stall-complete and stall-reject reuse stall-retry's fixtures
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

func TestMockCoderSubmitWorkFixturesDeclareFileIntents(t *testing.T) {
	const mockRoot = "../../test/e2e/fixtures/mock-responses"

	type toolCall struct {
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	type fixtureEnvelope struct {
		ToolCalls     []toolCall `json:"tool_calls"`
		FilesModified []string   `json:"files_modified"`
		FileIntents   []any      `json:"file_intents"`
	}

	checked := 0
	err := filepath.WalkDir(mockRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasPrefix(d.Name(), "mock-coder") || filepath.Ext(d.Name()) != ".json" {
			return nil
		}

		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var fixture fixtureEnvelope
		if err := json.Unmarshal(raw, &fixture); err != nil {
			t.Errorf("%s: invalid JSON fixture: %v", p, err)
			return nil
		}

		for _, call := range fixture.ToolCalls {
			if call.Function.Name != "submit_work" {
				continue
			}
			var args map[string]any
			if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
				t.Errorf("%s: submit_work arguments are not valid JSON: %v", p, err)
				continue
			}
			if _, ok := args["files_modified"]; !ok {
				continue
			}
			if err := terminal.ValidateDeveloperDeliverable(args); err != nil {
				t.Errorf("%s: coder submit_work fixture violates developer schema: %v", p, err)
			}
			checked++
		}

		if len(fixture.FilesModified) > 0 {
			checked++
			if len(fixture.FileIntents) != len(fixture.FilesModified) {
				t.Errorf("%s: top-level coder fixture has %d files_modified entries but %d file_intents entries",
					p, len(fixture.FilesModified), len(fixture.FileIntents))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk mock fixture root %s: %v", filepath.Clean(mockRoot), err)
	}
	if checked == 0 {
		t.Fatalf("no mock coder submit_work or top-level files_modified fixtures found under %s", filepath.Clean(mockRoot))
	}
}
