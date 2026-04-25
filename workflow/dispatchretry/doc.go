// Package dispatchretry provides per-key retry bookkeeping for components
// that re-dispatch agentic LLM loops on failure (parse error, agent loop
// outcome != success, semantic invalid).
//
// # Why a new package
//
// semstreams's pkg/retry covers a different shape: synchronous "call this
// function N times with exponential backoff, return the result." That fits
// HTTP/NATS request retries inside a single goroutine.
//
// Our retry crosses event boundaries:
//
//	NATS dispatch → agent loop → KV watch fires → handleLoopCompletion →
//	retryOrFail → NATS dispatch (next attempt) → ...
//
// State has to persist across separate event arrivals, keyed by something
// the caller chooses (plan slug, slug+requirement_id, etc.). The retry
// counter has to be authoritative across goroutines without losing the
// "last completed loop ID" needed to drop stale completions.
//
// # Why this matters
//
// Pre-WS2 (commits up to f805e81), each LLM-dispatching component
// hand-rolled the same sync.Map plumbing. qa-reviewer shipped with a
// count-reset bug — `dispatchReviewer` re-stored the entry with count=0,
// turning MaxReviewRetries into "infinite" and producing OOM-class storms
// when QA couldn't converge. The other 6 components didn't have the
// reset bug but all lacked backoff, leaving a thundering-herd window
// whenever multiple plans failed simultaneously. Centralizing the
// primitive makes both classes of bug structurally impossible.
//
// # Usage shape
//
//	state := dispatchretry.New(dispatchretry.Config{
//	    MaxRetries:     2,
//	    BackoffMs:      200,
//	})
//
//	// On initial trigger.
//	entry, fresh := state.Track(slug, plan)
//	if !fresh {
//	    return // already in flight; LoadOrStore-style dedup.
//	}
//	loopID := dispatchAgent(ctx, entry.Payload.(*workflow.Plan))
//	state.SetActiveLoop(slug, loopID)
//
//	// On loop completion.
//	if state.IsStaleLoop(slug, loop.TaskID) {
//	    return // re-dispatch already started a fresh loop.
//	}
//	if loop.Outcome != agentic.OutcomeSuccess {
//	    entry, ok := state.Tick(ctx, slug)
//	    if !ok {
//	        publishFailClosed(...)
//	        return
//	    }
//	    loopID := dispatchAgent(ctx, entry.Payload.(*workflow.Plan))
//	    state.SetActiveLoop(slug, loopID)
//	    return
//	}
//	state.Clear(slug)
//
// The helper is deliberately ignorant of NATS, KV, plans, and lessons —
// component-specific recovery (e.g. PLAN_STATES re-hydration on cold
// start) stays in the component, which calls Track with the recovered
// payload before the first dispatch.
package dispatchretry
