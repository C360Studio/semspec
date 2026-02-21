# Trajectory Aggregation API Design

**Task**: OBS-006
**Author**: Architect Agent
**Date**: 2026-02-21
**Status**: Implemented (OBS-007)

**Implementation Status**: ✅ Core logic implemented, workflow manager integration pending

## Overview

This document defines the API contracts for workflow aggregation and context stats endpoints in the trajectory-api component. These endpoints aggregate LLM call data to provide workflow-level observability and prove context management effectiveness.

## Background

The `CallRecord` type now includes proper token counting fields:

```go
type CallRecord struct {
    PromptTokens     int  `json:"prompt_tokens"`
    CompletionTokens int  `json:"completion_tokens"`
    TotalTokens      int  `json:"total_tokens"`
    ContextBudget    int  `json:"context_budget,omitempty"`
    ContextTruncated bool `json:"context_truncated,omitempty"`
    Capability       string `json:"capability"`
    // ... other fields
}
```

The existing trajectory-api provides loop and trace-level aggregation. We need workflow-level aggregation to answer:
1. "How many tokens did this entire plan consume?"
2. "Is context truncation happening and where?"

## Workflow-to-Trace Correlation

Workflows correlate with traces via `TriggerPayload.TraceID`. When a workflow is triggered:

1. `workflow-api` creates a `TriggerPayload` with a `TraceID`
2. The trace ID propagates through all workflow steps
3. Each LLM call stores the `TraceID` in `CallRecord`

To aggregate by workflow slug:
1. Query the `WORKFLOW_EXECUTIONS` KV bucket for executions with matching slug
2. Extract all trace IDs from those executions
3. Aggregate `CallRecord` entries by those trace IDs

## API Endpoints

### 1. GET /trajectory-api/workflows/{slug}

Aggregate all LLM calls for a workflow plan by its slug.

#### Path Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| slug | string | Yes | URL-friendly plan identifier (e.g., "add-user-authentication") |

#### Query Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| format | string | "summary" | Response format: "summary" (metrics only) or "json" (include trace details) |

#### Response: WorkflowTrajectory

```yaml
WorkflowTrajectory:
  type: object
  required:
    - slug
    - status
    - phases
    - totals
    - trace_ids
  properties:
    slug:
      type: string
      description: Plan slug identifier
    status:
      type: string
      description: Current workflow status (drafting, approved, implementing, complete)
    phases:
      type: object
      description: Token breakdown by workflow phase
      properties:
        planning:
          $ref: '#/components/schemas/PhaseMetrics'
        review:
          $ref: '#/components/schemas/PhaseMetrics'
        execution:
          $ref: '#/components/schemas/PhaseMetrics'
    totals:
      $ref: '#/components/schemas/AggregateMetrics'
    trace_ids:
      type: array
      description: All trace IDs associated with this workflow
      items:
        type: string
    truncation_summary:
      $ref: '#/components/schemas/TruncationSummary'
    started_at:
      type: string
      format: date-time
      nullable: true
    completed_at:
      type: string
      format: date-time
      nullable: true
```

#### Supporting Schemas

```yaml
PhaseMetrics:
  type: object
  required:
    - tokens_in
    - tokens_out
    - call_count
    - duration_ms
  properties:
    tokens_in:
      type: integer
      description: Total prompt tokens for this phase
    tokens_out:
      type: integer
      description: Total completion tokens for this phase
    call_count:
      type: integer
      description: Number of LLM calls in this phase
    duration_ms:
      type: integer
      format: int64
      description: Total duration in milliseconds
    capabilities:
      type: object
      description: Token usage by capability type
      additionalProperties:
        $ref: '#/components/schemas/CapabilityMetrics'

CapabilityMetrics:
  type: object
  required:
    - tokens_in
    - tokens_out
    - call_count
  properties:
    tokens_in:
      type: integer
    tokens_out:
      type: integer
    call_count:
      type: integer
    truncated_count:
      type: integer
      description: Number of calls where context was truncated

AggregateMetrics:
  type: object
  required:
    - tokens_in
    - tokens_out
    - total_tokens
    - call_count
    - duration_ms
  properties:
    tokens_in:
      type: integer
      description: Total prompt tokens across all phases
    tokens_out:
      type: integer
      description: Total completion tokens across all phases
    total_tokens:
      type: integer
      description: Sum of tokens_in + tokens_out
    call_count:
      type: integer
      description: Total LLM calls
    duration_ms:
      type: integer
      format: int64
      description: Total duration in milliseconds

TruncationSummary:
  type: object
  required:
    - total_calls
    - truncated_calls
    - truncation_rate
  properties:
    total_calls:
      type: integer
      description: Total number of LLM calls with context budget set
    truncated_calls:
      type: integer
      description: Number of calls where context was truncated
    truncation_rate:
      type: number
      format: float
      description: Percentage of calls with truncation (0-100)
    by_capability:
      type: object
      description: Truncation rate by capability type
      additionalProperties:
        type: number
        format: float
```

