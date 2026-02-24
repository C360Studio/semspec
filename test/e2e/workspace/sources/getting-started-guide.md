---
category: reference
scope: plan
domain:
  - onboarding
  - architecture
requirements: []
---

# Getting Started Guide

## Prerequisites and Environment Setup

Before running the Go web API, ensure the following tools are installed:

- Go 1.22 or later (for generics and slog support)
- PostgreSQL 15 or later (for JSONB and row-level security)
- Docker and Docker Compose (for local infrastructure)

## Environment Variables

Required environment variables for local development:

- PORT: HTTP server port (default: 8080)
- DATABASE_URL: PostgreSQL connection string (required, no default)
- JWT_SECRET: Secret key for JWT signing (required, minimum 32 characters)
- DATABASE_CONNECTION_POOL_SIZE: Maximum number of database connections (default: 25)
- LOG_LEVEL: Logging verbosity: debug, info, warn, error (default: info)
- RATE_LIMIT_RPS: Requests per second limit per client (default: 100)

## Running Locally

1. Start PostgreSQL: docker compose up -d postgres
2. Run migrations: make migrate-up
3. Start the server: make run
4. Verify startup: curl http://localhost:8080/healthz

## Verification Steps

After starting the service, perform the following checks:

1. Health check endpoint verification: GET /healthz should return 200 with
   status "ok" and the current version. GET /readyz should return 200 only
   when all dependencies (database, cache) are reachable.

2. Authentication flow: POST /api/v1/auth/login with valid credentials should
   return a TokenPair with access_token and refresh_token.

3. Rate limiting: Rapid requests to any endpoint should eventually receive
   429 Too Many Requests with appropriate X-RateLimit-* headers.

## Troubleshooting

- Connection refused on startup: Check DATABASE_URL and ensure PostgreSQL is running.
- JWT validation errors: Ensure JWT_SECRET matches between token issuance and validation.
- Slow queries: Check DATABASE_CONNECTION_POOL_SIZE — increase if seeing connection waits.
