---
category: sop
applies_to:
  - "*.go"
severity: error
summary: Go error handling guidelines
requirements:
  - Always wrap errors with context
  - Never ignore errors
---

# Error Handling

This document outlines the error handling guidelines for Go applications.

## Wrapping Errors

Always wrap errors with context using `fmt.Errorf`:

```go
if err != nil {
    return fmt.Errorf("failed to process request: %w", err)
}
```

## Never Ignore Errors

Every error must be handled explicitly. Do not use blank identifiers:

```go
// BAD - ignoring error
result, _ := doSomething()

// GOOD - handle the error
result, err := doSomething()
if err != nil {
    return err
}
```

## Error Types

Use custom error types for domain-specific errors that need to be checked programmatically.