#### Example Response

```json
{
  "slug": "add-user-authentication",
  "status": "complete",
  "phases": {
    "planning": {
      "tokens_in": 15234,
      "tokens_out": 3421,
      "call_count": 3,
      "duration_ms": 45230,
      "capabilities": {
        "planning": {"tokens_in": 15234, "tokens_out": 3421, "call_count": 3}
      }
    },
    "review": {
      "tokens_in": 28456,
      "tokens_out": 5123,
      "call_count": 4,
      "duration_ms": 62100
    },
    "execution": {
      "tokens_in": 142890,
      "tokens_out": 45670,
      "call_count": 12,
      "duration_ms": 312500,
      "capabilities": {
        "coding": {"tokens_in": 98000, "tokens_out": 32000, "call_count": 8, "truncated_count": 2},
        "writing": {"tokens_in": 44890, "tokens_out": 13670, "call_count": 4}
      }
    }
  },
  "totals": {
    "tokens_in": 186580,
    "tokens_out": 54214,
    "total_tokens": 240794,
    "call_count": 19,
    "duration_ms": 419830
  },
  "trace_ids": [
    "abc123def456...",
    "789ghi012jkl..."
  ],
  "truncation_summary": {
    "total_calls": 19,
    "truncated_calls": 2,
    "truncation_rate": 10.5,
    "by_capability": {
      "coding": 25.0,
      "planning": 0.0,
      "writing": 0.0
    }
  },
  "started_at": "2026-02-21T10:00:00Z",
  "completed_at": "2026-02-21T10:07:00Z"
}
```

---

### 2. GET /trajectory-api/context-stats

Get context utilization metrics to prove context management effectiveness.

#### Query Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| trace_id | string | No | Filter by specific trace ID |
| workflow | string | No | Filter by workflow slug |
| capability | string | No | Filter by capability type (planning, coding, writing, reviewing) |
| limit | integer | No | Maximum number of per-call details to return (default: 100) |

At least one of `trace_id` or `workflow` should be provided. If neither is provided, returns stats for the most recent 100 calls.

#### Response: ContextStats

```yaml
ContextStats:
  type: object
  required:
    - summary
    - by_capability
  properties:
    summary:
      $ref: '#/components/schemas/ContextSummary'
    by_capability:
      type: object
      description: Context utilization breakdown by capability type
      additionalProperties:
        $ref: '#/components/schemas/CapabilityContextStats'
    calls:
      type: array
      description: Per-call context details (only if format=json)
      items:
        $ref: '#/components/schemas/CallContextDetail'

ContextSummary:
  type: object
  required:
    - total_calls
    - calls_with_budget
    - avg_utilization
    - truncation_rate
    - total_budget
    - total_used
  properties:
    total_calls:
      type: integer
      description: Total number of LLM calls analyzed
    calls_with_budget:
      type: integer
      description: Calls that had a context budget set
    avg_utilization:
      type: number
      format: float
      description: Average context utilization percentage (used/budget * 100)
    truncation_rate:
      type: number
      format: float
      description: Percentage of calls where context was truncated
    total_budget:
      type: integer
      description: Sum of all context budgets
    total_used:
      type: integer
      description: Sum of all prompt tokens used

CapabilityContextStats:
  type: object
  required:
    - call_count
    - avg_utilization
    - truncation_rate
  properties:
    call_count:
      type: integer
      description: Number of calls for this capability
    avg_budget:
      type: integer
      description: Average context budget for this capability
    avg_used:
      type: integer
      description: Average tokens used for this capability
    avg_utilization:
      type: number
      format: float
      description: Average utilization percentage
    truncation_rate:
      type: number
      format: float
      description: Percentage of calls truncated
    max_utilization:
      type: number
      format: float
      description: Highest utilization seen

CallContextDetail:
  type: object
  required:
    - request_id
    - capability
    - budget
    - used
    - truncated
  properties:
    request_id:
      type: string
      description: Unique call identifier
    trace_id:
      type: string
      description: Trace correlation ID
    capability:
      type: string
      description: Capability type used
    model:
      type: string
      description: Model used for this call
    budget:
      type: integer
      description: Context budget (max tokens)
    used:
      type: integer
      description: Prompt tokens actually used
    utilization:
      type: number
      format: float
      description: Utilization percentage (used/budget * 100)
    truncated:
      type: boolean
      description: Whether context was truncated
    timestamp:
      type: string
      format: date-time
      description: When the call was made
```

