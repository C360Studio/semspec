package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/c360studio/semspec/source"
	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
	sourceVocab "github.com/c360studio/semspec/vocabulary/source"
)

// ContextPressureScenario tests context management under budget pressure.
// It generates a large multi-package Go web API project (~40-50KB) that exceeds a
// reduced token budget (~16KB), forcing truncation. The scenario exercises:
//   - Context truncation verification (utilization and truncation rate)
//   - Prompt structure validation (system/user messages, codebase context injection)
//   - Model routing verification (4 distinct models for 4 capabilities)
//   - Revision prompt quality (reviewer findings injected into planner revision)
//   - Standards injection resilience under budget pressure
//   - LLM artifact drill-down (full call records from ObjectStore)
//
// The plan goes through ONE rejection cycle: reviewer rejects iteration 1,
// approves iteration 2.
type ContextPressureScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
	nats        *client.NATSClient
	mockLLM     *client.MockLLMClient
}

// NewContextPressureScenario creates a context-pressure scenario that verifies
// correct behavior under token budget constraints.
func NewContextPressureScenario(cfg *config.Config) *ContextPressureScenario {
	return &ContextPressureScenario{
		name:        "context-pressure",
		description: "Go web API under context budget pressure: truncation, routing, revision quality, artifact drill-down",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *ContextPressureScenario) Name() string { return s.name }

// Description returns the scenario description.
func (s *ContextPressureScenario) Description() string { return s.description }

// Setup prepares the scenario environment.
func (s *ContextPressureScenario) Setup(ctx context.Context) error {
	s.fs = client.NewFilesystemClient(s.config.WorkspacePath)
	if err := s.fs.SetupWorkspace(); err != nil {
		return fmt.Errorf("setup workspace: %w", err)
	}

	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

	natsClient, err := client.NewNATSClient(ctx, s.config.NATSURL)
	if err != nil {
		return fmt.Errorf("create NATS client: %w", err)
	}
	s.nats = natsClient

	if s.config.MockLLMURL != "" {
		s.mockLLM = client.NewMockLLMClient(s.config.MockLLMURL)
	}

	return nil
}

// Execute runs the context-pressure scenario.
func (s *ContextPressureScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	t := s.timeout // shorthand

	stages := s.buildStages(t)

	for _, stage := range stages {
		stageStart := time.Now()
		stageCtx, cancel := context.WithTimeout(ctx, stage.timeout)
		err := stage.fn(stageCtx, result)
		cancel()
		stageDuration := time.Since(stageStart)
		result.SetMetric(fmt.Sprintf("%s_duration_us", stage.name), stageDuration.Microseconds())
		if err != nil {
			result.AddStage(stage.name, false, stageDuration, err.Error())
			result.AddError(fmt.Sprintf("%s: %v", stage.name, err))
			result.Error = fmt.Sprintf("%s failed: %v", stage.name, err)
			return result, nil
		}
		result.AddStage(stage.name, true, stageDuration, "")
	}

	result.Success = true
	return result, nil
}

// Teardown cleans up after the scenario.
func (s *ContextPressureScenario) Teardown(ctx context.Context) error {
	if s.nats != nil {
		return s.nats.Close(ctx)
	}
	return nil
}

// timeout returns fast if FastTimeouts is enabled, otherwise normal.
// Both values are in seconds and converted to time.Duration.
func (s *ContextPressureScenario) timeout(normalSec, fastSec int) time.Duration {
	if s.config.FastTimeouts {
		return time.Duration(fastSec) * time.Second
	}
	return time.Duration(normalSec) * time.Second
}

// ============================================================================
// Project Generator
// ============================================================================

// generateGoFile creates a realistic Go source file with the given package name,
// type name, and number of methods. Each method is ~15 lines of realistic-looking
// Go code with proper context handling, error returns, and logging.
func generateGoFile(pkg, typeName string, methodCount int) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("package %s\n\n", pkg))
	sb.WriteString("import (\n")
	sb.WriteString("\t\"context\"\n")
	sb.WriteString("\t\"fmt\"\n")
	sb.WriteString("\t\"sync\"\n")
	sb.WriteString("\t\"time\"\n")
	sb.WriteString(")\n\n")

	// Struct definition with 3-4 fields
	sb.WriteString(fmt.Sprintf("// %s implements the core %s functionality.\n", typeName, pkg))
	sb.WriteString(fmt.Sprintf("type %s struct {\n", typeName))
	sb.WriteString("\tmu      sync.RWMutex\n")
	sb.WriteString("\tclient  interface{}\n")
	sb.WriteString("\ttimeout time.Duration\n")
	sb.WriteString("\tlogger  interface{ Log(msg string, args ...any) }\n")
	sb.WriteString("}\n\n")

	// Constructor
	sb.WriteString(fmt.Sprintf("// New%s creates a new %s with the given configuration.\n", typeName, typeName))
	sb.WriteString(fmt.Sprintf("func New%s(timeout time.Duration) *%s {\n", typeName, typeName))
	sb.WriteString(fmt.Sprintf("\treturn &%s{\n", typeName))
	sb.WriteString("\t\ttimeout: timeout,\n")
	sb.WriteString("\t}\n")
	sb.WriteString("}\n\n")

	// Methods
	methodNames := []string{
		"Initialize", "Process", "Validate", "Execute",
		"Shutdown", "Reset", "Flush", "Reload",
		"Configure", "Inspect",
	}

	for i := 0; i < methodCount && i < len(methodNames); i++ {
		methodName := methodNames[i]
		sb.WriteString(fmt.Sprintf("// %s performs the %s operation on %s.\n", methodName, strings.ToLower(methodName), typeName))
		sb.WriteString("// It respects context cancellation and enforces the configured timeout.\n")
		sb.WriteString(fmt.Sprintf("func (s *%s) %s(ctx context.Context, input map[string]any) (map[string]any, error) {\n", typeName, methodName))
		sb.WriteString("\tctx, cancel := context.WithTimeout(ctx, s.timeout)\n")
		sb.WriteString("\tdefer cancel()\n\n")
		sb.WriteString("\ts.mu.Lock()\n")
		sb.WriteString("\tdefer s.mu.Unlock()\n\n")
		sb.WriteString("\tif input == nil {\n")
		sb.WriteString(fmt.Sprintf("\t\treturn nil, fmt.Errorf(\"%s: input must not be nil\")\n", strings.ToLower(methodName)))
		sb.WriteString("\t}\n\n")
		sb.WriteString("\tselect {\n")
		sb.WriteString("\tcase <-ctx.Done():\n")
		sb.WriteString(fmt.Sprintf("\t\treturn nil, fmt.Errorf(\"%s: context cancelled: %%w\", ctx.Err())\n", strings.ToLower(methodName)))
		sb.WriteString("\tdefault:\n")
		sb.WriteString("\t}\n\n")
		sb.WriteString("\tresult := make(map[string]any)\n")
		sb.WriteString(fmt.Sprintf("\tresult[\"operation\"] = \"%s\"\n", strings.ToLower(methodName)))
		sb.WriteString("\tresult[\"timestamp\"] = time.Now().UTC()\n")
		sb.WriteString("\tresult[\"input_keys\"] = len(input)\n\n")
		sb.WriteString("\treturn result, nil\n")
		sb.WriteString("}\n\n")
	}

	return sb.String()
}

