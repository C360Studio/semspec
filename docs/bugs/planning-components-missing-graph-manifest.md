# Bug: Planning components don't inject graph manifest into prompts

**Severity**: High — agents skip graph tools because they don't know what's indexed
**Component**: `planner`, `requirement-generator`, `scenario-generator`, `plan-reviewer`
**Found during**: UI E2E investigation (2026-03-29, run 17)
**Status**: OPEN

## Summary

Only `execution-manager` registers `GraphManifestFragment` in its prompt assembler.
All planning components (planner, requirement-generator, scenario-generator, plan-reviewer)
create their own assemblers without the manifest fragment. Agents never see the graph
summary in their system prompt and skip graph tools entirely.

## Evidence

execution-manager registers it (line 234):
```go
registry.Register(prompt.GraphManifestFragment(workflowtools.RegistrySummaryFetchFn()))
```

planner does NOT (lines 97-101):
```go
registry := prompt.NewRegistry()
registry.RegisterAll(promptdomain.Software()...)
registry.Register(prompt.ToolGuidanceFragment(prompt.DefaultToolGuidance()))
// Missing: registry.Register(prompt.GraphManifestFragment(...))
```

Graph has 61 entities indexed (golang functions, files, structs, config, docs).
Semsource `/source-manifest/summary` returns rich data with entity_id_format,
domain breakdowns, and type counts. But agents never see it.

## Fix

Add to each planning component's assembler setup:
```go
registry.Register(prompt.GraphManifestFragment(workflowtools.RegistrySummaryFetchFn()))
```

Components that need it:
- `processor/planner/component.go` (~line 100)
- `processor/requirement-generator/component.go`
- `processor/scenario-generator/component.go`
- `processor/plan-reviewer/component.go`

The manifest can be compact since agents can call graph_summary for full details.
The injected manifest tells them what's available so they know WHETHER to query.

## Also: Verify NLQ guidance in prompts

The tool guidance should explain that graph_search supports natural language queries
(NLQ), not just keyword search. Current guidance says "Search the knowledge graph"
but doesn't mention that queries like "health endpoint handler" work as NLQ.

Check `prompt/tools.go` graph_search guidance — should mention NLQ capability
explicitly so agents use natural language instead of trying exact predicate matches.

## Files

- `processor/planner/component.go` (~line 100)
- `processor/requirement-generator/component.go`
- `processor/scenario-generator/component.go`
- `processor/plan-reviewer/component.go`
- `prompt/tools.go` — graph_search guidance (verify NLQ mention)
