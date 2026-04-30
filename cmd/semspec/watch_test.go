package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunWatchBundle_WritesTarball exercises the end-to-end CLI flow
// against an in-process gateway. NATS is intentionally skipped via an
// empty URL — the bundle should still write, just without trajectories.
func TestRunWatchBundle_WritesTarball(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			_, _ = w.Write([]byte("semspec_loop_active_loops 2\n"))
		case "/message-logger/entries":
			_, _ = w.Write([]byte("[]"))
		case "/message-logger/kv/PLAN_STATES",
			"/message-logger/kv/AGENT_LOOPS":
			_, _ = w.Write([]byte(`{"bucket":"x","entries":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	out := filepath.Join(dir, "bundle.tar.gz")
	cfg := watchBundleConfig{
		BundlePath: out,
		HTTPURL:    srv.URL,
		NATSURL:    "", // skip NATS
		SkipOllama: true,
	}
	if err := runWatchBundle(context.Background(), cfg); err != nil {
		t.Fatalf("runWatchBundle: %v", err)
	}

	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open bundle: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	var sawBundle bool
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		if hdr.Name == "bundle.json" {
			sawBundle = true
			body, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("read bundle.json: %v", err)
			}
			var b map[string]any
			if err := json.Unmarshal(body, &b); err != nil {
				t.Fatalf("bundle.json invalid: %v", err)
			}
			meta, ok := b["bundle"].(map[string]any)
			if !ok || meta["format"] != "v1" {
				t.Errorf("bundle.json missing format=v1: %+v", b["bundle"])
			}
		}
	}
	if !sawBundle {
		t.Error("tarball missing bundle.json")
	}
}

func TestRunWatchBundle_RequiresBundlePath(t *testing.T) {
	cmd := watchCmd()
	cmd.SetArgs([]string{}) // no --bundle
	err := cmd.RunE(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--bundle") {
		t.Errorf("expected required-flag error, got %v", err)
	}
}
