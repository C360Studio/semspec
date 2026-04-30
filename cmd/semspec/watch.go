package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/c360studio/semspec/pkg/health"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/spf13/cobra"
)

// watchCmd builds the `semspec watch` subcommand. Today only
// `--bundle <path>` is implemented (one-shot capture to a tarball);
// `--live` is reserved for the heartbeat TTY mode that lands later
// in the ADR-034 cadence.
func watchCmd() *cobra.Command {
	var (
		bundlePath string
		httpURL    string
		natsURL    string
		limit      int
		skipOllama bool
	)
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Capture a diagnostic bundle from a running semspec",
		Long: `watch captures live state from a running semspec instance into a
tarball that can be shared for offline diagnosis.

The default form (--bundle <path>) writes a single .tar.gz with
bundle.json plus per-loop trajectory files. See ADR-034 for the
schema and the detector library that consumes it.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if bundlePath == "" {
				return errors.New("--bundle <path> is required")
			}
			return runWatchBundle(cmd.Context(), watchBundleConfig{
				BundlePath: bundlePath,
				HTTPURL:    httpURL,
				NATSURL:    natsURL,
				Limit:      limit,
				SkipOllama: skipOllama,
			})
		},
	}
	cmd.Flags().StringVar(&bundlePath, "bundle", "", "Output path for the .tar.gz bundle (required)")
	cmd.Flags().StringVar(&httpURL, "http", "http://localhost:8080", "Semspec HTTP gateway URL")
	cmd.Flags().StringVar(&natsURL, "nats", "nats://localhost:4222", "NATS URL for trajectory queries (empty to skip trajectories)")
	cmd.Flags().IntVar(&limit, "limit", 0, "Message-logger entry cap (0 = library default)")
	cmd.Flags().BoolVar(&skipOllama, "skip-ollama", false, "Skip the ollama --version / ollama ps probe")
	return cmd
}

// watchBundleConfig is the parameter object for runWatchBundle. Mirrors
// the cobra flags so the runner is testable without parsing argv.
type watchBundleConfig struct {
	BundlePath string
	HTTPURL    string
	NATSURL    string
	Limit      int
	SkipOllama bool
}

// runWatchBundle is the work-loop for `semspec watch --bundle`. Connects
// to NATS best-effort (a missing connection just skips the trajectory
// section), runs Capture, writes the tarball to disk, and prints a
// short summary to stderr so the operator sees what landed.
func runWatchBundle(ctx context.Context, cfg watchBundleConfig) error {
	ctx, cancel := context.WithTimeout(ctx, watchBundleTimeout)
	defer cancel()

	nats, natsCloser := dialNATSForBundle(ctx, cfg.NATSURL)
	if natsCloser != nil {
		defer natsCloser()
	}

	captureCfg := health.CaptureConfig{
		HTTPBaseURL:  cfg.HTTPURL,
		MessageLimit: cfg.Limit,
		CapturedBy:   "semspec-v" + Version,
		SkipOllama:   cfg.SkipOllama,
	}
	httpClient := &http.Client{Timeout: watchHTTPTimeout}

	result, err := health.Capture(ctx, captureCfg, httpClient, nats)
	if err != nil {
		return fmt.Errorf("capture: %w", err)
	}

	out, err := os.Create(cfg.BundlePath)
	if err != nil {
		return fmt.Errorf("open bundle output: %w", err)
	}
	defer out.Close()
	if err := health.WriteTarball(out, result); err != nil {
		return fmt.Errorf("write tarball: %w", err)
	}

	summarizeBundle(cfg.BundlePath, result)
	return nil
}

// dialNATSForBundle is a best-effort dial. We treat NATS as optional
// because adopters running an offline replay may not have a daemon up;
// the bundle is still useful without trajectories. A returned nil
// client is a valid input to health.Capture.
func dialNATSForBundle(ctx context.Context, natsURL string) (health.TrajectoryClient, func()) {
	if natsURL == "" {
		return nil, nil
	}
	client, err := natsclient.NewClient(natsURL, natsclient.WithMaxReconnects(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: nats client init failed (%v); trajectories will be skipped\n", err)
		return nil, nil
	}
	if err := client.Connect(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "warn: nats connect failed (%v); trajectories will be skipped\n", err)
		return nil, nil
	}
	return client, func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer closeCancel()
		_ = client.Close(closeCtx)
	}
}

// summarizeBundle prints a one-block summary so the operator confirms
// what's in the tarball before they ship it. Goes to stderr so a
// future `--bundle -` mode (stream to stdout) stays clean.
func summarizeBundle(path string, result *health.CaptureResult) {
	b := result.Bundle
	fmt.Fprintf(os.Stderr,
		"wrote %s\n  format=%s captured_at=%s captured_by=%s\n  plans=%d loops=%d messages=%d trajectories=%d errors=%d\n",
		path,
		b.Bundle.Format,
		b.Bundle.CapturedAt.Format(time.RFC3339),
		b.Bundle.CapturedBy,
		len(b.Plans), len(b.Loops), len(b.Messages), len(b.TrajectoryRefs),
		len(result.Errors),
	)
	for _, e := range result.Errors {
		fmt.Fprintf(os.Stderr, "  warn: %s\n", e.Error())
	}
}

// watchBundleTimeout caps the whole capture run. Generous because a
// real run with a slow NATS daemon + large message-logger may take a
// few seconds; the bundle still returns a useful partial result if
// individual fetchers time out internally.
const watchBundleTimeout = 30 * time.Second

// watchHTTPTimeout caps each HTTP source request. Tighter than the
// outer timeout so a wedged endpoint can't consume the whole budget.
const watchHTTPTimeout = 10 * time.Second
