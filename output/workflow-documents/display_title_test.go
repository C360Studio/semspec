package workflowdocuments

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestDisplayTitle_NormalLengthTitleUsedAsIs(t *testing.T) {
	plan := &workflow.Plan{Slug: "my-plan", Title: "Add /health endpoint"}
	got := displayTitle(plan)
	if got != "Add /health endpoint" {
		t.Errorf("got %q, want unchanged title", got)
	}
}

func TestDisplayTitle_EmptyTitleFallsBackToSlug(t *testing.T) {
	plan := &workflow.Plan{Slug: "my-plan"}
	got := displayTitle(plan)
	if got != "my-plan" {
		t.Errorf("got %q, want slug fallback", got)
	}
}

// TestDisplayTitle_OverlongTitleFallsBackToSlug pins the smoke 6 fix.
// The plan-manager HTTP API falls back title=description when only
// description is provided in the create request, so a verbose prompt
// becomes a multi-paragraph H1 on every rendered doc. Render time we
// substitute the slug to keep the heading scannable.
func TestDisplayTitle_OverlongTitleFallsBackToSlug(t *testing.T) {
	verbose := strings.Repeat("a", maxDisplayTitleChars+1)
	plan := &workflow.Plan{Slug: "my-plan", Title: verbose}
	got := displayTitle(plan)
	if got != "my-plan" {
		t.Errorf("got %q, want slug fallback when title > %d chars", got, maxDisplayTitleChars)
	}
}

func TestDisplayTitle_BoundaryExactMaxStillUsesTitle(t *testing.T) {
	atMax := strings.Repeat("x", maxDisplayTitleChars)
	plan := &workflow.Plan{Slug: "p", Title: atMax}
	got := displayTitle(plan)
	if got != atMax {
		t.Errorf("got %q, exactly maxDisplayTitleChars should still use the title", got)
	}
}

func TestDisplayTitle_NilPlanReturnsEmpty(t *testing.T) {
	if got := displayTitle(nil); got != "" {
		t.Errorf("got %q, want empty for nil plan", got)
	}
}
