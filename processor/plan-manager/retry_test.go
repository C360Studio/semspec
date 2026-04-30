package planmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// newRetryTestComponent builds a Component with a plan store but no NATS.
// Good enough to exercise the handler's request-validation branches; the
// happy path that actually publishes to NATS is covered by e2e tests.
func newRetryTestComponent(t *testing.T) *Component {
	t.Helper()
	ps, err := newPlanStore(context.Background(), nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("newPlanStore: %v", err)
	}
	return &Component{
		logger: slog.Default(),
		plans:  ps,
	}
}

func seedPlan(t *testing.T, c *Component, slug string, status workflow.Status) *workflow.Plan {
	t.Helper()
	plan := &workflow.Plan{
		ID:     workflow.PlanEntityID(slug),
		Slug:   slug,
		Title:  slug,
		Status: status,
	}
	_ = c.plans.save(context.Background(), plan)
	return plan
}

func postRetry(t *testing.T, c *Component, slug string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/plan-manager/plans/"+slug+"/retry", &buf)
	w := httptest.NewRecorder()
	c.handleRetryPlan(w, req, slug)
	return w
}

func TestHandleRetryPlan_RequirementsScopeRequiresIDs(t *testing.T) {
	c := newRetryTestComponent(t)
	seedPlan(t, c, "p", workflow.StatusImplementing)

	// Missing requirement_ids — must reject.
	w := postRetry(t, c, "p", map[string]any{"scope": "requirements"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}

	// Empty list — same rejection.
	w = postRetry(t, c, "p", map[string]any{
		"scope":           "requirements",
		"requirement_ids": []string{},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty list: status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleRetryPlan_UnknownScopeRejected(t *testing.T) {
	c := newRetryTestComponent(t)
	seedPlan(t, c, "p", workflow.StatusImplementing)

	w := postRetry(t, c, "p", map[string]any{"scope": "bogus"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestHandleRetryPlan_DefaultsToFailedScope(t *testing.T) {
	// No body means default scope="failed". The handler tolerates a missing
	// exec bucket (returns 0 reset) but still needs to publish to NATS; with
	// natsClient nil, that path panics. We therefore only validate that the
	// request-shape stage passes — a plan in a non-eligible status surfaces
	// the conflict cleanly without ever reaching the NATS call.
	c := newRetryTestComponent(t)
	seedPlan(t, c, "p", workflow.StatusDrafted) // not eligible for retry

	w := postRetry(t, c, "p", nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusConflict, w.Body.String())
	}
}

func TestHandleRetryPlan_RequirementsScopeShape(t *testing.T) {
	// A valid requirements-scope request against a non-eligible plan should
	// still pass shape validation and reach the status-eligibility check.
	// Proves requirement_ids threading doesn't break the existing eligibility
	// gate.
	c := newRetryTestComponent(t)
	seedPlan(t, c, "p", workflow.StatusDrafted)

	w := postRetry(t, c, "p", map[string]any{
		"scope":           "requirements",
		"requirement_ids": []string{"R1", "R3"},
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusConflict, w.Body.String())
	}
}
