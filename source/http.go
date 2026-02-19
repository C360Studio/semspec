package source

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/c360studio/semspec/source/weburl"
	"github.com/c360studio/semstreams/natsclient"
)

// maxJSONBodySize limits the size of JSON request bodies (1MB).
const maxJSONBodySize = 1 << 20 // 1MB

// allowedProtocols defines the git URL protocols that are permitted.
var allowedRepoProtocols = map[string]bool{
	"https": true,
	"git":   true,
	"ssh":   true,
}

// slugPattern validates that a slug contains only safe characters.
var slugPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// validateRepoURL validates that a repository URL uses an allowed protocol.
func validateRepoURL(rawURL string) error {
	// Handle SSH shorthand (git@github.com:owner/repo.git)
	if strings.HasPrefix(rawURL, "git@") {
		return nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if !allowedRepoProtocols[scheme] {
		return fmt.Errorf("protocol %q not allowed; must be https, git, or ssh", scheme)
	}

	return nil
}

// validateSlug ensures a slug is safe for use in file paths.
func validateSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("slug is required")
	}
	if strings.Contains(slug, "..") {
		return fmt.Errorf("path traversal not allowed")
	}
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("invalid slug format")
	}
	if len(slug) > 255 {
		return fmt.Errorf("slug too long")
	}
	return nil
}

// safeRepoPath returns a safe path within the repos directory, or an error.
func safeRepoPath(reposDir, slug string) (string, error) {
	if err := validateSlug(slug); err != nil {
		return "", err
	}

	// Build and verify path
	repoPath := filepath.Join(reposDir, slug)
	absRepo, err := filepath.Abs(repoPath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	absBase, err := filepath.Abs(reposDir)
	if err != nil {
		return "", fmt.Errorf("invalid base path: %w", err)
	}

	// Ensure path is within repos directory
	if !strings.HasPrefix(absRepo, absBase+string(filepath.Separator)) {
		return "", fmt.Errorf("path must be within repos directory")
	}

	return repoPath, nil
}

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
	mux.HandleFunc(prefix+"repos", h.handleRepos)
	mux.HandleFunc(prefix+"repos/", h.handleReposWithID)
	mux.HandleFunc(prefix+"web", h.handleWeb)
	mux.HandleFunc(prefix+"web/", h.handleWebWithID)
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
		Path:      destPath,
		ProjectID: project,
		AddedBy:   "http_upload",
		MimeType:  getMimeType(ext),
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
func (h *HTTPHandler) handleDelete(w http.ResponseWriter, _ *http.Request, entityID string) {
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

// handleRepos handles POST /api/sources/repos - add a new repository
// and GET /api/sources/repos - list repositories.
func (h *HTTPHandler) handleRepos(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.handleAddRepository(w, r)
	case http.MethodGet:
		// List repositories is handled via GraphQL on frontend
		// This could be implemented if needed
		writeJSON(w, http.StatusOK, map[string]string{"message": "Use GraphQL to list repositories"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAddRepository handles POST /api/sources/repos.
func (h *HTTPHandler) handleAddRepository(w http.ResponseWriter, r *http.Request) {
	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req AddRepositoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if err.Error() == "http: request body too large" {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "body_too_large", "Request body exceeds 1MB limit")
			return
		}
		writeJSONError(w, http.StatusBadRequest, "invalid_json", "Failed to parse request body")
		return
	}

	// Validate URL
	if req.URL == "" {
		writeJSONError(w, http.StatusBadRequest, "url_required", "Repository URL is required")
		return
	}

	// Validate URL protocol
	if err := validateRepoURL(req.URL); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_url", err.Error())
		return
	}

	// Generate entity ID from URL
	entityID := generateRepoID(req.URL)

	// Ensure repos directory exists
	reposDir := filepath.Join(h.sourcesDir, "repos")
	if err := os.MkdirAll(reposDir, 0755); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "io_error", "Failed to create repos directory")
		return
	}

	// Publish ingestion request to NATS
	if h.natsClient != nil {
		data, err := json.Marshal(req)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "marshal_error", "Failed to marshal request")
			return
		}

		subject := fmt.Sprintf("source.repo.ingest.%s", entityID)
		if err := h.natsClient.PublishToStream(r.Context(), subject, data); err != nil {
			writeJSON(w, http.StatusAccepted, RepositoryResponse{
				ID:      entityID,
				Status:  "pending",
				Message: "Request accepted but ingestion queue failed",
			})
			return
		}
	}

	writeJSON(w, http.StatusCreated, RepositoryResponse{
		ID:      entityID,
		Status:  "pending",
		Message: "Repository queued for cloning and indexing",
	})
}

