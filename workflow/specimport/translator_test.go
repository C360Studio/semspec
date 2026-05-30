package specimport

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/graph"
	"github.com/c360studio/semspec/vocabulary/spec"
	"github.com/c360studio/semspec/workflow"
)

// fakeQuerier is a minimal graph.Querier implementation that returns
// pre-canned entities. Only the methods Translate uses are implemented
// fully; the rest no-op.
type fakeQuerier struct {
	byPredicate map[string][]graph.Entity
	traversals  map[string][]graph.Entity // key = "<entityID>|<predicate>"
}

func (f *fakeQuerier) QueryEntitiesByPredicate(_ context.Context, predicatePrefix string) ([]graph.Entity, error) {
	return f.byPredicate[predicatePrefix], nil
}
func (f *fakeQuerier) QueryEntitiesByIDPrefix(_ context.Context, _ string) ([]graph.Entity, error) {
	return nil, nil
}
func (f *fakeQuerier) GetEntity(_ context.Context, _ string) (*graph.Entity, error) {
	return nil, nil
}
func (f *fakeQuerier) HydrateEntity(_ context.Context, _ string, _ int) (string, error) {
	return "", nil
}
func (f *fakeQuerier) GetCodebaseSummary(_ context.Context) (string, error) {
	return "", nil
}
func (f *fakeQuerier) TraverseRelationships(_ context.Context, startEntity, predicate, _ string, _ int) ([]graph.Entity, error) {
	return f.traversals[startEntity+"|"+predicate], nil
}
func (f *fakeQuerier) Ping(_ context.Context) error { return nil }
func (f *fakeQuerier) WaitForReady(_ context.Context, _ time.Duration) error {
	return nil
}
func (f *fakeQuerier) QueryProjectSources(_ context.Context, _ string) ([]graph.Entity, error) {
	return nil, nil
}
func (f *fakeQuerier) GraphSummary(_ context.Context) ([]graph.SourceSummary, error) {
	return nil, nil
}

// buildFakeGraph constructs a fake graph with one spec entity per
// capability, one requirement entity per spec, and one scenario per
// requirement. Capability file paths are anchored under changePath/specs.
func buildFakeGraph(changePath string, capNames []string) *fakeQuerier {
	q := &fakeQuerier{
		byPredicate: make(map[string][]graph.Entity),
		traversals:  make(map[string][]graph.Entity),
	}
	for _, capName := range capNames {
		specEntityID := "graph.spec.specification." + capName
		specFilePath := filepath.Join(changePath, "specs", capName, "spec.md")
		specEntity := graph.Entity{
			ID: specEntityID,
			Triples: []graph.Triple{
				{Predicate: spec.SpecType, Object: "specification"},
				{Predicate: spec.SpecFilePath, Object: specFilePath},
				{Predicate: spec.SpecTitle, Object: "Spec for " + capName},
			},
		}
		q.byPredicate[spec.SpecType] = append(q.byPredicate[spec.SpecType], specEntity)

		// One requirement under the spec.
		reqEntityID := "graph.req." + capName + ".req-1"
		reqEntity := graph.Entity{
			ID: reqEntityID,
			Triples: []graph.Triple{
				{Predicate: spec.SpecType, Object: "requirement"},
				{Predicate: spec.RequirementName, Object: capName + " primary requirement"},
				{Predicate: spec.RequirementDescription, Object: "Capability " + capName + " SHALL behave correctly."},
			},
		}
		q.traversals[specEntityID+"|"+spec.HasRequirement] = []graph.Entity{reqEntity}

		// One scenario under the requirement.
		scenEntityID := "graph.scen." + capName + ".s1"
		scenEntity := graph.Entity{
			ID: scenEntityID,
			Triples: []graph.Triple{
				{Predicate: spec.SpecType, Object: "scenario"},
				{Predicate: spec.ScenarioName, Object: "baseline"},
				{Predicate: spec.ScenarioGiven, Object: "preconditions hold"},
				{Predicate: spec.ScenarioWhen, Object: "action fires"},
				{Predicate: spec.ScenarioThen, Object: []any{"effect occurs"}},
			},
		}
		q.traversals[reqEntityID+"|"+spec.HasScenario] = []graph.Entity{scenEntity}
	}
	return q
}

func sampleStructural(changeName, changePath string, caps []string) *StructuralResult {
	return &StructuralResult{
		OK:         true,
		ChangeName: changeName,
		ChangePath: changePath,
		Proposal: StructuralProposal{
			Exists:          true,
			CapabilityNames: caps,
		},
	}
}