// ============================================================================
// Stage: Setup Project
// ============================================================================

// stageSetupProject creates the multi-package Go web API project in the workspace.
// Total size is ~40-50KB across ~25 files, exceeding the 16KB context budget.
func (s *ContextPressureScenario) stageSetupProject(_ context.Context, result *Result) error {
	ws := s.config.WorkspacePath

	type fileSpec struct {
		relPath string
		content string
	}

	files := []fileSpec{
		{"cmd/server/main.go", generateGoFile("main", "Server", 3)},
		{"internal/auth/service.go", generateGoFile("auth", "AuthService", 5)},
		{"internal/auth/middleware.go", generateGoFile("auth", "Middleware", 4)},
		{"internal/auth/service_test.go", generateGoFile("auth", "AuthServiceTest", 3)},
		{"internal/api/router.go", generateGoFile("api", "Router", 4)},
		{"internal/api/handlers.go", generateGoFile("api", "Handlers", 6)},
		{"internal/api/middleware.go", generateGoFile("api", "APIMiddleware", 3)},
		{"internal/api/handlers_test.go", generateGoFile("api", "HandlersTest", 3)},
		{"internal/db/models.go", generateGoFile("db", "Models", 4)},
		{"internal/db/queries.go", generateGoFile("db", "QueryBuilder", 5)},
		{"internal/db/migrations.go", generateGoFile("db", "Migrator", 3)},
		{"internal/config/config.go", generateGoFile("config", "AppConfig", 4)},
		{"internal/config/validation.go", generateGoFile("config", "Validator", 3)},
		{"pkg/logger/logger.go", generateGoFile("logger", "Logger", 3)},
		{"pkg/validator/validator.go", generateGoFile("validator", "InputValidator", 4)},
		{"pkg/validator/rules.go", generateGoFile("validator", "RuleEngine", 3)},
		{"internal/auth/types.go", authTypesContent()},
		{"docs/architecture.md", architectureMDContent()},
		{"docs/api-design.md", apiDesignMDContent()},
		// Architecture docs with frontmatter — written to sources/ for graph ingestion.
		// The planning strategy queries the graph for source.doc entities, so these
		// must be ingested via source-ingester (not just placed on disk).
		{"sources/architecture-overview.md", architectureSourceDoc()},
		{"sources/api-design-reference.md", apiDesignSourceDoc()},
		{"sources/getting-started-guide.md", gettingStartedSourceDoc()},
		{"go.mod", goModContent()},
		{"README.md", readmeMDContent()},
		{"Makefile", makefileContent()},
	}

	for _, f := range files {
		path := filepath.Join(ws, f.relPath)
		if err := s.fs.WriteFile(path, f.content); err != nil {
			return fmt.Errorf("write %s: %w", f.relPath, err)
		}
	}

	// Write standards and SOP files
	standardsJSON := `{
  "rules": [
    {"id": "api-testing", "severity": "error", "description": "All API endpoints must have corresponding test files"},
    {"id": "db-migration", "severity": "error", "description": "Database changes require migration files"},
    {"id": "error-format", "severity": "error", "description": "Error responses must use structured JSON format"},
    {"id": "auth-middleware", "severity": "error", "description": "Authentication must be implemented as middleware"},
    {"id": "input-validation", "severity": "warning", "description": "All user inputs must be validated before processing"},
    {"id": "logging", "severity": "warning", "description": "All handler functions must include structured logging"},
    {"id": "context-propagation", "severity": "warning", "description": "Context must be propagated through all service calls"},
    {"id": "doc-comments", "severity": "info", "description": "Exported types and functions should have doc comments"}
  ]
}`
	if err := s.fs.WriteFile(filepath.Join(ws, ".semspec", "standards.json"), standardsJSON); err != nil {
		return fmt.Errorf("write standards.json: %w", err)
	}

	testingSOP := `---
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
`
	if err := s.fs.WriteFileRelative("sources/testing-sop.md", testingSOP); err != nil {
		return fmt.Errorf("write testing-sop.md: %w", err)
	}

	apiStandardsSOP := `---
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
`
	if err := s.fs.WriteFileRelative("sources/api-standards-sop.md", apiStandardsSOP); err != nil {
		return fmt.Errorf("write api-standards-sop.md: %w", err)
	}

	// Initialize git repository
	if err := s.fs.InitGit(); err != nil {
		return fmt.Errorf("init git: %w", err)
	}
	if err := s.fs.GitAdd("."); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if err := s.fs.GitCommit("Initial commit"); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	result.SetDetail("project_ready", true)
	result.SetDetail("project_type", "go-web-api")
	return nil
}

// ============================================================================
// Handwritten file content helpers
// ============================================================================

func authTypesContent() string {
	return `package auth

import "time"

// User represents an authenticated user.
type User struct {
	ID        string    ` + "`" + `json:"id"` + "`" + `
	Email     string    ` + "`" + `json:"email"` + "`" + `
	Role      string    ` + "`" + `json:"role"` + "`" + `
	CreatedAt time.Time ` + "`" + `json:"created_at"` + "`" + `
}

// Claims represents JWT claims.
type Claims struct {
	UserID string ` + "`" + `json:"user_id"` + "`" + `
	Email  string ` + "`" + `json:"email"` + "`" + `
	Role   string ` + "`" + `json:"role"` + "`" + `
	Exp    int64  ` + "`" + `json:"exp"` + "`" + `
}

// TokenPair holds access and refresh tokens.
type TokenPair struct {
	AccessToken  string ` + "`" + `json:"access_token"` + "`" + `
	RefreshToken string ` + "`" + `json:"refresh_token"` + "`" + `
	ExpiresIn    int64  ` + "`" + `json:"expires_in"` + "`" + `
}
`
}

// architectureMDContent returns a short version for docs/ (file tree reference).
func architectureMDContent() string {
	return `# Architecture

See sources/architecture-overview.md for the full architecture document.

This Go web API follows a layered architecture pattern.
See the ingested source document for complete details.
`
}

