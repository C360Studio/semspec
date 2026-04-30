package planmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// BranchDiffFile mirrors the sandbox branchDiffFile shape. Duplicated here
// to avoid importing cmd/sandbox (which is package main).
type BranchDiffFile struct {
	Path       string `json:"path"`
	OldPath    string `json:"old_path,omitempty"`
	Status     string `json:"status"`
	Insertions int    `json:"insertions"`
	Deletions  int    `json:"deletions"`
	Binary     bool   `json:"binary,omitempty"`
}

// BranchDiffSummary is the response shape from sandbox /git/branch-diff.
type BranchDiffSummary struct {
	Base            string           `json:"base"`
	Branch          string           `json:"branch"`
	Files           []BranchDiffFile `json:"files"`
	TotalInsertions int              `json:"total_insertions"`
	TotalDeletions  int              `json:"total_deletions"`
}

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

// handleTasks proxies GET /plan-manager/workspace/tasks → sandbox GET /workspace/tasks.
func (p *workspaceProxy) handleTasks(w http.ResponseWriter, r *http.Request) {
	p.proxyTo(w, r, "/workspace/tasks")
}

// handleTree proxies GET /plan-manager/workspace/tree?task_id=X → sandbox GET /workspace/tree?task_id=X.
func (p *workspaceProxy) handleTree(w http.ResponseWriter, r *http.Request) {
	p.proxyTo(w, r, "/workspace/tree")
}

// handleFile proxies GET /plan-manager/workspace/file?task_id=X&path=Y → sandbox GET /file?task_id=X&path=Y.
func (p *workspaceProxy) handleFile(w http.ResponseWriter, r *http.Request) {
	p.proxyTo(w, r, "/file")
}

// handleDownload proxies GET /plan-manager/workspace/download?task_id=X → sandbox GET /workspace/download?task_id=X.
func (p *workspaceProxy) handleDownload(w http.ResponseWriter, r *http.Request) {
	p.proxyTo(w, r, "/workspace/download")
}

// postJSON issues a POST to the sandbox at path with a JSON body and decodes
// the JSON response. 404 is returned separately so callers can distinguish
// "branch never materialized" from real errors.
func (p *workspaceProxy) postJSON(ctx context.Context, path string, body, out any) (int, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.sandboxURL+path, bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return resp.StatusCode, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, fmt.Errorf("sandbox %s: %s", path, strings.TrimSpace(string(msg)))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("decode: %w", err)
		}
	}
	return resp.StatusCode, nil
}

// branchDiff calls sandbox POST /git/branch-diff. Returns (summary, found, err)
// where found=false signals the branch does not exist (e.g. requirement not
// yet started) — caller should treat as empty diff, not error.
func (p *workspaceProxy) branchDiff(ctx context.Context, branch, base string) (*BranchDiffSummary, bool, error) {
	var out BranchDiffSummary
	status, err := p.postJSON(ctx, "/git/branch-diff", map[string]string{
		"branch": branch,
		"base":   base,
	}, &out)
	if err != nil {
		return nil, false, err
	}
	if status == http.StatusNotFound {
		return nil, false, nil
	}
	return &out, true, nil
}

// branchFileDiff calls sandbox POST /git/branch-file-diff and returns the
// raw unified patch. 404 returns ("", false, nil).
func (p *workspaceProxy) branchFileDiff(ctx context.Context, branch, base, path string) (string, bool, error) {
	var out struct {
		Patch string `json:"patch"`
	}
	status, err := p.postJSON(ctx, "/git/branch-file-diff", map[string]string{
		"branch": branch,
		"base":   base,
		"path":   path,
	}, &out)
	if err != nil {
		return "", false, err
	}
	if status == http.StatusNotFound {
		return "", false, nil
	}
	return out.Patch, true, nil
}