func TestTranslate_HappyPath(t *testing.T) {
	changeName := "sample-change"
	changePath := filepath.Join(t.TempDir(), changeName)
	caps := []string{"user-auth", "session-store"}
	q := buildFakeGraph(changePath, caps)
	sr := sampleStructural(changeName, changePath, caps)

	tr, err := Translate(context.Background(), q, sr, TranslateOptions{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}

	if got := len(tr.Plan.Exploration.Capabilities); got != 2 {
		t.Errorf("expected 2 capabilities, got %d", got)
	}
	for i, capName := range caps {
		gotName := tr.Plan.Exploration.Capabilities[i].Name
		if gotName != capName {
			t.Errorf("cap[%d]: want %q, got %q", i, capName, gotName)
		}
		if tr.Plan.Exploration.Capabilities[i].Lifecycle != workflow.CapabilityModified {
			t.Errorf("expected lifecycle=modified for imported cap %q, got %q", capName, tr.Plan.Exploration.Capabilities[i].Lifecycle)
		}
	}
	if got := len(tr.Plan.Requirements); got != 2 {
		t.Errorf("expected 2 requirements, got %d", got)
	}
	for _, r := range tr.Plan.Requirements {
		if r.CapabilityName == "" {
			t.Errorf("requirement %s missing CapabilityName", r.ID)
		}
		if !strings.Contains(r.Title, "primary requirement") {
			t.Errorf("requirement %s title looks wrong: %q", r.ID, r.Title)
		}
	}
	if got := len(tr.Plan.Scenarios); got != 2 {
		t.Errorf("expected 2 scenarios, got %d", got)
	}
	if tr.Plan.Status != workflow.StatusExplored {
		t.Errorf("expected imported plan to land at StatusExplored, got %q", tr.Plan.Status)
	}

	// External refs: one per capability + one per requirement.
	if got := len(tr.ExternalRefs); got != 4 {
		t.Errorf("expected 4 external refs (2 caps + 2 reqs), got %d (%v)", got, tr.ExternalRefs)
	}
}

func TestTranslate_RejectsWhenStructuralCheckFailed(t *testing.T) {
	q := buildFakeGraph("/tmp/x", []string{"a"})
	sr := &StructuralResult{OK: false}
	_, err := Translate(context.Background(), q, sr, TranslateOptions{})
	if err == nil {
		t.Error("expected error when structural check failed")
	}
}

func TestTranslate_RejectsWhenNoCapabilities(t *testing.T) {
	q := buildFakeGraph("/tmp/x", []string{})
	sr := &StructuralResult{OK: true, ChangeName: "x"}
	_, err := Translate(context.Background(), q, sr, TranslateOptions{})
	if err == nil {
		t.Error("expected error when proposal declares no capabilities")
	}
}

func TestTranslate_RejectsWhenGraphReturnsNoRequirements(t *testing.T) {
	changeName := "x"
	changePath := "/tmp/x"
	caps := []string{"user-auth"}
	// Build a graph WITHOUT the requirement traversal — simulates semsource
	// indexing not yet complete.
	q := &fakeQuerier{
		byPredicate: map[string][]graph.Entity{
			spec.SpecType: {{
				ID: "graph.spec.user-auth",
				Triples: []graph.Triple{
					{Predicate: spec.SpecType, Object: "specification"},
					{Predicate: spec.SpecFilePath, Object: filepath.Join(changePath, "specs", "user-auth", "spec.md")},
				},
			}},
		},
		traversals: map[string][]graph.Entity{},
	}
	sr := sampleStructural(changeName, changePath, caps)
	_, err := Translate(context.Background(), q, sr, TranslateOptions{})
	if err == nil {
		t.Error("expected error when graph has spec but no requirement entities")
	}
}

func TestCapabilityNameFromSpecPath(t *testing.T) {
	cases := map[string]string{
		"/repo/openspec/changes/sample/specs/user-auth/spec.md":     "user-auth",
		"/repo/openspec/changes/sample/specs/session-store/spec.md": "session-store",
		"/repo/something/else.md":                                   "",
		"":                                                          "",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			got := capabilityNameFromSpecPath(in, "/repo/openspec/changes/sample/specs")
			if got != want {
				t.Errorf("capabilityNameFromSpecPath(%q) = %q, want %q", in, got, want)
			}
		})
	}
}

func TestSlugifyForID(t *testing.T) {
	cases := map[string]string{
		"User Authentication":  "user-authentication",
		"Spaces  and   stuff!": "spaces-and-stuff",
		"":                     "x",
		"   ":                  "x",
		"!!!":                  "x",
		"already-kebab":        "already-kebab",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			if got := slugifyForID(in); got != want {
				t.Errorf("slugifyForID(%q) = %q, want %q", in, got, want)
			}
		})
	}
}