// architectureSourceDoc returns a ~4.5KB architecture document with YAML frontmatter
// for source-ingester graph ingestion. Contains unique content markers that the
// verify-context-truncation stage checks for in the assembled LLM prompt.
//
// Content markers tested: "layered architecture pattern",
// "Context propagation through all service boundaries",
// "Table-driven tests with explicit synchronization"
func architectureSourceDoc() string {
	return `---
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
`
}

// apiDesignMDContent returns a short version for docs/ (file tree reference).
func apiDesignMDContent() string {
	return `# API Design

See sources/api-design-reference.md for the full API design document.

This API follows REST conventions with a consistent JSON response envelope.
`
}

// apiDesignSourceDoc returns a ~2KB API design document with YAML frontmatter
// for source-ingester graph ingestion.
//
// Content markers tested: "Response Envelope Standard",
// "VALIDATION_ERROR", "rate limiting per authenticated user"
func apiDesignSourceDoc() string {
	return `---
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
`
}

// gettingStartedSourceDoc returns a ~2KB getting-started guide with YAML frontmatter
// for source-ingester graph ingestion.
//
// Content markers tested: "Prerequisites and Environment Setup",
// "DATABASE_CONNECTION_POOL_SIZE", "health check endpoint verification"
func gettingStartedSourceDoc() string {
	return `---
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
`
}

func goModContent() string {
	return `module github.com/example/go-web-api

go 1.22

require (
	github.com/golang-jwt/jwt/v5 v5.2.0
	github.com/lib/pq v1.10.9
)
`
}

func readmeMDContent() string {
	return `# Go Web API

A production-grade Go REST API with JWT authentication, rate limiting, and PostgreSQL.

## Project Structure

` + "```" + `
cmd/server/      - Binary entry point
internal/
  auth/          - JWT authentication and middleware
  api/           - HTTP routing and handlers
  db/            - Data models and queries
  config/        - Configuration management
pkg/
  logger/        - Structured logging
  validator/     - Input validation
docs/            - Architecture and API documentation
sources/         - SOPs and standards
` + "```" + `

## Getting Started

` + "```bash" + `
make build        # Build the binary
make test         # Run all tests
make run          # Start the server
` + "```" + `

## Environment Variables

| Variable         | Default     | Description                  |
|------------------|-------------|------------------------------|
| PORT             | 8080        | HTTP server port             |
| DATABASE_URL     | (required)  | PostgreSQL connection string |
| JWT_SECRET       | (required)  | JWT signing secret           |
| RATE_LIMIT_RPS   | 100         | Requests per second limit    |

## Development

This project follows the standards documented in sources/testing-sop.md and
sources/api-standards-sop.md. All contributions must pass the checks in Makefile.
`
}

func makefileContent() string {
	return `.PHONY: build test run lint

build:
	go build -o bin/server ./cmd/server

test:
	go test ./... -race -coverprofile=coverage.out

lint:
	golangci-lint run ./...

run:
	go run ./cmd/server

coverage:
	go tool cover -html=coverage.out
`
}

// ============================================================================
// Standard Workflow Stages
// ============================================================================

// stageCheckNotInitialized verifies the project is NOT initialized.
func (s *ContextPressureScenario) stageCheckNotInitialized(ctx context.Context, result *Result) error {
	status, err := s.http.GetProjectStatus(ctx)
	if err != nil {
		return fmt.Errorf("get project status: %w", err)
	}

	if status.Initialized {
		return fmt.Errorf("expected project NOT to be initialized, but it is")
	}

	result.SetDetail("pre_init_initialized", status.Initialized)
	result.SetDetail("pre_init_has_project_json", status.HasProjectJSON)
	result.SetDetail("pre_init_has_checklist", status.HasChecklist)
	result.SetDetail("pre_init_has_standards", status.HasStandards)
	return nil
}

// stageDetectStack runs filesystem-based stack detection on the workspace.
func (s *ContextPressureScenario) stageDetectStack(ctx context.Context, result *Result) error {
	detection, err := s.http.DetectProject(ctx)
	if err != nil {
		return fmt.Errorf("detect project: %w", err)
	}

	// The workspace has go.mod at root — Go should be detected.
	if len(detection.Languages) == 0 {
		return fmt.Errorf("no languages detected (expected Go from go.mod)")
	}

	goDetected := false
	var langNames []string
	for _, lang := range detection.Languages {
		langNames = append(langNames, lang.Name)
		if strings.EqualFold(lang.Name, "go") {
			goDetected = true
		}
	}

	if !goDetected {
		result.AddWarning(fmt.Sprintf("Go not explicitly detected; detected: %v", langNames))
	}

	result.SetDetail("detected_languages", langNames)
	result.SetDetail("detected_go", goDetected)
	result.SetDetail("detected_frameworks_count", len(detection.Frameworks))
	result.SetDetail("detected_tooling_count", len(detection.Tooling))
	result.SetDetail("detected_docs_count", len(detection.ExistingDocs))
	result.SetDetail("proposed_checks_count", len(detection.ProposedChecklist))
	result.SetDetail("detection_result", detection)
	return nil
}

