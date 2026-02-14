package source

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateRepoURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		// Valid URLs
		{"https github", "https://github.com/owner/repo.git", false},
		{"https gitlab", "https://gitlab.com/owner/repo", false},
		{"git protocol", "git://github.com/owner/repo.git", false},
		{"ssh protocol", "ssh://git@github.com/owner/repo.git", false},
		{"ssh shorthand", "git@github.com:owner/repo.git", false},

		// Invalid URLs
		{"file protocol", "file:///path/to/repo", true},
		{"http insecure", "http://github.com/owner/repo.git", true},
		{"ftp protocol", "ftp://example.com/repo", true},
		{"empty url", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRepoURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRepoURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestValidateSlug(t *testing.T) {
	tests := []struct {
		name    string
		slug    string
		wantErr bool
	}{
		// Valid slugs
		{"simple", "owner-repo", false},
		{"with dots", "owner.repo", false},
		{"with underscore", "owner_repo", false},
		{"alphanumeric", "repo123", false},

		// Invalid slugs
		{"empty", "", true},
		{"path traversal", "../escape", true},
		{"double dot", "foo..bar", true},
		{"starts with dot", ".hidden", true},
		{"starts with dash", "-repo", true},
		{"too long", strings.Repeat("a", 256), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSlug(tt.slug)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSlug(%q) error = %v, wantErr %v", tt.slug, err, tt.wantErr)
			}
		})
	}
}

func TestSafeRepoPath(t *testing.T) {
	reposDir := t.TempDir()

	tests := []struct {
		name    string
		slug    string
		wantErr bool
	}{
		{"valid slug", "owner-repo", false},
		{"path traversal attempt", "../escape", true},
		{"empty slug", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := safeRepoPath(reposDir, tt.slug)
			if (err != nil) != tt.wantErr {
				t.Errorf("safeRepoPath(%q, %q) error = %v, wantErr %v", reposDir, tt.slug, err, tt.wantErr)
			}
			if !tt.wantErr && path == "" {
				t.Error("expected non-empty path for valid slug")
			}
		})
	}
}

func TestGenerateDocID(t *testing.T) {
	tests := []struct {
		name     string
		baseName string
		want     string
	}{
		{"simple", "auth-sop", "source.doc.auth-sop"},
		{"with spaces", "auth sop", "source.doc.auth-sop"},
		{"with underscores", "auth_sop", "source.doc.auth-sop"},
		{"uppercase", "AUTH-SOP", "source.doc.auth-sop"},
		{"multiple dashes", "auth--sop", "source.doc.auth-sop"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateDocID(tt.baseName)
			if got != tt.want {
				t.Errorf("generateDocID(%q) = %q, want %q", tt.baseName, got, tt.want)
			}
		})
	}
}

