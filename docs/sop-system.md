# SOP System

Standard Operating Procedures (SOPs) are project-specific rules that semspec enforces
structurally during planning and code review. SOPs let teams encode institutional
knowledge—coding standards, testing requirements, migration procedures—so the system
enforces them automatically, rather than relying on developers to remember them.

## What SOPs Are

An SOP is a Markdown file with YAML frontmatter stored in `.semspec/sources/docs/`. When
a plan is generated or reviewed, semspec retrieves relevant SOPs from the knowledge graph
and injects them into the LLM context. The plan-reviewer then validates the plan against
each SOP requirement and blocks approval when violations are found.

SOPs differ from `standards.json` rules in two ways:

- **Scope**: SOPs can target specific file patterns, semantic domains, or workflow stages.
  Standards apply globally.
- **Structure**: SOPs carry machine-readable requirements extracted from the frontmatter,
  including ground truth, violation examples, and file-scoped applicability.

## Authoring SOPs

### File Location

Store SOPs in `.semspec/sources/docs/`. Semsource (external service) watches this directory and
ingests new or modified files automatically.

```
.semspec/
└── sources/
    └── docs/
        ├── api-testing-standards.md
        ├── go-error-handling.md
        └── migration-procedures.md
```

### YAML Frontmatter

Every SOP begins with a YAML frontmatter block that controls ingestion and matching.

| Field | Type | Description |
|-------|------|-------------|
| `category` | string | Must be `"sop"` to be treated as an SOP |
| `scope` | string | When the SOP applies: `"plan"`, `"code"`, or `"all"` |
| `severity` | string | Enforcement level: `"error"`, `"warning"`, or `"info"` |
| `applies_to` | array of strings | Glob patterns for file matching (e.g., `["api/**", "*.go"]`) |
| `domain` | array of strings | Semantic domains (e.g., `["testing", "api-design"]`) |
| `requirements` | array of strings | Checkable rules, each expressed as a complete sentence |

**Scope values:**

- `"plan"` — Enforced during plan generation and plan review only
- `"code"` — Enforced during code review only
- `"all"` — Enforced in both planning and code review contexts

**Severity values:**

- `"error"` — Violation blocks approval; plan or code review returns `needs_changes`
- `"warning"` — Violation is flagged in the review findings but does not block approval
- `"info"` — Informational; included in context but not checked against requirements

### Markdown Body Structure

The body of an SOP follows a three-section convention that helps the LLM reason about
violations precisely:

1. **Ground Truth** — Factual statements about the existing codebase, established patterns,
   or frameworks in use. These are descriptive, not prescriptive.
2. **Rules** — Numbered enforcement rules. Each rule should correspond to one entry in the
   frontmatter `requirements` list.
3. **Violations** — Concrete examples of what constitutes a violation. These anchor the
   LLM's judgment and reduce false positives.

### Example SOP

```markdown
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
  - "API responses must use JSON format"
  - "README must document all endpoints"
---

# API Testing Standards

## Ground Truth

- The project uses Flask for the API backend
- Tests are expected alongside endpoint definitions

## Rules

1. Every API endpoint must have a corresponding test file
2. All API responses must return JSON format
3. Error responses must include error codes and messages

## Violations

- Adding an endpoint without a test file
- Returning plain text instead of JSON from an API endpoint
```

## Ingestion Pipeline

SOPs enter the knowledge graph through **semsource** — an external service that watches the
repository and publishes document entities to `graph.ingest.entity`. Semsource is not a semspec
processor; it runs as a separate container configured via `GRAPH_SOURCES` (or legacy `SEMSOURCE_URL`).

### Entry Points

There are two ways an SOP is ingested:

1. **NATS message** on `source.ingest.>` — Any component or external tool can publish
   an `IngestRequest` to this subject. Semsource consumes the message and processes the
   referenced file.
2. **File watcher** — Semsource watches `.semspec/sources/docs/` for changes. New files
   and modifications trigger ingestion automatically within seconds of being written.

### Frontmatter Path vs LLM Path

Semsource uses the YAML frontmatter when available to avoid an LLM call:

