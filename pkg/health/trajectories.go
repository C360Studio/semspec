package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TrajectoryClient is the small surface of natsclient.Client that
// trajectory capture needs. Exported so adopters embedding Capture in
// custom tooling can name the contract; *natsclient.Client satisfies
// it without an explicit declaration.
type TrajectoryClient interface {
	Request(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error)
}

// trajectorySubject is the agentic-loop's request/reply subject for
// pulling a single loop's trajectory by ID.
const trajectorySubject = "agentic.query.trajectory"

// trajectoryRequestTimeout caps each request. Trajectories are served
// from in-memory cache so 5s is generous; the adopter's bundle should
// not hang for minutes when one loop's data is missing.
const trajectoryRequestTimeout = 5 * time.Second

// errTrajectoryNotFound is returned when the agentic-loop reports no
// trajectory for the requested loop ID — a normal terminal condition
// the orchestrator records as a benign skip rather than a failure.
var errTrajectoryNotFound = errors.New("trajectory not found")

// trajectoryMeta picks just the fields TrajectoryRef populates.
// Used to walk the bundle's loop list, count steps, and read the
// outcome without unmarshalling the full Trajectory struct (which
// would couple this package to semstreams' agentic types).
type trajectoryMeta struct {
	LoopID  string            `json:"loop_id"`
	Outcome string            `json:"outcome,omitempty"`
	Steps   []json.RawMessage `json:"steps"`
}

// FetchTrajectory issues a NATS request/reply for one loop's
// trajectory. Returns the raw JSON bytes (so the orchestrator can
// drop them straight into trajectories/<loop_id>.json without
// re-marshalling) plus a TrajectoryRef stub with Steps + Outcome
// pre-populated; the caller fills Filename based on its on-disk
// layout.
//
// Returns (nil, _, errTrajectoryNotFound) for the not-found terminal
// case so the orchestrator can record a benign skip — adopters often
// have stale loop IDs in AGENT_LOOPS that the agentic-loop's cache
// has already evicted.
// Errors returned from FetchTrajectory are NOT prefixed with the
// loop ID — the orchestrator's CaptureError.Source field carries
// "trajectory:<loop_id>" which produces the same final string as a
// prefix here would, so prefixing was producing
// "trajectory:X: trajectory:X: decode: ..." in the bundle's error
// list. Caught 2026-04-30 by the first watch CLI exercise.
func FetchTrajectory(ctx context.Context, client TrajectoryClient, loopID string) ([]byte, TrajectoryRef, error) {
	if client == nil {
		return nil, TrajectoryRef{}, errors.New("nats client required")
	}
	if loopID == "" {
		return nil, TrajectoryRef{}, errors.New("loop id required")
	}
	reqBody, err := json.Marshal(struct {
		LoopID string `json:"loopId"`
		Limit  int    `json:"limit,omitempty"`
	}{LoopID: loopID})
	if err != nil {
		return nil, TrajectoryRef{}, fmt.Errorf("marshal: %w", err)
	}
	resp, err := client.Request(ctx, trajectorySubject, reqBody, trajectoryRequestTimeout)
	if err != nil {
		// agentic-loop emits exactly "trajectory not found: <wrapped>"
		// from handleTrajectoryQuery (semstreams processor/agentic-loop).
		// HasPrefix is a tighter anchor than Contains — protects against
		// a future error string that mentions "trajectory not found"
		// inside a different failure mode.
		if strings.HasPrefix(err.Error(), "trajectory not found") {
			return nil, TrajectoryRef{}, errTrajectoryNotFound
		}
		return nil, TrajectoryRef{}, err
	}
	var meta trajectoryMeta
	if err := json.Unmarshal(resp, &meta); err != nil {
		return nil, TrajectoryRef{}, fmt.Errorf("decode: %w", err)
	}
	if meta.LoopID == "" {
		// Empty loop_id with a non-error body is a buggy responder, not
		// a benign not-found. Surface it loudly so adopters debugging a
		// wedge see "responder returned no loop_id" in the bundle's
		// error list rather than silently zero trajectories.
		return nil, TrajectoryRef{}, errors.New("responder returned empty loop_id")
	}
	return resp, TrajectoryRef{
		LoopID:  meta.LoopID,
		Steps:   len(meta.Steps),
		Outcome: meta.Outcome,
	}, nil
}