// handleReposWithID handles requests to /api/sources/repos/{id}* endpoints.
func (h *HTTPHandler) handleReposWithID(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	prefix := "/api/sources/repos/"
	if !strings.HasPrefix(path, prefix) {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	remaining := path[len(prefix):]
	parts := strings.SplitN(remaining, "/", 2)
	entityID := parts[0]

	if entityID == "" {
		http.Error(w, "Repository ID required", http.StatusBadRequest)
		return
	}

	// Check for subpath actions
	if len(parts) > 1 {
		switch parts[1] {
		case "pull":
			h.handleRepoPull(w, r, entityID)
			return
		case "reindex":
			h.handleRepoReindex(w, r, entityID)
			return
		}
	}

	// Handle base operations on /api/sources/repos/{id}
	switch r.Method {
	case http.MethodGet:
		// Get repository details - handled via GraphQL on frontend
		writeJSON(w, http.StatusOK, map[string]string{"message": "Use GraphQL to get repository details"})
	case http.MethodPatch:
		h.handleRepoUpdate(w, r, entityID)
	case http.MethodDelete:
		h.handleRepoDelete(w, r, entityID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleRepoPull handles POST /api/sources/repos/{id}/pull.
func (h *HTTPHandler) handleRepoPull(w http.ResponseWriter, r *http.Request, entityID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Publish pull request to NATS
	if h.natsClient != nil {
		data, err := json.Marshal(map[string]string{"id": entityID, "action": "pull"})
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "marshal_error", "Failed to marshal request")
			return
		}

		subject := fmt.Sprintf("source.repo.pull.%s", entityID)
		if err := h.natsClient.PublishToStream(r.Context(), subject, data); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "publish_error", "Failed to queue pull request")
			return
		}
	}

	writeJSON(w, http.StatusOK, PullResponse{
		ID:      entityID,
		Status:  "pulling",
		Message: "Pull request queued",
	})
}

// handleRepoReindex handles POST /api/sources/repos/{id}/reindex.
func (h *HTTPHandler) handleRepoReindex(w http.ResponseWriter, r *http.Request, entityID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Publish reindex request to NATS
	if h.natsClient != nil {
		data, err := json.Marshal(map[string]string{"id": entityID, "action": "reindex"})
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "marshal_error", "Failed to marshal request")
			return
		}

		subject := fmt.Sprintf("source.repo.reindex.%s", entityID)
		if err := h.natsClient.PublishToStream(r.Context(), subject, data); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "publish_error", "Failed to queue reindex request")
			return
		}
	}

	writeJSON(w, http.StatusOK, RepositoryResponse{
		ID:      entityID,
		Status:  "indexing",
		Message: "Reindex request queued",
	})
}

// handleRepoUpdate handles PATCH /api/sources/repos/{id}.
func (h *HTTPHandler) handleRepoUpdate(w http.ResponseWriter, r *http.Request, entityID string) {
	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req UpdateRepositoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if err.Error() == "http: request body too large" {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "body_too_large", "Request body exceeds 1MB limit")
			return
		}
		writeJSONError(w, http.StatusBadRequest, "invalid_json", "Failed to parse request body")
		return
	}

	// Publish update request to NATS
	if h.natsClient != nil {
		updateData := map[string]any{
			"id":     entityID,
			"action": "update",
		}
		if req.AutoPull != nil {
			updateData["auto_pull"] = *req.AutoPull
		}
		if req.PullInterval != nil {
			updateData["pull_interval"] = *req.PullInterval
		}
		if req.ProjectID != nil {
			updateData["project_id"] = *req.ProjectID
		}

		data, err := json.Marshal(updateData)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "marshal_error", "Failed to marshal request")
			return
		}

		subject := fmt.Sprintf("source.repo.update.%s", entityID)
		if err := h.natsClient.PublishToStream(r.Context(), subject, data); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "publish_error", "Failed to queue update request")
			return
		}
	}

	writeJSON(w, http.StatusOK, RepositoryResponse{
		ID:      entityID,
		Status:  "updated",
		Message: "Repository settings updated",
	})
}

