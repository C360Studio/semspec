---
category: sop
scope: all
severity: error
applies_to:
  - "internal/api/**"
  - "cmd/**"
domain:
  - api-design
  - rest
requirements:
  - "Error responses must use structured JSON format"
  - "Authentication must be implemented as middleware"
  - "All user inputs must be validated before processing"
---

# API Standards SOP

## Ground Truth

- Router: internal/api/router.go
- Handlers: internal/api/handlers.go
- Auth middleware: internal/auth/middleware.go
- Input validator: pkg/validator/validator.go

## Rules

1. All HTTP handlers must return JSON responses with a consistent envelope.
2. Error responses must include: code, message, and request_id fields.
3. Authentication checks must use the middleware in internal/auth/middleware.go.
4. All path and query parameters must be validated via pkg/validator.
5. Rate limiting must be applied to all public endpoints.
6. Database changes require migration files in internal/db/migrations.go.

## Violations

- Handlers that bypass authentication middleware
- Error responses returning plain text or inconsistent JSON shapes
- Missing input validation for user-provided query or body parameters
- Adding database columns without corresponding migration
