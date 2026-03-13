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
