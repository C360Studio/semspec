package health

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"sync"
	"time"
)

// CaptureResult holds the assembled bundle plus any per-source
// CaptureError records the orchestrator collected while assembling.
// Errors is non-fatal — the bundle field is populated even when
// individual sources failed; callers that need a strict bundle should
// check len(Errors) == 0 themselves.
//
// Trajectories holds the raw trajectory JSON bodies keyed by loop ID
// so a tarball writer (or a custom downstream) can lay them out.
// Bundle.TrajectoryRefs has the metadata pointers.
type CaptureResult struct {
	Bundle       *Bundle
	Trajectories map[string][]byte
	Errors       []*CaptureError
}

// Capture fans out to every configured source, assembles the bundle,
// and returns it with any non-fatal CaptureErrors collected. The
// orchestrator stamps Bundle.Bundle.CapturedAt and Bundle.Metrics.
// CapturedAt with one shared instant so bundle readers can use them
// interchangeably for cross-section correlation.
//
// The function only returns a non-nil error when assembly itself
// failed (e.g. context cancelled before any source completed). Any
// per-source failure becomes an entry in CaptureResult.Errors and
// the corresponding bundle section stays at its zero value.
func Capture(ctx context.Context, cfg CaptureConfig, http *http.Client, nats TrajectoryClient) (*CaptureResult, error) {
	now := time.Now().UTC()
	bundle := &Bundle{
		Bundle: BundleMeta{
			Format:     BundleFormat,
			CapturedAt: now,
			CapturedBy: defaultCapturedBy(cfg),
		},
		Diagnoses: []Diagnosis{},
	}
	// All bundle.Host writes happen on this orchestrator goroutine,
	// not the fan-out below. CaptureOllama is the only second writer;
	// keep both calls here so a future refactor that pushes Ollama
	// into the goroutine pool would obviously violate the invariant.
	bundle.Host = CaptureHost(buildVersionFallback)
	if !cfg.SkipOllama {
		hostInfo, state := CaptureOllama(ctx, cfg)
		bundle.Host.Ollama = hostInfo
		if state != nil && (len(state.Running) > 0 || state.LastError != "") {
			bundle.Ollama = state
		}
	}

	collector := newErrCollector()
	bucketResults := captureHTTPSources(ctx, cfg, http, bundle, now, collector)
	bundle.Plans = bucketResults["PLAN_STATES"]
	bundle.Loops = bucketResults["AGENT_LOOPS"]

	// Trajectories: serial walk of loop IDs from AGENT_LOOPS. Each request
	// has its own 5s timeout (inside FetchTrajectory) so a single wedged
	// loop can't block the whole capture; concurrency would be cheap, but
	// the agentic-loop responder is single-process and we'd just queue.
	trajResults := make(map[string][]byte)
	if nats != nil {
		if _, ok := bucketResults["AGENT_LOOPS"]; !ok {
			// AGENT_LOOPS fetch failed (collector already has a
			// kv:AGENT_LOOPS error) — record the causal link so a
			// reader sees "no trajectories" with a reason rather than
			// confusing it with "no loops were running."
			collector.add(&CaptureError{
				Source: "trajectories",
				Err:    errors.New("skipped: AGENT_LOOPS bucket unavailable"),
			})
		} else {
			for _, loop := range bundle.Loops {
				ref, body, err := captureTrajectory(ctx, nats, loop)
				if err != nil {
					collector.add(err)
					continue
				}
				if body == nil {
					continue
				}
				trajResults[ref.LoopID] = body
				bundle.TrajectoryRefs = append(bundle.TrajectoryRefs, ref)
			}
		}
	}

	// v1 default redactions run unconditionally — the env-var/auth-header
	// scrub is cheap and the failure mode (a leaked key in a shared
	// bundle) is unrecoverable. Heavier redactions (prompt content) are
	// deferred per ADR-034 §3 until external adopters require them.
	Redact(bundle)

	return &CaptureResult{
		Bundle:       bundle,
		Trajectories: trajResults,
		Errors:       collector.snapshot(),
	}, nil
}

// captureHTTPSources fans out the metrics, messages, and KV-bucket
// fetchers concurrently. Extracted from Capture to keep that function
// under the package's function-length budget.
//
// Concurrency invariant: each goroutine writes ONLY its dedicated
// bundle field (Metrics, Messages) or into bucketResults under
// bucketMu. NEVER write bundle.Plans / bundle.Loops from inside a
// goroutine — those are assigned by the orchestrator goroutine after
// wg.Wait so the post-wait read happens-after the goroutine's write
// to bucketResults. Future maintainers: a "simplification" that
// pushes Plans/Loops assignment into the goroutine breaks the memory-
// model ordering and the race detector won't catch it on every run.
func captureHTTPSources(
	ctx context.Context,
	cfg CaptureConfig,
	httpClient *http.Client,
	bundle *Bundle,
	stamp time.Time,
	collector *errCollector,
) map[string][]KVEntry {
	buckets := resolvedKVBuckets(cfg)
	subjects := resolvedMessageSubjects(cfg)
	bucketResults := make(map[string][]KVEntry, len(buckets))
	messageResults := make(map[string][]Message, len(subjects))
	var bucketMu, messageMu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(1 + len(subjects) + len(buckets))

	go func() {
		defer wg.Done()
		snap, err := FetchMetrics(ctx, httpClient, cfg.HTTPBaseURL)
		if err != nil {
			collector.add(&CaptureError{Source: "metrics", Err: err})
			return
		}
		snap.CapturedAt = stamp
		bundle.Metrics = snap
	}()

	for _, subject := range subjects {
		go func(pattern string) {
			defer wg.Done()
			msgs, err := FetchMessages(ctx, httpClient, cfg.HTTPBaseURL, cfg.MessageLimit, pattern)
			if err != nil {
				collector.add(&CaptureError{Source: "messages:" + pattern, Err: err})
				return
			}
			messageMu.Lock()
			messageResults[pattern] = msgs
			messageMu.Unlock()
		}(subject)
	}

	for _, bucket := range buckets {
		go func(name string) {
			defer wg.Done()
			entries, err := FetchKVBucket(ctx, httpClient, cfg.HTTPBaseURL, name)
			if err != nil {
				collector.add(&CaptureError{Source: "kv:" + name, Err: err})
				return
			}
			bucketMu.Lock()
			bucketResults[name] = entries
			bucketMu.Unlock()
		}(bucket)
	}
	wg.Wait()

	// Merge per-subject pulls into bundle.Messages, deduped by
	// sequence (in case patterns overlap) and sorted newest-first to
	// preserve the message-logger's wire convention. Detectors
	// re-sort by sequence ascending for chronological order, but
	// other readers (UIs, ad-hoc inspection) expect newest-first.
	bundle.Messages = mergeMessages(messageResults)

	return bucketResults
}

