# Requirements: 84360c511c18

*Generated from the requirement-generator role's output. **1 requirements** partition the implementation work.*

## Health endpoint MUST return JSON with status, uptime, and version

**ID:** `requirement.84360c511c18.1` | **Status:** `active`

The system MUST expose a /health HTTP endpoint. WHEN a client issues a GET request to /health, THEN the system MUST return an HTTP 200 OK response with a JSON body containing "status": "ok", "uptime" as seconds since server start, and "version" as the Go runtime version. The handler MUST be covered by unit tests verifying this behavior.

**Verified by 1 scenario(s)** — see `scenarios.md`.

