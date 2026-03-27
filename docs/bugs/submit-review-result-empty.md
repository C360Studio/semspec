# Bug: submit_review tool — reviewer verdicts always empty

**Severity**: High — code reviewer verdict always empty, execution always escalates
**Found during**: UI E2E mock T1 tests (2026-03-27)
**Status**: FIXED (two issues)

## Summary

Two bugs prevented the reviewer from producing verdicts:

1. **Tool not registered** (a56eba7): `submit_review` was defined in
   `terminal.Executor` but never registered with `agentictools.RegisterTool()`.
   Additionally, reviewer tool filters listed `submit_work` instead of
   `submit_review`.

2. **Stale mock fixtures** (this fix): Mock-coder fixtures 6-7 were written for
   the structural-validator when it was an agentic loop. After it became
   synchronous (no LLM calls), those fixtures were consumed by the reviewer
   instead. The reviewer got `bash` + `submit_work` (a generic completion)
   instead of `submit_review` (the verdict).

## Root Cause: Fixture Call Index Mismatch

The TDD pipeline call sequence for `mock-coder`:

| Call | Stage | Tool | Fixture |
|------|-------|------|---------|
| 1 | Tester | bash (write tests) | mock-coder.1 |
| 2 | Tester | submit_work | mock-coder.2 |
| 3 | Builder | bash (write code) | mock-coder.3 |
| 4 | Builder | bash (write code) | mock-coder.4 |
| 5 | Builder | submit_work | mock-coder.5 |
| — | Structural-validator | (synchronous, no LLM) | — |
| 6 | **Reviewer** | **submit_review** | mock-coder.6 |

The structural-validator was refactored from an agentic loop to a synchronous
NATS request/response (`runStructuralValidation` in execution-manager). The old
fixtures at positions 6-7 (bash for pytest + submit_work for "validation
complete") became stale — consumed by the reviewer, which then produced an
empty verdict because `submit_work` has no `verdict` field.

## Fix

Removed stale structural-validator fixtures (old positions 6-7), renumbered
`submit_review` fixture from position 8 to position 6.

## Note on semstreams

The semstreams `agentic-loop/handlers.go:805-806` correctly maps
`ToolResult.Content` to `LoopCompletedEvent.Result` when `StopLoop` is true.
No upstream fix needed.
