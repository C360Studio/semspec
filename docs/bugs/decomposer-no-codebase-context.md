# Bug: Decomposer has no codebase context — invents file paths

**Severity**: High — causes tester/builder file location mismatches
**Component**: `requirement-executor` (`processor/requirement-executor/component.go`)
**Found during**: UI E2E T2 @easy with Gemini (2026-03-28, run 12)
**Status**: OPEN

## Summary

The decomposer prompt (`buildDecomposerPrompt`) receives only the requirement title,
description, prerequisites, and BDD scenarios. It has NO information about:
- What files exist in the project
- The plan's scope (include/exclude paths)
- The project structure

As a result, decomposer outputs `file_scope: ["main.go"]` for every node because it
doesn't know what other files exist. This cascades: testers create test files in
invented directories (pkg/handler/, internal/server/handlers/) because they don't
know where the implementation will actually go.

## Evidence

All 5 decomposers in run 12 produced `file_scope: ["main.go"]`:
```json
{"nodes": [{"id": "implement_health_endpoint", "file_scope": ["main.go"]}]}
```

But the fixture project has `internal/auth/auth.go`, `internal/auth/auth_test.go` —
a clear pattern for where new handlers should go.

## Expected Behavior

`buildDecomposerPrompt` should include:
1. Plan scope (include/exclude paths)
2. Project file listing (from graph or workspace ls)
3. Existing code structure so decomposer can reference real paths

Example improved output:
```json
{"nodes": [{"id": "implement_health_handler",
            "file_scope": ["internal/server/health.go", "internal/server/health_test.go"]}]}
```

## Impact

- Every tester/builder pair disagrees on file locations
- 80%+ of validation failures are caused by import path mismatches
- Builders burn all retry iterations trying to fix structural issues

## Files

- `processor/requirement-executor/component.go:595` — `buildDecomposerPrompt()` (missing context)
- `processor/requirement-executor/execution_state.go` — `requirementExecution` (needs scope field)