```
Document read from disk
        │
        ▼
Parser extracts frontmatter
        │
        ├─── Has frontmatter with "category" field?
        │              │
        │              ▼ Yes
        │    Metadata extracted directly (fast path)
        │    No LLM call needed
        │
        └─── No frontmatter or no category?
                       │
                       ▼
             LLM analyzes content
             (Analyzer.Analyze)
             Returns category, scope, severity, etc.
```

For SOPs, always include frontmatter. The fast path is more reliable and avoids LLM
classification errors.

### Entity Construction

After metadata extraction, semsource builds graph entities using vocabulary constants
from the source vocabulary. Chunk entities are published first to prevent
orphan references, followed by the parent document entity.

The resulting entity triples include:

| Predicate | Value |
|-----------|-------|
| `source.doc.category` | `"sop"` |
| `source.doc.scope` | `"plan"` / `"code"` / `"all"` |
| `source.doc.severity` | `"error"` / `"warning"` / `"info"` |
| `source.doc.applies_to` | Glob pattern string |
| `source.doc.requirements` | Array of requirement strings |
| `source.meta.name` | Document title |
| `source.doc.content` | Full markdown body |
| `source.meta.status` | `"ready"` |

All entities are published to `graph.ingest.entity` via JetStream for durable delivery.

## How SOPs Are Used

The semstreams `context-builder` component retrieves SOPs from the knowledge graph during
context assembly. SOPs are matched by scope, file patterns, and semantic domain, then
injected into the LLM prompt for the relevant workflow stage.

### Context Strategies

Context strategies use SOPs differently based on workflow stage:

| Stage | Budget Behavior |
|-------|-----------------|
| Planning | Best-effort — SOPs included if token budget allows; skipped silently if not |
| Plan Review | All-or-nothing — if SOPs exceed budget, context build fails. Plan review never proceeds without SOPs |
| Code Review | All-or-nothing — pattern match + domain match + cross-domain |

The all-or-nothing policy in plan review is intentional. Reviewing a plan without the
applicable SOPs could produce an incorrect approval decision. It is safer to fail the
context build and retry than to review with incomplete enforcement context.

## Plan Review Enforcement

The plan-reviewer component (`processor/plan-reviewer/`) validates generated plans against
SOPs. It watches the `PLAN_STATES` KV bucket for plans in status `drafted`, fetches SOP
context, and either approves the plan (advancing status to `reviewed`) or returns findings
so the planner can regenerate.

### Review Flow

```mermaid
flowchart TD
    A[PLAN_STATES\nstatus: drafted] --> B[plan-reviewer\nwatches KV bucket]
    B --> C[Fetch SOPs via SOPGatherer\nall-or-nothing policy]
    C --> D[LLM: validate plan against\neach SOP requirement]
    D -->|verdict: approved| E[PLAN_STATES\nstatus: reviewed]
    D -->|verdict: needs_changes| F[Findings published\nwith violation details]
    F --> G[planner regenerates\nwith findings in context]
    G -->|attempt 1-3| B
    G -->|attempt 4 — max retries| H[Session failed\nuser notified]
```

The planner receives the violation findings in its next prompt and regenerates the plan
with those constraints. This retry loop runs up to three times with a five-second backoff
between attempts.

### Review Result Format

The LLM returns a structured JSON result that the plan-reviewer parses:

```json
{
  "verdict": "needs_changes",
  "summary": "Two SOP violations found in the proposed plan.",
  "findings": [
    {
      "sop_id": "c360.semspec.source.doc.markdown.api-testing-standards",
      "sop_title": "API Testing Standards",
      "severity": "warning",
      "status": "violation",
      "issue": "Plan adds /users endpoint but does not mention test files.",
      "suggestion": "Add a task to create tests/test_users.py alongside the endpoint."
    },
    {
      "sop_id": "c360.semspec.source.doc.markdown.api-testing-standards",
      "sop_title": "API Testing Standards",
      "severity": "warning",
      "status": "compliant",
      "issue": "",
      "suggestion": ""
    }
  ]
}
```

**Finding status values:**

- `"compliant"` — The plan satisfies this requirement
- `"violation"` — The plan violates this requirement
- `"not_applicable"` — The requirement does not apply to this plan's scope

### Retry Behavior

When the plan-reviewer returns `needs_changes`, the planner regenerates the plan with the
violation findings included in its LLM context. This gives the planner concrete feedback on
what to fix. After three failed attempts, the session fails and the user is notified.

