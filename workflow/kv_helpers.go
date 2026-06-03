package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/retry"
	"github.com/nats-io/nats.go/jetstream"
)

// WaitForKVBucket retries opening a KV bucket until it exists or ctx is cancelled.
// Components that watch a bucket owned by another component use this to handle
// start-order races. Should move to natsclient as a framework primitive.
func WaitForKVBucket(ctx context.Context, js jetstream.JetStream, bucket string) (jetstream.KeyValue, error) {
	return retry.DoWithResult(ctx, retry.Config{
		MaxAttempts:  30,
		InitialDelay: 200 * time.Millisecond,
		MaxDelay:     2 * time.Second,
		Multiplier:   1.5,
	}, func() (jetstream.KeyValue, error) {
		kv, err := js.KeyValue(ctx, bucket)
		if err != nil {
			return nil, fmt.Errorf("bucket %s: %w", bucket, err)
		}
		return kv, nil
	})
}

// WaitForStream retries looking up a JetStream stream until it exists or ctx
// is cancelled. Use this before setting up a consumer on a stream owned by
// another component (e.g., sandbox / qa-runner subscribing to WORKFLOW which
// plan-manager creates). Same retry budget as WaitForKVBucket — ~30 attempts,
// exponential backoff capped at 2s — so the caller blocks at most ~45s.
func WaitForStream(ctx context.Context, js jetstream.JetStream, name string) (jetstream.Stream, error) {
	return retry.DoWithResult(ctx, retry.Config{
		MaxAttempts:  30,
		InitialDelay: 200 * time.Millisecond,
		MaxDelay:     2 * time.Second,
		Multiplier:   1.5,
	}, func() (jetstream.Stream, error) {
		s, err := js.Stream(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("stream %s: %w", name, err)
		}
		return s, nil
	})
}

// ClaimPlanStatus sends a plan.mutation.claim request to plan-manager to atomically
// transition a plan to an in-progress status. Returns true if the claim succeeded.
// On failure (already claimed, invalid transition, network error), returns false
// and logs at Debug level — callers should skip processing.
func ClaimPlanStatus(ctx context.Context, nc *natsclient.Client, slug string, target Status, logger *slog.Logger) bool {
	req, _ := json.Marshal(struct {
		Slug   string `json:"slug"`
		Status Status `json:"status"`
	}{Slug: slug, Status: target})

	resp, err := nc.RequestWithRetry(ctx, "plan.mutation.claim", req, 5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		logger.Debug("Claim request failed", "slug", slug, "status", target, "error", err)
		return false
	}

	var result struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(resp, &result); err != nil || !result.Success {
		logger.Debug("Claim rejected", "slug", slug, "status", target, "error", result.Error)
		return false
	}

	logger.Info("Claimed plan for processing", "slug", slug, "status", target)
	return true
}

// ClaimStoryStatus sends a plan.mutation.story.status request to plan-manager
// to atomically transition a Story's lifecycle state. Returns true if the
// transition succeeded — used by requirement-executor for the ADR-044
// reservation pattern: claim Executing before dispatching the dev loop so
// N parallel executors covering the same M:N Story can't all dispatch
// simultaneously. Returns false on contention, invalid transition, or
// network error; callers should treat false as "another executor owns
// this Story; skip dispatch."
func ClaimStoryStatus(ctx context.Context, nc *natsclient.Client, slug, storyID string, target StoryStatus, logger *slog.Logger) bool {
	req, _ := json.Marshal(struct {
		Slug    string      `json:"slug"`
		StoryID string      `json:"story_id"`
		Target  StoryStatus `json:"target"`
	}{Slug: slug, StoryID: storyID, Target: target})

	resp, err := nc.RequestWithRetry(ctx, "plan.mutation.story.status", req, 5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		logger.Debug("Story status claim request failed",
			"slug", slug, "story_id", storyID, "target", target, "error", err)
		return false
	}

	var result struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(resp, &result); err != nil || !result.Success {
		logger.Debug("Story status claim rejected",
			"slug", slug, "story_id", storyID, "target", target, "error", result.Error)
		return false
	}

	logger.Info("Story status transitioned",
		"slug", slug, "story_id", storyID, "target", target)
	return true
}
