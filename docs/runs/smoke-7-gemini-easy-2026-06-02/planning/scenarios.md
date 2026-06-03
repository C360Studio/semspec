# Scenarios: 84360c511c18

*Generated from the scenario-generator role's output. **1 scenarios** verify the implementation, grouped by the requirement they cover.*

## Health endpoint MUST return JSON with status, uptime, and version

*Requirement `requirement.84360c511c18.1` — 1 scenario(s)*

### a GET request is made to the health handler function

*ID: `scenario.84360c511c18.1.1.1`*

**Given** the HTTP server is initialized with a health handler

**When** a GET request is made to the health handler function

**Then:**

- the response status code is 200 OK
- the response Content-Type is 'application/json'
- the response body contains 'status' equal to 'ok'
- the response body contains 'uptime' as a non-negative integer representing seconds since start
- the response body contains 'version' matching the Go runtime version

