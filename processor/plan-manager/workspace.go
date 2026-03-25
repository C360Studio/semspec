package planmanager

import (
	"io"
	"net/http"
	"strings"
	"time"
)

// workspaceProxy forwards read-only workspace requests to the sandbox server.
type workspaceProxy struct {
	sandboxURL string
	client     *http.Client
}

func newWorkspaceProxy(sandboxURL string) *workspaceProxy {
	if sandboxURL == "" {
		return nil
	}
	return &workspaceProxy{
		sandboxURL: strings.TrimRight(sandboxURL, "/"),
		client:     &http.Client{Timeout: 30 * time.Second},
	}
}

// proxyTo forwards an incoming GET request to the sandbox at the given path,
// preserving query parameters and copying the status, Content-Type,
// Content-Disposition, and body back to the caller.
func (p *workspaceProxy) proxyTo(w http.ResponseWriter, r *http.Request, path string) {
	url := p.sandboxURL + path
	if r.URL.RawQuery != "" {
		url += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	if err != nil {
		http.Error(w, `{"error":"proxy request failed"}`, http.StatusBadGateway)
		return
	}

	resp, err := p.client.Do(req)
	if err != nil {
		http.Error(w, `{"error":"sandbox unavailable"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers we care about.
	for _, h := range []string{"Content-Type", "Content-Disposition"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body) //nolint:errcheck
}

// handleTasks proxies GET /plan-api/workspace/tasks → sandbox GET /workspace/tasks.
func (p *workspaceProxy) handleTasks(w http.ResponseWriter, r *http.Request) {
	p.proxyTo(w, r, "/workspace/tasks")
}

// handleTree proxies GET /plan-api/workspace/tree?task_id=X → sandbox GET /workspace/tree?task_id=X.
func (p *workspaceProxy) handleTree(w http.ResponseWriter, r *http.Request) {
	p.proxyTo(w, r, "/workspace/tree")
}

// handleFile proxies GET /plan-api/workspace/file?task_id=X&path=Y → sandbox GET /file?task_id=X&path=Y.
func (p *workspaceProxy) handleFile(w http.ResponseWriter, r *http.Request) {
	p.proxyTo(w, r, "/file")
}

// handleDownload proxies GET /plan-api/workspace/download?task_id=X → sandbox GET /workspace/download?task_id=X.
func (p *workspaceProxy) handleDownload(w http.ResponseWriter, r *http.Request) {
	p.proxyTo(w, r, "/workspace/download")
}
