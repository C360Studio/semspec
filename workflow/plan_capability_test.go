package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semspec/vocabulary/semspec"
)

func TestValidateCapabilitySet(t *testing.T) {
	tests := []struct {
		name    string
		caps    []Capability
		wantErr string // empty = expect success; otherwise must be a substring of err.Error()
	}{
		{
			name: "valid single capability",
			caps: []Capability{
				{Name: "user-auth", Lifecycle: CapabilityNew, Description: "Authenticate users via password."},
			},
		},
		{
			name: "valid multiple capabilities with deps",
			caps: []Capability{
				{Name: "user-auth", Lifecycle: CapabilityNew, Description: "Auth users."},
				{Name: "session-store", Lifecycle: CapabilityNew, Description: "Persist sessions.", DependsOn: []string{"user-auth"}},
			},
		},
		{
			name:    "empty capability set is OK (called only on populated lists)",
			caps:    nil,
			wantErr: "",
		},
		{
			name: "missing name rejected",
			caps: []Capability{
				{Lifecycle: CapabilityNew, Description: "Missing name."},
			},
			wantErr: "missing name",
		},
		{
			name: "invalid lifecycle rejected",
			caps: []Capability{
				{Name: "weird-cap", Lifecycle: "ancient", Description: "Bad lifecycle."},
			},
			wantErr: "invalid lifecycle",
		},
		{
			name: "duplicate name rejected",
			caps: []Capability{
				{Name: "user-auth", Lifecycle: CapabilityNew, Description: "First."},
				{Name: "user-auth", Lifecycle: CapabilityModified, Description: "Second."},
			},
			wantErr: "declared more than once",
		},
		{
			name: "depends_on orphan rejected",
			caps: []Capability{
				{Name: "user-auth", Lifecycle: CapabilityNew, Description: "Auth.", DependsOn: []string{"nonexistent"}},
			},
			wantErr: "not declared",
		},
		{
			name: "depends_on cycle (direct) rejected",
			caps: []Capability{
				{Name: "a", Lifecycle: CapabilityNew, Description: "A.", DependsOn: []string{"b"}},
				{Name: "b", Lifecycle: CapabilityNew, Description: "B.", DependsOn: []string{"a"}},
			},
			wantErr: "cycle",
		},
		{
			name: "depends_on cycle (3-node) rejected",
			caps: []Capability{
				{Name: "a", Lifecycle: CapabilityNew, Description: "A.", DependsOn: []string{"b"}},
				{Name: "b", Lifecycle: CapabilityNew, Description: "B.", DependsOn: []string{"c"}},
				{Name: "c", Lifecycle: CapabilityNew, Description: "C.", DependsOn: []string{"a"}},
			},
			wantErr: "cycle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCapabilitySet(tt.caps)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected success, got error: %v", err)
				}
				return
			}
			if err == nil {
				t.Errorf("expected error containing %q, got nil", tt.wantErr)
				return
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestCapabilityEntityID_UniquenessAcrossPlans(t *testing.T) {
	// Two different plans declaring the same capability name must get
	// distinct entity IDs.
	a := CapabilityEntityID("plan-a", "mavsdk-bootstrap")
	b := CapabilityEntityID("plan-b", "mavsdk-bootstrap")
	if a == b {
		t.Errorf("expected distinct entity IDs across plans, got %q == %q", a, b)
	}
	// Same plan + same capability name must be deterministic.
	a2 := CapabilityEntityID("plan-a", "mavsdk-bootstrap")
	if a != a2 {
		t.Errorf("expected deterministic entity ID, got %q != %q", a, a2)
	}
}

// TestPlanFromTripleMap_RestoresExploration pins the graph-rehydrate
// fix: when a plan's KV bucket is wiped and the planStore reconciles
// from ENTITY_STATES, the Exploration must come back. Without this
// restoration the plan would silently regress to StatusCreated and
// re-run the analyst sub-phase, losing the prior capability identity.
func TestPlanFromTripleMap_RestoresExploration(t *testing.T) {
	source := &Exploration{
		Capabilities: []Capability{
			{Name: "mavsdk-bootstrap", Lifecycle: CapabilityNew, Description: "Boot MAVSDK server."},
			{Name: "telemetry-stream", Lifecycle: CapabilityModified, Description: "CS DataStream."},
		},
		OpenQuestions: []string{"Static or runtime coverage check?"},
	}
	blob, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	triples := map[string]string{
		semspec.PlanSlug:        "test-slug",
		semspec.PlanTitle:       "Test Plan",
		semspec.PlanExploration: string(blob),
	}

	plan := PlanFromTripleMap("plan-entity-id", triples)
	if plan == nil {
		t.Fatal("expected non-nil plan")
	}
	if plan.Exploration == nil {
		t.Fatal("Exploration was not restored from triples")
	}
	if got := len(plan.Exploration.Capabilities); got != 2 {
		t.Errorf("expected 2 capabilities, got %d", got)
	}
	if plan.Exploration.Capabilities[0].Name != "mavsdk-bootstrap" {
		t.Errorf("first capability name mismatch: %q", plan.Exploration.Capabilities[0].Name)
	}
	if plan.Exploration.Capabilities[1].Lifecycle != CapabilityModified {
		t.Errorf("second capability lifecycle mismatch: %q", plan.Exploration.Capabilities[1].Lifecycle)
	}
	if got := len(plan.Exploration.OpenQuestions); got != 1 {
		t.Errorf("expected 1 open question, got %d", got)
	}
}

func TestPlanFromTripleMap_NoExplorationTriple(t *testing.T) {
	// A plan that never went through the analyst sub-phase has no
	// PlanExploration triple. Restoration must yield nil Exploration —
	// not a zero-valued empty Exploration struct that would change
	// EffectiveStatus() behavior.
	triples := map[string]string{
		semspec.PlanSlug:  "no-analyst",
		semspec.PlanTitle: "Legacy",
	}
	plan := PlanFromTripleMap("plan-entity-id", triples)
	if plan == nil {
		t.Fatal("expected non-nil plan")
	}
	if plan.Exploration != nil {
		t.Errorf("expected nil Exploration when no triple present, got %+v", plan.Exploration)
	}
}

func TestValidateRequirementCapabilityCoverage(t *testing.T) {
	tests := []struct {
		name    string
		exp     *Exploration
		reqs    []Requirement
		wantErr string
	}{
		{
			name: "happy path: every cap has a req and every req has a valid cap",
			exp: &Exploration{
				Capabilities: []Capability{
					{Name: "user-auth", Lifecycle: CapabilityNew, Description: "Auth."},
					{Name: "session-store", Lifecycle: CapabilityNew, Description: "Sessions."},
				},
			},
			reqs: []Requirement{
				{ID: "r1", CapabilityName: "user-auth"},
				{ID: "r2", CapabilityName: "session-store"},
			},
		},
		{
			name: "nil exploration: legacy path, no validation",
			exp:  nil,
			reqs: []Requirement{{ID: "r1"}},
		},
		{
			name: "all-empty cap names: legacy mid-cascade, no validation",
			exp: &Exploration{
				Capabilities: []Capability{
					{Name: "x", Lifecycle: CapabilityNew, Description: "X."},
				},
			},
			reqs: []Requirement{{ID: "r1"}, {ID: "r2"}},
		},
		{
			name: "mixed state: some reqs with cap, others without — inconsistency rejected",
			exp: &Exploration{
				Capabilities: []Capability{
					{Name: "x", Lifecycle: CapabilityNew, Description: "X."},
				},
			},
			reqs: []Requirement{
				{ID: "r1", CapabilityName: "x"},
				{ID: "r2"},
			},
			wantErr: "inconsistent capability linkage",
		},
		{
			name: "orphan req cap: cap name doesn't resolve",
			exp: &Exploration{
				Capabilities: []Capability{
					{Name: "x", Lifecycle: CapabilityNew, Description: "X."},
				},
			},
			reqs: []Requirement{
				{ID: "r1", CapabilityName: "ghost"},
			},
			wantErr: "not declared in Plan.Exploration",
		},
		{
			name: "orphan capability: cap with no implementing req",
			exp: &Exploration{
				Capabilities: []Capability{
					{Name: "covered", Lifecycle: CapabilityNew, Description: "Covered."},
					{Name: "uncovered", Lifecycle: CapabilityNew, Description: "Uncovered."},
				},
			},
			reqs: []Requirement{
				{ID: "r1", CapabilityName: "covered"},
			},
			wantErr: "capability_orphan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRequirementCapabilityCoverage(tt.exp, tt.reqs)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected success, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Errorf("expected error containing %q, got nil", tt.wantErr)
				return
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestFindDocsOnlyCapabilities(t *testing.T) {
	exp := &Exploration{
		Capabilities: []Capability{
			{Name: "docs-cap", Lifecycle: CapabilityNew, Description: "Docs only."},
			{Name: "mixed-cap", Lifecycle: CapabilityNew, Description: "Mixed files."},
			{Name: "impl-cap", Lifecycle: CapabilityNew, Description: "Impl only."},
			{Name: "no-req-cap", Lifecycle: CapabilityNew, Description: "Orphan."},
		},
	}
	reqs := []Requirement{
		{ID: "r1", CapabilityName: "docs-cap", FilesOwned: []string{"README.md", "docs/x.md"}},
		{ID: "r2", CapabilityName: "mixed-cap", FilesOwned: []string{"x.go", "x.md"}},
		{ID: "r3", CapabilityName: "impl-cap", FilesOwned: []string{"impl.go"}},
		// no-req-cap has no req — caught by orphan check, not docs-only
	}
	got := FindDocsOnlyCapabilities(exp, reqs)
	if len(got) != 1 || got[0] != "docs-cap" {
		t.Errorf("expected [docs-cap], got %v", got)
	}
}

func TestIsDocumentationPath(t *testing.T) {
	tests := map[string]bool{
		"README.md":              true,
		"docs/coverage.md":       true,
		"src/lib.go":             false,
		"package.json":           false,
		"LICENSE":                true,
		"contributing":           true,
		"path/to/CHANGELOG":      true,
		"src/foo.adoc":           true,
		"path/with/dir/file.txt": true,
	}
	for path, want := range tests {
		t.Run(path, func(t *testing.T) {
			if got := isDocumentationPath(path); got != want {
				t.Errorf("isDocumentationPath(%q) = %v, want %v", path, got, want)
			}
		})
	}
}

func TestExploration_FindCapability(t *testing.T) {
	exp := &Exploration{
		Capabilities: []Capability{
			{Name: "user-auth", Lifecycle: CapabilityNew, Description: "Auth."},
			{Name: "session-store", Lifecycle: CapabilityModified, Description: "Sessions."},
		},
	}

	cap, idx := exp.FindCapability("user-auth")
	if cap == nil || idx != 0 {
		t.Errorf("expected user-auth at index 0, got %+v idx=%d", cap, idx)
	}
	cap, idx = exp.FindCapability("session-store")
	if cap == nil || idx != 1 {
		t.Errorf("expected session-store at index 1, got %+v idx=%d", cap, idx)
	}
	cap, idx = exp.FindCapability("nonexistent")
	if cap != nil || idx != -1 {
		t.Errorf("expected nil/-1 for unknown name, got %+v idx=%d", cap, idx)
	}

	// Nil exploration is safe.
	var nilExp *Exploration
	cap, idx = nilExp.FindCapability("user-auth")
	if cap != nil || idx != -1 {
		t.Errorf("expected nil/-1 from nil exploration, got %+v idx=%d", cap, idx)
	}
}
