package domain

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/prompt"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/harnesscatalog"
)

// TestRenderResolvedHarnessProfilesFromRealCatalog pins three load-bearing
// guarantees for ADR-039 Phase 1a:
//
//  1. The intro guidance is the post-ADR-039 shape (services vs testcontainers
//     vs pure-fixture) — NOT the old "do not edit GitHub Actions services"
//     string that contradicted what we now ship.
//  2. Every rendered profile carries an Orchestration line so architect/dev
//     see the orchestration choice surfaced.
//  3. services-orchestrated profiles render as `services` and pure-fixture
//     profiles render as `pure-fixture` from real catalog data — pinning the
//     EffectiveOrchestration() inference against the built-in mavlink entries.
func TestRenderResolvedHarnessProfilesFromRealCatalog(t *testing.T) {
	cat, err := harnesscatalog.LoadBuiltIn()
	if err != nil {
		t.Fatalf("LoadBuiltIn() error = %v", err)
	}
	resolved, err := cat.ResolveSelections([]workflow.HarnessProfileSelection{
		{ProfileID: "mavlink.px4-sitl.mavsdk-smoke", UsedBy: []string{"driver"}, Purpose: "SITL smoke"},
		{ProfileID: "mavlink.raw-mavlink-direct", UsedBy: []string{"parser"}, Purpose: "frame round-trip"},
	})
	if err != nil {
		t.Fatalf("ResolveSelections() error = %v", err)
	}

	ctxs := make([]prompt.ResolvedHarnessProfileContext, 0, len(resolved))
	for _, r := range resolved {
		p := r.Profile
		ctxs = append(ctxs, prompt.ResolvedHarnessProfileContext{
			ProfileID:     p.ID,
			Tier:          p.Tier,
			Orchestration: p.EffectiveOrchestration(),
			UsedBy:        r.Selection.UsedBy,
			Purpose:       r.Selection.Purpose,
		})
	}

	out := renderResolvedHarnessProfiles("## Harness Profiles", ctxs)

	if !strings.Contains(out, "`services`-orchestrated profiles, qa-runner brings the stack up") {
		t.Errorf("intro missing services-orchestrated guidance:\n%s", out)
	}
	if !strings.Contains(out, "`testcontainers` or `pure-fixture` profiles, the test fixture owns") {
		t.Errorf("intro missing testcontainers/pure-fixture guidance:\n%s", out)
	}
	if strings.Contains(out, "do not edit GitHub Actions services") {
		t.Errorf("stale pre-ADR-039 guidance leaked back into intro:\n%s", out)
	}

	if !strings.Contains(out, "### mavlink.px4-sitl.mavsdk-smoke (required)") {
		t.Errorf("missing PX4 SITL profile header:\n%s", out)
	}
	if !strings.Contains(out, "### mavlink.raw-mavlink-direct (compatibility)") {
		t.Errorf("missing raw-mavlink-direct profile header:\n%s", out)
	}

	pxBlock := profileBlock(out, "### mavlink.px4-sitl.mavsdk-smoke")
	if !strings.Contains(pxBlock, "- **Orchestration:** services") {
		t.Errorf("PX4 SITL profile missing Orchestration: services line:\n%s", pxBlock)
	}
	rawBlock := profileBlock(out, "### mavlink.raw-mavlink-direct")
	if !strings.Contains(rawBlock, "- **Orchestration:** pure-fixture") {
		t.Errorf("raw-mavlink-direct profile missing Orchestration: pure-fixture line:\n%s", rawBlock)
	}
}

// profileBlock returns the substring of out starting at headerPrefix up to
// (but not including) the next `### ` header or end-of-string. Helper for
// per-profile assertions when multiple profiles are rendered.
func profileBlock(out, headerPrefix string) string {
	start := strings.Index(out, headerPrefix)
	if start < 0 {
		return ""
	}
	rest := out[start+len(headerPrefix):]
	next := strings.Index(rest, "### ")
	if next < 0 {
		return out[start:]
	}
	return out[start : start+len(headerPrefix)+next]
}
