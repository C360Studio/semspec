package contextbuilder

import (
	"errors"
	"net/http"
	"strings"

	"github.com/nats-io/nats.go/jetstream"
)

// RegisterHTTPHandlers registers HTTP handlers for the context-builder component.
// The prefix includes the trailing slash (e.g., "/context-builder/").
func (c *Component) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	mux.HandleFunc(prefix+"responses/", c.handleGetResponse)
}

// handleGetResponse handles GET /responses/{request_id}
// Returns the stored context build response for the given request ID.
func (c *Component) handleGetResponse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract request_id from path: /context-builder/responses/{request_id}
	requestID := extractRequestID(r.URL.Path)
	if requestID == "" {
		http.Error(w, "Request ID required", http.StatusBadRequest)
		return
	}

	// Get response from KV bucket
	c.mu.RLock()
	bucket := c.responseBucket
	c.mu.RUnlock()

	if bucket == nil {
		http.Error(w, "Context storage not initialized", http.StatusServiceUnavailable)
		return
	}

	entry, err := bucket.Get(r.Context(), requestID)
	if err != nil {
		// Check if it's a "not found" error
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			http.Error(w, "Context not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to get context response",
			"request_id", requestID,
			"error", err)
		http.Error(w, "Failed to retrieve context", http.StatusInternalServerError)
		return
	}

	// Return the stored response directly (it's already JSON)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(entry.Value()); err != nil {
		c.logger.Warn("Failed to write response", "error", err)
	}
}

// extractRequestID extracts the request ID from a path like /context-builder/responses/{request_id}
func extractRequestID(path string) string {
	// Find the last occurrence of "/responses/"
	idx := strings.LastIndex(path, "/responses/")
	if idx == -1 {
		return ""
	}
	requestID := path[idx+len("/responses/"):]
	// Remove any trailing slashes
	requestID = strings.TrimSuffix(requestID, "/")
	return requestID
}
