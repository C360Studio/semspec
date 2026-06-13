package scenarioorchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/c360studio/semstreams/config"
)

// TestE2EConfigsOrchestratorHasSandboxURL guards the config-sweep regression that
// surfaced on a paid mavlink-hard run (2026-06-13): the branch-derivation PR added
// scenario-orchestrator.config.sandbox_url to semspec.json + the hybrid/mock configs
// but missed 6 of the e2e configs (gemini/claude/local/openrouter/sparky/e2e). The
// orchestrator needs a sandbox client to merge >=2 prerequisite owner branches into a
// reqbase derivation base; without sandbox_url it dispatches 0/1-prereq requirements
// fine but fails every requirement with >=2 branch prerequisites:
//
//	"requirement X has N branch prerequisites but no sandbox is configured to merge
//	 them into a derivation base"
//
// which silently blocks any plan whose dependency graph fans in. This is a pure
// config-content defect (the resolveRequirementBase logic is unit-covered), so only a
// config-content assertion catches it. Reproduces offline in <1s — the kind of
// plumbing bug that should never cost a paid LLM run to discover.
func TestE2EConfigsOrchestratorHasSandboxURL(t *testing.T) {
	files, err := filepath.Glob("../../configs/e2e-*.json")
	if err != nil {
		t.Fatalf("glob e2e configs: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no configs/e2e-*.json found — wrong working directory?")
	}

	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read %s: %v", f, err)
			}
			// Expand ${VAR:-default} the same way cmd/semspec does at load time so
			// the file becomes valid JSON (configs embed numeric env templates).
			expanded := config.ExpandEnvWithDefaults(string(raw))

			var doc struct {
				Components map[string]struct {
					Enabled bool `json:"enabled"`
					Config  struct {
						SandboxURL string `json:"sandbox_url"`
					} `json:"config"`
				} `json:"components"`
			}
			if err := json.Unmarshal([]byte(expanded), &doc); err != nil {
				t.Fatalf("parse %s after env-expansion: %v", f, err)
			}

			orch, ok := doc.Components["scenario-orchestrator"]
			if !ok || !orch.Enabled {
				t.Skipf("scenario-orchestrator not enabled in %s", filepath.Base(f))
			}
			if orch.Config.SandboxURL == "" {
				t.Errorf("%s: scenario-orchestrator.config.sandbox_url is empty — the >=2-prerequisite "+
					"reqbase merge path (branch derivation) cannot resolve a base and will fail dispatch. "+
					"Add \"sandbox_url\": \"http://sandbox:8090\" to the orchestrator config.", filepath.Base(f))
			}
		})
	}
}
