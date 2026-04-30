//go:build integration

package planmanager

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandlePromotePlan requires NATS infrastructure because promote triggers
// the requirement generation cascade via PublishToStream. Run with:
//
//	go test -tags integration ./processor/plan-manager/...
func TestHandlePromotePlan(t *testing.T) {
	slug := "promote-plan"

	c := setupTestComponent(t)
	setupTestPlan(t, c, slug)

	req := httptest.NewRequest(http.MethodPost, "/plan-manager/plans/"+slug+"/promote", nil)
	w := httptest.NewRecorder()

	c.handlePromotePlan(w, req, slug)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got PlanWithStatus
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.Plan == nil {
		t.Fatal("Plan is nil in response")
	}
	if !got.Plan.Approved {
		t.Error("Plan.Approved should be true after promote")
	}
}
