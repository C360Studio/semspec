package health

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func TestWriteTarball_LayoutAndContents(t *testing.T) {
	bundle := &Bundle{
		Bundle: BundleMeta{
			Format:     BundleFormat,
			CapturedAt: time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC),
			CapturedBy: "semspec-test",
		},
		TrajectoryRefs: []TrajectoryRef{
			{LoopID: "loop-1", Filename: "trajectories/loop-1.json", Steps: 3, Outcome: "success"},
		},
	}
	result := &CaptureResult{
		Bundle: bundle,
		Trajectories: map[string][]byte{
			"loop-1": []byte(`{"loop_id":"loop-1","steps":[{}]}`),
			"loop-2": []byte(`{"loop_id":"loop-2","steps":[]}`),
		},
	}

	var buf bytes.Buffer
	if err := WriteTarball(&buf, result); err != nil {
		t.Fatalf("WriteTarball: %v", err)
	}

	gz, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var names []string
	bodies := make(map[string][]byte)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		names = append(names, hdr.Name)
		bodies[hdr.Name] = body
	}
	if len(names) < 1 || names[0] != "bundle.json" {
		t.Errorf("first entry should be bundle.json; got %v", names)
	}
	// Trajectory entries should be sorted by loop ID for determinism.
	wantOrder := []string{"bundle.json", "trajectories/loop-1.json", "trajectories/loop-2.json"}
	if len(names) != len(wantOrder) {
		t.Fatalf("entries = %v, want %v", names, wantOrder)
	}
	for i, want := range wantOrder {
		if names[i] != want {
			t.Errorf("names[%d] = %q, want %q", i, names[i], want)
		}
	}
	// bundle.json should round-trip back into a Bundle struct cleanly.
	var got Bundle
	if err := json.Unmarshal(bodies["bundle.json"], &got); err != nil {
		t.Fatalf("bundle.json invalid: %v", err)
	}
	if got.Bundle.Format != BundleFormat {
		t.Errorf("bundle.json missing format: %+v", got.Bundle)
	}
}

func TestWriteTarball_NilResult(t *testing.T) {
	var buf bytes.Buffer
	err := WriteTarball(&buf, nil)
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("expected nil-result error, got %v", err)
	}
}

func TestWriteTarball_EmptyTrajectoriesIsValid(t *testing.T) {
	bundle := &Bundle{Bundle: BundleMeta{Format: BundleFormat, CapturedAt: time.Now().UTC()}}
	result := &CaptureResult{Bundle: bundle}

	var buf bytes.Buffer
	if err := WriteTarball(&buf, result); err != nil {
		t.Fatalf("WriteTarball: %v", err)
	}
	gz, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	tr := tar.NewReader(gz)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("expected at least bundle.json: %v", err)
	}
	if hdr.Name != "bundle.json" {
		t.Errorf("first entry %q", hdr.Name)
	}
	if _, err := tr.Next(); err != io.EOF {
		t.Errorf("expected EOF after bundle.json, got more entries")
	}
}
