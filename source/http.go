package source

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/c360studio/semstreams/natsclient"
)

// HTTPHandler handles HTTP requests for sources management.
type HTTPHandler struct {
	sourcesDir   string
	natsClient   *natsclient.Client
	ingestStream string
}

// NewHTTPHandler creates a new HTTP handler for sources.
func NewHTTPHandler(sourcesDir string, natsClient *natsclient.Client, ingestStream string) *HTTPHandler {
	return &HTTPHandler{
		sourcesDir:   sourcesDir,
		natsClient:   natsClient,
		ingestStream: ingestStream,
	}
}

// RegisterHTTPHandlers registers HTTP handlers for sources management.
// The prefix should include the trailing slash (e.g., "/api/sources/").
func (h *HTTPHandler) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	mux.HandleFunc(prefix+"docs", h.handleUpload)
	mux.HandleFunc(prefix+"docs/", h.handleDocsWithID)
}

// UploadResponse is the JSON response for POST /api/sources/docs.
type UploadResponse struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// ReindexResponse is the JSON response for POST /api/sources/docs/{id}/reindex.
type ReindexResponse struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// ErrorResponse is a standard error response.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// handleUpload handles POST /api/sources/docs - upload a new document.
func (h *HTTPHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse multipart form (32MB max)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSONError(w, http.StatusBadRequest, "parse_error", "Failed to parse multipart form: "+err.Error())
		return
	}

	// Get uploaded file
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "file_required", "File field is required")
		return
	}
	defer file.Close()

	// Validate file extension
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".md" && ext != ".txt" && ext != ".pdf" {
		writeJSONError(w, http.StatusBadRequest, "invalid_type", "Unsupported file type. Supported: .md, .txt, .pdf")
		return
	}

	// Get optional fields
	project := r.FormValue("project")
	category := r.FormValue("category")

	// Ensure sources directory exists
	docsDir := filepath.Join(h.sourcesDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "io_error", "Failed to create sources directory")
		return
	}

	// Save file to disk
	destPath := filepath.Join(docsDir, header.Filename)
	destFile, err := os.Create(destPath)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "io_error", "Failed to create destination file")
		return
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, file); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "io_error", "Failed to save file")
		return
	}

	// Generate entity ID
	baseName := strings.TrimSuffix(header.Filename, ext)
	entityID := generateDocID(baseName)

	// Publish ingestion request to NATS
	ingestReq := IngestRequest{
		Path:     destPath,
		Project:  project,
		AddedBy:  "http_upload",
		MimeType: getMimeType(ext),
	}

	// Only set category in frontmatter/metadata, not in the IngestRequest
	// The ingester will extract category from file content or use LLM analysis
	_ = category // Category is handled by the ingester via frontmatter or LLM

	if h.natsClient != nil {
		data, err := json.Marshal(ingestReq)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "marshal_error", "Failed to marshal ingestion request")
			return
		}

		subject := fmt.Sprintf("source.ingest.%s", entityID)
		if err := h.natsClient.PublishToStream(r.Context(), subject, data); err != nil {
			// Log error but don't fail - file is saved and can be processed later
			// In production, you might want to handle this differently
			writeJSON(w, http.StatusAccepted, UploadResponse{
				ID:      entityID,
				Status:  "pending",
				Message: "File saved but ingestion request failed to queue",
			})
			return
		}
	}

	writeJSON(w, http.StatusCreated, UploadResponse{
		ID:      entityID,
		Status:  "pending",
		Message: "File uploaded and queued for ingestion",
	})
}

