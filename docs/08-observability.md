# 08: Observability

This document covers semspec's observability features for tracking LLM calls, token usage, and workflow metrics.

## Overview

Semspec provides comprehensive observability through:

- **Trace context propagation** - Every request gets a trace ID that follows it through all components
- **LLM call recording** - All LLM interactions are captured with full metadata
- **Tool call tracking** - Tool executions are logged with timing and results
- **Workflow aggregation** - Metrics can be queried at the workflow level

## Trace Context Propagation

Every workflow execution in semspec is assigned a trace ID that propagates through all components. This enables:

- Correlating LLM calls across multiple components
- Debugging end-to-end request flows
- Aggregating metrics per workflow

### How Trace IDs Flow

```
User Request
     │
     ▼
┌─────────────────┐
│ agentic-dispatch│ ← Generates trace_id
└────────┬────────┘
         │ trace_id in message
         ▼
┌─────────────────┐
│ plan-coordinator│ ← Records trace_id in plan.json
└────────┬────────┘
         │ forwards trace_id
         ▼
┌─────────────────┐
│    planner      │ ← Uses llm.WithTraceContext()
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  plan-reviewer  │ ← Uses llm.WithTraceContext()
└─────────────────┘
```

### Components with Trace Support

All LLM-calling components inject trace context before making LLM calls:

| Component | Trace Fields |
|-----------|-------------|
| `plan-coordinator` | TraceID, LoopID |
| `planner` | TraceID, LoopID |
| `plan-reviewer` | TraceID, LoopID |
| `task-generator` | TraceID, LoopID |
| `question-answerer` | TraceID, LoopID |

### Code Pattern

Components use `llm.WithTraceContext()` to inject trace information:

```go
// Inject trace context before LLM calls
llmCtx := ctx
if trigger.TraceID != "" || trigger.LoopID != "" {
    llmCtx = llm.WithTraceContext(ctx, llm.TraceContext{
        TraceID: trigger.TraceID,
        LoopID:  trigger.LoopID,
    })
}

// LLM calls made with llmCtx are automatically tracked
result, err := c.llmClient.Complete(llmCtx, request)
```

## LLM Call Recording

Every LLM call is recorded to the `LLM_CALLS` JetStream KV bucket with:

| Field | Description |
|-------|-------------|
| `request_id` | Unique call identifier |
| `trace_id` | Correlation ID for the request flow |
| `loop_id` | Agent loop ID (if applicable) |
| `model` | Model name used |
| `provider` | LLM provider (openai, anthropic, etc.) |
| `capability` | Requested capability (planning, reviewing, coding) |
| `prompt_tokens` | Input token count |
| `completion_tokens` | Output token count |
| `duration_ms` | Call duration in milliseconds |
| `started_at` | Call start timestamp |
| `finish_reason` | Why the model stopped (stop, length, etc.) |
| `context_budget` | Max tokens allowed for context |
| `context_truncated` | Whether context was truncated |
| `retries` | Number of retry attempts |
| `error` | Error message if failed |

## Tool Call Recording

Tool executions are recorded to the `TOOL_CALLS` KV bucket:

| Field | Description |
|-------|-------------|
| `call_id` | Unique call identifier |
| `trace_id` | Correlation ID |
| `loop_id` | Agent loop ID |
| `tool_name` | Name of the tool executed |
| `status` | Result status (success, error) |
| `duration_ms` | Execution duration |
| `started_at` | Execution start timestamp |
| `result` | Tool output (truncated) |
| `error` | Error message if failed |

## Trajectory API

The `trajectory-api` component provides HTTP endpoints for querying observability data.

### Base URL

```
http://localhost:8080/trajectory-api/
```

### Endpoints

#### GET /loops/{loop_id}

Query trajectory data for a specific agent loop.

**Parameters:**
- `loop_id` (path) - Agent loop identifier
- `format` (query) - `summary` (default) or `json` for detailed entries

**Response:**
```json
{
  "loop_id": "loop-abc123",
  "trace_id": "trace-xyz789",
  "steps": 5,
  "tool_calls": 12,
  "model_calls": 5,
  "tokens_in": 45000,
  "tokens_out": 8500,
  "duration_ms": 32000,
  "status": "completed",
  "started_at": "2024-01-15T10:30:00Z",
  "ended_at": "2024-01-15T10:30:32Z"
}
```

With `format=json`, includes detailed entries:
```json
{
  "entries": [
    {
      "type": "model_call",
      "timestamp": "2024-01-15T10:30:01Z",
      "duration_ms": 2500,
      "model": "gpt-4",
      "capability": "planning",
      "tokens_in": 8000,
      "tokens_out": 1500,
      "finish_reason": "stop"
    },
    {
      "type": "tool_call",
      "timestamp": "2024-01-15T10:30:04Z",
      "duration_ms": 150,
      "tool_name": "file_read",
      "status": "success"
    }
  ]
}
```

#### GET /traces/{trace_id}

Query trajectory data for a specific trace across all components.

**Parameters:**
- `trace_id` (path) - Trace correlation identifier
- `format` (query) - `summary` (default) or `json`

**Response:** Same structure as `/loops/{loop_id}`

#### GET /workflows/{slug}

Query aggregated metrics for an entire workflow. This endpoint links plan executions to their LLM calls via the `ExecutionTraceIDs` field stored in each plan.

