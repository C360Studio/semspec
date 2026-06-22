package architecturegenerator

import (
	"context"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestValidateGeneratedArchitecture_ScopedIncludeUnowned pins ADR-051's
// architecture-phase ownership gate: a concrete scope.include deliverable owned
// by no component is rejected at arch-gen (so the architect revises in-loop
// before any stories/scenarios are generated against it), an owned include is
// not flagged, and a do_not_touch include is exempt.
func TestValidateGeneratedArchitecture_ScopedIncludeUnowned(t *testing.T) {
	c := &Component{}
	ctx := context.Background()

	orphanArch := &workflow.ArchitectureDocument{
		ComponentBoundaries: []workflow.ComponentDef{
			{Name: "core", ImplementationFiles: []string{"src/main/java/Foo.java"}},
		},
	}
	includePlan := &workflow.Plan{Scope: workflow.Scope{Include: []string{"build.gradle"}}}

	rule, msg := c.validateGeneratedArchitecture(ctx, orphanArch, includePlan)
	if !strings.Contains(rule, "scoped_include_unowned") {
		t.Fatalf("orphaned scope.include must be rejected; got rule=%q msg=%q", rule, msg)
	}
	if !strings.Contains(msg, "build.gradle") {
		t.Errorf("rejection message should name the orphaned file; got %q", msg)
	}

	// Architect now owns build.gradle on a component → not flagged.
	ownedArch := &workflow.ArchitectureDocument{
		ComponentBoundaries: []workflow.ComponentDef{
			{Name: "core", ImplementationFiles: []string{"build.gradle", "src/main/java/Foo.java"}},
		},
	}
	if r, _ := c.validateGeneratedArchitecture(ctx, ownedArch, includePlan); strings.Contains(r, "scoped_include_unowned") {
		t.Errorf("owned scope.include must not be flagged; got rule=%q", r)
	}

	// do_not_touch include is a read-only reference → exempt.
	roPlan := &workflow.Plan{Scope: workflow.Scope{
		Include:    []string{"build.gradle"},
		DoNotTouch: []string{"build.gradle"},
	}}
	if r, _ := c.validateGeneratedArchitecture(ctx, orphanArch, roPlan); strings.Contains(r, "scoped_include_unowned") {
		t.Errorf("do_not_touch include must be exempt; got rule=%q", r)
	}
}
