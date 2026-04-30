package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/c360studio/semspec/pkg/health"
)

// liveDefaults captures the knobs --live exposes. Mirrors the cobra
// flags so the runner is testable without parsing argv.
type liveConfig struct {
	HTTPURL     string
	NATSURL     string
	Interval    time.Duration
	BailOn      string // "" / "warning" / "critical"
	SkipOllama  bool
	MaxDuration time.Duration // 0 = no cap
	Out         io.Writer     // where to stream output; defaults to os.Stderr
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

	tick := time.NewTicker(cfg.Interval)
	defer tick.Stop()

	fmt.Fprintf(cfg.Out, "semspec watch --live · interval=%s · http=%s%s\n",
		cfg.Interval, cfg.HTTPURL, bailSuffix(cfg.BailOn))

	// Run one capture immediately so the operator sees output without
	// waiting for the first tick — important for "did this even start?"
	if shouldExit, err := runLiveTick(ctx, cfg, httpClient, nats, detectors, seen); err != nil {
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
			if shouldExit, err := runLiveTick(ctx, cfg, httpClient, nats, detectors, seen); err != nil {
				fmt.Fprintf(cfg.Out, "warn: tick failed (%v); continuing\n", err)
				continue
			} else if shouldExit {
				return nil
			}
		}
	}
}

// runLiveTick performs one capture + detector pass and renders.
// Returns (true, nil) when the bail condition fired and the loop
// should exit normally.
func runLiveTick(
	ctx context.Context,
	cfg liveConfig,
	httpClient *http.Client,
	nats health.TrajectoryClient,
	detectors []health.Detector,
	seen map[string]struct{},
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
	fmt.Fprintf(cfg.Out, "[%s] plans=%d loops=%d msgs=%d active_loops=%d ctx_util=%.2f errors=%d\n",
		now,
		len(result.Bundle.Plans),
		len(result.Bundle.Loops),
		len(result.Bundle.Messages),
		result.Bundle.Metrics.LoopActiveLoops,
		result.Bundle.Metrics.LoopContextUtilization,
		len(result.Errors),
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

// liveDetectors returns the detector set used by --live. Same as the
// bundle ships when readers run RunAll; centralizing here means the
// CLI is one place to add a new detector without touching pkg/health.
func liveDetectors() []health.Detector {
	return []health.Detector{
		health.EmptyStopAfterToolCalls{},
		health.JSONInText{},
		health.ThinkingSpiral{},
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

// liveDefaultInterval is the default poll cadence. 10s is the bash-
// blob's de-facto cadence and matches the "did anything change?"
// feel without spamming the message-logger.
const liveDefaultInterval = 10 * time.Second

// liveMessageLimit caps the per-tick message-logger fetch. Smaller
// than the bundle default because live mode runs continuously and a
// 500-entry pull every 10s is wasteful when the detector set only
// needs the most recent activity.
const liveMessageLimit = 100
