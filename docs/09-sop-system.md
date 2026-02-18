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

SOPs differ from the project constitution (`.semspec/constitution.yaml`) in two ways:

- **Scope**: SOPs can target specific file patterns, semantic domains, or workflow stages.
  A constitution applies globally.
- **Structure**: SOPs carry machine-readable requirements extracted from the frontmatter,
  enabling precise requirement-by-requirement validation.

## Authoring SOPs

### File Location

Store SOPs in `.semspec/sources/docs/`. The source-ingester watches this directory and
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

SOPs enter the knowledge graph through the source-ingester component
(`processor/source-ingester/`). See [Components](04-components.md) for full
source-ingester configuration details.

### Entry Points

There are two ways an SOP is ingested:

1. **NATS message** on `source.ingest.>` — Any component or external tool can publish
   an `IngestRequest` to this subject. The source-ingester consumes the message and
   processes the referenced file.
2. **File watcher** — The source-ingester watches `.semspec/sources/docs/` using
   `fsnotify`. New files and modifications trigger ingestion automatically within seconds
   of being written.

### Frontmatter Path vs LLM Path

The ingestion handler (`processor/source-ingester/handler.go`) uses the YAML frontmatter
when available to avoid an LLM call:

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

After metadata extraction, the handler builds graph entities using vocabulary constants
from `vocabulary/source/predicates.go`. Chunk entities are published first to prevent
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

The `SOPGatherer` (`processor/context-builder/gatherers/sop.go`) retrieves SOPs from the
knowledge graph for context assembly. It provides four retrieval methods that context
strategies compose to build SOP context.

### Retrieval Methods

**GetSOPsByScope(scope, patterns)**

Filters SOPs by their `scope` field. SOPs with `scope: all` match any requested scope and
are always included. The optional `patterns` parameter further restricts results to SOPs
whose `applies_to` field overlaps with the requested file patterns.

**GetSOPsForFiles(files)**

Matches the `applies_to` glob pattern of each SOP against a list of changed file paths.
SOPs without an `applies_to` pattern apply universally. Pattern matching uses both
`filepath.Match` for simple patterns and custom `**` expansion for recursive patterns
like `api/**/*.go`.

**GetSOPsByDomain(domains)**

Returns SOPs whose `domain` field (or `related_domains` field) overlaps with the provided
semantic domains. This enables cross-cutting matches—when a plan touches authentication
code, all auth-domain SOPs are included regardless of file path.

**GetSOPsByKeywords(keywords)**

Fuzzy keyword matching against the SOP's keyword index. Short keywords (fewer than four
characters) require an exact match. Longer keywords allow substring matching in either
direction, which surfaces related SOPs without exact domain or path correspondence.

### Context Strategies

Context strategies use SOPs differently based on workflow stage:

| Stage | Strategy | Budget Behavior |
|-------|----------|-----------------|
| Planning (Step 7) | Best-effort | SOPs included if token budget allows; skipped silently if not |
| Plan Review (Step 1) | All-or-nothing | If SOPs exceed budget, context build fails entirely. Plan review never proceeds without SOPs. |
| Code Review (Step 1) | All-or-nothing | Three-layer merge: pattern match + domain match + cross-domain |

The all-or-nothing policy in plan review is intentional. Reviewing a plan without the
applicable SOPs could produce an incorrect approval decision. It is safer to fail the
context build and retry than to review with incomplete enforcement context.

## Plan Review Enforcement

The plan-reviewer component (`processor/plan-reviewer/`) validates generated plans against
SOPs before the plan-coordinator progresses to task generation.

### Review Flow

```
PlanCoordinatorTrigger
        │
        ▼
plan-coordinator builds context
  - Fetches SOPs via SOPGatherer (all-or-nothing)
  - Enriches with graph entities (related plans, code patterns)
  - Packages as PlanReviewTrigger
        │
        ▼
workflow.trigger.plan-reviewer (JetStream)
        │
        ▼
plan-reviewer receives trigger
  - Validates PlanReviewTrigger payload
  - Resolves LLM model (capability: reviewing, temperature 0.3)
  - Calls LLM with PlanReviewerSystemPrompt + PlanReviewerUserPrompt
  - Parses JSON review result
        │
        ├─── verdict: "approved"
        │       │
        │       ▼
        │   Publishes result to workflow.result.plan-reviewer.<slug>
        │   plan-coordinator proceeds to task generation
        │
        └─── verdict: "needs_changes"
                │
                ▼
            Publishes result with findings
            plan-coordinator retries planning
            (up to 3 attempts, 5s backoff)
```

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

When the plan-reviewer returns `needs_changes`, the plan-coordinator regenerates the plan
with the violation findings included in the LLM context. This gives the planner concrete
feedback on what to fix. The retry loop runs up to three times with a five-second backoff
between attempts.

If all three attempts produce `needs_changes`, the plan-coordinator fails the session and
notifies the user.

### LLM Configuration

The plan-reviewer uses the `reviewing` capability at temperature `0.3`. Lower temperature
produces more consistent, deterministic verdicts for the same plan and SOP combination.
See [Model Configuration](07-model-configuration.md) for capability-to-model mapping.

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

The file watcher detects the new file within seconds. The source-ingester reads the YAML
frontmatter, skips the LLM analysis step, and publishes the entity to `graph.ingest.entity`.

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

The plan-coordinator builds context for the plan-reviewer. Because the plan title
references API work and the SOP has `scope: plan` and `applies_to: ["api/**"]`, the
SOPGatherer returns the new SOP.

### Step 5: Plan Review

The plan-reviewer calls the LLM with the plan content and SOP context. The LLM checks
each requirement:

- "Every new API endpoint must have a documented error response schema" — does the plan
  mention error schemas?
- "Endpoints must be listed in the project README" — does the plan include a README update
  task?

### Step 6: Verdict

**If compliant**: The plan-reviewer publishes `verdict: "approved"`. The plan-coordinator
proceeds to task generation.

**If violated**: The plan-reviewer publishes `verdict: "needs_changes"` with specific
findings. The plan-coordinator regenerates the plan with the findings in context and
re-reviews. This repeats up to three times.

Once approved, the plan-coordinator triggers task generation and the plan appears in
`.semspec/plans/<slug>/plan.json` with full Goal/Context/Scope content.

## NATS Subjects

| Subject | Transport | Direction | Purpose |
|---------|-----------|-----------|---------|
| `source.ingest.>` | JetStream | Input | Ingest requests for new documents |
| `graph.ingest.entity` | JetStream | Output | Ingested SOP entities |
| `workflow.trigger.plan-reviewer` | JetStream | Input | Plan review triggers |
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
| [Components](04-components.md) | Source-ingester and plan-reviewer component configuration |
| [Workflow System](05-workflow-system.md) | How plan generation and the planning loop work |
| [How It Works](01-how-it-works.md) | End-to-end command execution overview |
