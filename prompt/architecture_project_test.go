package prompt_test

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/prompt"
	"github.com/c360studio/semspec/workflow"
)

// TestProjectArchitecture_CarriesUpstreamAndComponentFacts verifies the single
// faithful graph→role projection carries the load-bearing facts that the
// per-role hand-rolled projections used to drop: the resolved upstream
// Coordinate + API surface, and each component's UpstreamRefs.
func TestProjectArchitecture_CarriesUpstreamAndComponentFacts(t *testing.T) {
	t.Parallel()
	arch := &workflow.ArchitectureDocument{
		ComponentBoundaries: []workflow.ComponentDef{{
			Name:                "driver",
			Responsibility:      "owns the mavsdk driver",
			UpstreamRefs:        []string{"MAVSDK"},
			ImplementationFiles: []string{"src/Driver.java"},
			Capabilities:        []string{"mavsdk-bootstrap"},
		}},
		UpstreamResolutions: []workflow.UpstreamResolution{{
			Name:       "MAVSDK",
			Coordinate: "io.mavsdk:mavsdk:3.16.0",
			Role:       "runtime_dep",
			UsedBy:     []string{"driver"},
			APIs: []workflow.APISurface{{
				Symbol:    "System.connect",
				Import:    "io.mavsdk.System",
				Artifact:  "io.mavsdk:mavsdk-server:3.16.0",
				Kind:      "method",
				Signature: "void connect(String url)",
				Citation:  "https://example/javadoc",
			}},
		}},
	}

	proj := prompt.ProjectArchitecture(arch)
	if len(proj.Upstreams) != 1 || proj.Upstreams[0].Coordinate != "io.mavsdk:mavsdk:3.16.0" {
		t.Fatalf("upstream coordinate dropped by projection: %+v", proj.Upstreams)
	}
	if len(proj.Upstreams[0].APIs) != 1 || proj.Upstreams[0].APIs[0].Symbol != "System.connect" {
		t.Fatalf("API surface dropped by projection: %+v", proj.Upstreams[0].APIs)
	}
	if len(proj.Components) != 1 || len(proj.Components[0].UpstreamRefs) != 1 {
		t.Fatalf("component UpstreamRefs dropped by projection: %+v", proj.Components)
	}

	// The dev/reviewer-facing formatter must surface the build-manifest
	// coordinate and the symbol — the facts that stop dep hallucination.
	if proj.Upstreams[0].APIs[0].Import != "io.mavsdk.System" {
		t.Fatalf("API import dropped by projection: %+v", proj.Upstreams[0].APIs[0])
	}
	out := prompt.FormatUpstreamResolutions(proj.Upstreams)
	mustContainStr(t, out, "io.mavsdk:mavsdk:3.16.0", "dev needs the exact resolved coordinate")
	mustContainStr(t, out, "System.connect", "dev needs the resolved API symbol")
	// The verified fully-qualified import + artifact must reach the dev so it
	// pastes them instead of guessing the package (2026-06-07 javap-thrash fix).
	mustContainStr(t, out, "import: `io.mavsdk.System`", "dev needs the verified fully-qualified import")
	mustContainStr(t, out, "io.mavsdk:mavsdk-server:3.16.0", "dev needs the artifact the symbol resolves in")

	// The full architecture context must surface the component→upstream link.
	full := prompt.FormatArchitectureContext(proj)
	mustContainStr(t, full, "depends on upstream: MAVSDK", "Sarah/Bob need the component→upstream link")
}

// TestProjectArchitecture_NilSafe confirms the projector and formatters are
// safe on a nil architecture (early plan-layer wedges, skip-architecture).
func TestProjectArchitecture_NilSafe(t *testing.T) {
	t.Parallel()
	proj := prompt.ProjectArchitecture(nil)
	if len(proj.Upstreams) != 0 || len(proj.Components) != 0 {
		t.Fatalf("nil architecture must project to empty: %+v", proj)
	}
	if prompt.FormatArchitectureContext(proj) != "" {
		t.Fatal("empty projection must render empty string")
	}
	if prompt.FormatUpstreamResolutions(nil) != "" {
		t.Fatal("nil upstreams must render empty string")
	}
}

func mustContainStr(t *testing.T, haystack, needle, why string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("output missing %q (%s)\n--- output ---\n%s", needle, why, haystack)
	}
}
