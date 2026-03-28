# Bug: Worktree cleaned up before requirement-level reviewer can access files

**Severity**: High — causes fixable rejections and unnecessary retries
**Component**: `requirement-executor`, `execution-manager`
**Found during**: UI E2E T2 @easy with Gemini (2026-03-28, run 7)
**Status**: OPEN

## Summary

After a task node's TDD pipeline completes and is approved, the requirement-level
reviewer is dispatched. But the reviewer can't access the worktree files — it gets
"worktree not found" from the sandbox. This triggers a fixable rejection and
re-runs all nodes from scratch.

## Evidence

From run 7 logs:
```
22:11:39 Node completed entity_id=...requirement-58c0838190ee-2 node_id=implement_health_endpoint completed=1 total=1
22:11:39 Task execution approved task_id=node-f94f9a... iteration=1
22:11:59 Starting fixable retry — re-running dirty nodes entity_id=...requirement-58c0838190ee-2
         retry_count=1 dirty_nodes=0 clean_nodes=1
         feedback="Cannot access files in the worktree. The `ls` command is failing
         with \"worktree not found\". Unable to perform any review without file access."
```

## Root Cause (Hypothesis)

When the TDD pipeline completes with `approved`, execution-manager calls
`mergeWorktree(exec)` which merges and then removes the worktree. But the
requirement-level reviewer needs to access that worktree (or the merged result)
to perform its review.

The requirement-level reviewer is dispatched by requirement-executor AFTER the
node completion event, but by that time the worktree has already been cleaned up.

## Expected Behavior

Either:
1. Keep the worktree alive until the requirement-level review completes
2. Give the requirement reviewer access to the merged branch (not the worktree)
3. Pass the file contents as context to the reviewer prompt instead of expecting
   tool-based file access

## Impact

- Approved work gets rejected at the requirement level due to file access failure
- Triggers a full requirement retry (re-runs all nodes from scratch)
- Wastes LLM tokens and execution time on work that was already approved

## Files

- `processor/execution-manager/component.go` — `mergeWorktree()` called on approval
- `processor/requirement-executor/component.go` — dispatches requirement reviewer after node completion
- `tools/sandbox/client.go` — `ListWorktreeFiles()` fails with "worktree not found"
