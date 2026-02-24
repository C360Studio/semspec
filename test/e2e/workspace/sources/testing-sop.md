---
category: sop
scope: all
severity: error
applies_to:
  - "internal/**"
  - "pkg/**"
domain:
  - testing
  - quality
requirements:
  - "All API endpoints must have corresponding test files"
  - "Test files must be co-located with the code they test"
  - "Unit tests must achieve minimum 80% coverage on critical paths"
---

# Testing Standards SOP

## Ground Truth

- Existing test files: internal/auth/service_test.go, internal/api/handlers_test.go
- Testing framework: Go standard testing package with testify for assertions
- Test naming convention: TestFunctionName_Scenario

## Rules

1. Every exported function in internal/ must have at least one test.
2. Test files must be in the same package as the code under test.
3. Use table-driven tests for functions with multiple input scenarios.
4. Mock external dependencies using interfaces.
5. All tests must pass context with timeout to async operations.

## Violations

- Adding code in internal/ without corresponding _test.go file
- Tests with hardcoded sleep() instead of explicit synchronization
- Tests that depend on external services without mocking