func TestGenerateRepoID(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"github https", "https://github.com/owner/repo.git", "source.repo.owner-repo"},
		{"github no .git", "https://github.com/owner/repo", "source.repo.owner-repo"},
		{"gitlab", "https://gitlab.com/group/project.git", "source.repo.group-project"},
		{"trailing slash", "https://github.com/owner/repo/", "source.repo.owner-repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateRepoID(tt.url)
			if got != tt.want {
				t.Errorf("generateRepoID(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestHandleAddRepository(t *testing.T) {
	handler := NewHTTPHandler(t.TempDir(), nil, "")
	mux := http.NewServeMux()
	handler.RegisterHTTPHandlers("/api/sources/", mux)

	t.Run("valid request", func(t *testing.T) {
		body := `{"url":"https://github.com/owner/repo.git"}`
		req := httptest.NewRequest(http.MethodPost, "/api/sources/repos", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusCreated {
			t.Errorf("expected status %d, got %d: %s", http.StatusCreated, rr.Code, rr.Body.String())
		}

		var resp RepositoryResponse
		json.NewDecoder(rr.Body).Decode(&resp)
		if resp.ID == "" {
			t.Error("expected non-empty ID")
		}
		if resp.Status != "pending" {
			t.Errorf("expected status 'pending', got %q", resp.Status)
		}
	})

	t.Run("missing url", func(t *testing.T) {
		body := `{}`
		req := httptest.NewRequest(http.MethodPost, "/api/sources/repos", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
		}
	})

	t.Run("invalid url protocol", func(t *testing.T) {
		body := `{"url":"file:///path/to/repo"}`
		req := httptest.NewRequest(http.MethodPost, "/api/sources/repos", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		body := `{invalid json}`
		req := httptest.NewRequest(http.MethodPost, "/api/sources/repos", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
		}
	})

	t.Run("body too large", func(t *testing.T) {
		// Create a body larger than 1MB
		largeBody := `{"url":"` + strings.Repeat("a", 2*1024*1024) + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/sources/repos", bytes.NewBufferString(largeBody))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("expected status %d, got %d", http.StatusRequestEntityTooLarge, rr.Code)
		}
	})
}

func TestHandleRepoDelete(t *testing.T) {
	handler := NewHTTPHandler(t.TempDir(), nil, "")
	mux := http.NewServeMux()
	handler.RegisterHTTPHandlers("/api/sources/", mux)

	t.Run("path traversal attempt", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/sources/repos/source.repo.../etc/passwd", nil)
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		// Should return bad request due to path traversal detection
		if rr.Code != http.StatusBadRequest && rr.Code != http.StatusNotFound {
			t.Errorf("expected status 400 or 404, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/sources/repos/source.repo.nonexistent", nil)
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("expected status %d, got %d", http.StatusNotFound, rr.Code)
		}
	})
}

func TestHandleRepoUpdate(t *testing.T) {
	handler := NewHTTPHandler(t.TempDir(), nil, "")
	mux := http.NewServeMux()
	handler.RegisterHTTPHandlers("/api/sources/", mux)

	t.Run("valid update", func(t *testing.T) {
		body := `{"auto_pull":true,"pull_interval":"1h"}`
		req := httptest.NewRequest(http.MethodPatch, "/api/sources/repos/source.repo.owner-repo", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		body := `{invalid}`
		req := httptest.NewRequest(http.MethodPatch, "/api/sources/repos/source.repo.owner-repo", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
		}
	})
}

func TestHandleRepos(t *testing.T) {
	handler := NewHTTPHandler(t.TempDir(), nil, "")
	mux := http.NewServeMux()
	handler.RegisterHTTPHandlers("/api/sources/", mux)

	t.Run("GET list", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sources/repos", nil)
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("unsupported method", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/sources/repos", nil)
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rr.Code)
		}
	})
}

func TestGetMimeType(t *testing.T) {
	tests := []struct {
		ext  string
		want string
	}{
		{".md", "text/markdown"},
		{".txt", "text/plain"},
		{".pdf", "application/pdf"},
		{".unknown", "application/octet-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			got := getMimeType(tt.ext)
			if got != tt.want {
				t.Errorf("getMimeType(%q) = %q, want %q", tt.ext, got, tt.want)
			}
		})
	}
}

func TestWriteJSON(t *testing.T) {
	rr := httptest.NewRecorder()
	data := map[string]string{"key": "value"}

	writeJSON(rr, http.StatusOK, data)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if rr.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", rr.Header().Get("Content-Type"))
	}

	body, _ := io.ReadAll(rr.Body)
	if !strings.Contains(string(body), "value") {
		t.Errorf("expected body to contain 'value', got %s", body)
	}
}

func TestWriteJSONError(t *testing.T) {
	rr := httptest.NewRecorder()

	writeJSONError(rr, http.StatusBadRequest, "test_error", "Test message")

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}

	var resp ErrorResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Error != "test_error" {
		t.Errorf("expected error 'test_error', got %q", resp.Error)
	}
	if resp.Message != "Test message" {
		t.Errorf("expected message 'Test message', got %q", resp.Message)
	}
}
