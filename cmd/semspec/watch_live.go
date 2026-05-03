package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/c360studio/semspec/pkg/health"
)

// liveDefaults captures the knobs --live exposes. Mirrors the cobra
// flags so the runner is testable without parsing argv.
type liveConfig struct {
	HTTPURL          string
	NATSURL          string
	Interval         time.Duration
	BailOn           string // "" / "warning" / "critical"
	SkipOllama       bool
	MaxDuration      time.Duration // 0 = no cap
	SnapshotInterval time.Duration // 0 = no snapshots
	OutDir           string        // where to write snapshots; required if SnapshotInterval > 0
	Out              io.Writer     // where to stream stdout; defaults to os.Stderr
}

// runWatchLive implements `semspec watch --live`. Polls the same
// endpoints as --bundle on a timer, runs detectors against each
// snapshot, prints a per-tick state line, and emits an "ALERT:"
// line the first time each new diagnosis appears so adopters can
// grep / tail the stream.
//
// Bails early when --bail-on is set to a severity and the highest
// severity in the latest pass matches or exceeds it. The intent is
// "kill the run before it burns more tokens." Returns nil on Ctrl-C
// or when the bail condition fires; non-nil on a setup failure.
func runWatchLive(ctx context.Context, cfg liveConfig) error {
	if cfg.Interval <= 0 {
		cfg.Interval = liveDefaultInterval
	}
	if cfg.Out == nil {
		cfg.Out = os.Stderr
	}
	if cfg.MaxDuration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.MaxDuration)
		defer cancel()
	}

	// Auto-disable the per-tick ollama probe if the binary isn't on
	// PATH. Without this, every tick on a cloud-LLM run shells out
	// to a missing `ollama --version` — wasteful, and contributes
	// noise to the bundle's OllamaState. Operators on local Ollama
	// stacks remain unaffected.
	if !cfg.SkipOllama {
		if _, err := exec.LookPath("ollama"); err != nil {
			fmt.Fprintln(cfg.Out, "info: ollama binary not on PATH; auto-disabling per-tick probe (--skip-ollama)")
			cfg.SkipOllama = true
		}
	}

	nats, natsCloser := dialNATSForBundle(ctx, cfg.NATSURL)
	if natsCloser != nil {
		defer natsCloser()
	}
	httpClient := &http.Client{Timeout: watchHTTPTimeout}

	// Detectors to run live. Same set the bundle ships — keeping this
	// list in one place would mean forcing detector ownership somewhere
	// shared, which is a refactor for when we have a third caller.
	detectors := liveDetectors()

	// Track diagnoses we've already alerted on so each one fires once
	// per session, not once per tick.
	seen := make(map[string]struct{})

	// Track CaptureError sources we've already surfaced so the
	// heartbeat shows `[new: kv:AGENT_LOOPS]` only the first tick
	// each new source appears. Operators reading the stream can
	// scan `[new:` to see what failed without dumping a bundle.
	seenErrorSources := make(map[string]struct{})

	if cfg.SnapshotInterval > 0 && cfg.OutDir == "" {
		return fmt.Errorf("--snapshot-interval requires --out-dir")
	}

	tick := time.NewTicker(cfg.Interval)
	defer tick.Stop()

	var snapshotTick <-chan time.Time
	if cfg.SnapshotInterval > 0 {
		snapshotTicker := time.NewTicker(cfg.SnapshotInterval)
		defer snapshotTicker.Stop()
		snapshotTick = snapshotTicker.C
	}

	fmt.Fprintf(cfg.Out, "semspec watch --live · interval=%s · http=%s%s%s\n",
		cfg.Interval, cfg.HTTPURL, bailSuffix(cfg.BailOn), snapshotSuffix(cfg))

	// Run one capture immediately so the operator sees output without
	// waiting for the first tick — important for "did this even start?"
	if shouldExit, err := runLiveTick(ctx, cfg, httpClient, nats, detectors, seen, seenErrorSources); err != nil {
		fmt.Fprintf(cfg.Out, "warn: tick failed (%v); continuing\n", err)
	} else if shouldExit {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(cfg.Out, "shutting down (context done)")
			return nil
		case <-tick.C:
			if shouldExit, err := runLiveTick(ctx, cfg, httpClient, nats, detectors, seen, seenErrorSources); err != nil {
				fmt.Fprintf(cfg.Out, "warn: tick failed (%v); continuing\n", err)
				continue
			} else if shouldExit {
				return nil
			}
		case <-snapshotTick:
			// Independent capture so a periodic snapshot survives
			// even if the live tick that's about to fire produces a
			// bail or partial failure. Snapshot errors are logged
			// but never break the loop — operators want the live
			// stream to keep running.
			if err := writeLiveSnapshot(ctx, cfg, httpClient, nats); err != nil {
				fmt.Fprintf(cfg.Out, "warn: snapshot failed (%v); continuing\n", err)
			}
		}
	}
}

