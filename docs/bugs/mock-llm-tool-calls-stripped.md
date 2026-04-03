# Scenario-Generator Mock Fixture Mismatches Deliverable Validator

## Status: OPEN

## Severity: Critical (blocks mock E2E scenario generation)

## Summary

Two issues found during UI E2E regression testing:

### Issue 1 (FIXED): Missing `supports_tools` on mock endpoints
Generator endpoints in `e2e-mock-ui.json` were missing `supports_tools: true`, causing
semstreams to strip tool definitions from requests and downgrade tool_calls responses.
**Fixed by adding `supports_tools: true` + `tool_format: "openai"` to all generator endpoints.**

### Issue 2 (PARTIALLY FIXED): `then` array accepted, but `title` still missing
The `then` array issue was fixed in fe70cfa. However, all 18 `mock-scenario-generator.json`
fixtures are **missing the required `title` field**:

```json
// Fixture has:
{"given": "...", "when": "...", "then": ["..."]}

// Validator requires (validators.go:75-77):
title, _ := sc["title"].(string)
if title == "" { return error }  // ← fails here
```

This causes `submit_work` to return `StopLoop=false` (validation error → retry), the mock
returns the same fixture, and the loop burns through 50 iterations in ~180ms.

## Fix

Add `"title": "..."` to every scenario in all 18 `mock-scenario-generator.json` fixtures.

## Found During

UI E2E regression testing (2026-04-02).
