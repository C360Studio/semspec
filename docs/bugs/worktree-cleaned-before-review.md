# Bug: Worktree cleaned up before requirement-level reviewer can access files

**Severity**: High — causes fixable rejections and unnecessary retries
**Component**: `requirement-executor`, `execution-manager`, `sandbox`
**Found during**: UI E2E T2 @easy with Gemini (2026-03-28, run 7)
**Status**: FIXED

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