// handleRepoDelete handles DELETE /api/sources/repos/{id}.
func (h *HTTPHandler) handleRepoDelete(w http.ResponseWriter, _ *http.Request, entityID string) {
	// Extract slug from entity ID
	slug := strings.TrimPrefix(entityID, "source.repo.")

	// Validate and get safe path
	reposDir := filepath.Join(h.sourcesDir, "repos")
	repoPath, err := safeRepoPath(reposDir, slug)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_path", err.Error())
		return
	}

	// Check if directory exists
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		writeJSONError(w, http.StatusNotFound, "not_found", "Repository directory not found")
		return
	}

	// Delete the repository directory
	if err := os.RemoveAll(repoPath); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "io_error", "Failed to delete repository: "+err.Error())
		return
	}

	// Note: Graph entities will be cleaned up by a separate process or TTL

	w.WriteHeader(http.StatusNoContent)
}

// generateRepoID creates a repository entity ID from a URL.
func generateRepoID(url string) string {
	// Extract repo name from URL
	// e.g., "https://github.com/owner/repo.git" -> "owner-repo"
	url = strings.TrimSuffix(url, ".git")
	url = strings.TrimSuffix(url, "/")

	// Get last two path segments (owner/repo)
	parts := strings.Split(url, "/")
	var slug string
	if len(parts) >= 2 {
		slug = parts[len(parts)-2] + "-" + parts[len(parts)-1]
	} else if len(parts) >= 1 {
		slug = parts[len(parts)-1]
	} else {
		slug = "repo"
	}

	// Normalize
	slug = strings.ToLower(slug)
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "_", "-")

	return "source.repo." + slug
}

// handleWeb handles POST /api/sources/web - add a new web source
// and GET /api/sources/web - list web sources.
func (h *HTTPHandler) handleWeb(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.handleAddWebSource(w, r)
	case http.MethodGet:
		// List web sources is handled via GraphQL on frontend
		writeJSON(w, http.StatusOK, map[string]string{"message": "Use GraphQL to list web sources"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAddWebSource handles POST /api/sources/web.
func (h *HTTPHandler) handleAddWebSource(w http.ResponseWriter, r *http.Request) {
	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req AddWebSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if err.Error() == "http: request body too large" {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "body_too_large", "Request body exceeds 1MB limit")
			return
		}
		writeJSONError(w, http.StatusBadRequest, "invalid_json", "Failed to parse request body")
		return
	}

	// Validate URL
	if req.URL == "" {
		writeJSONError(w, http.StatusBadRequest, "url_required", "URL is required")
		return
	}

	// Validate URL format and security (SSRF prevention)
	if err := weburl.ValidateURL(req.URL); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_url", err.Error())
		return
	}

	// Validate refresh interval if provided
	if req.RefreshInterval != "" {
		if _, err := time.ParseDuration(req.RefreshInterval); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_interval", "Invalid refresh interval format")
			return
		}
	}

	// Generate entity ID from URL
	entityID := weburl.GenerateEntityID(req.URL)

	// Publish ingestion request to NATS
	if h.natsClient != nil {
		data, err := json.Marshal(req)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "marshal_error", "Failed to marshal request")
			return
		}

		subject := fmt.Sprintf("source.web.ingest.%s", entityID)
		if err := h.natsClient.PublishToStream(r.Context(), subject, data); err != nil {
			writeJSON(w, http.StatusAccepted, WebSourceResponse{
				ID:      entityID,
				Status:  "pending",
				Message: "Request accepted but ingestion queue failed",
			})
			return
		}
	}

	writeJSON(w, http.StatusCreated, WebSourceResponse{
		ID:      entityID,
		Status:  "pending",
		Message: "Web source queued for fetching and indexing",
	})
}

