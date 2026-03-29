# Bug: Go-project fixture not reset between E2E runs

**Severity**: High — accumulated merges corrupt the fixture
**Component**: E2E infrastructure, sandbox worktree management
**Found during**: Run 18 green pass — main.go was a dumpster fire from prior run merges
**Status**: OPEN

## Summary

The go-project fixture (`test/e2e/fixtures/go-project/`) accumulates changes
from multiple E2E runs:
- Worktrees from prior runs remain in `.semspec/worktrees/`
- Merged code from prior runs modifies main.go, adds pkg/ directories
- Each run starts with the corrupted state from the previous run

Run 18 passed all tests but main.go had the HTTP server removed and replaced
with auth calls — the fixture was already corrupted from runs 7-17.

## Expected Behavior

Each E2E run should start with a pristine fixture. Options:
1. `git checkout -- . && git clean -fd .` in the fixture before each run
2. Run sandbox with `--reset-on-start` flag
3. Add fixture reset to the task runner (`e2e:ui:test:llm` task)

## DEBUG Mode

When DEBUG=1, the afterAll should NOT delete the plan or reset the fixture.
The whole point of DEBUG is to inspect what the agents produced.

Currently fixed: afterAll skips deletePlan when DEBUG is set.
Still needed: fixture worktrees preserved in DEBUG mode.

## Files

- `taskfiles/e2e.yml` — add fixture git reset before stack up
- `ui/e2e/plan-lifecycle-llm.spec.ts:37` — afterAll cleanup (fixed for DEBUG)
