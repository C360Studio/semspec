package projectmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/c360studio/semspec/graph"
)

// infraHealthResponse matches semdragon's health endpoint shape.
type infraHealthResponse struct {
	Overall string        `json:"overall"`
	Checks  []healthCheck `json:"checks"`
}

type healthCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "ok", "error", "warning"
	Message string `json:"message"`
}

// handleInfraHealth verifies NATS connection, JetStream streams, and KV buckets.
// Returns HTTP 200 when all checks pass, HTTP 503 when any critical check fails.
// Used as the Docker healthcheck endpoint to gate dependent services (semsource).
func (c *Component) handleInfraHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var checks []healthCheck
	hasError := false

	// 1. NATS connection
	if c.natsClient == nil {
		checks = append(checks, healthCheck{Name: "nats", Status: "error", Message: "NATS client not configured"})
		hasError = true
	} else if !c.natsClient.IsHealthy() {
		checks = append(checks, healthCheck{Name: "nats", Status: "error", Message: "NATS not connected"})
		hasError = true
	} else {
		checks = append(checks, healthCheck{Name: "nats", Status: "ok", Message: "Connected"})
	}

	// Stream and bucket checks require a healthy NATS connection.
	if c.natsClient != nil && c.natsClient.IsHealthy() {
		js, err := c.natsClient.JetStream()
		if err != nil {
			checks = append(checks, healthCheck{Name: "jetstream", Status: "error", Message: "JetStream unavailable"})
			hasError = true
		} else {
			// 2. AGENT stream
			if _, err := js.Stream(ctx, "AGENT"); err != nil {
				checks = append(checks, healthCheck{Name: "stream:AGENT", Status: "error", Message: "AGENT stream not found"})
				hasError = true
			} else {
				checks = append(checks, healthCheck{Name: "stream:AGENT", Status: "ok", Message: "Stream exists"})
			}

			// 3. GRAPH stream
			if _, err := js.Stream(ctx, "GRAPH"); err != nil {
				checks = append(checks, healthCheck{Name: "stream:GRAPH", Status: "error", Message: "GRAPH stream not found"})
				hasError = true
			} else {
				checks = append(checks, healthCheck{Name: "stream:GRAPH", Status: "ok", Message: "Stream exists"})
			}

			// 4. PLAN_STATES bucket
			if _, err := js.KeyValue(ctx, "PLAN_STATES"); err != nil {
				checks = append(checks, healthCheck{Name: "bucket:PLAN_STATES", Status: "warning", Message: "Bucket not found (created on first plan)"})
			} else {
				checks = append(checks, healthCheck{Name: "bucket:PLAN_STATES", Status: "ok", Message: "Bucket exists"})
			}

			// 5. ENTITY_STATES bucket
			if _, err := js.KeyValue(ctx, "ENTITY_STATES"); err != nil {
				checks = append(checks, healthCheck{Name: "bucket:ENTITY_STATES", Status: "warning", Message: "Bucket not found (created on first entity)"})
			} else {
				checks = append(checks, healthCheck{Name: "bucket:ENTITY_STATES", Status: "ok", Message: "Bucket exists"})
			}
		}
	}

	overall := "healthy"
	if hasError {
		overall = "unhealthy"
	}

	resp := infraHealthResponse{Overall: overall, Checks: checks}
	w.Header().Set("Content-Type", "application/json")
	if hasError {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(resp)
}

// handleGraphSummary returns the same formatted summary that the graph_summary
// agent tool produces. Exercises the full SourceRegistry chain: IsReady() →
// FormatSummaryForPrompt() → fetchSummaryWithCache(). Returns 503 if the
// registry has no data (semsource not ready or not configured).
func (c *Component) handleGraphSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	reg := graph.GlobalSources()
	if reg == nil {
		http.Error(w, "graph sources not configured", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	text := reg.FormatSummaryForPrompt(ctx)
	if text == "" {
		http.Error(w, "no graph data available (semsource may still be indexing)", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(text))
}
