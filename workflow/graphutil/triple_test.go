package graphutil

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

// ---------------------------------------------------------------------------
// upsertOutcome.String — N1
// ---------------------------------------------------------------------------

func TestUpsertOutcomeString(t *testing.T) {
	cases := []struct {
		o    upsertOutcome
		want string
	}{
		{upsertDone, "done"},
		{upsertNeedCreate, "need_create"},
		{upsertRetryUpdate, "retry_update"},
		{upsertOutcome(99), "upsertOutcome(99)"},
	}
	for _, tc := range cases {
		if got := tc.o.String(); got != tc.want {
			t.Errorf("%d.String() = %q, want %q", int(tc.o), got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// decideUpdateOutcome — pure response-parsing logic, no NATS required
// ---------------------------------------------------------------------------

func TestDecideUpdateOutcome_Success(t *testing.T) {
	resp := graph.UpdateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{Success: true},
	}
	got, err := decideUpdateOutcome(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != upsertDone {
		t.Errorf("outcome = %s, want done", got)
	}
}

func TestDecideUpdateOutcome_Degraded_TreatedAsSuccess(t *testing.T) {
	// Degraded = write committed, read-back failed.
	// Per MutationResponse contract: DO NOT retry; treat as success.
	resp := graph.UpdateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{
			Success:  false,
			Degraded: true,
			Error:    "read-back context cancelled",
		},
	}
	got, err := decideUpdateOutcome(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != upsertDone {
		t.Errorf("Degraded response should yield done, got %s", got)
	}
}

func TestDecideUpdateOutcome_EntityNotFound_TriggersCreate(t *testing.T) {
	resp := graph.UpdateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{
			Success:   false,
			ErrorCode: graph.ErrorCodeEntityNotFound,
			Error:     "entity not found: some.entity.id",
		},
	}
	got, err := decideUpdateOutcome(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != upsertNeedCreate {
		t.Errorf("entity_not_found should yield need_create, got %s", got)
	}
}

func TestDecideUpdateOutcome_OtherError_ReturnsError(t *testing.T) {
	resp := graph.UpdateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{
			Success:   false,
			ErrorCode: graph.ErrorCodeInvalidRequest,
			Error:     "entity cannot be nil",
		},
	}
	_, err := decideUpdateOutcome(resp)
	if err == nil {
		t.Fatal("expected error for invalid_request, got nil")
	}
}

// ---------------------------------------------------------------------------
// decideCreateOutcome — pure response-parsing logic, no NATS required
// ---------------------------------------------------------------------------

func TestDecideCreateOutcome_Success(t *testing.T) {
	resp := graph.CreateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{Success: true},
	}
	got, err := decideCreateOutcome(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != upsertDone {
		t.Errorf("outcome = %s, want done", got)
	}
}

func TestDecideCreateOutcome_Degraded_TreatedAsSuccess(t *testing.T) {
	resp := graph.CreateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{
			Success:  false,
			Degraded: true,
			Error:    "read-back timeout",
		},
	}
	got, err := decideCreateOutcome(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != upsertDone {
		t.Errorf("Degraded create should yield done, got %s", got)
	}
}

func TestDecideCreateOutcome_EntityExists_TriggersUpdateRetry(t *testing.T) {
	// Concurrent writer created the entity between our update and create attempts.
	resp := graph.CreateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{
			Success:   false,
			ErrorCode: graph.ErrorCodeEntityExists,
			Error:     "entity already exists",
		},
	}
	got, err := decideCreateOutcome(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != upsertRetryUpdate {
		t.Errorf("entity_already_exists should yield retry_update, got %s", got)
	}
}

func TestDecideCreateOutcome_OtherError_ReturnsError(t *testing.T) {
	resp := graph.CreateEntityWithTriplesResponse{
		MutationResponse: graph.MutationResponse{
			Success:   false,
			ErrorCode: graph.ErrorCodeInternal,
			Error:     "bucket write failed",
		},
	}
	_, err := decideCreateOutcome(resp)
	if err == nil {
		t.Fatal("expected error for internal code, got nil")
	}
}

// ---------------------------------------------------------------------------
// upsertEntityVia routing — full create/retry-update wiring, no NATS (M1)
//
// Uses the upsertSender seam to inject stub update/create functions so the
// routing switch in upsertEntityVia is covered without a live NATS server.
// ---------------------------------------------------------------------------

// stubSender builds a upsertSender from fixed outcome sequences.
// Each call to update or create consumes the next entry; an exhausted sequence
// returns an error so test misconfiguration fails loudly.
func stubSender(updateOutcomes []upsertOutcome, createOutcomes []upsertOutcome) upsertSender {
	ui, ci := 0, 0
	return upsertSender{
		update: func(_ context.Context, _ graph.UpdateEntityWithTriplesRequest) (upsertOutcome, error) {
			if ui >= len(updateOutcomes) {
				return 0, fmt.Errorf("stub: unexpected update call #%d", ui+1)
			}
			o := updateOutcomes[ui]
			ui++
			return o, nil
		},
		create: func(_ context.Context, _ graph.CreateEntityWithTriplesRequest) (upsertOutcome, error) {
			if ci >= len(createOutcomes) {
				return 0, fmt.Errorf("stub: unexpected create call #%d", ci+1)
			}
			o := createOutcomes[ci]
			ci++
			return o, nil
		},
	}
}

var (
	testEntityType = message.Type{Domain: "workflow", Category: "question", Version: "v1"}
	testEntityID   = "test.entity.id"
	testTriples    = []message.Triple{
		{Subject: testEntityID, Predicate: "workflow.status", Object: "pending"},
	}
)

// (a) update returns Success → done immediately, create never called.
func TestUpsertEntityVia_UpdateSuccess_Done(t *testing.T) {
	s := stubSender([]upsertOutcome{upsertDone}, nil)
	err := upsertEntityVia(t.Context(), s, testEntityType, testEntityID, testTriples)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

// (b) update returns entity_not_found → create called → create returns Success.
func TestUpsertEntityVia_NotFound_Create_Done(t *testing.T) {
	s := stubSender(
		[]upsertOutcome{upsertNeedCreate}, // update: entity absent
		[]upsertOutcome{upsertDone},       // create: success
	)
	err := upsertEntityVia(t.Context(), s, testEntityType, testEntityID, testTriples)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

// (c) update returns entity_not_found → create returns entity_already_exists
//
//	→ retry update returns Success.
func TestUpsertEntityVia_NotFound_CreateExists_RetryUpdate_Done(t *testing.T) {
	s := stubSender(
		[]upsertOutcome{upsertNeedCreate, upsertDone}, // update: not-found, then success on retry
		[]upsertOutcome{upsertRetryUpdate},            // create: concurrent-writer collision
	)
	err := upsertEntityVia(t.Context(), s, testEntityType, testEntityID, testTriples)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

// (d) update not_found → create exists → retry update returns not_done → error.
func TestUpsertEntityVia_RetryUpdate_StillNotDone_Error(t *testing.T) {
	// Retry update returns need_create again — this is unexpected and must surface as error.
	s := stubSender(
		[]upsertOutcome{upsertNeedCreate, upsertNeedCreate}, // update: not-found twice
		[]upsertOutcome{upsertRetryUpdate},                  // create: concurrent collision
	)
	err := upsertEntityVia(t.Context(), s, testEntityType, testEntityID, testTriples)
	if err == nil {
		t.Fatal("expected error when retry update returns non-done, got nil")
	}
}

// update transport error propagates immediately.
func TestUpsertEntityVia_UpdateError_Propagates(t *testing.T) {
	sentinel := errors.New("nats timeout")
	s := upsertSender{
		update: func(_ context.Context, _ graph.UpdateEntityWithTriplesRequest) (upsertOutcome, error) {
			return 0, sentinel
		},
		create: func(_ context.Context, _ graph.CreateEntityWithTriplesRequest) (upsertOutcome, error) {
			t.Error("create should not be called when update errors")
			return 0, nil
		},
	}
	err := upsertEntityVia(t.Context(), s, testEntityType, testEntityID, testTriples)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got: %v", err)
	}
}

// create transport error propagates, update not retried.
func TestUpsertEntityVia_CreateError_Propagates(t *testing.T) {
	sentinel := errors.New("nats timeout on create")
	calls := 0
	s := upsertSender{
		update: func(_ context.Context, _ graph.UpdateEntityWithTriplesRequest) (upsertOutcome, error) {
			calls++
			return upsertNeedCreate, nil // first call: entity absent
		},
		create: func(_ context.Context, _ graph.CreateEntityWithTriplesRequest) (upsertOutcome, error) {
			return 0, sentinel
		},
	}
	err := upsertEntityVia(t.Context(), s, testEntityType, testEntityID, testTriples)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got: %v", err)
	}
	if calls != 1 {
		t.Errorf("update called %d times, want 1", calls)
	}
}

// ---------------------------------------------------------------------------
// distinctPredicates — replace-not-append request-level contract
// ---------------------------------------------------------------------------

func TestDistinctPredicates_UniquePredicatesOfInputTriples(t *testing.T) {
	// When an entity is republished with a changed scalar (e.g. status), the
	// distinct predicate set must include that predicate so the old value is
	// dropped by the server-side remove step before the new value is appended.
	triples := []message.Triple{
		{Subject: "ent1", Predicate: "workflow.status", Object: "pending"},
		{Subject: "ent1", Predicate: "workflow.title", Object: "First title"},
		{Subject: "ent1", Predicate: "workflow.status", Object: "answered"}, // same predicate, new value
	}

	got := distinctPredicates(triples)
	sort.Strings(got) // order is non-deterministic from map iteration

	want := []string{"workflow.status", "workflow.title"}
	if len(got) != len(want) {
		t.Fatalf("distinctPredicates length = %d, want %d; got %v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestDistinctPredicates_EmptyInput(t *testing.T) {
	got := distinctPredicates(nil)
	if len(got) != 0 {
		t.Errorf("distinctPredicates(nil) = %v, want empty", got)
	}
}

func TestDistinctPredicates_AllUnique(t *testing.T) {
	triples := []message.Triple{
		{Subject: "e", Predicate: "a", Object: "1"},
		{Subject: "e", Predicate: "b", Object: "2"},
		{Subject: "e", Predicate: "c", Object: "3"},
	}
	got := distinctPredicates(triples)
	if len(got) != 3 {
		t.Errorf("expected 3 distinct predicates, got %d: %v", len(got), got)
	}
}

// ---------------------------------------------------------------------------
// UpsertEntity — nil NATSClient guard (integration boundary)
// ---------------------------------------------------------------------------

func TestUpsertEntity_NilNATSClient_Noop(t *testing.T) {
	tw := &TripleWriter{
		NATSClient:    nil, // no NATS
		ComponentName: "test",
	}
	triples := []message.Triple{
		{Subject: "ent.id", Predicate: "workflow.status", Object: "pending"},
	}
	entityType := message.Type{Domain: "workflow", Category: "question", Version: "v1"}

	err := tw.UpsertEntity(t.Context(), entityType, "ent.id", triples)
	if err != nil {
		t.Errorf("UpsertEntity with nil NATSClient should be no-op, got error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// distinctPredicates-based regression guard: changed predicate appears in
// the remove set (validates the remove-before-append invariant at the
// request-construction level — the routing tests above cover the send path).
// ---------------------------------------------------------------------------

func TestDistinctPredicates_ChangedPredicateInRemoveSet(t *testing.T) {
	// Simulates the second publish of a question whose status changed
	// from pending → answered. "workflow.status" must be in the remove set.
	triples := []message.Triple{
		{Subject: "q.entity.id", Predicate: "workflow.status", Object: "answered"},
		{Subject: "q.entity.id", Predicate: "workflow.title", Object: "What is auth scope?"},
	}

	removePredicates := distinctPredicates(triples)

	found := false
	for _, p := range removePredicates {
		if p == "workflow.status" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("workflow.status must be in the remove set to prevent stale value accumulation; got %v", removePredicates)
	}
}
