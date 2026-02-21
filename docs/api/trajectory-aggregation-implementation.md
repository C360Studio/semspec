# Trajectory Aggregation Implementation Summary

**Task**: OBS-007
**Author**: go-developer agent
**Date**: 2026-02-21
**Status**: Core implementation complete, workflow integration pending

## Overview

Implemented workflow-level aggregation and context statistics endpoints for the trajectory-api component. These endpoints provide observability into token usage, context truncation, and capability-specific metrics.

## Implementation Details

### 1. New HTTP Endpoints

#### GET /trajectory-api/workflows/{slug}

Aggregates all LLM calls for a workflow by slug.

**Status**: Partially implemented - core logic complete, workflow manager integration pending

**Implementation**:
- Route registered in `RegisterHTTPHandlers`
- Handler `handleGetWorkflowTrajectory` implemented
- Aggregation logic in `buildWorkflowTrajectory` function
- Phase detection via `determinePhase` helper

**Currently returns**: 404 (requires workflow.Manager integration)

**When complete, will return**:
```json
{
  "slug": "add-user-authentication",
  "status": "approved",
  "phases": {
    "planning": {
      "tokens_in": 15000,
      "tokens_out": 3000,
      "call_count": 3,
      "duration_ms": 10000,
      "capabilities": {
        "planning": {
          "tokens_in": 15000,
          "tokens_out": 3000,
          "call_count": 3
        }
      }
    },
    "execution": {
      "tokens_in": 82000,
      "tokens_out": 24000,
      "call_count": 9,
      "duration_ms": 180000
    }
  },
  "totals": {
    "tokens_in": 186580,
    "tokens_out": 54214,
    "total_tokens": 240794,
    "call_count": 19,
    "duration_ms": 419830
  },
  "truncation_summary": {
    "total_calls": 19,
    "truncated_calls": 2,
    "truncation_rate": 10.5,
    "by_capability": {
      "coding": 25.0,
      "planning": 0.0
    }
  },
  "trace_ids": ["abc123...", "def456..."]
}
```

#### GET /trajectory-api/context-stats

Returns context utilization metrics.

**Status**: Fully functional with trace_id filter

**Query Parameters**:
- `trace_id` - Filter by trace (working)
- `workflow` - Filter by workflow slug (pending workflow manager)
- `capability` - Filter by capability type (working)
- `format` - "summary" or "json" for per-call details (working)

**Implementation**:
- Route registered in `RegisterHTTPHandlers`
- Handler `handleGetContextStats` implemented
- Calculation logic in `buildContextStats` function

**Example Response**:
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
    }
  }
}
```

### 2. Core Functions

#### buildWorkflowTrajectory

Aggregates LLM calls into workflow-level metrics.

**Features**:
- Phase-based aggregation (planning, review, execution)
- Capability-specific breakdowns within each phase
- Token totals (prompt, completion, total)
- Duration aggregation
- Truncation summary with per-capability rates

**Phase Detection Logic**:
```go
func determinePhase(capability string) string {
    switch capability {
    case "planning":
        return "planning"
    case "reviewing":
        return "review"
    case "coding", "writing":
        return "execution"
    default:
        return "execution"
    }
}
```

#### buildContextStats

Calculates context utilization metrics.

**Features**:
- Average utilization percentage across all calls
- Truncation rate (percentage of calls truncated)
- Per-capability breakdowns
- Budget vs. usage tracking
- Optional per-call details

**Metrics Calculated**:
- Total/average budget and usage
- Utilization = (used / budget) * 100
- Truncation rate = (truncated / total) * 100
- Max utilization per capability

### 3. Type Definitions

All types defined in `/Users/coby/Code/c360/semspec/processor/trajectory-api/types.go`:

**Response Types**:
- `WorkflowTrajectory` - Workflow-level aggregation
- `PhaseMetrics` - Per-phase token breakdown
- `CapabilityMetrics` - Per-capability metrics
- `AggregateMetrics` - Overall totals
- `TruncationSummary` - Truncation statistics
- `ContextStats` - Context utilization metrics
- `ContextSummary` - Aggregate context stats
- `CapabilityContextStats` - Per-capability context stats
- `CallContextDetail` - Per-call context details

## Testing

Comprehensive test coverage added to `http_test.go`:

**Test Cases**:
- `TestHandleGetContextStats` - Handler behavior
- `TestBuildWorkflowTrajectory` - Phase aggregation logic
- `TestBuildContextStats` - Context calculation logic

**All tests passing**:
```bash
go test -v ./processor/trajectory-api
# PASS: 39 tests
```

## Pending Work

### Workflow Manager Integration

To make `/trajectory-api/workflows/{slug}` fully functional:

1. Wire `workflow.Manager` into `trajectory-api` Component
2. Add method to get trace IDs from workflow executions
3. Query WORKFLOW_EXECUTIONS KV bucket by slug
4. Extract trace IDs and workflow metadata

**Suggested approach**:
```go
// In component.go
type Component struct {
    // ... existing fields
    workflowMgr *workflow.Manager
}

// In http.go
func (c *Component) handleGetWorkflowTrajectory(...) {
    // Get workflow from manager
    plan, err := c.workflowMgr.LoadPlan(ctx, slug)

    // Get trace IDs from executions (new method needed)
    traceIDs, err := c.getWorkflowTraceIDs(ctx, slug)

    // Get calls by trace IDs
    calls := []
    for _, traceID := range traceIDs {
        traceCalls, _ := c.getLLMCallsByTraceID(ctx, traceID)
        calls = append(calls, traceCalls...)
    }

    // Build response
    wt := c.buildWorkflowTrajectory(slug, plan.Status, traceIDs, calls, ...)
}
```

## Files Modified

- `/Users/coby/Code/c360/semspec/processor/trajectory-api/http.go` - Added handlers and aggregation logic
- `/Users/coby/Code/c360/semspec/processor/trajectory-api/http_test.go` - Added comprehensive tests
- `/Users/coby/Code/c360/semspec/processor/trajectory-api/types.go` - Types already defined by architect
- `/Users/coby/Code/c360/semspec/docs/api/trajectory-aggregation-api.md` - Updated status

## Usage Examples

### Get Context Stats by Trace

```bash
curl http://localhost:8080/trajectory-api/context-stats?trace_id=abc123
```

### Get Context Stats with Capability Filter

```bash
curl http://localhost:8080/trajectory-api/context-stats?trace_id=abc123&capability=coding
```

### Get Per-Call Details

```bash
curl http://localhost:8080/trajectory-api/context-stats?trace_id=abc123&format=json
```

### Get Workflow Trajectory (pending)

```bash
# Will work after workflow manager integration
curl http://localhost:8080/trajectory-api/workflows/add-user-authentication
```

## Next Steps

1. Create follow-up task to wire workflow.Manager into trajectory-api
2. Implement method to query WORKFLOW_EXECUTIONS by slug
3. Test workflow endpoint with real workflow data
4. Update OpenAPI spec via `task generate:openapi`
5. Integration testing with full workflow lifecycle

## Success Criteria

- ✅ Core aggregation logic implemented
- ✅ Context stats endpoint fully functional with trace_id
- ✅ All unit tests passing
- ✅ Type definitions complete
- ✅ Documentation updated
- ⏳ Workflow endpoint integration (pending workflow manager)