// stageInitProject initializes the project using detection results.
func (s *ContextPressureScenario) stageInitProject(ctx context.Context, result *Result) error {
	detectionRaw, ok := result.GetDetail("detection_result")
	if !ok {
		return fmt.Errorf("detection_result not found in result details")
	}
	detection := detectionRaw.(*client.ProjectDetectionResult)

	var languages []string
	for _, lang := range detection.Languages {
		languages = append(languages, lang.Name)
	}
	var frameworks []string
	for _, fw := range detection.Frameworks {
		frameworks = append(frameworks, fw.Name)
	}

	initReq := &client.ProjectInitRequest{
		Project: client.ProjectInitInput{
			Name:        "Go Web API",
			Description: "Production-grade Go REST API with JWT authentication and rate limiting",
			Languages:   languages,
			Frameworks:  frameworks,
		},
		Checklist: detection.ProposedChecklist,
		Standards: client.StandardsInput{
			Version: "1.0.0",
			Rules:   []any{},
		},
	}

	resp, err := s.http.InitProject(ctx, initReq)
	if err != nil {
		return fmt.Errorf("init project: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("init project returned success=false")
	}

	result.SetDetail("init_success", resp.Success)
	result.SetDetail("init_files_written", resp.FilesWritten)
	return nil
}

// stageVerifyInitialized confirms the project is now fully initialized.
func (s *ContextPressureScenario) stageVerifyInitialized(ctx context.Context, result *Result) error {
	status, err := s.http.GetProjectStatus(ctx)
	if err != nil {
		return fmt.Errorf("get project status: %w", err)
	}

	if !status.Initialized {
		missing := []string{}
		if !status.HasProjectJSON {
			missing = append(missing, "project.json")
		}
		if !status.HasChecklist {
			missing = append(missing, "checklist.json")
		}
		if !status.HasStandards {
			missing = append(missing, "standards.json")
		}
		return fmt.Errorf("project not fully initialized — missing: %s", strings.Join(missing, ", "))
	}

	result.SetDetail("post_init_initialized", status.Initialized)
	result.SetDetail("post_init_has_project_json", status.HasProjectJSON)
	result.SetDetail("post_init_has_checklist", status.HasChecklist)
	result.SetDetail("post_init_has_standards", status.HasStandards)

	projectJSON := filepath.Join(s.config.WorkspacePath, ".semspec", "project.json")
	if _, err := os.Stat(projectJSON); os.IsNotExist(err) {
		return fmt.Errorf(".semspec/project.json not found on disk")
	}

	checklistJSON := filepath.Join(s.config.WorkspacePath, ".semspec", "checklist.json")
	if _, err := os.Stat(checklistJSON); os.IsNotExist(err) {
		return fmt.Errorf(".semspec/checklist.json not found on disk")
	}

	standardsJSON := filepath.Join(s.config.WorkspacePath, ".semspec", "standards.json")
	if _, err := os.Stat(standardsJSON); os.IsNotExist(err) {
		return fmt.Errorf(".semspec/standards.json not found on disk")
	}

	result.SetDetail("project_files_on_disk", true)
	return nil
}

// stageIngestDocs publishes ingestion requests for SOPs AND architecture documents.
// Uses YAML frontmatter so the source-ingester skips LLM analysis (fast + deterministic).
// Architecture docs are ingested so the planning strategy can discover them via
// graph queries (source.doc entities) instead of hardcoded filesystem paths.
func (s *ContextPressureScenario) stageIngestDocs(ctx context.Context, result *Result) error {
	docFiles := []string{
		// SOPs
		"testing-sop.md",
		"api-standards-sop.md",
		// Architecture reference docs (graph-first context assembly)
		"architecture-overview.md",
		"api-design-reference.md",
		"getting-started-guide.md",
	}

	for _, relPath := range docFiles {
		req := source.IngestRequest{
			Path:      relPath,
			ProjectID: "default",
			AddedBy:   "e2e-test",
		}
		data, err := json.Marshal(req)
		if err != nil {
			return fmt.Errorf("marshal ingest request for %s: %w", relPath, err)
		}

		if err := s.nats.PublishToStream(ctx, config.SourceIngestSubject, data); err != nil {
			return fmt.Errorf("publish ingest request for %s: %w", relPath, err)
		}
	}

	result.SetDetail("docs_ingested_count", len(docFiles))
	result.SetDetail("docs_ingest_published", true)
	return nil
}

// stageVerifyDocsIngested polls the message-logger for graph.ingest.entity entries
// containing document entities (both SOPs and architecture docs), confirming the
// source-ingester processed all documents and published them to the graph.
func (s *ContextPressureScenario) stageVerifyDocsIngested(ctx context.Context, result *Result) error {
	ticker := time.NewTicker(kvPollInterval)
	defer ticker.Stop()

	// We expect at least 5 doc entities: 2 SOPs + 3 architecture docs.
	// Source-ingester may also create chunk entities, so we count conservatively
	// by looking for entities with source.doc.category predicates.
	const minExpectedDocs = 5

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("expected %d+ doc entities in graph, timed out: %w", minExpectedDocs, ctx.Err())
		case <-ticker.C:
			entries, err := s.http.GetMessageLogEntries(ctx, 100, "graph.ingest.entity")
			if err != nil {
				continue
			}
			if len(entries) == 0 {
				continue
			}

			docEntities := 0
			for _, entry := range entries {
				raw := string(entry.RawData)
				if strings.Contains(raw, sourceVocab.DocCategory) {
					docEntities++
				}
			}

			if docEntities >= minExpectedDocs {
				result.SetDetail("doc_entities_found", docEntities)
				result.SetDetail("total_graph_entities", len(entries))
				result.SetDetail("arch_docs_ingested", true)
				return nil
			}
		}
	}
}

// stageCreatePlan creates a plan via the REST API.
func (s *ContextPressureScenario) stageCreatePlan(ctx context.Context, result *Result) error {
	resp, err := s.http.CreatePlan(ctx, "Add JWT authentication with rate limiting to the Go API")
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("plan creation returned error: %s", resp.Error)
	}

	if resp.Slug == "" {
		return fmt.Errorf("plan creation returned empty slug")
	}

	result.SetDetail("plan_slug", resp.Slug)
	result.SetDetail("plan_request_id", resp.RequestID)
	result.SetDetail("plan_trace_id", resp.TraceID)
	result.SetDetail("plan_response", resp)
	return nil
}

// stageWaitForPlan waits for the plan to be approved after the rejection cycle.
// This scenario has ONE rejection: reviewer rejects iter 1, approves iter 2.
// The poll loop handles: planning → reviewing → needs_changes → planning → reviewing → approved.
func (s *ContextPressureScenario) stageWaitForPlan(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	// Wait for plan directory and file to appear first
	if err := s.fs.WaitForPlan(ctx, slug); err != nil {
		return fmt.Errorf("plan directory not created: %w", err)
	}
	if err := s.fs.WaitForPlanFile(ctx, slug, "plan.json"); err != nil {
		return fmt.Errorf("plan.json not created: %w", err)
	}

	// Wait for the LLM to populate the plan
	planPath := s.fs.DefaultProjectPlanPath(slug) + "/plan.json"
	ticker := time.NewTicker(kvPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("plan never received goal from LLM: %w", ctx.Err())
		case <-ticker.C:
			var plan map[string]any
			if err := s.fs.ReadJSON(planPath, &plan); err != nil {
				continue
			}
			if goal, ok := plan["goal"].(string); ok && goal != "" {
				result.SetDetail("plan_file_exists", true)
				result.SetDetail("plan_goal_preview", truncate(goal, 100))
				goto planReady
			}
		}
	}

