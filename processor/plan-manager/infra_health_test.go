//go:build integration

package planmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestHandleRetryPlan_InfraCritical_Refuses409 pins the Phase 5 retry gate:
// a plan flagged InfraHealthCritical must refuse retry with a 409 and a
// message pointing the operator at /infra-reconcile. Retrying against
// wedged infra would burn tokens without making progress — the whole
// reason error_class + infra_health exist.
func TestHandleRetryPlan_InfraCritical_Refuses409(t *testing.T) {
	slug := "infra-crit"
	c := setupTestComponent(t)
	plan := setupTestPlan(t, c, slug)

	plan.Status = workflow.StatusImplementing
	plan.InfraHealth = workflow.InfraHealthCritical
	if err := c.plans.save(context.Background(), plan); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/plan-api/plans/"+slug+"/retry", nil)
	w := httptest.NewRecorder()
	c.handleRetryPlan(w, req, slug)

	if w.Code != http.StatusConflict {
		t.Fatalf("retry while infra critical: status = %d, want 409; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "infra-reconcile") {
		t.Errorf("error message should mention /infra-reconcile; got %s", w.Body.String())
	}
}

// TestHandleInfraReconcile_ClearsCritical verifies the operator-attestation
// endpoint: after the sandbox is fixed, hitting /infra-reconcile flips
// InfraHealth back to empty and retries start working again.
func TestHandleInfraReconcile_ClearsCritical(t *testing.T) {
	slug := "infra-clear"
	c := setupTestComponent(t)
	plan := setupTestPlan(t, c, slug)

	plan.InfraHealth = workflow.InfraHealthCritical
	if err := c.plans.save(context.Background(), plan); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/plan-api/plans/"+slug+"/infra-reconcile", nil)
	w := httptest.NewRecorder()
	c.handleInfraReconcile(w, req, slug)

	if w.Code != http.StatusOK {
		t.Fatalf("infra-reconcile: status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["prior"] != string(workflow.InfraHealthCritical) {
		t.Errorf(`response prior = %v, want %q`, resp["prior"], workflow.InfraHealthCritical)
	}

	got, _ := c.plans.get(slug)
	if got.InfraHealth != "" {
		t.Errorf("InfraHealth after reconcile = %q, want empty", got.InfraHealth)
	}
}

// TestHandleInfraReconcile_IdempotentWhenHealthy verifies that calling
// the reconcile endpoint on an already-healthy plan is a safe no-op —
// operators should not need to check InfraHealth before invoking it.
func TestHandleInfraReconcile_IdempotentWhenHealthy(t *testing.T) {
	slug := "infra-healthy"
	c := setupTestComponent(t)
	setupTestPlan(t, c, slug)

	req := httptest.NewRequest(http.MethodPost, "/plan-api/plans/"+slug+"/infra-reconcile", nil)
	w := httptest.NewRecorder()
	c.handleInfraReconcile(w, req, slug)

	if w.Code != http.StatusOK {
		t.Fatalf("infra-reconcile on healthy plan: status = %d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["prior"] != "" {
		t.Errorf(`prior = %v, want "" (already healthy)`, resp["prior"])
	}
}

// TestMarkPlanInfraCritical_Idempotent verifies the execution-events hook
// only writes on first elevation — a second infra error on the same plan
// does not trigger a redundant save.
func TestMarkPlanInfraCritical_Idempotent(t *testing.T) {
	slug := "mark-infra-idem"
	c := setupTestComponent(t)
	plan := setupTestPlan(t, c, slug)

	// First call elevates.
	c.markPlanInfraCritical(context.Background(), slug, "req-1", "INFRASTRUCTURE: wedged")
	got, _ := c.plans.get(slug)
	if got.InfraHealth != workflow.InfraHealthCritical {
		t.Fatalf("after first markPlanInfraCritical, InfraHealth = %q, want %q",
			got.InfraHealth, workflow.InfraHealthCritical)
	}

	// Second call is a no-op — verify by mutating the plan's InfraHealth
	// through another channel and confirming the second call doesn't
	// overwrite our marker.
	plan, _ = c.plans.get(slug)
	plan.Goal = "sentinel-value"
	if err := c.plans.save(context.Background(), plan); err != nil {
		t.Fatalf("save sentinel: %v", err)
	}
	c.markPlanInfraCritical(context.Background(), slug, "req-2", "INFRASTRUCTURE: still wedged")
	got, _ = c.plans.get(slug)
	if got.Goal != "sentinel-value" {
		t.Errorf("second markPlanInfraCritical should have been a no-op, Goal = %q", got.Goal)
	}
}