// captureTrajectory fetches one loop's trajectory and returns the
// TrajectoryRef + raw bytes. Returns (zero, nil, nil) for not-found —
// not-found is benign and shouldn't pollute the error log; the loop
// just won't appear in TrajectoryRefs. FetchTrajectory has already
// validated the body is well-formed JSON via the trajectoryMeta
// unmarshal, so this layer is purely orchestration.
func captureTrajectory(ctx context.Context, nats TrajectoryClient, loop KVEntry) (TrajectoryRef, []byte, *CaptureError) {
	body, ref, err := FetchTrajectory(ctx, nats, loop.Key)
	if errors.Is(err, errTrajectoryNotFound) {
		return TrajectoryRef{}, nil, nil
	}
	if err != nil {
		return TrajectoryRef{}, nil, &CaptureError{Source: "trajectory:" + loop.Key, Err: err}
	}
	ref.Filename = "trajectories/" + ref.LoopID + ".json"
	return ref, body, nil
}

// resolvedKVBuckets returns the bucket list to capture: cfg.KVBuckets
// if set, otherwise DefaultKVBuckets. Defensive copy so callers can't
// mutate the package default through a returned slice.
func resolvedKVBuckets(cfg CaptureConfig) []string {
	if len(cfg.KVBuckets) > 0 {
		out := make([]string, len(cfg.KVBuckets))
		copy(out, cfg.KVBuckets)
		return out
	}
	out := make([]string, len(DefaultKVBuckets))
	copy(out, DefaultKVBuckets)
	return out
}

// resolvedMessageSubjects returns the subject pattern list to pull:
// cfg.MessageSubjects if set, otherwise DefaultMessageSubjects.
// Same defensive-copy treatment as resolvedKVBuckets.
func resolvedMessageSubjects(cfg CaptureConfig) []string {
	if len(cfg.MessageSubjects) > 0 {
		out := make([]string, len(cfg.MessageSubjects))
		copy(out, cfg.MessageSubjects)
		return out
	}
	out := make([]string, len(DefaultMessageSubjects))
	copy(out, DefaultMessageSubjects)
	return out
}

// mergeMessages flattens per-subject pull results into a single
// slice, dedupes by Sequence (which is unique within the
// message-logger's append-only buffer), and sorts newest-first.
//
// Newest-first matches the wire format and what FetchMessages returns
// for a single pull, so a downstream detector can't tell whether the
// bundle.Messages came from one pull or many. Detectors that need
// chronological order re-sort by sequence ascending themselves
// (groupResponsesByLoop in agent_response_walk.go does this).
func mergeMessages(byPattern map[string][]Message) []Message {
	if len(byPattern) == 0 {
		return nil
	}
	seen := make(map[int64]struct{})
	var total int
	for _, msgs := range byPattern {
		total += len(msgs)
	}
	out := make([]Message, 0, total)
	for _, msgs := range byPattern {
		for _, m := range msgs {
			if _, dup := seen[m.Sequence]; dup {
				continue
			}
			seen[m.Sequence] = struct{}{}
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sequence > out[j].Sequence })
	return out
}

// defaultCapturedBy returns cfg.CapturedBy verbatim if set, else a
// "semspec-dev" sentinel that signals "this bundle wasn't tagged with
// a real version." Bundle readers can switch on the prefix.
func defaultCapturedBy(cfg CaptureConfig) string {
	if cfg.CapturedBy != "" {
		return cfg.CapturedBy
	}
	return "semspec-dev"
}

// buildVersionFallback is the version stamp used when runtime/debug
// build info is unreadable. CLI callers (`cmd/semspec watch`) can
// override CaptureConfig.CapturedBy with a linker-injected version
// for a stronger stamp.
const buildVersionFallback = "0.0.0-dev"

// errCollector aggregates per-source CaptureError records under a
// mutex so concurrent fetchers don't race when they fail. Snapshot
// returns a stable copy for the result.
type errCollector struct {
	mu   sync.Mutex
	errs []*CaptureError
}

func newErrCollector() *errCollector {
	return &errCollector{}
}

func (c *errCollector) add(err *CaptureError) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errs = append(c.errs, err)
}

func (c *errCollector) snapshot() []*CaptureError {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*CaptureError, len(c.errs))
	copy(out, c.errs)
	return out
}