// runLiveTick performs one capture + detector pass and renders.
// Returns (true, nil) when the bail condition fired and the loop
// should exit normally.
//
// seen and seenErrorSources are persistent across ticks so each
// diagnosis and each error-source surfaces exactly once per session.
func runLiveTick(
	ctx context.Context,
	cfg liveConfig,
	httpClient *http.Client,
	nats health.TrajectoryClient,
	detectors []health.Detector,
	seen map[string]struct{},
	seenErrorSources map[string]struct{},
) (bool, error) {
	captureCfg := health.CaptureConfig{
		HTTPBaseURL:  cfg.HTTPURL,
		MessageLimit: liveMessageLimit,
		CapturedBy:   "semspec-watch-live",
		SkipOllama:   cfg.SkipOllama,
	}
	tickCtx, cancel := context.WithTimeout(ctx, watchHTTPTimeout)
	defer cancel()

	result, err := health.Capture(tickCtx, captureCfg, httpClient, nats)
	if err != nil {
		return false, err
	}
	health.RunAll(result.Bundle, detectors)

	now := time.Now().Format("15:04:05")
	newSources := newErrorSources(result.Errors, seenErrorSources)
	suffix := ""
	if len(newSources) > 0 {
		suffix = " [new: " + strings.Join(newSources, ",") + "]"
	}
	fmt.Fprintf(cfg.Out, "[%s] plans=%d loops=%d msgs=%d active_loops=%d ctx_util=%.2f errors=%d%s\n",
		now,
		len(result.Bundle.Plans),
		len(result.Bundle.Loops),
		len(result.Bundle.Messages),
		result.Bundle.Metrics.LoopActiveLoops,
		result.Bundle.Metrics.LoopContextUtilization,
		len(result.Errors),
		suffix,
	)

	maxSeverity := health.SeverityInfo
	for _, d := range result.Bundle.Diagnoses {
		key := alertKey(d)
		if _, alreadyAlerted := seen[key]; alreadyAlerted {
			continue
		}
		seen[key] = struct{}{}
		fmt.Fprintf(cfg.Out, "ALERT: %s severity=%s evidence_id=%s remediation=%q\n",
			d.Shape, d.Severity, evidenceIDOrEmpty(d), d.Remediation)
		if severityRank(d.Severity) > severityRank(maxSeverity) {
			maxSeverity = d.Severity
		}
	}
	if cfg.BailOn != "" && severityRank(maxSeverity) >= severityRank(health.Severity(cfg.BailOn)) {
		fmt.Fprintf(cfg.Out, "BAIL: severity=%s reached --bail-on=%s threshold; exiting\n", maxSeverity, cfg.BailOn)
		return true, nil
	}
	return false, nil
}

// newErrorSources returns the source names in errs that aren't yet
// in seen, then marks them seen. Sorted for deterministic test
// output. Each source surfaces in the heartbeat exactly once across
// the lifetime of a --live session — operators can grep `[new:` to
// find first appearance of each failure mode.
func newErrorSources(errs []*health.CaptureError, seen map[string]struct{}) []string {
	if len(errs) == 0 {
		return nil
	}
	var fresh []string
	for _, e := range errs {
		if e == nil || e.Source == "" {
			continue
		}
		if _, ok := seen[e.Source]; ok {
			continue
		}
		seen[e.Source] = struct{}{}
		fresh = append(fresh, e.Source)
	}
	sort.Strings(fresh)
	return fresh
}

// liveDetectors returns the detector set used by --live. Same as the
// bundle ships when readers run RunAll; centralizing here means the
// CLI is one place to add a new detector without touching pkg/health.
func liveDetectors() []health.Detector {
	return []health.Detector{
		health.EmptyStopAfterToolCalls{},
		health.JSONInText{},
		health.ThinkingSpiral{},
		health.RapidShallowToolCalls{},
		health.GraphToolFailure{},
	}
}

// alertKey returns a stable identifier for a diagnosis so we don't
// re-alert on the same shape+evidence on subsequent ticks. Includes
// the evidence ID (sequence) so two empty-stops in the same loop on
// different sequences each fire once.
func alertKey(d health.Diagnosis) string {
	return d.Shape + ":" + evidenceIDOrEmpty(d)
}

// evidenceIDOrEmpty returns the first evidence ref's ID or "" if
// none. Used in alerts so the operator can correlate to the message
// sequence.
func evidenceIDOrEmpty(d health.Diagnosis) string {
	if len(d.Evidence) == 0 {
		return ""
	}
	return d.Evidence[0].ID
}