#### Example Response

```json
{
  "summary": {
    "total_calls": 42,
    "calls_with_budget": 38,
    "avg_utilization": 72.5,
    "truncation_rate": 7.9,
    "total_budget": 3040000,
    "total_used": 2204000
  },
  "by_capability": {
    "planning": {
      "call_count": 8,
      "avg_budget": 128000,
      "avg_used": 45000,
      "avg_utilization": 35.2,
      "truncation_rate": 0.0,
      "max_utilization": 52.3
    },
    "coding": {
      "call_count": 24,
      "avg_budget": 64000,
      "avg_used": 58000,
      "avg_utilization": 90.6,
      "truncation_rate": 12.5,
      "max_utilization": 100.0
    },
    "writing": {
      "call_count": 6,
      "avg_budget": 32000,
      "avg_used": 18000,
      "avg_utilization": 56.3,
      "truncation_rate": 0.0,
      "max_utilization": 78.1
    },
    "reviewing": {
      "call_count": 4,
      "avg_budget": 64000,
      "avg_used": 42000,
      "avg_utilization": 65.6,
      "truncation_rate": 0.0,
      "max_utilization": 82.4
    }
  },
  "calls": [
    {
      "request_id": "call_abc123",
      "trace_id": "trace_xyz789",
      "capability": "coding",
      "model": "claude-3-opus",
      "budget": 64000,
      "used": 64000,
      "utilization": 100.0,
      "truncated": true,
      "timestamp": "2026-02-21T10:15:32Z"
    }
  ]
}
```

---

## Go Response Structs

The following Go structs should be added to `processor/trajectory-api/types.go`:

```go
// WorkflowTrajectory aggregates LLM call data for an entire workflow.
type WorkflowTrajectory struct {
    // Slug is the plan identifier.
    Slug string `json:"slug"`

    // Status is the current workflow status.
    Status string `json:"status"`

    // Phases contains token breakdown by workflow phase.
    Phases map[string]*PhaseMetrics `json:"phases"`

    // Totals contains aggregate metrics across all phases.
    Totals *AggregateMetrics `json:"totals"`

    // TraceIDs lists all trace IDs associated with this workflow.
    TraceIDs []string `json:"trace_ids"`

    // TruncationSummary summarizes context truncation across the workflow.
    TruncationSummary *TruncationSummary `json:"truncation_summary,omitempty"`

    // StartedAt is when the workflow began.
    StartedAt *time.Time `json:"started_at,omitempty"`

    // CompletedAt is when the workflow finished.
    CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// PhaseMetrics contains token metrics for a workflow phase.
type PhaseMetrics struct {
    // TokensIn is total prompt tokens for this phase.
    TokensIn int `json:"tokens_in"`

    // TokensOut is total completion tokens for this phase.
    TokensOut int `json:"tokens_out"`

    // CallCount is the number of LLM calls in this phase.
    CallCount int `json:"call_count"`

    // DurationMs is total duration in milliseconds.
    DurationMs int64 `json:"duration_ms"`

    // Capabilities breaks down usage by capability type.
    Capabilities map[string]*CapabilityMetrics `json:"capabilities,omitempty"`
}

// CapabilityMetrics contains metrics for a specific capability type.
type CapabilityMetrics struct {
    // TokensIn is total prompt tokens for this capability.
    TokensIn int `json:"tokens_in"`

    // TokensOut is total completion tokens for this capability.
    TokensOut int `json:"tokens_out"`

    // CallCount is the number of calls for this capability.
    CallCount int `json:"call_count"`

    // TruncatedCount is calls where context was truncated.
    TruncatedCount int `json:"truncated_count,omitempty"`
}

// AggregateMetrics contains totals across all phases.
type AggregateMetrics struct {
    // TokensIn is total prompt tokens.
    TokensIn int `json:"tokens_in"`

    // TokensOut is total completion tokens.
    TokensOut int `json:"tokens_out"`

    // TotalTokens is sum of TokensIn + TokensOut.
    TotalTokens int `json:"total_tokens"`

    // CallCount is total LLM calls.
    CallCount int `json:"call_count"`

    // DurationMs is total duration in milliseconds.
    DurationMs int64 `json:"duration_ms"`
}

// TruncationSummary summarizes context truncation statistics.
type TruncationSummary struct {
    // TotalCalls is total calls with context budget set.
    TotalCalls int `json:"total_calls"`

    // TruncatedCalls is calls where context was truncated.
    TruncatedCalls int `json:"truncated_calls"`

    // TruncationRate is percentage of truncated calls.
    TruncationRate float64 `json:"truncation_rate"`

    // ByCapability breaks down truncation rate by capability.
    ByCapability map[string]float64 `json:"by_capability,omitempty"`
}

// ContextStats provides context utilization metrics.
type ContextStats struct {
    // Summary contains aggregate context statistics.
    Summary *ContextSummary `json:"summary"`

    // ByCapability breaks down stats by capability type.
    ByCapability map[string]*CapabilityContextStats `json:"by_capability"`

    // Calls contains per-call details (only with format=json).
    Calls []CallContextDetail `json:"calls,omitempty"`
}

// ContextSummary contains aggregate context statistics.
type ContextSummary struct {
    // TotalCalls is total number of LLM calls analyzed.
    TotalCalls int `json:"total_calls"`

    // CallsWithBudget is calls that had a context budget set.
    CallsWithBudget int `json:"calls_with_budget"`

    // AvgUtilization is average context utilization percentage.
    AvgUtilization float64 `json:"avg_utilization"`

    // TruncationRate is percentage of calls truncated.
    TruncationRate float64 `json:"truncation_rate"`

    // TotalBudget is sum of all context budgets.
    TotalBudget int `json:"total_budget"`

    // TotalUsed is sum of all prompt tokens used.
    TotalUsed int `json:"total_used"`
}

// CapabilityContextStats contains context stats for a capability.
type CapabilityContextStats struct {
    // CallCount is number of calls for this capability.
    CallCount int `json:"call_count"`

    // AvgBudget is average context budget.
    AvgBudget int `json:"avg_budget,omitempty"`

    // AvgUsed is average tokens used.
    AvgUsed int `json:"avg_used,omitempty"`

    // AvgUtilization is average utilization percentage.
    AvgUtilization float64 `json:"avg_utilization"`

    // TruncationRate is percentage of calls truncated.
    TruncationRate float64 `json:"truncation_rate"`

    // MaxUtilization is highest utilization seen.
    MaxUtilization float64 `json:"max_utilization,omitempty"`
}

// CallContextDetail contains context details for a single call.
type CallContextDetail struct {
    // RequestID uniquely identifies the call.
    RequestID string `json:"request_id"`

    // TraceID is the trace correlation ID.
    TraceID string `json:"trace_id,omitempty"`

    // Capability is the capability type used.
    Capability string `json:"capability"`

    // Model is the model used for this call.
    Model string `json:"model,omitempty"`

    // Budget is the context budget (max tokens).
    Budget int `json:"budget"`

    // Used is prompt tokens actually used.
    Used int `json:"used"`

    // Utilization is utilization percentage.
    Utilization float64 `json:"utilization"`

    // Truncated indicates if context was truncated.
    Truncated bool `json:"truncated"`

    // Timestamp is when the call was made.
    Timestamp time.Time `json:"timestamp"`
}
```

---

## Phase Detection Logic

To categorize LLM calls into phases, use the following heuristics based on `Capability` and workflow context:

| Capability | Phase |
|------------|-------|
| planning | planning |
| reviewing | review |
| coding | execution |
| writing | execution |
| fast | (inherit from workflow stage) |

For more accurate phase detection, examine the trace structure:
1. Calls made during `plan-review-loop` workflow = planning/review phase
2. Calls made during `task-dispatcher` workflow = execution phase

---

## Implementation Notes

### Linking CallRecords to Workflows

1. **Query WORKFLOW_EXECUTIONS bucket** for executions where trigger contains the slug:
   ```go
   // Pseudo-code
   for key := range bucket.Keys() {
       exec := bucket.Get(key)
       if exec.Trigger.Data.Slug == slug {
           traceIDs = append(traceIDs, exec.Trigger.TraceID)
       }
   }
   ```

2. **Aggregate CallRecords by trace IDs**:
   ```go
   for _, traceID := range traceIDs {
       calls := llmCallsBucket.GetByPrefix(traceID + ".")
       aggregate(calls)
   }
   ```

### Configuration

Add to `trajectory-api` Config:

```go
type Config struct {
    // ... existing fields
    
    // WorkflowExecBucket is the KV bucket for workflow executions.
    WorkflowExecBucket string `json:"workflow_exec_bucket" schema:"type:string,description:KV bucket for workflow executions,category:basic,default:WORKFLOW_EXECUTIONS"`
}
```

### HTTP Handler Registration

```go
func (c *Component) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
    // Existing handlers
    mux.HandleFunc(prefix+"loops/", c.handleGetLoopTrajectory)
    mux.HandleFunc(prefix+"traces/", c.handleGetTraceTrajectory)
    
    // New handlers
    mux.HandleFunc(prefix+"workflows/", c.handleGetWorkflowTrajectory)
    mux.HandleFunc(prefix+"context-stats", c.handleGetContextStats)
}
```

---

## Error Responses

| Status | Condition |
|--------|-----------|
| 400 Bad Request | Invalid slug format or missing required query parameter |
| 404 Not Found | Workflow slug not found or no data available |
| 500 Internal Server Error | KV bucket access failure |

---

## OpenAPI YAML Addition

Add to `specs/openapi.v3.yaml`:

```yaml
  /trajectory-api/workflows/{slug}:
    get:
      summary: Get workflow trajectory
      description: Aggregates all LLM calls for a workflow plan, providing phase-level token breakdowns and truncation statistics
      tags:
        - Trajectory
      parameters:
        - name: slug
          in: path
          required: true
          description: URL-friendly plan identifier
          schema:
            type: string
        - name: format
          in: query
          description: Response format (summary or json for trace details)
          schema:
            type: string
            enum: [summary, json]
            default: summary
      responses:
        "200":
          description: Workflow trajectory with phase metrics
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/WorkflowTrajectory'
        "404":
          description: Workflow not found

  /trajectory-api/context-stats:
    get:
      summary: Get context utilization statistics
      description: Returns context utilization metrics to validate context management effectiveness
      tags:
        - Trajectory
      parameters:
        - name: trace_id
          in: query
          description: Filter by trace ID
          schema:
            type: string
        - name: workflow
          in: query
          description: Filter by workflow slug
          schema:
            type: string
        - name: capability
          in: query
          description: Filter by capability type
          schema:
            type: string
        - name: limit
          in: query
          description: Maximum per-call details to return
          schema:
            type: integer
            default: 100
      responses:
        "200":
          description: Context utilization statistics
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ContextStats'
```

---

## Summary

These two endpoints provide the observability needed to:

1. **Track workflow costs**: Total token usage per plan, broken down by phase
2. **Validate context management**: Prove that truncation is working correctly and identify over/under-utilization
3. **Debug performance**: Identify which capabilities consume the most tokens
4. **Optimize prompts**: Find workflows with high truncation rates for prompt engineering

The APIs build on existing infrastructure (LLM_CALLS KV bucket, trace correlation) and integrate cleanly with the trajectory-api component.

---

## Implementation Status (OBS-007)

### Completed

- ✅ Route handlers registered in `RegisterHTTPHandlers`
- ✅ `handleGetWorkflowTrajectory` - workflow aggregation endpoint
- ✅ `handleGetContextStats` - context utilization stats endpoint
- ✅ `buildWorkflowTrajectory` - phase-based aggregation logic
- ✅ `buildContextStats` - context utilization calculation
- ✅ `determinePhase` - capability to phase mapping
- ✅ All types defined in `types.go`
- ✅ Comprehensive test coverage

### Pending

The `/trajectory-api/workflows/{slug}` endpoint currently returns 404 because it requires integration with `workflow.Manager` to:
1. Look up workflows by slug
2. Extract trace IDs from workflow executions
3. Map workflow status to response

This integration will be completed in a follow-up task that wires `workflow.Manager` into the trajectory-api component.

### Current Functionality

**Working Now:**
- `/trajectory-api/context-stats?trace_id=X` - Full context stats by trace ID
- `/trajectory-api/context-stats?trace_id=X&capability=coding` - Filtered by capability
- All aggregation and calculation logic is implemented and tested

**Requires Workflow Manager:**
- `/trajectory-api/workflows/{slug}` - Will work once workflow manager is wired in
- `/trajectory-api/context-stats?workflow=slug` - Requires workflow-to-trace mapping

### Testing

All unit tests pass:
```bash
go test -v ./processor/trajectory-api
```

Test coverage includes:
- Phase detection (planning, review, execution)
- Token aggregation across capabilities
- Truncation rate calculation
- Context utilization metrics
- Capability-specific breakdowns
