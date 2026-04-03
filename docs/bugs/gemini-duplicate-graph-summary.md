# Gemini 400: Duplicate function declaration for graph_summary

## Status: OPEN

## Severity: Critical (blocks all Gemini LLM calls)

## Summary

Gemini returns HTTP 400 on the first LLM request:
```
Duplicate function declaration found: graph_summary
```

The tool `graph_summary` is registered once in `tools/workflow/register.go:21` via
`agentictools.RegisterTool("graph_summary", graphExec)`. No other code path registers it.
The duplicate must occur at request assembly time in the semstreams agentic framework
when building the `tools` array for the OpenAI-compatible chat completion request.

## Evidence

```
ERROR Failed to complete chat error="400 Bad Request: Duplicate function declaration found: graph_summary" model=gemini-pro
ERROR Generation failed via mutation slug=97cb188cbaf8 phase=plan-generation error="planner loop failed: error..."
```

Plan transitions to `rejected` immediately. No LLM calls succeed.

## Notes

- Mock LLM doesn't validate tool schemas, so this never surfaced in mock E2E
- The `submit_work` singleToolAdapter was specifically created to prevent this exact
  Gemini error for terminal tools (see tools/register.go:66 comment)
- `graph_summary` does NOT use singleToolAdapter — it uses the shared `graphExec`
  executor which implements `ListTools()` returning all 3 graph tools

## Likely Root Cause

`GraphExecutor.ListTools()` returns all 3 graph tools (graph_search, graph_query,
graph_summary). When the agentic-tools component collects tools, it calls `ListTools()`
on each registered executor. Since all 3 graph tools share the same executor, each
registration's `ListTools()` returns all 3, resulting in 3×3=9 tool definitions with
duplicates.

## Fix

Apply the same `singleToolAdapter` pattern used for terminal tools — wrap each graph
tool registration so `ListTools()` returns only the registered tool name.

## Found During

UI E2E @easy Gemini test (2026-04-02). First real LLM attempt after BMAD refactor.