**Parameters:**
- `slug` (path) - Workflow/plan slug identifier

**Response:**
```json
{
  "slug": "add-user-auth",
  "status": "approved",
  "trace_ids": ["trace-abc", "trace-def"],
  "phases": {
    "planning": {
      "tokens_in": 12000,
      "tokens_out": 3500,
      "call_count": 2,
      "duration_ms": 8500,
      "capabilities": {
        "planning": {
          "tokens_in": 12000,
          "tokens_out": 3500,
          "call_count": 2
        }
      }
    },
    "review": {
      "tokens_in": 8000,
      "tokens_out": 2000,
      "call_count": 1,
      "duration_ms": 4200,
      "capabilities": {
        "reviewing": {
          "tokens_in": 8000,
          "tokens_out": 2000,
          "call_count": 1,
          "truncated_count": 0
        }
      }
    }
  },
  "totals": {
    "tokens_in": 20000,
    "tokens_out": 5500,
    "total_tokens": 25500,
    "call_count": 3,
    "duration_ms": 12700
  },
  "truncation_summary": {
    "total_calls": 3,
    "truncated_calls": 0,
    "truncation_rate": 0,
    "by_capability": {}
  },
  "started_at": "2024-01-15T10:00:00Z",
  "completed_at": "2024-01-15T10:05:00Z"
}
```

#### GET /context-stats

Query context utilization statistics for proving context management effectiveness.

**Parameters:**
- `trace_id` (query) - Filter by trace ID
- `workflow` (query) - Filter by workflow slug
- `capability` (query) - Filter by capability type
- `format` (query) - `summary` (default) or `json` for per-call details

At least one of `trace_id` or `workflow` is required.

**Response:**
```json
{
  "summary": {
    "total_calls": 15,
    "calls_with_budget": 12,
    "avg_utilization": 72.5,
    "truncation_rate": 8.3,
    "total_budget": 480000,
    "total_used": 348000
  },
  "by_capability": {
    "planning": {
      "call_count": 3,
      "avg_budget": 40000,
      "avg_used": 32000,
      "avg_utilization": 80.0,
      "truncation_rate": 0,
      "max_utilization": 92.5
    },
    "coding": {
      "call_count": 9,
      "avg_budget": 40000,
      "avg_used": 28000,
      "avg_utilization": 70.0,
      "truncation_rate": 11.1,
      "max_utilization": 98.2
    }
  }
}
```

With `format=json`, includes per-call details:
```json
{
  "calls": [
    {
      "request_id": "req-123",
      "trace_id": "trace-abc",
      "capability": "planning",
      "model": "gpt-4",
      "budget": 40000,
      "used": 37000,
      "utilization": 92.5,
      "truncated": false,
      "timestamp": "2024-01-15T10:30:00Z"
    }
  ]
}
```

## Workflow Trace Tracking

Plans store their execution trace IDs in the `execution_trace_ids` field:

```json
{
  "slug": "add-user-auth",
  "status": "approved",
  "execution_trace_ids": [
    "trace-abc123",
    "trace-def456"
  ]
}
```

This enables:
- Querying all LLM calls for a workflow via `GET /workflows/{slug}`
- Tracking multiple executions of the same plan
- Aggregating metrics across retries and revisions

## Usage Examples

### Debug a Specific Request

```bash
# Get trace ID from a recent workflow
curl -s "http://localhost:8080/trajectory-api/workflows/my-plan" | jq .trace_ids

# Query detailed trajectory
curl "http://localhost:8080/trajectory-api/traces/trace-abc123?format=json"
```

### Analyze Token Usage

```bash
# Get workflow-level token breakdown
curl -s "http://localhost:8080/trajectory-api/workflows/my-plan" | jq '.totals'

# Check context utilization
curl -s "http://localhost:8080/trajectory-api/context-stats?workflow=my-plan" | jq '.summary'
```

### Compare Phases

```bash
# Compare planning vs execution costs
curl -s "http://localhost:8080/trajectory-api/workflows/my-plan" | \
  jq '{planning: .phases.planning.tokens_in, execution: .phases.execution.tokens_in}'
```

### Monitor Truncation

```bash
# Check if context is being truncated
curl -s "http://localhost:8080/trajectory-api/context-stats?workflow=my-plan" | \
  jq '.by_capability | to_entries | map(select(.value.truncation_rate > 0))'
```

## KV Bucket Configuration

The trajectory-api reads from these JetStream KV buckets:

| Bucket | Purpose | Key Format |
|--------|---------|------------|
| `LLM_CALLS` | LLM call records | `{trace_id}.{request_id}` |
| `TOOL_CALLS` | Tool execution records | `{trace_id}.{call_id}` |
| `AGENT_LOOPS` | Agent loop state | `{loop_id}` |

These buckets are created automatically when the corresponding components start.

## Trajectory Comparison

For comparing model performance across the same task, see the comparison feature which groups runs by `comparison_id`. This enables side-by-side analysis of different models completing identical tasks.

## Best Practices

1. **Always include trace context** - Ensure trigger payloads include `trace_id` and `loop_id` fields
2. **Query at the right level** - Use `/loops/` for agent-level debugging, `/workflows/` for cost analysis
3. **Monitor truncation rates** - High truncation rates may indicate context budget issues
4. **Track capability breakdown** - Different capabilities have different token profiles
