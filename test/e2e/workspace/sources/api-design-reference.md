---
category: reference
scope: plan
domain:
  - api-design
  - rest
requirements:
  - "All responses must use the Response Envelope Standard"
  - "Error codes must be machine-readable constants"
---

# API Design Reference

## Response Envelope Standard

All API responses use a consistent JSON envelope. This is the Response Envelope Standard
that every endpoint must follow without exception:

- data: The response payload (null on error)
- error: Error details (null on success) with code, message, and optional field
- request_id: Unique request identifier for tracing
- timestamp: ISO 8601 response timestamp

## Error Codes

Error responses include machine-readable error codes:

- VALIDATION_ERROR: Request validation failed (missing/invalid fields)
- AUTHENTICATION_ERROR: Invalid or expired credentials
- AUTHORIZATION_ERROR: Insufficient permissions for the requested resource
- NOT_FOUND: Requested resource does not exist
- RATE_LIMITED: Too many requests from this client
- INTERNAL_ERROR: Unexpected server-side failure

## Authentication

All non-public endpoints require Bearer token authentication.
JWT access tokens expire after 15 minutes; refresh tokens after 7 days.

## Rate Limiting

Rate limiting is applied per authenticated user for protected endpoints
and per IP address for public endpoints. The rate limiting per authenticated user
is set to 1000 requests per minute, while public endpoints allow 100 per minute.

## Endpoints

Auth: POST /api/v1/auth/login, /auth/refresh, /auth/logout
Users: GET /api/v1/users/me, PATCH /api/v1/users/me
Health: GET /healthz, GET /readyz
