# Spec: service-health-check

## Overview

Expose a /health endpoint that returns JSON containing the service status, uptime, and Go runtime version.

## Applies To

- `main.go`

## Requirements

### Health endpoint MUST return JSON with status, uptime, and version

The system MUST expose a /health HTTP endpoint. WHEN a client issues a GET request to /health, THEN the system MUST return an HTTP 200 OK response with a JSON body containing "status": "ok", "uptime" as seconds since server start, and "version" as the Go runtime version. The handler MUST be covered by unit tests verifying this behavior.

#### Scenario: Health endpoint returns successful JSON response with required fields

`@unit`

**GIVEN** the HTTP server is initialized with a health handler
**WHEN** a GET request is made to the health handler function
**THEN** the response status code is 200 OK
**AND** the response Content-Type is 'application/json'
**AND** the response body contains 'status' equal to 'ok'
**AND** the response body contains 'uptime' as a non-negative integer representing seconds since start
**AND** the response body contains 'version' matching the Go runtime version

