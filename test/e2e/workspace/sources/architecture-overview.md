---
category: reference
scope: plan
domain:
  - architecture
  - design
requirements: []
---

# Architecture Overview

## System Design Philosophy

This Go web API follows a layered architecture pattern separating concerns
across the cmd, internal, and pkg directories. Each layer has clear boundaries
and communicates through well-defined interfaces, ensuring that changes in one
layer do not cascade unpredictably to others.

## Layers

### cmd/server
Entry point. Wires dependencies and starts the HTTP server.
Handles graceful shutdown via context cancellation and signal handling.
The main function is kept deliberately thin — it creates dependencies,
wires them together, and starts the server. No business logic lives here.

### internal/auth
Authentication and authorization layer.
- service.go: JWT token generation, validation, and refresh logic
- middleware.go: HTTP middleware for request authentication and role checking
- types.go: Shared auth data types (User, Claims, TokenPair)

The auth layer enforces a strict separation between authentication (identity
verification) and authorization (permission checking). Middleware handles
authentication, while service methods handle authorization decisions.

### internal/api
HTTP routing and request handling.
- router.go: Route registration, middleware attachment, and path grouping
- handlers.go: Business logic for each endpoint with structured error responses
- middleware.go: API-level middleware (structured logging, rate limiting, recovery)

All handlers follow a consistent pattern: validate input, call service layer,
format response. No database access happens directly in handlers.

### internal/db
Data persistence layer.
- models.go: Domain model definitions with JSON and SQL tags
- queries.go: SQL query builder and executor with parameterized queries
- migrations.go: Schema migration management with up/down versioning

The persistence layer uses the repository pattern. All SQL is parameterized
to prevent injection. Connection pooling is configured via environment variables.

### internal/config
Configuration management.
- config.go: Application configuration struct and environment-based loading
- validation.go: Configuration validation rules with descriptive error messages

Configuration is loaded once at startup and passed as a read-only dependency.
Hot-reloading is not supported — restart for config changes.

### pkg/logger
Structured logging abstraction.
Wraps slog for consistent log format across the service.
All log entries include request_id for correlation across service boundaries.

### pkg/validator
Input validation library.
- validator.go: Validates request structs using reflection and tag parsing
- rules.go: Built-in validation rules (required, min/max, email, uuid, etc.)

## Key Design Decisions

1. Context propagation through all service boundaries: Every function that performs
   I/O accepts context.Context as its first parameter. Timeouts, cancellation, and
   trace IDs all flow through context. This is non-negotiable for production services.

2. Error wrapping with fmt.Errorf and %w: Every error is wrapped with context about
   where it occurred. This preserves the full error chain for debugging while keeping
   error messages readable. Never log-and-return — either log or return, not both.

3. Interface abstractions for all external dependencies: Database connections, HTTP
   clients, and third-party services are injected as interfaces. This enables unit
   testing without real infrastructure and makes it easy to swap implementations.

4. Table-driven tests with explicit synchronization: All handler and service tests
   use table-driven patterns with named subtests. Async operations use explicit
   synchronization primitives (channels, sync.WaitGroup), never time.Sleep.

5. Structured JSON responses with envelope pattern: Every response uses the same
   JSON envelope with data, error, request_id, and timestamp fields. Error responses
   include machine-readable error codes alongside human-readable messages.

6. Middleware composition for cross-cutting concerns: Authentication, rate limiting,
   logging, and panic recovery are all middleware. Handlers focus purely on business
   logic. The middleware chain is configured at the router level, not per-handler.

## Request Flow

Request → router → auth middleware → rate limiter → structured logger → handler → service → repository → DB

## Concurrency Model

The server uses Go's standard net/http server with goroutine-per-request. Shared
state (caches, connection pools) is protected by sync.RWMutex. Long-running
background tasks use context-aware goroutines with proper shutdown coordination.

## Deployment Topology

The service is designed to run as a single binary behind a load balancer. Session
state is stored externally (database), so any instance can handle any request.
Health checks (/healthz and /readyz) support graceful rolling deployments.