planReady:
	// Now poll the API until the plan is approved (handles rejection cycle).
	reviewTimeout := time.Duration(maxReviewAttempts) * 4 * time.Minute
	backoff := reviewRetryBackoff
	if s.config.FastTimeouts {
		reviewTimeout = time.Duration(maxReviewAttempts) * config.FastReviewStepTimeout
		backoff = config.FastReviewBackoff
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, reviewTimeout)
	defer cancel()

	pollTicker := time.NewTicker(backoff)
	defer pollTicker.Stop()

	var lastStage string
	lastIterationSeen := 0
	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("plan approval timed out (last stage: %s, iteration: %d/%d)",
				lastStage, lastIterationSeen, maxReviewAttempts)
		case <-pollTicker.C:
			plan, err := s.http.GetPlan(timeoutCtx, slug)
			if err != nil {
				continue
			}

			lastStage = plan.Stage
			result.SetDetail("review_stage", plan.Stage)
			result.SetDetail("review_verdict", plan.ReviewVerdict)
			result.SetDetail("review_summary", plan.ReviewSummary)

			if plan.Approved {
				result.SetDetail("approve_response", plan)
				result.SetDetail("review_revisions", lastIterationSeen)
				return nil
			}

			// Track revision cycles by actual iteration number (not poll count)
			if plan.ReviewIteration > lastIterationSeen {
				lastIterationSeen = plan.ReviewIteration
				if plan.ReviewVerdict == "needs_changes" {
					result.AddWarning(fmt.Sprintf("plan review iteration %d/%d returned needs_changes: %s",
						lastIterationSeen, maxReviewAttempts, plan.ReviewSummary))
					if lastIterationSeen >= maxReviewAttempts {
						return fmt.Errorf("plan review exhausted %d revision attempts: %s",
							maxReviewAttempts, plan.ReviewSummary)
					}
				}
			}
		}
	}
}

// stageApprovePlan waits for the plan to be approved via the review loop.
// For this scenario the approval already happened in stageWaitForPlan.
// This stage is a no-op guard that verifies the plan is in approved state.
func (s *ContextPressureScenario) stageApprovePlan(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	plan, err := s.http.GetPlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("get plan for approval check: %w", err)
	}

	if !plan.Approved {
		// Plan not yet approved — use PromotePlan to trigger the review workflow
		promoteResp, err := s.http.PromotePlan(ctx, slug)
		if err != nil {
			return fmt.Errorf("promote plan: %w", err)
		}

		result.SetDetail("promote_verdict", promoteResp.ReviewVerdict)
		result.SetDetail("promote_summary", promoteResp.ReviewSummary)

		if !promoteResp.IsApproved() {
			return fmt.Errorf("plan promotion returned verdict %q: %s",
				promoteResp.ReviewVerdict, promoteResp.ReviewSummary)
		}
	}

	result.SetDetail("plan_approved", true)
	return nil
}

// ============================================================================
// Phase Stages
// ============================================================================

// stageGeneratePhases triggers LLM-based phase generation via the REST API.
func (s *ContextPressureScenario) stageGeneratePhases(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	resp, err := s.http.GeneratePhases(ctx, slug)
	if err != nil {
		return fmt.Errorf("generate phases: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("generate phases returned error: %s", resp.Error)
	}

	result.SetDetail("phases_generate_response", resp)
	result.SetDetail("phases_request_id", resp.RequestID)
	result.SetDetail("phases_trace_id", resp.TraceID)
	return nil
}

// stageWaitForPhases waits for phases.json to be created by the phase generator.
func (s *ContextPressureScenario) stageWaitForPhases(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	if err := s.fs.WaitForPlanFile(ctx, slug, "phases.json"); err != nil {
		return fmt.Errorf("phases.json not created: %w", err)
	}

	return nil
}

// stageApprovePhases waits for the phase-review-loop to approve phases.
func (s *ContextPressureScenario) stageApprovePhases(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	backoff := reviewRetryBackoff
	if s.config.FastTimeouts {
		backoff = config.FastReviewBackoff
	}

	ticker := time.NewTicker(backoff)
	defer ticker.Stop()

	var lastStage string
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("phases never generated/reviewed (last stage: %s): %w",
				lastStage, ctx.Err())
		case <-ticker.C:
			plan, err := s.http.GetPlan(ctx, slug)
			if err != nil {
				continue
			}

			lastStage = plan.Stage

			if plan.PhasesApproved {
				result.SetDetail("phases_approved", true)
				return nil
			}

			if plan.Status == "phases_approved" || plan.Status == "tasks_generated" || plan.Status == "tasks_approved" {
				result.SetDetail("phases_approved", true)
				return nil
			}

			if plan.Status == "phases_generated" && plan.PhaseReviewVerdict == "approved" {
				phases, err := s.http.ApproveAllPhases(ctx, slug, "e2e-test")
				if err != nil {
					return fmt.Errorf("approve all phases: %w", err)
				}
				result.SetDetail("phases_approved_count", len(phases))
				result.SetDetail("phases_approved", true)
				return nil
			}
		}
	}
}

