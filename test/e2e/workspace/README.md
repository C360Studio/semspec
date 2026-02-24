# Go Web API

A production-grade Go REST API with JWT authentication, rate limiting, and PostgreSQL.

## Project Structure

```
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
```

## Getting Started

```bash
make build        # Build the binary
make test         # Run all tests
make run          # Start the server
```

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
