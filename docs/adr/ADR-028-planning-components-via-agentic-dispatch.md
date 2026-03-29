# ADR-028: Migrate planning components to agentic-dispatch

**Status**: Proposed
**Date**: 2026-03-28

## Context

Four planning-stage components inline LLM calls via `llmClient.Complete()` instead of dispatching through agentic-dispatch:

- **planner** — generates Goal/Context/Scope
- **plan-reviewer** — validates plan against SOPs
- **requirement-generator** — generates requirements from plan
- **scenario-generator** — generates BDD scenarios from requirements

Their system prompts promise tools (`bash`, `graph_search`, `graph_query`) that never execute because the LLM runs in single-turn completion mode. The planner tells the LLM "Read the codebase to understand the current state" but it can't — no tool execution path exists.

**Impact**: The planner generates `scope.include: ["main.go"]` for a brownfield project with `internal/auth/`, `internal/server/`, etc. because it has zero codebase visibility. This cascades: decomposer invents file paths, testers create files in wrong directories, builders burn retry budgets on structural issues.

## Decision

Migrate all four components to dispatch TaskMessages to agentic-dispatch and watch AGENT_LOOPS KV for completion. This gives them:

- Real tool execution (bash, graph_search, graph_query, graph_summary)
- Trajectory tracking (AGENT_TRAJECTORIES)
- Token accounting
- Timeout/cancellation via agent signals
- Consistent completion routing via AGENT_LOOPS KV (same pattern as execution pipeline)

## Approach

For each component:

1. Replace `llmClient.Complete()` with `publishTask()` to `agent.task.<role>`
2. Watch AGENT_LOOPS KV for the dispatched task ID reaching terminal state
3. Parse the result from `LoopEntity.Result`
4. Send the existing mutation (e.g., `plan.mutation.drafted`) with the parsed result
5. Remove `llmClient` dependency

The PLAN_STATES KV watch → claim → dispatch pattern stays. Only the LLM call path changes from inline to agentic loop.

## Components

| Component | Current | Target | Mutation |
|-----------|---------|--------|----------|
| planner | `llmClient.Complete()` | `agent.task.planning` | `plan.mutation.drafted` |
| plan-reviewer | `llmClient.Complete()` | `agent.task.reviewer` | `plan.mutation.reviewed` |
| requirement-generator | `llmClient.Complete()` | `agent.task.development` | `plan.mutation.requirements` |
| scenario-generator | `llmClient.Complete()` | `agent.task.development` | `plan.mutation.scenarios` |

## Risks

- **Latency**: Agentic loop has overhead (KV writes, trajectory tracking). Planning may take slightly longer.
- **Tool abuse**: LLMs with real bash access may run unnecessary commands. Prompt engineering needed to constrain tool use.
- **Migration scope**: 4 components × refactor. Should be done incrementally — planner first (highest impact), then others.

## References

- [Planner Inlines LLM Calls (tech debt)](../memory/project_planner_inline_llm_debt.md)
- [decomposer-no-codebase-context.md](../bugs/decomposer-no-codebase-context.md)
- KV Twofer pattern: `processor/requirement-executor/req_completions.go`