// handleWebWithID handles requests to /api/sources/web/{id}* endpoints.
func (h *HTTPHandler) handleWebWithID(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	prefix := "/api/sources/web/"
	if !strings.HasPrefix(path, prefix) {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	remaining := path[len(prefix):]
	parts := strings.SplitN(remaining, "/", 2)
	entityID := parts[0]

	if entityID == "" {
		http.Error(w, "Web source ID required", http.StatusBadRequest)
		return
	}

	// Validate entity ID format to prevent NATS subject injection
	if !weburl.ValidateEntityID(entityID) {
		writeJSONError(w, http.StatusBadRequest, "invalid_id", "Invalid web source ID format")
		return
	}

	// Check for subpath actions
	if len(parts) > 1 {
		switch parts[1] {
		case "refresh":
			h.handleWebRefresh(w, r, entityID)
			return
		}
	}

	// Handle base operations on /api/sources/web/{id}
	switch r.Method {
	case http.MethodGet:
		// Get web source details - handled via GraphQL on frontend
		writeJSON(w, http.StatusOK, map[string]string{"message": "Use GraphQL to get web source details"})
	case http.MethodPatch:
		h.handleWebUpdate(w, r, entityID)
	case http.MethodDelete:
		h.handleWebDelete(w, r, entityID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleWebRefresh handles POST /api/sources/web/{id}/refresh.
func (h *HTTPHandler) handleWebRefresh(w http.ResponseWriter, r *http.Request, entityID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Publish refresh request to NATS
	if h.natsClient != nil {
		data, err := json.Marshal(map[string]string{"id": entityID, "action": "refresh"})
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "marshal_error", "Failed to marshal request")
			return
		}

		subject := fmt.Sprintf("source.web.refresh.%s", entityID)
		if err := h.natsClient.PublishToStream(r.Context(), subject, data); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "publish_error", "Failed to queue refresh request")
			return
		}
	}

	writeJSON(w, http.StatusOK, RefreshResponse{
		ID:      entityID,
		Status:  "refreshing",
		Message: "Refresh request queued",
	})
}

// handleWebUpdate handles PATCH /api/sources/web/{id}.
func (h *HTTPHandler) handleWebUpdate(w http.ResponseWriter, r *http.Request, entityID string) {
	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	var req UpdateWebSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if err.Error() == "http: request body too large" {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "body_too_large", "Request body exceeds 1MB limit")
			return
		}
		writeJSONError(w, http.StatusBadRequest, "invalid_json", "Failed to parse request body")
		return
	}

	// Validate refresh interval if provided
	if req.RefreshInterval != nil && *req.RefreshInterval != "" {
		if _, err := time.ParseDuration(*req.RefreshInterval); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_interval", "Invalid refresh interval format")
			return
		}
	}

	// Publish update request to NATS
	if h.natsClient != nil {
		updateData := map[string]any{
			"id":     entityID,
			"action": "update",
		}
		if req.AutoRefresh != nil {
			updateData["auto_refresh"] = *req.AutoRefresh
		}
		if req.RefreshInterval != nil {
			updateData["refresh_interval"] = *req.RefreshInterval
		}
		if req.ProjectID != nil {
			updateData["project_id"] = *req.ProjectID
		}

		data, err := json.Marshal(updateData)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "marshal_error", "Failed to marshal request")
			return
		}

		subject := fmt.Sprintf("source.web.update.%s", entityID)
		if err := h.natsClient.PublishToStream(r.Context(), subject, data); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "publish_error", "Failed to queue update request")
			return
		}
	}

	writeJSON(w, http.StatusOK, WebSourceResponse{
		ID:      entityID,
		Status:  "updated",
		Message: "Web source settings updated",
	})
}

// handleWebDelete handles DELETE /api/sources/web/{id}.
func (h *HTTPHandler) handleWebDelete(w http.ResponseWriter, r *http.Request, entityID string) {
	// Publish delete request to NATS
	// Unlike docs/repos, web sources don't have local files to delete
	// We just need to remove the graph entities
	if h.natsClient != nil {
		data, err := json.Marshal(map[string]string{"id": entityID, "action": "delete"})
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "marshal_error", "Failed to marshal request")
			return
		}

		subject := fmt.Sprintf("source.web.delete.%s", entityID)
		if err := h.natsClient.PublishToStream(r.Context(), subject, data); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "publish_error", "Failed to queue delete request")
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
