package domain

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/prompt"
)

func TestResearchFragments(t *testing.T) {
	fragments := Research()
	if len(fragments) == 0 {
		t.Fatal("expected non-empty research fragments")
	}

	ids := make(map[string]bool)
	for _, f := range fragments {
		if f.ID == "" {
			t.Error("fragment with empty ID")
		}
		if ids[f.ID] {
			t.Errorf("duplicate fragment ID: %s", f.ID)
		}
		ids[f.ID] = true
	}

	required := []string{
		"research.analyst.system-base",
		"research.synthesizer.system-base",
		"research.reviewer.system-base",
	}
	for _, id := range required {
		if !ids[id] {
			t.Errorf("missing required fragment: %s", id)
		}
	}
}

func TestResearchAnalystAssembly(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Research()...)

	a := prompt.NewAssembler(r)
	result := a.Assemble(&prompt.AssemblyContext{
		Role:     prompt.RoleDeveloper,
		Provider: prompt.ProviderAnthropic,
	})

	if !strings.Contains(result.SystemMessage, "research analyst") {
		t.Error("expected research analyst identity")
	}
	if !strings.Contains(result.SystemMessage, "evidence") {
		t.Error("expected evidence-based language")
	}
}

func TestResearchReviewerAssembly(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Research()...)

	a := prompt.NewAssembler(r)
	result := a.Assemble(&prompt.AssemblyContext{
		Role:     prompt.RoleReviewer,
		Provider: prompt.ProviderOpenAI,
	})

	if !strings.Contains(result.SystemMessage, "peer-reviewing research findings") {
		t.Error("expected research reviewer identity")
	}
	if !strings.Contains(result.SystemMessage, "Evidence Quality") {
		t.Error("expected evidence quality criteria")
	}
}

func TestResearchSynthesizerAssembly(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Research()...)

	a := prompt.NewAssembler(r)
	result := a.Assemble(&prompt.AssemblyContext{
		Role:     prompt.RolePlanner,
		Provider: prompt.ProviderOllama,
	})

	if !strings.Contains(result.SystemMessage, "synthesizing findings") {
		t.Error("expected synthesizer identity")
	}
}

// TestResearchGraphResultOrientation pins the take-33 fix mirrored from the
// software domain: all three research roles (analyst, synthesizer, reviewer)
// must see world-model framing for graph search results — entities tagged
// [doc]/[author]/[project]/[component] are indexed facts from real sources,
// silence is itself signal about what the indexed corpus references. Goal
// is reasoning enrichment, NOT procedural directives. Goodhart guard
// asserts the fragment does not reintroduce "MUST"/"before X do Y" shapes.
func TestResearchGraphResultOrientation(t *testing.T) {
	r := prompt.NewRegistry()
	r.RegisterAll(Research()...)
	a := prompt.NewAssembler(r)

	mustContain := []string{
		"Indexed graph entities",
		"[doc]",
		"[author]",
		"facts at index time",
		"Silence is also signal",
	}
	mustNotContain := []string{
		"You MUST call graph_query",
		"Before writing claims",
		"Before you cite",
	}

	for _, role := range []prompt.Role{prompt.RoleDeveloper, prompt.RolePlanner, prompt.RoleReviewer} {
		result := a.Assemble(&prompt.AssemblyContext{
			Role:           role,
			Provider:       prompt.ProviderOpenAI,
			AvailableTools: []string{"bash", "submit_work", "graph_summary", "graph_search", "graph_query"},
		})
		for _, want := range mustContain {
			if !strings.Contains(result.SystemMessage, want) {
				t.Errorf("role=%s missing %q from graph-results orientation", role, want)
			}
		}
		for _, banned := range mustNotContain {
			if strings.Contains(result.SystemMessage, banned) {
				t.Errorf("role=%s reintroduced procedural directive %q (Goodhart guard)", role, banned)
			}
		}
	}
}