// stageGenerateTasks triggers LLM-based task generation via the REST API.
func (s *ContextPressureScenario) stageGenerateTasks(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	resp, err := s.http.GenerateTasks(ctx, slug)
	if err != nil {
		return fmt.Errorf("generate tasks: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("generate tasks returned error: %s", resp.Error)
	}

	result.SetDetail("generate_response", resp)
	result.SetDetail("tasks_request_id", resp.RequestID)
	result.SetDetail("tasks_trace_id", resp.TraceID)
	return nil
}

// stageWaitForTasks waits for tasks.json to be created by the LLM.
func (s *ContextPressureScenario) stageWaitForTasks(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	if err := s.fs.WaitForPlanFile(ctx, slug, "tasks.json"); err != nil {
		return fmt.Errorf("tasks.json not created: %w", err)
	}

	return nil
}

// stageApproveTasksIndividually approves each task individually.
func (s *ContextPressureScenario) stageApproveTasksIndividually(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	tasks, err := s.http.GetTasks(ctx, slug)
	if err != nil {
		return fmt.Errorf("get tasks: %w", err)
	}

	approvedCount := 0
	for _, task := range tasks {
		approvedTask, err := s.http.ApproveTask(ctx, slug, task.ID, "e2e-test")
		if err != nil {
			return fmt.Errorf("approve task %s: %w", task.ID, err)
		}

		if approvedTask.Status != "approved" {
			return fmt.Errorf("task %s approval returned status %q, expected approved", task.ID, approvedTask.Status)
		}

		approvedCount++
	}

	result.SetDetail("tasks_approved_count", approvedCount)
	return nil
}

// ============================================================================
// Verification Stages
// ============================================================================

// stageVerifyContextTruncation verifies that the context-builder operated under
// budget pressure by inspecting the actual prompt content sent to the mock planner.
//
// The planning strategy queries the graph for source.doc entities (architecture docs
// ingested in the ingest-docs stage). Under the 2000-token budget, only SOME docs
// fit — the first architecture doc should be included, while later docs are excluded
// due to budget exhaustion.
//
// We verify this by checking for unique content markers from each architecture doc.
// Markers from the first doc (architecture-overview.md) should be FOUND, while
// markers from later docs (api-design-reference.md, getting-started-guide.md)
// should be MISSING — proving budget-constrained partial inclusion.
func (s *ContextPressureScenario) stageVerifyContextTruncation(ctx context.Context, result *Result) error {
	if s.mockLLM == nil {
		return fmt.Errorf("mock LLM client required for context truncation verification")
	}

	// Get planner's first call to inspect the assembled prompt
	reqs, err := s.mockLLM.GetRequestsByCall(ctx, "mock-planner", 1)
	if err != nil {
		return fmt.Errorf("get mock planner requests: %w", err)
	}
	if len(reqs) == 0 {
		return fmt.Errorf("no captured requests for mock-planner call 1")
	}

	req := reqs[0]
	if len(req.Messages) < 2 {
		return fmt.Errorf("expected at least 2 messages, got %d", len(req.Messages))
	}

	// Concatenate all message content to search for content markers
	var fullPrompt strings.Builder
	for _, msg := range req.Messages {
		fullPrompt.WriteString(msg.Content)
	}
	promptText := fullPrompt.String()
	promptLen := len(promptText)
	result.SetDetail("context_prompt_length_chars", promptLen)

	// Content markers from our graph-ingested architecture documents.
	// Each marker is a unique string that only appears in one specific source doc.
	// Under budget pressure (~2000 tokens), the first doc should fit but later
	// docs should be excluded — proving graph-first context assembly under budget.
	contentMarkers := []struct {
		marker string
		source string // which source doc contains this marker
	}{
		// From architecture-overview.md (~4.5KB — should fit first in budget)
		{"layered architecture pattern", "architecture-overview.md"},
		{"Context propagation through all service boundaries", "architecture-overview.md"},
		{"Table-driven tests with explicit synchronization", "architecture-overview.md"},

		// From api-design-reference.md (~2KB — likely budget-excluded)
		{"Response Envelope Standard", "api-design-reference.md"},
		{"VALIDATION_ERROR", "api-design-reference.md"},
		{"rate limiting per authenticated user", "api-design-reference.md"},

		// From getting-started-guide.md (~2KB — definitely budget-excluded)
		{"Prerequisites and Environment Setup", "getting-started-guide.md"},
		{"DATABASE_CONNECTION_POOL_SIZE", "getting-started-guide.md"},
		{"health check endpoint verification", "getting-started-guide.md"},
	}

	markersFound := 0
	markersNotFound := 0
	var foundMarkers, missingMarkers []string
	for _, m := range contentMarkers {
		if strings.Contains(promptText, m.marker) {
			markersFound++
			foundMarkers = append(foundMarkers, fmt.Sprintf("%s (%s)", m.marker, m.source))
		} else {
			markersNotFound++
			missingMarkers = append(missingMarkers, fmt.Sprintf("%s (%s)", m.marker, m.source))
		}
	}

	result.SetDetail("context_content_markers_found", markersFound)
	result.SetDetail("context_content_markers_missing", markersNotFound)
	result.SetDetail("context_content_markers_total", len(contentMarkers))
	result.SetDetail("context_found_markers", foundMarkers)
	result.SetDetail("context_missing_markers", missingMarkers)

	// Also record context-stats for informational purposes (not gating)
	slug, _ := result.GetDetailString("plan_slug")
	if slug != "" {
		stats, _, statsErr := s.http.GetContextStats(ctx, slug)
		if statsErr == nil && stats != nil && stats.Summary != nil {
			result.SetDetail("context_stats_truncation_rate", stats.Summary.TruncationRate)
			result.SetDetail("context_stats_avg_utilization", stats.Summary.AvgUtilization)
			result.SetDetail("context_stats_total_budget", stats.Summary.TotalBudget)
			result.SetDetail("context_stats_total_used", stats.Summary.TotalUsed)
		}
	}

	// KEY BEHAVIORAL ASSERTION:
	// Graph-first context assembly under budget pressure must show PARTIAL inclusion:
	// - markersFound > 0 proves the graph query worked and content was assembled
	// - markersNotFound > 0 proves the budget constrained what was included
	if markersFound == 0 {
		return fmt.Errorf("no content markers found in %d-char prompt — "+
			"context-builder not including graph-sourced architecture docs. "+
			"Missing markers: %v", promptLen, missingMarkers)
	}

	if markersNotFound == 0 {
		return fmt.Errorf("all %d markers found in prompt (%d chars) — "+
			"budget not constraining content (expected partial inclusion under 2000-token budget)",
			len(contentMarkers), promptLen)
	}

	// GOOD: some found, some missing = graph-first context assembly under budget pressure
	result.SetDetail("context_under_pressure", true)
	result.SetDetail("context_truncation_evidence", fmt.Sprintf(
		"%d/%d content markers included, %d excluded (prompt %d chars)",
		markersFound, len(contentMarkers), markersNotFound, promptLen))

	return nil
}

// stageVerifyPromptStructure inspects the captured first LLM call to mock-planner
// and verifies the prompt contains system/user messages with codebase context and
// file references.
func (s *ContextPressureScenario) stageVerifyPromptStructure(ctx context.Context, result *Result) error {
	if s.mockLLM == nil {
		return nil
	}

	// Get planner's first call
	reqs, err := s.mockLLM.GetRequestsByCall(ctx, "mock-planner", 1)
	if err != nil {
		return fmt.Errorf("get mock planner requests: %w", err)
	}
	if len(reqs) == 0 {
		return fmt.Errorf("no captured requests for mock-planner call 1")
	}

	req := reqs[0]
	if len(req.Messages) < 2 {
		return fmt.Errorf("expected at least 2 messages (system + user), got %d", len(req.Messages))
	}

	// Verify system message structure
	systemMsg := req.Messages[0]
	if systemMsg.Role != "system" {
		return fmt.Errorf("expected first message role 'system', got %q", systemMsg.Role)
	}
	result.SetDetail("prompt_system_length", len(systemMsg.Content))

	// System prompt should reference JSON format
	if !strings.Contains(systemMsg.Content, "JSON") && !strings.Contains(systemMsg.Content, "json") {
		result.AddWarning("system prompt may lack JSON format instructions")
	}

	// Verify user message content
	userMsg := req.Messages[len(req.Messages)-1]
	if userMsg.Role != "user" {
		return fmt.Errorf("expected last message role 'user', got %q", userMsg.Role)
	}
	result.SetDetail("prompt_user_length", len(userMsg.Content))

	// Check for codebase context section
	hasCodebaseContext := strings.Contains(userMsg.Content, "Codebase Context") ||
		strings.Contains(userMsg.Content, "## Context") ||
		strings.Contains(userMsg.Content, "File Tree")
	result.SetDetail("prompt_has_codebase_context", hasCodebaseContext)

	// Check for project file references from our generated files
	hasFileRef := strings.Contains(userMsg.Content, "internal/auth") ||
		strings.Contains(userMsg.Content, "internal/api") ||
		strings.Contains(userMsg.Content, "cmd/server")
	result.SetDetail("prompt_has_file_references", hasFileRef)

	// Check for standards/SOP content
	hasStandards := strings.Contains(userMsg.Content, "standard") ||
		strings.Contains(userMsg.Content, "Standard") ||
		strings.Contains(userMsg.Content, "SOP") ||
		strings.Contains(userMsg.Content, "requirement")
	result.SetDetail("prompt_has_standards", hasStandards)

	if !hasCodebaseContext && !hasFileRef {
		return fmt.Errorf("user prompt lacks codebase context — context builder may not be assembling prompts")
	}

	return nil
}

// stageVerifyModelRouting inspects all captured mock requests and verifies that
// each capability used its designated model. With ONE plan rejection cycle, we
// expect mock-planner called 2x and mock-reviewer called 2x.
func (s *ContextPressureScenario) stageVerifyModelRouting(ctx context.Context, result *Result) error {
	if s.mockLLM == nil {
		return nil
	}

	allReqs, err := s.mockLLM.GetRequests(ctx, "")
	if err != nil {
		return fmt.Errorf("get all mock requests: %w", err)
	}

	// Build model usage map
	modelsSeen := make(map[string]int)
	for model, reqs := range allReqs.RequestsByModel {
		modelsSeen[model] = len(reqs)
	}
	result.SetDetail("model_routing_models_seen", modelsSeen)

	// Verify capability → model routing:
	//   planning          → mock-planner
	//   reviewing         → mock-reviewer
	//   phase_generation  → mock-phase-generator
	//   task_generation   → mock-task-generator
	//   task_reviewing    → mock-task-reviewer
	expectedModels := map[string]string{
		"mock-planner":         "planning/writing",
		"mock-reviewer":        "reviewing",
		"mock-phase-generator": "phase_generation",
		"mock-task-generator":  "task_generation",
		"mock-task-reviewer":   "task_reviewing",
	}

	for model, capability := range expectedModels {
		if count, ok := modelsSeen[model]; !ok || count == 0 {
			return fmt.Errorf("model %q (capability: %s) was never called — routing may be broken", model, capability)
		}
		result.SetDetail(fmt.Sprintf("model_routing_%s_calls", model), modelsSeen[model])
	}

	// Verify planner called exactly 2 times (initial + revision after rejection)
	if plannerCalls := modelsSeen["mock-planner"]; plannerCalls != 2 {
		return fmt.Errorf("expected mock-planner called 2 times (initial + revision), got %d", plannerCalls)
	}

	// Verify reviewer called exactly 3 times (plan reject + plan approve + phase approve)
	if reviewerCalls := modelsSeen["mock-reviewer"]; reviewerCalls != 3 {
		return fmt.Errorf("expected mock-reviewer called 3 times (plan reject + plan approve + phase approve), got %d", reviewerCalls)
	}

	result.SetDetail("model_routing_verified", true)
	return nil
}

// stageVerifyRevisionFeedback inspects the planner's second call and verifies that
// the revision prompt contains the reviewer's actual findings — not template variables.
func (s *ContextPressureScenario) stageVerifyRevisionFeedback(ctx context.Context, result *Result) error {
	if s.mockLLM == nil {
		return nil
	}

	// Get planner's second call (the revision)
	reqs, err := s.mockLLM.GetRequestsByCall(ctx, "mock-planner", 2)
	if err != nil {
		return fmt.Errorf("get mock planner revision request: %w", err)
	}
	if len(reqs) == 0 {
		return fmt.Errorf("no captured request for mock-planner call 2 (revision)")
	}

	req := reqs[0]
	userMsg := req.Messages[len(req.Messages)-1]

	// KEY ASSERTION: Revision prompt must contain "REVISION REQUEST" prefix
	hasRevisionPrefix := strings.Contains(userMsg.Content, "REVISION REQUEST")
	result.SetDetail("revision_has_prefix", hasRevisionPrefix)
	if !hasRevisionPrefix {
		return fmt.Errorf("revision prompt missing 'REVISION REQUEST' prefix — feedback not injected")
	}

	// Verify the prompt contains reviewer finding text from mock-reviewer.1.json
	finding1 := "Missing test files for authentication middleware"
	finding2Variants := []string{"database migration", "migration plan"}

	hasFinding1 := strings.Contains(userMsg.Content, finding1)
	hasFinding2 := containsAnyCI(userMsg.Content, finding2Variants...)
	result.SetDetail("revision_has_finding_1", hasFinding1)
	result.SetDetail("revision_has_finding_2", hasFinding2)

	if !hasFinding1 {
		return fmt.Errorf("revision prompt missing reviewer finding: %q", finding1)
	}
	if !hasFinding2 {
		result.AddWarning("revision prompt may not contain database migration finding")
	}

	// Compare prompt lengths: revision should be longer (includes feedback)
	call1Reqs, _ := s.mockLLM.GetRequestsByCall(ctx, "mock-planner", 1)
	if len(call1Reqs) > 0 {
		call1UserMsg := call1Reqs[0].Messages[len(call1Reqs[0].Messages)-1]
		result.SetDetail("revision_call1_prompt_length", len(call1UserMsg.Content))
		result.SetDetail("revision_call2_prompt_length", len(userMsg.Content))
		result.SetDetail("revision_prompt_grew", len(userMsg.Content) > len(call1UserMsg.Content))
	}

	result.SetDetail("revision_feedback_verified", true)
	return nil
}

// stageVerifyStandardsUnderPressure checks that standards rules from .semspec/standards.json
// appear in the planner prompt even under budget pressure. Standards are budget-agnostic —
// they must be injected regardless of token constraints.
func (s *ContextPressureScenario) stageVerifyStandardsUnderPressure(ctx context.Context, result *Result) error {
	if s.mockLLM == nil {
		return nil
	}

	// Get planner's first call
	reqs, err := s.mockLLM.GetRequestsByCall(ctx, "mock-planner", 1)
	if err != nil || len(reqs) == 0 {
		return fmt.Errorf("get mock planner requests for standards check: %w", err)
	}

	userMsg := reqs[0].Messages[len(reqs[0].Messages)-1]

	// These are rule description texts from the standards.json we wrote in stageSetupProject.
	standardsRuleTexts := []string{
		"All API endpoints must have corresponding test files",
		"Database changes require migration files",
		"Error responses must use structured JSON format",
	}

	foundStandards := 0
	for _, ruleText := range standardsRuleTexts {
		if strings.Contains(userMsg.Content, ruleText) {
			foundStandards++
		}
	}

	result.SetDetail("standards_rules_found", foundStandards)
	result.SetDetail("standards_rules_checked", len(standardsRuleTexts))

	if foundStandards == 0 {
		return fmt.Errorf("no standards rules found in prompt — standards injection may be broken under pressure")
	}

	result.SetDetail("standards_under_pressure_verified", true)
	return nil
}

// stageVerifyArtifactsStrict verifies that LLM call artifacts are properly stored
// and retrievable. Unlike the warning-only approach in hello_world, this stage treats
// 404 from the /calls/ endpoint as a hard failure to confirm ObjectStore is functioning.
func (s *ContextPressureScenario) stageVerifyArtifactsStrict(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")
	traceID, _ := result.GetDetailString("plan_trace_id")

	// Get plan and verify LLMCallHistory
	plan, err := s.http.GetPlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("get plan: %w", err)
	}

	if plan.LLMCallHistory == nil {
		return fmt.Errorf("plan.llm_call_history is nil — request IDs not propagated")
	}

	// Should have exactly 2 plan review iterations (reject + approve)
	if len(plan.LLMCallHistory.PlanReview) != 2 {
		return fmt.Errorf("expected 2 plan_review iterations, got %d", len(plan.LLMCallHistory.PlanReview))
	}

	result.SetDetail("artifacts_plan_review_iterations", len(plan.LLMCallHistory.PlanReview))
	result.SetDetail("artifacts_plan_review_iter1_verdict", plan.LLMCallHistory.PlanReview[0].Verdict)
	result.SetDetail("artifacts_plan_review_iter2_verdict", plan.LLMCallHistory.PlanReview[1].Verdict)

	// Collect all request IDs
	var allRequestIDs []string
	for _, iter := range plan.LLMCallHistory.PlanReview {
		allRequestIDs = append(allRequestIDs, iter.LLMRequestIDs...)
	}
	if plan.LLMCallHistory.TaskReview != nil {
		for _, iter := range plan.LLMCallHistory.TaskReview {
			allRequestIDs = append(allRequestIDs, iter.LLMRequestIDs...)
		}
	}
	result.SetDetail("artifacts_total_request_ids", len(allRequestIDs))

	if len(allRequestIDs) == 0 {
		return fmt.Errorf("no LLM request IDs in call history")
	}

	// STRICT: Drill down to full call record via /calls/ endpoint.
	// Use the first request ID from plan review iteration 1.
	requestID := allRequestIDs[0]
	fullCall, statusCode, err := s.http.GetFullLLMCall(ctx, requestID, traceID)
	if err != nil {
		if statusCode == 404 {
			// STRICT: Do NOT treat 404 as acceptable.
			return fmt.Errorf("/calls/%s returned 404 — ObjectStore not returning full call records", requestID)
		}
		return fmt.Errorf("get full LLM call: %w", err)
	}

	result.SetDetail("artifacts_full_call_request_id", requestID)

	// Verify message structure
	if len(fullCall.Messages) < 2 {
		return fmt.Errorf("expected at least 2 messages in full call, got %d", len(fullCall.Messages))
	}

	// Verify roles
	if fullCall.Messages[0].Role != "system" {
		return fmt.Errorf("expected first message role 'system', got %q", fullCall.Messages[0].Role)
	}
	if fullCall.Messages[len(fullCall.Messages)-1].Role != "user" {
		return fmt.Errorf("expected last message role 'user', got %q", fullCall.Messages[len(fullCall.Messages)-1].Role)
	}

	// Verify response is present and non-empty
	if fullCall.Response == "" {
		return fmt.Errorf("full call response is empty")
	}
	result.SetDetail("artifacts_response_length", len(fullCall.Response))

	// Verify model and capability
	result.SetDetail("artifacts_model", fullCall.Model)
	result.SetDetail("artifacts_capability", fullCall.Capability)
	result.SetDetail("artifacts_messages_count", len(fullCall.Messages))

	result.SetDetail("artifacts_strict_verified", true)
	return nil
}

