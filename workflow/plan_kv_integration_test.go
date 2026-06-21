//go:build integration

package workflow

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semspec/test/integration/graphmock"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semstreams/natsclient"
)

// newTestTripleWriter creates a TripleWriter backed by a real NATS for integration
// tests. It also starts the in-memory graph-ingest mock so that TripleWriter
// calls to graph.mutation.triple.add and the read-back queries all succeed.
func newTestTripleWriter(t *testing.T) *graphutil.TripleWriter {
	t.Helper()
	tc := natsclient.NewTestClient(t,
		natsclient.WithKVBuckets("ENTITY_STATES"),
	)
	graphmock.Start(t, tc.Client)
	return &graphutil.TripleWriter{
		NATSClient:    tc.Client,
		Logger:        slog.Default(),
		ComponentName: "test",
	}
}

func TestKV_CreatePlan_DuplicateRejected(t *testing.T) {
	tw := newTestTripleWriter(t)
	ctx := context.Background()

	if _, err := CreatePlan(ctx, tw, "dupe-test", "First"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	_, err := CreatePlan(ctx, tw, "dupe-test", "Second")
	if err == nil {
		t.Fatal("CreatePlan with duplicate slug should fail")
	}
}

func TestKV_InvalidSlug(t *testing.T) {
	tw := newTestTripleWriter(t)
	ctx := context.Background()

	slugs := []string{"", "../escape", "has spaces", "UPPERCASE", "a/b"}
	for _, slug := range slugs {
		if _, err := CreatePlan(ctx, tw, slug, "Bad"); err == nil {
			t.Errorf("CreatePlan(%q) should fail", slug)
		}
	}
}

func TestKV_RequirementDAGValidation(t *testing.T) {
	tw := newTestTripleWriter(t)
	ctx := context.Background()

	if _, err := CreatePlan(ctx, tw, "dag-test", "DAG Test"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	now := time.Now().Truncate(time.Second)

	reqs := []Requirement{
		{ID: "req-self", PlanID: PlanEntityID("dag-test"), Title: "Self", Status: RequirementStatusActive, DependsOn: []string{"req-self"}, CreatedAt: now, UpdatedAt: now},
	}
	if err := SaveRequirements(ctx, tw, reqs, "dag-test"); err == nil {
		t.Error("SaveRequirements with self-reference should fail")
	}

	cycleReqs := []Requirement{
		{ID: "req-a", PlanID: PlanEntityID("dag-test"), Title: "A", Status: RequirementStatusActive, DependsOn: []string{"req-b"}, CreatedAt: now, UpdatedAt: now},
		{ID: "req-b", PlanID: PlanEntityID("dag-test"), Title: "B", Status: RequirementStatusActive, DependsOn: []string{"req-a"}, CreatedAt: now, UpdatedAt: now},
	}
	if err := SaveRequirements(ctx, tw, cycleReqs, "dag-test"); err == nil {
		t.Error("SaveRequirements with cycle should fail")
	}
}
