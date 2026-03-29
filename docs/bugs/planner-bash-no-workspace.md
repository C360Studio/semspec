# Bug: Planner bash calls return empty — no workspace access

**Severity**: High — planner can't explore codebase, generates blind plans
**Component**: Planning via agentic-dispatch (ADR-028)
**Found during**: UI E2E T2 @easy with Gemini (2026-03-29, run 16)
**Status**: OPEN

## Summary

After migrating planning to agentic-dispatch (ADR-028), the planner's bash tool
calls return empty strings. `ls -R`, `ls`, `find . -name "main.go"` all return
zero-length results. The planner has no access to the project workspace.

## Evidence

From trajectory `e5955618-2817-4185-b1c8-98978c9d1575`:
```
Step 1: tool=bash args={"command": "ls -R"} result='' (0 chars)
Step 3: tool=bash args={"command": "ls"}    result='' (0 chars)
Step 5: tool=bash args={"command": "find . -name \"main.go\""} result='' (0 chars)
Step 7: tool=ask_question → "Where is the main package located?" → timed out 5min
```

The planner can't see any files, so it asks a question that times out, wasting
5+ minutes of the 10-minute Playwright budget.

## Root Cause

The planner runs via agentic-dispatch → agentic-loop → agentic-tools. The bash
tool executor routes commands through the sandbox, but the planner has no worktree
or workspace mapping. Unlike execution agents (which have task IDs mapped to
worktrees), the planner's task ID doesn't correspond to any sandbox worktree.

The planner needs to run bash commands against the main workspace (`/workspace`),
not a worktree.

## Impact

- Planner generates plans blind — scope is always `["main.go"]` by default
- Planner wastes iterations on ls/find that return nothing
- Falls back to ask_question which times out (5 min)
- Context/scope quality suffers, cascading to decomposer and developer

## Files

- Bash tool executor — needs workspace fallback when no worktree exists
- Planner dispatch — needs to pass workspace path in metadata