// ============================================================================
// Stage List
// ============================================================================

// buildStages returns the ordered stage list for the context-pressure scenario.
func (s *ContextPressureScenario) buildStages(t func(int, int) time.Duration) []stageDefinition {
	return []stageDefinition{
		// Setup
		{"setup-project", s.stageSetupProject, t(30, 15)},
		{"check-not-initialized", s.stageCheckNotInitialized, t(10, 5)},
		{"detect-stack", s.stageDetectStack, t(30, 15)},
		{"init-project", s.stageInitProject, t(30, 15)},
		{"verify-initialized", s.stageVerifyInitialized, t(10, 5)},
		{"ingest-docs", s.stageIngestDocs, t(30, 15)},
		{"verify-docs-ingested", s.stageVerifyDocsIngested, t(120, 30)},
		// Plan workflow (with ONE rejection cycle)
		{"create-plan", s.stageCreatePlan, t(30, 15)},
		{"wait-for-plan", s.stageWaitForPlan, t(600, 60)},
		{"approve-plan", s.stageApprovePlan, t(600, 30)},
		// Phase workflow
		{"generate-phases", s.stageGeneratePhases, t(30, 15)},
		{"wait-for-phases", s.stageWaitForPhases, t(600, 30)},
		{"approve-phases", s.stageApprovePhases, t(600, 30)},
		// Task workflow
		{"generate-tasks", s.stageGenerateTasks, t(30, 15)},
		{"wait-for-tasks", s.stageWaitForTasks, t(600, 30)},
		{"approve-tasks", s.stageApproveTasksIndividually, t(30, 15)},
		// Verification stages
		{"verify-context-truncation", s.stageVerifyContextTruncation, t(15, 10)},
		{"verify-prompt-structure", s.stageVerifyPromptStructure, t(15, 10)},
		{"verify-model-routing", s.stageVerifyModelRouting, t(15, 10)},
		{"verify-revision-feedback", s.stageVerifyRevisionFeedback, t(15, 10)},
		{"verify-standards-under-pressure", s.stageVerifyStandardsUnderPressure, t(15, 10)},
		{"verify-artifacts-strict", s.stageVerifyArtifactsStrict, t(15, 10)},
	}
}
