package workflow

import (
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow/graphutil"
)

// ---------------------------------------------------------------------------
// Slice #4 (issue #154): writePlanTriples must create the plan entity via the
// metadata-bearing UpsertEntity path (update_with_triples + create_with_triples
// fallback), not through a sequence of bare triple.add writes.
//
// The seam is buildPlanTriples — a pure helper that constructs the
// []message.Triple slice writePlanTriples hands to UpsertEntity. Exercising it
// directly lets the test assert the full predicate set in one place without
// needing a live NATS connection.
// ---------------------------------------------------------------------------

// nilPlanTripleWriter returns a TripleWriter with nil NATSClient.
// UpsertEntity returns nil (no-op), so writePlanTriples/CreatePlan
// can be exercised for the triple-shape path without NATS.
func nilPlanTripleWriter() *graphutil.TripleWriter {
	return &graphutil.TripleWriter{
		Logger:        slog.Default(),
		ComponentName: "test",
	}
}

// indexPlanTriplesByPred builds a predicate → []string map from a triple slice.
// All triple Objects must be strings; non-string Objects cause test failure.
func indexPlanTriplesByPred(t *testing.T, eid string, triples []message.Triple) map[string][]string {
	t.Helper()
	byPred := make(map[string][]string)
	for _, tr := range triples {
		if tr.Subject != eid {
			t.Errorf("triple subject %q != entity ID %q", tr.Subject, eid)
		}
		val, ok := tr.Object.(string)
		if !ok {
			t.Errorf("predicate %q: Object is %T (%v), want string", tr.Predicate, tr.Object, tr.Object)
			continue
		}
		byPred[tr.Predicate] = append(byPred[tr.Predicate], val)
	}
	return byPred
}

// TestBuildPlanTriples_RequiredScalars verifies that buildPlanTriples always
// emits the unconditional core scalar predicates, regardless of optional-field
// state. This test fails until buildPlanTriples is extracted.
func TestBuildPlanTriples_RequiredScalars(t *testing.T) {
	slug := "test-plan"
	eid := PlanEntityID(slug)
	plan := &Plan{
		ID:        eid,
		Slug:      slug,
		Title:     "My Test Plan",
		Approved:  false,
		CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	}

	byPred := indexPlanTriplesByPred(t, eid, buildPlanTriples(eid, plan))

	require := func(pred, want string) {
		t.Helper()
		vals := byPred[pred]
		if len(vals) == 0 {
			t.Errorf("required predicate %q absent from triples", pred)
			return
		}
		if vals[0] != want {
			t.Errorf("predicate %q = %q, want %q", pred, vals[0], want)
		}
	}

	require(semspec.PlanSlug, "test-plan")
	require(semspec.PlanTitle, "My Test Plan")
	require(semspec.DCTitle, "My Test Plan")
	require(semspec.PredicatePlanStatus, string(plan.EffectiveStatus()))
	require(semspec.PlanCreatedAt, plan.CreatedAt.Format(time.RFC3339))
	require(semspec.PlanApproved, "false")
}

// TestBuildPlanTriples_ConditionalScalars verifies that optional scalar
// predicates are present when the corresponding Plan field is non-zero and
// absent when it is zero/nil.
func TestBuildPlanTriples_ConditionalScalars(t *testing.T) {
	approvedAt := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	reviewedAt := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	lastErrorAt := time.Date(2026, 6, 4, 11, 0, 0, 0, time.UTC)

	slug := "full-plan"
	eid := PlanEntityID(slug)
	plan := &Plan{
		ID:                      eid,
		Slug:                    slug,
		Title:                   "Full Plan",
		Approved:                true,
		ApprovedAt:              &approvedAt,
		CreatedAt:               time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		ProjectID:               ProjectEntityID("my-project"),
		Goal:                    "Build the thing",
		Context:                 "Because we need it",
		ReviewVerdict:           "approved",
		ReviewSummary:           "Looks good",
		ReviewedAt:              &reviewedAt,
		ReviewFormattedFindings: "All findings addressed.",
		ReviewIteration:         3,
		LastError:               "some transient error",
		LastErrorAt:             &lastErrorAt,
	}

	byPred := indexPlanTriplesByPred(t, eid, buildPlanTriples(eid, plan))

	require := func(pred, want string) {
		t.Helper()
		vals := byPred[pred]
		if len(vals) == 0 {
			t.Errorf("predicate %q absent, want %q", pred, want)
			return
		}
		if vals[0] != want {
			t.Errorf("predicate %q = %q, want %q", pred, vals[0], want)
		}
	}

	require(semspec.PlanProject, ProjectEntityID("my-project"))
	require(semspec.PlanGoal, "Build the thing")
	require(semspec.PlanContext, "Because we need it")
	require(semspec.PlanApproved, "true")
	require(semspec.PlanApprovedAt, approvedAt.Format(time.RFC3339))
	require(semspec.PlanReviewVerdict, "approved")
	require(semspec.PlanReviewSummary, "Looks good")
	require(semspec.PlanReviewedAt, reviewedAt.Format(time.RFC3339))
	require(semspec.PlanReviewFormattedFindings, "All findings addressed.")
	require(semspec.PlanReviewIteration, "3")
	require(semspec.PlanLastError, "some transient error")
	require(semspec.PlanLastErrorAt, lastErrorAt.Format(time.RFC3339))
}

