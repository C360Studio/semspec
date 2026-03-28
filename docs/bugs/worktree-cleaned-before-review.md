# Bug: Worktree cleaned up before requirement-level reviewer can access files

**Severity**: High — causes fixable rejections and unnecessary retries
**Component**: `requirement-executor`, `execution-manager`, `sandbox`
**Found during**: UI E2E T2 @easy with Gemini (2026-03-28, run 7)
**Status**: PARTIALLY FIXED — still happening in run 10

## Root Cause

The sandbox server's merge endpoint deletes the worktree after a successful merge.
execution-manager calls `MergeWorktree` when TDD is approved, which deletes the
worktree. Then requirement-executor dispatches the requirement-level reviewer, which
tries to access the worktree and gets "worktree not found".

## Fix

- Added `WithKeepWorktree()` merge option to sandbox client
- Sandbox server skips worktree deletion when `keep_worktree: true`
- execution-manager passes `WithKeepWorktree()` on approved merges
- requirement-executor tracks `NodeTaskIDs` and calls `DeleteWorktree` for
  each in `cleanupExecutionLocked` (after requirement review completes or fails)

## Run 10 Update — still happening

Despite the fix (91295e9), the requirement-level reviewer still gets "worktree not found"
on every requirement that completes its TDD pipeline. This blocks EVERY requirement from
completing — the TDD pipeline works perfectly (80% first-attempt validation pass, 3/4
code reviews approved) but requirement-level review fails on file access.

Evidence from run 10:
```
23:10:24 Node completed req.4 node_id=set_health_content_type 1/1
23:10:59 Starting fixable retry req.4 retry_count=1 feedback="Cannot verify... ls -R failed"
23:13:22 Node completed req.1 node_id=implement-health-endpoint 1/1
23:13:45 Starting fixable retry req.1 feedback="worktree not found"
```

## Actual Root Cause (from DEBUG investigation)

The worktrees ARE kept alive (verified: `ls /workspace/.semspec/worktrees/` shows 8
node worktrees after run completes). The issue is that the **requirement reviewer runs
under its own task ID** (`requirement-rev-...`) which has NO corresponding worktree.

Node worktrees: `node-2810f0dbb9b9c5e4-...`, `node-4d8daa4a27b11b4f-...`, etc.
Reviewer task ID: `requirement-rev-semspec.local.exec.req.run.55c2668069aa-requirement-55c2668069aa-4-...`

When the reviewer calls `bash ls`, the sandbox routes it to a worktree matching the
task ID — but no worktree exists for the reviewer's ID. It needs to either:
1. Run in the last completed node's worktree (or merged branch)
2. Have its own worktree created from the merged state
3. Access the main workspace directly (post-merge)
