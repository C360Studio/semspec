package health

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"time"
)

// WriteTarball assembles a .tar.gz from the capture result. Layout:
//
//	bundle.json                      ← Bundle struct (TrajectoryRefs are pointers)
//	trajectories/<loop_id>.json      ← one file per captured trajectory
//
// The function does NOT close w — that's the caller's responsibility
// (so callers can write to either an *os.File or a buffer).
//
// Determinism: trajectory files are written in lexicographic order of
// loop ID so two captures of the same input produce byte-identical
// tarballs (modulo timestamps inside bundle.json itself).
func WriteTarball(w io.Writer, result *CaptureResult) error {
	if result == nil || result.Bundle == nil {
		return fmt.Errorf("tarball: capture result required")
	}
	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	bundleBytes, err := json.MarshalIndent(result.Bundle, "", "  ")
	if err != nil {
		return fmt.Errorf("tarball: marshal bundle: %w", err)
	}
	bundleTime := result.Bundle.Bundle.CapturedAt
	if bundleTime.IsZero() {
		bundleTime = time.Now().UTC()
	}
	if err := writeTarFile(tw, "bundle.json", bundleBytes, bundleTime); err != nil {
		return err
	}

	loopIDs := make([]string, 0, len(result.Trajectories))
	for id := range result.Trajectories {
		loopIDs = append(loopIDs, id)
	}
	sort.Strings(loopIDs)
	for _, id := range loopIDs {
		body := result.Trajectories[id]
		name := path.Join("trajectories", id+".json")
		if err := writeTarFile(tw, name, body, bundleTime); err != nil {
			return err
		}
	}
	return nil
}

// writeTarFile writes one regular-file entry into the tar stream.
// Mode 0644 is hard-coded — bundle entries are read-only consumer
// data; nothing in the bundle ever needs +x.
func writeTarFile(tw *tar.Writer, name string, body []byte, modTime time.Time) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o644,
		Size:    int64(len(body)),
		ModTime: modTime,
		Format:  tar.FormatPAX,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("tarball: header %s: %w", name, err)
	}
	if _, err := tw.Write(body); err != nil {
		return fmt.Errorf("tarball: write %s: %w", name, err)
	}
	return nil
}
