# ADR-002: Async Response Delivery via NATS KV

## Status

Proposed (requires semstreams changes)

## Context

During UI E2E testing, we discovered that LLM responses from async workflows (like `/propose`) never reach the UI. Investigation revealed a fundamental gap in how completion data flows through the system.

### Current Architecture

```
User Command → agentic-dispatch → workflow-processor → agentic-loop
                                                            ↓
                                              AGENT_LOOPS KV (LoopInfo)
                                                            ↓
                                              SSE /activity stream → UI
```

The SSE activity stream watches the `AGENT_LOOPS` KV bucket and emits `loop_updated` events when entries change. This is the reactive mechanism for delivering async updates to clients.

### The Problem

**Two distinct issues were identified:**

1. **Correlation Gap (Workflow Path)**
   - Direct task submission: `agentic-dispatch` creates `loop_id`, returns it in `in_reply_to`, `agentic-loop` uses same ID
   - Workflow path: `/propose` creates `task_id`, workflow-processor triggers execution, `agentic-loop` creates NEW `loop_id`
   - UI receives `task_id` but SSE events use `loop_id` → no correlation

2. **Missing Response Content**
   - `LoopInfo` (stored in KV) contains: `loop_id`, `task_id`, `state`, `iterations`, `workflow_slug`...
   - `LoopCompletedEvent` (published to `agent.complete.*`) contains: `outcome`, `result` (LLM response)
   - The completion data never updates the KV, so SSE never delivers the response content

### Data Flow Analysis

| Stage | What Exists | What's Missing |
|-------|-------------|----------------|
| `/propose` response | `response_id`, `content` | `in_reply_to` (task_id) |
| `LoopInfo` in KV | `loop_id`, `task_id`, `state` | `outcome`, `result`, `error` |
| `LoopCompletedEvent` | All completion data | Not written to KV |
| SSE `loop_updated` | Full `LoopInfo` | Completion data |

## Decision

### Enrich LoopInfo with Completion Data

When a loop completes (success or failure), update the `AGENT_LOOPS` KV entry with the completion data. The existing SSE activity stream will automatically deliver the enriched data to clients.

### Proposed LoopInfo Changes (semstreams)

```go
type LoopInfo struct {
    // Existing fields
    LoopID        string    `json:"loop_id"`
    TaskID        string    `json:"task_id"`
    UserID        string    `json:"user_id"`
    ChannelType   string    `json:"channel_type"`
    ChannelID     string    `json:"channel_id"`
    State         string    `json:"state"`
    Iterations    int       `json:"iterations"`
    MaxIterations int       `json:"max_iterations"`
    CreatedAt     time.Time `json:"created_at"`
    WorkflowSlug  string    `json:"workflow_slug,omitempty"`
    WorkflowStep  string    `json:"workflow_step,omitempty"`

    // NEW: Completion data
    Outcome     string    `json:"outcome,omitempty"`      // success, failed, cancelled
    Result      string    `json:"result,omitempty"`       // LLM response content
    Error       string    `json:"error,omitempty"`        // Error message on failure
    CompletedAt time.Time `json:"completed_at,omitempty"`
}
```

### Implementation Location (semstreams)

In `processor/agentic-loop/component.go`, when handling `agent.complete.*` or `agent.failed.*`:

```go
func (c *Component) updateLoopWithResult(ctx context.Context, loopID, outcome, result string) error {
    entry, err := c.loopsBucket.Get(ctx, loopID)
    if err != nil {
        return err
    }

    var info LoopInfo
    json.Unmarshal(entry.Value(), &info)

    info.State = "completed"
    info.Outcome = outcome
    info.Result = result
    info.CompletedAt = time.Now()

    data, _ := json.Marshal(info)
    _, err = c.loopsBucket.Put(ctx, loopID, data)
    return err
}
```

### Correlation Fix (semspec)

Commands that trigger workflows should return `task_id` in `in_reply_to`:

```go
// commands/propose.go (and design.go, spec.go, tasks.go)
return agentic.UserResponse{
    ResponseID:  taskID,
    InReplyTo:   taskID,  // ← Add this
    Type:        agentic.ResponseTypeStatus,
    Content:     sb.String(),
    // ...
}
```

The UI can then match by `task_id` (which flows through to `LoopInfo.TaskID`) instead of requiring the `loop_id` upfront.

## Consequences

### Positive

- **Reactive updates work**: SSE automatically delivers completion data via existing KV watch mechanism
- **No new SSE event types**: Leverages existing `loop_updated` infrastructure
- **Correlation preserved**: `task_id` flows through the entire chain and is available in SSE events
- **Backward compatible**: New fields use `omitempty`, existing consumers unaffected

### Negative

- **KV size increase**: LLM responses (10KB-100KB+) stored in KV entries
- **Requires semstreams changes**: Can't be implemented in semspec alone

### Mitigations

- Add `max_age` TTL to `AGENT_LOOPS` bucket to auto-expire old entries
- Future optimization: Store large results in object store, reference in KV

## Files to Modify

### semstreams (required)

| File | Change |
|------|--------|
| `processor/agentic-dispatch/loop_tracker.go` | Add `Outcome`, `Result`, `Error`, `CompletedAt` to `LoopInfo` |
| `processor/agentic-loop/component.go` | Update KV on completion with result data |
| `agentic/loop_types.go` | If `LoopInfo` is defined here instead |

### semspec (after semstreams changes)

| File | Change |
|------|--------|
| `commands/propose.go` | Add `InReplyTo: taskID` |
| `commands/design.go` | Add `InReplyTo: taskID` |
| `commands/spec.go` | Add `InReplyTo: taskID` |
| `commands/tasks.go` | Add `InReplyTo: taskID` |
| `ui/src/lib/stores/messages.svelte.ts` | Match by `task_id` in addition to `loop_id` |

## Migration Path

1. **Phase 1**: Add new fields to `LoopInfo` (backward compatible, semstreams)
2. **Phase 2**: Update agentic-loop to populate fields on completion (semstreams)
3. **Phase 3**: Update semspec commands to set `InReplyTo`
4. **Phase 4**: Update UI to match by `task_id` and display `result`

## Alternatives Considered

### Separate SSE Event Type

Add `loop_result` event that watches `agent.complete.>` stream.

- Rejected: Adds complexity, requires parallel event handling in clients

### Direct Response Publishing

Have agentic-loop publish to `user.response.<channel>` on completion.

- Rejected: Different handling for CLI/HTTP paths, doesn't leverage KV reactivity

### UI Polling

UI fetches result via HTTP when state=completed.

- Rejected: Latency, extra requests, not reactive

## Related

- ADR-001: UI Observability Boundaries (defines what semspec-ui should show)
- semstreams: agentic-loop component
- semstreams: agentic-dispatch SSE activity handler