// handleDocsWithID handles requests to /api/sources/docs/{id}* endpoints.
func (h *HTTPHandler) handleDocsWithID(w http.ResponseWriter, r *http.Request) {
	// Extract ID and subpath from URL
	// Path: /api/sources/docs/{id} or /api/sources/docs/{id}/reindex
	path := r.URL.Path
	prefix := "/api/sources/docs/"
	if !strings.HasPrefix(path, prefix) {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	remaining := path[len(prefix):]
	parts := strings.SplitN(remaining, "/", 2)
	entityID := parts[0]

	if entityID == "" {
		http.Error(w, "Document ID required", http.StatusBadRequest)
		return
	}

	// Check for subpath
	if len(parts) > 1 && parts[1] == "reindex" {
		h.handleReindex(w, r, entityID)
		return
	}

	// Handle DELETE /api/sources/docs/{id}
	if r.Method == http.MethodDelete {
		h.handleDelete(w, r, entityID)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// handleDelete handles DELETE /api/sources/docs/{id}.
func (h *HTTPHandler) handleDelete(w http.ResponseWriter, r *http.Request, entityID string) {
	// Find and delete the file
	// Entity ID format: source.doc.{slug}
	// We need to find the corresponding file

	// For simplicity, we'll try common extensions
	docsDir := filepath.Join(h.sourcesDir, "docs")

	// Extract the slug from entity ID
	slug := strings.TrimPrefix(entityID, "source.doc.")
	slug = strings.ReplaceAll(slug, "-", "_")

	// Try to find matching file
	extensions := []string{".md", ".txt", ".pdf"}
	var foundFile string

	for _, ext := range extensions {
		// Try exact match first
		path := filepath.Join(docsDir, slug+ext)
		if _, err := os.Stat(path); err == nil {
			foundFile = path
			break
		}
	}

	// If not found, scan directory for matching files
	if foundFile == "" {
		entries, err := os.ReadDir(docsDir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := entry.Name()
				nameSlug := strings.TrimSuffix(name, filepath.Ext(name))
				if generateDocID(nameSlug) == entityID {
					foundFile = filepath.Join(docsDir, name)
					break
				}
			}
		}
	}

	if foundFile == "" {
		writeJSONError(w, http.StatusNotFound, "not_found", "Document file not found")
		return
	}

	// Delete the file
	if err := os.Remove(foundFile); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "io_error", "Failed to delete file: "+err.Error())
		return
	}

	// Note: The graph entities will be cleaned up by a separate process or TTL
	// For now, we just delete the source file

	w.WriteHeader(http.StatusNoContent)
}

// handleReindex handles POST /api/sources/docs/{id}/reindex.
func (h *HTTPHandler) handleReindex(w http.ResponseWriter, r *http.Request, entityID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Find the source file
	docsDir := filepath.Join(h.sourcesDir, "docs")
	slug := strings.TrimPrefix(entityID, "source.doc.")

	// Try to find matching file
	extensions := []string{".md", ".txt", ".pdf"}
	var foundFile string

	for _, ext := range extensions {
		// Build potential filename patterns
		patterns := []string{
			filepath.Join(docsDir, slug+ext),
			filepath.Join(docsDir, strings.ReplaceAll(slug, "-", "_")+ext),
			filepath.Join(docsDir, strings.ReplaceAll(slug, "_", "-")+ext),
		}
		for _, path := range patterns {
			if _, err := os.Stat(path); err == nil {
				foundFile = path
				break
			}
		}
		if foundFile != "" {
			break
		}
	}

	// Scan directory if not found
	if foundFile == "" {
		entries, err := os.ReadDir(docsDir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := entry.Name()
				nameSlug := strings.TrimSuffix(name, filepath.Ext(name))
				if generateDocID(nameSlug) == entityID {
					foundFile = filepath.Join(docsDir, name)
					break
				}
			}
		}
	}

	if foundFile == "" {
		writeJSONError(w, http.StatusNotFound, "not_found", "Document file not found")
		return
	}

	// Publish re-ingestion request
	ingestReq := IngestRequest{
		Path:    foundFile,
		AddedBy: "http_reindex",
	}

	if h.natsClient != nil {
		data, err := json.Marshal(ingestReq)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "marshal_error", "Failed to marshal reindex request")
			return
		}

		subject := fmt.Sprintf("source.ingest.%s", entityID)
		if err := h.natsClient.PublishToStream(r.Context(), subject, data); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "publish_error", "Failed to queue reindex request")
			return
		}
	}

	writeJSON(w, http.StatusOK, ReindexResponse{
		ID:      entityID,
		Status:  "pending",
		Message: "Document queued for re-ingestion",
	})
}

// Helper functions

// generateDocID creates a document entity ID from a filename.
func generateDocID(baseName string) string {
	// Normalize: lowercase, replace spaces and underscores with hyphens
	slug := strings.ToLower(baseName)
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "_", "-")
	// Remove multiple consecutive hyphens
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")
	return "source.doc." + slug
}

// getMimeType returns the MIME type for a file extension.
func getMimeType(ext string) string {
	switch ext {
	case ".md":
		return "text/markdown"
	case ".txt":
		return "text/plain"
	case ".pdf":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, status int, errorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error:   errorCode,
		Message: message,
	})
}

// Ensure HTTPHandler can be used with a timestamp for testing
var _ = time.Now
