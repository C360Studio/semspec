---
category: sop
scope: all
severity: warning
applies_to:
  - "api/**"
domain:
  - testing
  - api-design
requirements:
  - "All API endpoints must have corresponding tests"
  - "API responses must use JSON format with consistent structure"
  - "New endpoints must be documented in README"
---

# API Development SOP

## Ground Truth

- Existing endpoints are defined in api/app.py
- Test patterns should follow the project's testing framework (pytest for Python)
- Response format is established by the /hello endpoint: JSON with a "message" key

## Rules

1. Every new API endpoint must have at least one test covering the happy path.
2. All API responses must return JSON with a "message" or "data" key.
3. New endpoints must be added to the README documentation.
4. Plan scope must reference actual project files (api/app.py, not invented paths).

## Violations

- Adding an endpoint without a corresponding test file or test task
- Returning plain text or HTML instead of JSON from an API route
- Referencing files that don't exist in the project (e.g., src/routes/api.js when the project uses api/app.py)