// TestBuildPlanTriples_ConditionalScalarsAbsent verifies that optional scalar
// predicates are NOT emitted when their corresponding Plan fields are zero/nil.
func TestBuildPlanTriples_ConditionalScalarsAbsent(t *testing.T) {
	slug := "minimal-plan"
	eid := PlanEntityID(slug)
	plan := &Plan{
		ID:        eid,
		Slug:      slug,
		Title:     "Minimal",
		CreatedAt: time.Now(),
	}

	byPred := indexPlanTriplesByPred(t, eid, buildPlanTriples(eid, plan))

	absent := []string{
		semspec.PlanProject,
		semspec.PlanGoal,
		semspec.PlanContext,
		semspec.PlanApprovedAt,
		semspec.PlanReviewVerdict,
		semspec.PlanReviewSummary,
		semspec.PlanReviewedAt,
		semspec.PlanReviewFormattedFindings,
		semspec.PlanReviewIteration,
		semspec.PlanLastError,
		semspec.PlanLastErrorAt,
	}
	for _, pred := range absent {
		if len(byPred[pred]) > 0 {
			t.Errorf("predicate %q should be absent when Plan field is zero/nil, got %v", pred, byPred[pred])
		}
	}
}

// TestBuildPlanTriples_ListPredicates verifies that list predicates
// (Scope.Include/Exclude/DoNotTouch/Create and ExecutionTraceIDs) emit one
// triple per element — never as a JSON-encoded blob — mirroring the
// ReplaceTripleList behavior that UpsertEntity replaces.
func TestBuildPlanTriples_ListPredicates(t *testing.T) {
	slug := "list-plan"
	eid := PlanEntityID(slug)
	plan := &Plan{
		ID:    eid,
		Slug:  slug,
		Title: "List Plan",
		Scope: Scope{
			Include:    []string{"api/", "lib/"},
			Exclude:    []string{"vendor/"},
			DoNotTouch: []string{"config.yaml", "secrets.yaml"},
			Create:     []string{"docs/"},
		},
		ExecutionTraceIDs: []string{"trace-aaa", "trace-bbb", "trace-ccc"},
		CreatedAt:         time.Now(),
	}

	byPred := indexPlanTriplesByPred(t, eid, buildPlanTriples(eid, plan))

	checkList := func(pred string, want []string) {
		t.Helper()
		got := byPred[pred]
		if len(got) != len(want) {
			t.Errorf("predicate %q: got %d triples, want %d (%v)", pred, len(got), len(want), want)
			return
		}
		for i, w := range want {
			if got[i] != w {
				t.Errorf("predicate %q[%d] = %q, want %q", pred, i, got[i], w)
			}
		}
	}

	checkList(semspec.PlanScopeInclude, []string{"api/", "lib/"})
	checkList(semspec.PlanScopeExclude, []string{"vendor/"})
	checkList(semspec.PlanScopeProtected, []string{"config.yaml", "secrets.yaml"})
	checkList(semspec.PlanScopeCreate, []string{"docs/"})
	checkList(semspec.PlanExecutionTraceID, []string{"trace-aaa", "trace-bbb", "trace-ccc"})
}

// TestBuildPlanTriples_EmptyListsProduceNoTriples verifies that nil/empty list
// fields produce no triples for their predicates, so the entity stays lean.
func TestBuildPlanTriples_EmptyListsProduceNoTriples(t *testing.T) {
	slug := "empty-lists"
	eid := PlanEntityID(slug)
	plan := &Plan{
		ID:        eid,
		Slug:      slug,
		Title:     "Empty Lists",
		CreatedAt: time.Now(),
		// Scope fields nil/empty, ExecutionTraceIDs nil.
	}

	byPred := indexPlanTriplesByPred(t, eid, buildPlanTriples(eid, plan))

	for _, pred := range []string{
		semspec.PlanScopeInclude,
		semspec.PlanScopeExclude,
		semspec.PlanScopeProtected,
		semspec.PlanScopeCreate,
		semspec.PlanExecutionTraceID,
	} {
		if len(byPred[pred]) > 0 {
			t.Errorf("predicate %q should be absent for empty list, got %v", pred, byPred[pred])
		}
	}
}

// TestBuildPlanTriples_StatusViaEffectiveStatus verifies that the status
// triple reflects plan.EffectiveStatus() rather than plan.Status directly,
// preserving the pre-refactor behaviour where the explicit UpdateTriple call
// passed string(plan.EffectiveStatus()).
func TestBuildPlanTriples_StatusViaEffectiveStatus(t *testing.T) {
	slug := "approved-plan"
	eid := PlanEntityID(slug)
	plan := &Plan{
		ID:        eid,
		Slug:      slug,
		Title:     "Approved Plan",
		Approved:  true, // Status is zero → EffectiveStatus() returns StatusApproved
		CreatedAt: time.Now(),
	}

	byPred := indexPlanTriplesByPred(t, eid, buildPlanTriples(eid, plan))

	vals := byPred[semspec.PredicatePlanStatus]
	if len(vals) == 0 {
		t.Fatal("PredicatePlanStatus absent from triples")
	}
	want := string(plan.EffectiveStatus())
	if vals[0] != want {
		t.Errorf("PredicatePlanStatus = %q, want EffectiveStatus() = %q", vals[0], want)
	}
}