// severityRank orders severities for the bail-on comparison. Unknown
// severities rank below info so a typo in --bail-on never bails.
func severityRank(s health.Severity) int {
	switch s {
	case health.SeverityCritical:
		return 3
	case health.SeverityWarning:
		return 2
	case health.SeverityInfo:
		return 1
	}
	return 0
}

// bailSuffix renders the bail-on flag in the startup banner. Visual
// noise reduction when the flag isn't set.
func bailSuffix(bailOn string) string {
	if bailOn == "" {
		return ""
	}
	return " · bail_on=" + strings.ToLower(bailOn)
}

// writeLiveSnapshot captures a one-off bundle and writes it to
// cfg.OutDir as snapshot-<timestamp>.tar.gz. Used by --snapshot-interval
// so the most recent pre-cleanup capture survives stack teardown —
// adopters running `task e2e:watch:llm` saw final bundles with
// plans=0 because Playwright's afterAll deletes the plan before the
// task wrapper's post-run capture runs. A periodic snapshot fixes
// that without coupling the bundle layer to the test harness.
func writeLiveSnapshot(ctx context.Context, cfg liveConfig, httpClient *http.Client, nats health.TrajectoryClient) error {
	captureCfg := health.CaptureConfig{
		HTTPBaseURL:  cfg.HTTPURL,
		MessageLimit: liveMessageLimit,
		CapturedBy:   "semspec-watch-snapshot",
		SkipOllama:   cfg.SkipOllama,
	}
	tickCtx, cancel := context.WithTimeout(ctx, watchHTTPTimeout)
	defer cancel()

	result, err := health.Capture(tickCtx, captureCfg, httpClient, nats)
	if err != nil {
		return fmt.Errorf("capture: %w", err)
	}

	// Skip writing if the bundle has no useful state. The snapshot
	// ticker keeps firing for a moment after the docker stack tears
	// down (HTTP/NATS connections fail their own timeouts before
	// ctx.Done() reaches the loop), and those terminal ticks would
	// otherwise pollute the snapshot directory with sub-1KB empty
	// archives that `ls | tail -1` could pick over the real data.
	// Caught 2026-04-30 round 2.
	if isTriviallyEmptyBundle(result.Bundle) {
		fmt.Fprintln(cfg.Out, "snapshot: skipped (no plans/loops/messages — likely stack tear-down)")
		return nil
	}

	// File name is sortable so `ls snapshot-*.tar.gz | tail -1` always
	// returns the newest. Fixed-width seconds resolution is enough; the
	// snapshot interval is bounded below at 10s in practice.
	name := fmt.Sprintf("snapshot-%s.tar.gz", time.Now().UTC().Format("20060102-150405"))
	path := cfg.OutDir + "/" + name
	out, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("open snapshot: %w", err)
	}
	defer out.Close()
	if err := health.WriteTarball(out, result); err != nil {
		return fmt.Errorf("write tarball: %w", err)
	}
	fmt.Fprintf(cfg.Out, "snapshot: %s (plans=%d loops=%d trajectories=%d)\n",
		name,
		len(result.Bundle.Plans),
		len(result.Bundle.Loops),
		len(result.Bundle.TrajectoryRefs),
	)
	return nil
}

// isTriviallyEmptyBundle reports whether bundle has no observable
// state worth preserving. Used by writeLiveSnapshot to skip ticks
// that fired while the underlying stack was tearing down.
func isTriviallyEmptyBundle(b *health.Bundle) bool {
	if b == nil {
		return true
	}
	return len(b.Plans) == 0 &&
		len(b.Loops) == 0 &&
		len(b.Messages) == 0 &&
		len(b.TrajectoryRefs) == 0
}

// snapshotSuffix renders the snapshot-interval flag in the startup
// banner. Empty when snapshots are disabled.
func snapshotSuffix(cfg liveConfig) string {
	if cfg.SnapshotInterval <= 0 {
		return ""
	}
	return " · snapshot=" + cfg.SnapshotInterval.String() + "→" + cfg.OutDir
}

// liveDefaultInterval is the default poll cadence. 10s is the bash-
// blob's de-facto cadence and matches the "did anything change?"
// feel without spamming the message-logger.
const liveDefaultInterval = 10 * time.Second

// liveMessageLimit caps the per-tick message-logger fetch. Matches the
// bundle's DefaultMessageLimit because the message-logger applies its
// limit BEFORE the subject filter — on graph-busy runs ~84% of the
// most-recent-N window is graph.mutation noise, so a tight limit
// strands the agent.response messages our detectors walk. With 100
// (the previous value) RapidShallowToolCalls' per-loop threshold of 6
// could not be met when 5+ active loops competed for the surviving
// agent.responses. Caught 2026-05-02 hybrid run: detector silent
// across two iter=50 wedges that met every other condition.
const liveMessageLimit = 5000
