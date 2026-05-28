package qarender

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/harnesscatalog"
)

func TestRenderEmptySelectionsReturnsEmptyMapping(t *testing.T) {
	node, err := Render(nil, Options{})
	if err != nil {
		t.Fatalf("Render(nil) error = %v", err)
	}
	if node == nil {
		t.Fatal("Render(nil) returned nil node; want non-nil empty mapping")
	}
	if len(node.Content) != 0 {
		t.Errorf("Render(nil) content len = %d, want 0", len(node.Content))
	}
}

func TestRenderSkipsPureFixtureAndTestcontainersProfiles(t *testing.T) {
	selections := []harnesscatalog.ResolvedSelection{
		{
			Selection: workflow.HarnessProfileSelection{ProfileID: "x.pure"},
			Profile: harnesscatalog.Profile{
				ID:            "x.pure",
				Orchestration: harnesscatalog.OrchestrationPureFixture,
			},
		},
		{
			Selection: workflow.HarnessProfileSelection{ProfileID: "x.tc"},
			Profile: harnesscatalog.Profile{
				ID:            "x.tc",
				Orchestration: harnesscatalog.OrchestrationTestcontainers,
				Images:        []harnesscatalog.ImageRef{{Name: "irrelevant"}},
			},
		},
	}
	got, err := RenderYAML(selections, Options{})
	if err != nil {
		t.Fatalf("RenderYAML() error = %v", err)
	}
	if got != "" {
		t.Errorf("RenderYAML() = %q, want empty string for non-services profiles", got)
	}
}

func TestRenderServicesProfileWithoutPortOffset(t *testing.T) {
	selections := mustResolve(t, "mavlink.px4-sitl.mavsdk-smoke")
	got, err := RenderYAML(selections, Options{})
	if err != nil {
		t.Fatalf("RenderYAML() error = %v", err)
	}
	const want = `# Profile: mavlink.px4-sitl.mavsdk-smoke
# Readiness (operator must enforce in qa-runner; not emitted as docker healthcheck):
#   - Wait for MAVLink HEARTBEAT frames on UDP 14540.
#   - Wait for MAVSDK connection state to become connected.
mavlink-px4-sitl-mavsdk-smoke:
  image: px4io/px4-sitl:latest
  env:
    PX4_SIM_MODEL: iris
  ports:
    - 14540/udp
`
	if got != want {
		t.Errorf("RenderYAML() mismatch.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderServicesProfileWithPortOffset(t *testing.T) {
	selections := mustResolve(t, "mavlink.px4-sitl.mavsdk-smoke")
	got, err := RenderYAML(selections, Options{PortOffset: 12000})
	if err != nil {
		t.Fatalf("RenderYAML() error = %v", err)
	}
	if !strings.Contains(got, "- 26540:14540/udp") {
		t.Errorf("expected port mapping 26540:14540/udp in output, got:\n%s", got)
	}
}

func TestRenderMultipleServicesProfilesDeterministic(t *testing.T) {
	selections := mustResolve(t, "mavlink.px4-sitl.mavsdk-smoke", "mavlink.ardupilot-sitl.compat")
	got, err := RenderYAML(selections, Options{})
	if err != nil {
		t.Fatalf("RenderYAML() error = %v", err)
	}
	pxIdx := strings.Index(got, "mavlink-px4-sitl-mavsdk-smoke:")
	apIdx := strings.Index(got, "mavlink-ardupilot-sitl-compat:")
	if pxIdx < 0 || apIdx < 0 {
		t.Fatalf("missing expected service names in output:\n%s", got)
	}
	if pxIdx >= apIdx {
		t.Errorf("services rendered out of input order: px4 at %d, ardupilot at %d", pxIdx, apIdx)
	}
}

func TestRenderMixedSelectionEmitsOnlyServicesProfiles(t *testing.T) {
	selections := mustResolve(t,
		"mavlink.raw-mavlink-direct",
		"mavlink.px4-sitl.mavsdk-smoke",
	)
	got, err := RenderYAML(selections, Options{})
	if err != nil {
		t.Fatalf("RenderYAML() error = %v", err)
	}
	if strings.Contains(got, "mavlink-raw-mavlink-direct") {
		t.Errorf("pure-fixture profile leaked into rendered output:\n%s", got)
	}
	if !strings.Contains(got, "mavlink-px4-sitl-mavsdk-smoke:") {
		t.Errorf("services profile missing from rendered output:\n%s", got)
	}
}

func TestRenderRejectsServicesProfileWithoutImages(t *testing.T) {
	bad := []harnesscatalog.ResolvedSelection{{
		Selection: workflow.HarnessProfileSelection{ProfileID: "x.bad"},
		Profile: harnesscatalog.Profile{
			ID:            "x.bad",
			Orchestration: harnesscatalog.OrchestrationServices,
		},
	}}
	_, err := Render(bad, Options{})
	if err == nil || !strings.Contains(err.Error(), "no images") {
		t.Fatalf("Render(services without images) error = %v, want substring 'no images'", err)
	}
}

// TestRenderRealCatalogMixedSelection is the cross-package integration shape
// for ADR-039 Phase 1a: load the real built-in catalog, select a services
// profile and a pure-fixture profile in the same call, and verify the renderer
// emits a services block for the former and skips the latter without error.
// Mirrors what Phase 1c will actually receive at qa.yml emission time.
func TestRenderRealCatalogMixedSelection(t *testing.T) {
	selections := mustResolve(t,
		"mavlink.raw-mavlink-direct",    // pure-fixture, no images
		"mavlink.px4-sitl.mavsdk-smoke", // services, images present
		"mavlink.ardupilot-sitl.compat", // services, images present
	)
	got, err := RenderYAML(selections, Options{PortOffset: 12000})
	if err != nil {
		t.Fatalf("RenderYAML() error = %v", err)
	}
	if got == "" {
		t.Fatal("RenderYAML() returned empty string; want services block")
	}
	if strings.Contains(got, "mavlink-raw-mavlink-direct") {
		t.Errorf("pure-fixture profile leaked into output:\n%s", got)
	}
	for _, want := range []string{
		"mavlink-px4-sitl-mavsdk-smoke:",
		"image: px4io/px4-sitl:latest",
		"- 26540:14540/udp",
		"mavlink-ardupilot-sitl-compat:",
		"image: ardupilot/ardupilot:latest",
		"- 26550:14550/udp",
		"PX4_SIM_MODEL: iris",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestServiceNameReplacesDots(t *testing.T) {
	cases := map[string]string{
		"mavlink.px4-sitl.mavsdk-smoke": "mavlink-px4-sitl-mavsdk-smoke",
		"no.dots.here":                  "no-dots-here",
		"plain":                         "plain",
	}
	for in, want := range cases {
		if got := ServiceName(in); got != want {
			t.Errorf("ServiceName(%q) = %q, want %q", in, got, want)
		}
	}
}

func mustResolve(t *testing.T, ids ...string) []harnesscatalog.ResolvedSelection {
	t.Helper()
	catalog, err := harnesscatalog.LoadBuiltIn()
	if err != nil {
		t.Fatalf("LoadBuiltIn() error = %v", err)
	}
	sels := make([]workflow.HarnessProfileSelection, len(ids))
	for i, id := range ids {
		sels[i] = workflow.HarnessProfileSelection{ProfileID: id, Purpose: "test"}
	}
	resolved, err := catalog.ResolveSelections(sels)
	if err != nil {
		t.Fatalf("ResolveSelections() error = %v", err)
	}
	return resolved
}
