# Proposal: 84360c511c18

## Why

Add a /health endpoint to the Go HTTP service that returns a JSON response containing the status ('ok'), uptime in seconds, and the Go runtime version. Implement the handler and its registration in main.go, and add unit tests in a new main_test.go file.

## What Changes

### New Capabilities

- `service-health-check` — Expose a /health endpoint that returns JSON containing the service status, uptime, and Go runtime version.