// TestWritePlanTriples_NilTWIsNoop verifies the existing guard: when
// TripleWriter is nil (no NATS), writePlanTriples returns nil without panicking.
func TestWritePlanTriples_NilTWIsNoop(t *testing.T) {
	plan := &Plan{Slug: "noop", Title: "Noop"}
	plan.ID = PlanEntityID(plan.Slug)
	if err := writePlanTriples(t.Context(), nil, plan); err != nil {
		t.Fatalf("writePlanTriples(nil tw) should return nil, got %v", err)
	}
}

// TestWritePlanTriples_NilNATSClientIsNoop verifies that a TripleWriter with
// nil NATSClient is a no-op (UpsertEntity returns nil when NATSClient==nil).
func TestWritePlanTriples_NilNATSClientIsNoop(t *testing.T) {
	tw := nilPlanTripleWriter()
	plan := &Plan{
		Slug:      "nats-noop",
		Title:     "NATS Noop",
		CreatedAt: time.Now(),
	}
	plan.ID = PlanEntityID(plan.Slug)
	if err := writePlanTriples(t.Context(), tw, plan); err != nil {
		t.Fatalf("writePlanTriples with nil NATSClient should return nil, got %v", err)
	}
}

// TestBuildPlanTriples_RoundTripScalars exercises the scalar fields end-to-end
// through PlanFromTripleMap: build triples → collapse to predicate map →
// reconstruct plan and verify field values. This pins that the predicate names
// used in buildPlanTriples match the predicate names PlanFromTripleMap reads.
func TestBuildPlanTriples_RoundTripScalars(t *testing.T) {
	approvedAt := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	reviewedAt := time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC)

	slug := "roundtrip"
	eid := PlanEntityID(slug)
	orig := &Plan{
		ID:              eid,
		Slug:            slug,
		Title:           "Roundtrip Plan",
		Approved:        true,
		ApprovedAt:      &approvedAt,
		CreatedAt:       time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		ProjectID:       ProjectEntityID("proj"),
		Goal:            "Do the thing",
		Context:         "Because reasons",
		ReviewVerdict:   "approved",
		ReviewSummary:   "All good",
		ReviewedAt:      &reviewedAt,
		ReviewIteration: 2,
		LastError:       "transient",
	}

	triples := buildPlanTriples(eid, orig)

	// Collapse []message.Triple → map[string]string (take first value per
	// predicate) to match what PlanFromTripleMap expects.
	collapsed := make(map[string]string)
	for _, tr := range triples {
		val := fmt.Sprintf("%v", tr.Object)
		if _, exists := collapsed[tr.Predicate]; !exists {
			collapsed[tr.Predicate] = val
		}
	}

	got := PlanFromTripleMap(eid, collapsed)

	if got.Slug != orig.Slug {
		t.Errorf("Slug = %q, want %q", got.Slug, orig.Slug)
	}
	if got.Title != orig.Title {
		t.Errorf("Title = %q, want %q", got.Title, orig.Title)
	}
	if got.ProjectID != orig.ProjectID {
		t.Errorf("ProjectID = %q, want %q", got.ProjectID, orig.ProjectID)
	}
	if got.Goal != orig.Goal {
		t.Errorf("Goal = %q, want %q", got.Goal, orig.Goal)
	}
	if got.Context != orig.Context {
		t.Errorf("Context = %q, want %q", got.Context, orig.Context)
	}
	if !got.Approved {
		t.Error("Approved should be true")
	}
	if got.ApprovedAt == nil || !got.ApprovedAt.Equal(approvedAt) {
		t.Errorf("ApprovedAt = %v, want %v", got.ApprovedAt, approvedAt)
	}
	if got.ReviewVerdict != orig.ReviewVerdict {
		t.Errorf("ReviewVerdict = %q, want %q", got.ReviewVerdict, orig.ReviewVerdict)
	}
	if got.ReviewSummary != orig.ReviewSummary {
		t.Errorf("ReviewSummary = %q, want %q", got.ReviewSummary, orig.ReviewSummary)
	}
	if got.ReviewedAt == nil || !got.ReviewedAt.Equal(reviewedAt) {
		t.Errorf("ReviewedAt = %v, want %v", got.ReviewedAt, reviewedAt)
	}
	if got.ReviewIteration != 2 {
		t.Errorf("ReviewIteration = %d, want 2", got.ReviewIteration)
	}
	if got.LastError != orig.LastError {
		t.Errorf("LastError = %q, want %q", got.LastError, orig.LastError)
	}
}
