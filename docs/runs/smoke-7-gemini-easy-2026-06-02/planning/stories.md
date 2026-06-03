# Stories: 84360c511c18

*Generated from the story-preparer role (Sarah). **1 stories** ready the per-Requirement work for the executor pipeline, each carrying its own Tasks checklist and FilesOwned scope.*

## Health endpoint MUST return JSON with status, uptime, and version

*Requirement `requirement.84360c511c18.1` — 1 story(ies)*

### Implement /health endpoint

*`story.84360c511c18.1.1`*

Add a /health endpoint to the Go HTTP service that returns JSON status, uptime, and Go runtime version for monitoring.

**Components:** http-service

**Files owned:**

- `main.go`

**Tasks:**

- `task.84360c511c18.1.1.1` — Write failing unit tests for the health handler (targeting main_test.go) verifying JSON structure and 200 OK
- `task.84360c511c18.1.1.2` — Initialize a start time variable in main.go to track service uptime from boot
- `task.84360c511c18.1.1.3` — Implement the health handler returning JSON with status 'ok', uptime in seconds, and runtime.Version()
- `task.84360c511c18.1.1.4` — Register the /health endpoint in the main HTTP ServeMux
- `task.84360c511c18.1.1.5` — Verify the /health endpoint returns the expected JSON response via a smoke test

