# ENTITY_STATES Startup Race — Graph Tools Not Available

## Status: OPEN

## Severity: High (graph tools unavailable for all agents)

## Summary

At startup, agentic-tools tries to register graph tools but ENTITY_STATES KV bucket
doesn't exist yet. The registration silently fails:

```
WARN ENTITY_STATES bucket not available — agentic tools will not register
```

This means `graph_summary`, `graph_search`, and `graph_query` are never available
to agents. The planner falls back to bash-only exploration, missing all indexed
knowledge graph context (SOPs, architecture docs, code patterns).

## Impact

- Agents have no access to the knowledge graph
- Planner uses bash-only exploration, resulting in lower quality plans
- Graph investment (semsource indexing) is wasted
- Likely contributes to planner failing to produce valid deliverables

## Likely Cause

ENTITY_STATES is created by graph-ingest when it first processes an entity. If
semsource hasn't indexed anything yet, the bucket doesn't exist. The agentic-tools
component checks for it at startup and gives up without retry.

## Fix

Either:
1. Create ENTITY_STATES bucket eagerly (in NATS config or during component startup)
2. Or have agentic-tools retry graph tool registration with a readiness budget

## Found During

UI E2E @easy Gemini test (2026-04-02). Planner dispatched to gemini-pro but only
bash and submit_work were available — no graph tools in tool list.