### LLM Configuration

The plan-reviewer uses the `plan_review` capability at temperature `0.3`. Lower temperature
produces more consistent, deterministic verdicts for the same plan and SOP combination.
See [Model Configuration](model-configuration.md) for capability-to-model mapping.

## Quick Start

The following example walks through the complete SOP lifecycle from file creation to plan
enforcement.

### Step 1: Write an SOP

```markdown
---
category: sop
scope: plan
severity: error
applies_to:
  - "api/**"
domain:
  - api-design
requirements:
  - "Every new API endpoint must have a documented error response schema"
  - "Endpoints must be listed in the project README"
---

# API Documentation Standards

## Ground Truth

- The project maintains a README at the repository root
- API error responses follow the {code, message} JSON schema

## Rules

1. Every new API endpoint must include a documented error response schema
2. All endpoints must appear in the README endpoints table

## Violations

- Adding an endpoint definition without updating the README
- Returning undocumented error formats from a new endpoint
```

Save this to `.semspec/sources/docs/api-doc-standards.md`.

### Step 2: SOP is Ingested

The file watcher detects the new file within seconds. Semsource reads the YAML frontmatter,
skips the LLM analysis step, and publishes the entity to `graph.ingest.entity`.

You can confirm ingestion by checking the message logger:

```bash
curl -s "http://localhost:8080/message-logger/entries?subject=graph.ingest.entity&limit=5" \
  | jq '.[0].payload.entity_id'
```

### Step 3: Run a Plan

```bash
/plan "Add user registration endpoint to the API"
```

### Step 4: Context Assembly

The planner watches `PLAN_STATES` for the new plan (status `created`) and triggers. After
generating Goal/Context/Scope (status → `drafted`), the plan-reviewer picks up the plan
automatically. Because the plan title references API work and the SOP has `scope: plan` and
`applies_to: ["api/**"]`, the SOPGatherer returns the new SOP.

### Step 5: Plan Review

The plan-reviewer calls the LLM with the plan content and SOP context. The LLM checks
each requirement:

- "Every new API endpoint must have a documented error response schema" — does the plan
  mention error schemas?
- "Endpoints must be listed in the project README" — does the plan include a README update
  task?

### Step 6: Verdict

**If compliant**: The plan-reviewer writes status `reviewed` to `PLAN_STATES`. The
requirement-generator picks it up and begins generating Requirements.

**If violated**: The plan-reviewer publishes `verdict: "needs_changes"` with specific
findings. The planner receives those findings in its next prompt, regenerates the plan, and
the plan-reviewer evaluates again. This repeats up to three times.

Once approved, plan state (status, Goal, Context, Scope) is stored as triples in the
`ENTITY_STATES` KV bucket.

## NATS Subjects

| Subject | Transport | Direction | Purpose |
|---------|-----------|-----------|---------|
| `source.ingest.>` | JetStream | Input | Ingest requests for new documents |
| `graph.ingest.entity` | JetStream | Output | Ingested SOP entities |
| `workflow.async.plan-reviewer` | JetStream | Input | Plan review triggers |
| `workflow.result.plan-reviewer.<slug>` | JetStream | Output | Review verdicts |

## Frontmatter Field Reference

| Field | Type | Required | Values | Description |
|-------|------|----------|--------|-------------|
| `category` | string | yes | `"sop"` | Identifies document as an SOP |
| `scope` | string | yes | `"plan"`, `"code"`, `"all"` | Controls when the SOP is applied |
| `severity` | string | yes | `"error"`, `"warning"`, `"info"` | Determines if violations block approval |
| `applies_to` | array | no | Glob strings | File patterns the SOP targets |
| `domain` | array | no | Domain strings | Semantic domains for domain-based matching |
| `requirements` | array | yes | Sentence strings | Checkable rules validated by the plan-reviewer |

When `applies_to` is omitted, the SOP matches all files within its scope. When `domain`
is omitted, the SOP is excluded from domain-based retrieval but still appears in scope and
pattern queries.

## Related Documentation

| Document | Description |
|----------|-------------|
| [How It Works](how-it-works.md) | End-to-end command execution overview |
| [Model Configuration](model-configuration.md) | LLM capability-to-model mapping |
| [API Reference](api.md) | REST API surface map |
