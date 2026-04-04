package planmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

// SSE event names for plan streams.
const (
	sseEventPlanUpdated = "plan_updated"
	sseEventConnected   = "connected"
	sseEventHeartbeat   = "heartbeat"
	sseEventError       = "error"
)

// handlePlanStream serves an SSE stream for a specific plan's state changes.
// It watches the PLAN_STATES KV bucket for mutations to the plan's key
// and emits events to the connected client.
//
// GET /plan-manager/plans/{slug}/stream
func (c *Component) handlePlanStream(w http.ResponseWriter, r *http.Request, slug string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Check that the plan store has a KV bucket.
	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()

	if ps == nil || ps.kvBucket == nil {
		http.Error(w, "Plan state streaming not available", http.StatusServiceUnavailable)
		return
	}

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}
	flusher.Flush()

	// Watch the specific key for this plan.
	watcher, err := ps.kvBucket.Watch(ctx, slug)
	if err != nil {
		c.logger.Error("Failed to create KV watcher for plan", "slug", slug, "error", err)
		writeSSEEvent(w, flusher, sseEventError, map[string]string{"message": "failed to watch plan"})
		return
	}
	defer watcher.Stop()

	// Send connected event.
	if err := writeSSEEvent(w, flusher, sseEventConnected, map[string]string{"slug": slug}); err != nil {
		return
	}

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	var eventID uint64
	updates := watcher.Updates()

	for {
		select {
		case <-ctx.Done():
			return

		case <-heartbeat.C:
			eventID++
			if err := writeSSEEventWithID(w, flusher, eventID, sseEventHeartbeat, nil); err != nil {
				return
			}

		case entry, ok := <-updates:
			if !ok {
				return // watcher closed
			}
			if entry == nil {
				// End of initial values — send sync complete.
				eventID++
				writeSSEEventWithID(w, flusher, eventID, "sync_complete", nil)
				continue
			}

			if entry.Operation() == jetstream.KeyValueDelete {
				eventID++
				writeSSEEventWithID(w, flusher, eventID, "plan_deleted", map[string]string{"slug": slug})
				return
			}

			// Wrap raw Plan in PlanWithStatus so the SSE payload includes
			// the computed "stage" field that the UI needs for state-driven rendering.
			eventID++
			payload := c.enrichPlanSSEPayload(ctx, entry.Value())
			if err := writeSSEEventWithID(w, flusher, eventID, sseEventPlanUpdated, payload); err != nil {
				return
			}
		}
	}
}

// enrichPlanSSEPayload unmarshals a raw Plan KV value and wraps it in
// PlanWithStatus so the SSE payload includes the computed "stage" field and,
// when the plan is implementing, the ExecutionSummary counts.
// Falls back to the raw bytes if unmarshaling fails.
func (c *Component) enrichPlanSSEPayload(ctx context.Context, raw []byte) any {
	var plan workflow.Plan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return json.RawMessage(raw)
	}
	pws := &PlanWithStatus{
		Plan:  &plan,
		Stage: c.determinePlanStage(&plan),
	}
	pws.ExecutionSummary = c.computeExecutionSummary(ctx, &plan)
	return pws
}

// writeSSEEvent writes a named SSE event with JSON data.
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, event string, data any) error {
	return writeSSEEventWithID(w, flusher, 0, event, data)
}

// writeSSEEventWithID writes a named SSE event with an optional ID.
func writeSSEEventWithID(w http.ResponseWriter, flusher http.Flusher, id uint64, event string, data any) error {
	if id > 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", id); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
		return err
	}

	if data != nil {
		var jsonData []byte
		switch v := data.(type) {
		case json.RawMessage:
			jsonData = v
		default:
			var err error
			jsonData, err = json.Marshal(data)
			if err != nil {
				jsonData = []byte(`{}`)
			}
		}
		if _, err := fmt.Fprintf(w, "data: %s\n", jsonData); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprint(w, "data: {}\n"); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprint(w, "\n"); err != nil {
		return err
	}

	flusher.Flush()
	return nil
}
