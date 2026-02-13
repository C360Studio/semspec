# ADR-003: Pipeline Simplification and Adversarial Roles

## Status

Proposed

## Context

### The Strategic Corporal Insight

> "Quality emerges from tension between roles with different objective functions."

- Developer optimizes for **completion**
- Reviewer optimizes for **trustworthiness**

No single role produces quality. The adversarial interplay does.

### Current State

The existing pipeline (`/propose → /design → /spec → /tasks`) has:

- **4 documents, 4 validation cycles** - Heavy ceremony
- **Single roles only** - proposal-writer, design-writer, etc.
- **Self-validation** - Writer validates own work
- **No execution** - Tasks generated but not implemented

### Problems

1. **Document proliferation** - Four similar documents repeating information
2. **No adversarial validation** - Writer validates own output
3. **No execution** - Human must implement tasks manually
4. **Exploration conflated with commitment** - No scratchpad phase

## Decision

### Replace 4-Document Pipeline with Explore → Plan → Execute

```
OLD: /propose → /design → /spec → /tasks (4 docs, heavy)
NEW: /explore → /plan → /execute (2 modes, adaptive)
```

| Mode | Purpose | Artifact |
|------|---------|----------|
| Explore | Understand problem space | Uncommitted plan (scratchpad) |
| Plan | Commit to approach | Plan document (mission orders) |
| Execute | Developer→Reviewer loop | Implemented code |

### Plan Entity with Committed Field

Single entity type replaces four documents:

```go
type Plan struct {
    ID           string      `json:"id"`
    Slug         string      `json:"slug"`
    Title        string      `json:"title"`
    Committed    bool        `json:"committed"`  // false=exploration, true=plan
    Situation    string      `json:"situation"`
    Mission      string      `json:"mission"`
    Execution    string      `json:"execution"`
    Constraints  Constraints `json:"constraints"`
    Coordination string      `json:"coordination"`
    CreatedAt    time.Time   `json:"created_at"`
    CommittedAt  *time.Time  `json:"committed_at,omitempty"`
}

type Constraints struct {
    In         []string `json:"in"`           // What's in scope
    Out        []string `json:"out"`          // What's excluded
    DoNotTouch []string `json:"do_not_touch"` // Protected files/systems
}
```

**Uncommitted** (exploration mode):
- Scratchpad for ideas and investigation
- Can be modified freely
- Not visible to execution engine

**Committed** (plan mode):
- Frozen intent document
- Drives task generation
- Referenced during execution

### Plan Document Template

```markdown
# Plan: [title]

## Situation
What exists now. Entity references to existing code/specs.

## Mission
What we're doing and why. Success criteria.

## Execution
Approach and sketch. Architectural decisions.

## Constraints
- IN: what's in scope
- OUT: what's excluded
- DO NOT TOUCH: files/systems that must not be modified

## Coordination
Dependencies, sequencing, entity references.
```

### New Commands

| Command | Action |
|---------|--------|
| `/explore [topic]` | Create uncommitted plan, open exploration mode |
| `/plan [title]` | Create committed plan directly |
| `/promote` | Convert current exploration to committed plan |
| `/auto [topic]` | Full auto: plan → tasks → execute |

### Developer → Reviewer Execution Loop

Adversarial tension through role separation, with **iterative back-edges**:

```
                    ┌─────────────────────────────────────┐
                    │                                     │
Plan ◄──────────────┤ "misscoped" or "architectural"      │
  │                 │                                     │
  ▼                 │                                     │
Task ◄──────────────┤ "too_big"                           │
  │                 │                                     │
  ▼                 │                                     │
Developer ◄─────────┤ "fixable"                           │
  │                 │                                     │
  ▼                 │                                     │
Gates ──────────────┤                                     │
  │                 │                                     │
  ▼                 │                                     │
Reviewer ───────────┴─────────────────────────────────────┘
  │
  ▼ "approved"
Done
```

**Rejection types** allow routing to appropriate recovery:

| Rejection Type | Meaning | Action |
|----------------|---------|--------|
| `fixable` | Code issue developer can fix | Retry developer |
| `misscoped` | Task boundaries wrong | Back to planning |
| `architectural` | Design flaw | Back to design |
| `too_big` | Task should be decomposed | Split into subtasks |

### Role Definitions

**Developer Role:**

| Aspect | Description |
|--------|-------------|
| Access | Write to files, git |
| Input | Plan intent + task context + acceptance criteria |
| Objective | Task completion |
| Blind to | Reviewer criteria, SOP details |

The developer sees only what's needed to complete the task. They cannot see what the reviewer will check.

**Reviewer Role:**

| Aspect | Description |
|--------|-------------|
| Access | Read-only |
| Input | Task output + conventions + context utilization |
| Objective | "Would I trust this in production?" |
| Catches | Lifecycle issues, test gaming, architectural drift |

The reviewer optimizes for a different objective. This tension produces quality.

### Role Prompts

Developer prompt focuses on:
- Understanding the task acceptance criteria
- Writing clean, functional code
- Following explicit constraints from plan
- Not knowing what will be reviewed

Reviewer prompt focuses on:
- Production readiness judgment
- Checking for shortcuts under context pressure
- Validating adherence to conventions
- Categorizing rejection type for proper routing
- Explaining rejections with specific, actionable feedback

### Two-Layer Validation

Quality assurance uses two complementary layers:

```
Developer output
       │
       ▼
┌─────────────────────────────────┐
│ Layer 1: Structural Gates       │
│ (No LLM - deterministic)        │
│ - go build ./...                │
│ - go vet ./...                  │
│ - golangci-lint run             │
│ - go test ./...                 │
│ - go test -cover (threshold)    │
└────────────────┬────────────────┘
                 │ ALL PASS
                 ▼
┌─────────────────────────────────┐
│ Layer 2: LLM Reviewer           │
│ (Judgment + SOP interpretation) │
│ - Production readiness          │
│ - Architectural compliance      │
│ - Semantic correctness          │
│ - SOP coverage verification     │
└─────────────────────────────────┘
```

**Why two layers:**

| Layer | Catches | Benefit |
|-------|---------|---------|
| Structural Gates | Compile errors, test failures, lint issues | LLM can't charm past `go vet` |
| LLM Reviewer | Semantic issues, SOP violations, architectural drift | Judgment that tooling can't provide |

**Hard enforcement**: LLM never sees code that fails structural checks. This saves cost and prevents negotiation over objective failures.

### Gate Configuration (Presets + Overrides)

Language presets that projects can extend:

```yaml
# .semspec/config.yaml
gates:
  preset: go  # Uses built-in Go preset
  overrides:
    - name: coverage
      required: true  # Override: make coverage required
      threshold: 80
  additional:
    - name: custom-check
      command: "./scripts/check-migrations.sh"
      required: true
```

**Built-in Go Preset:**

```yaml
# internal preset (user doesn't write this)
go:
  - name: build
    command: "go build ./..."
    required: true
  - name: vet
    command: "go vet ./..."
    required: true
  - name: lint
    command: "golangci-lint run"
    required: true
  - name: test
    command: "go test ./..."
    required: true
  - name: coverage
    command: "go test -coverprofile=coverage.out ./..."
    required: false
    threshold: 80  # Warn if below
```

**Future presets:** `typescript`, `python`, `rust` (add when needed)

### Reviewer Output Schema

The reviewer must output structured JSON for workflow routing:

```json
{
  "verdict": "approved",
  "rejection_type": null,
  "sop_review": [
    {
      "sop_id": "source.doc.sops.error-handling",
      "status": "passed",
      "evidence": "Error wrapping uses fmt.Errorf with %w at lines 45, 67, 89",
      "violations": []
    },
    {
      "sop_id": "source.doc.sops.test-coverage",
      "status": "violated",
      "evidence": "New function ProcessData has no test coverage",
      "violations": ["Missing unit test for ProcessData"]
    }
  ],
  "confidence": 0.85,
  "feedback": "Overall implementation is solid but missing test coverage for ProcessData",
  "patterns": [
    {
      "name": "Context timeout in handlers",
      "pattern": "All HTTP handlers use context.WithTimeout",
      "applies_to": "handlers/*.go"
    }
  ]
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `verdict` | Yes | `approved` or `rejected` |
| `rejection_type` | If rejected | `fixable`, `misscoped`, `architectural`, or `too_big` |
| `sop_review` | Yes | Array of SOP evaluations (see below) |
| `confidence` | Yes | Reviewer confidence (0.0-1.0) |
| `feedback` | Yes | Summary with specific, actionable details |
| `patterns` | No | New patterns to remember (convention learning) |

**SOP Review Entry:**

| Field | Required | Description |
|-------|----------|-------------|
| `sop_id` | Yes | Entity ID of the SOP (e.g., `source.doc.sops.error-handling`) |
| `status` | Yes | `passed`, `violated`, or `not_applicable` |
| `evidence` | Yes | Specific code locations/patterns observed |
| `violations` | If violated | List of specific violations found |

### Reviewer Integrity: Preventing Rubber Stamps

Structural validation before accepting reviewer output:

1. **Coverage check**: All applicable SOPs must have entries in `sop_review`
2. **Evidence required**: Each SOP entry must have non-empty `evidence` field
3. **Violation consistency**: If any SOP has `status: "violated"` → verdict cannot be "approved"
4. **Confidence threshold**: If `confidence < 0.7` → escalate to human

```
Reviewer output
       │
       ▼
┌─────────────────────────────────┐
│ validate_review step            │
│                                 │
│ 1. All SOPs have entries?       │
│ 2. All entries have evidence?   │
│ 3. Violations → rejected?       │
│ 4. Confidence ≥ 0.7?            │
└────────────────┬────────────────┘
                 │ ALL PASS
                 ▼
       Accept verdict
```

This creates **engineered tension**:
- Reviewer can't approve without checking each applicable SOP
- Must provide evidence (can't just claim "checked")
- Violations force rejection (can't approve with known issues)
- Low confidence triggers human review

### SOP Context Assembly

Before the reviewer runs, the context assembler queries SOPs matching task files:

```
assemble_context step
        │
        ▼
┌─────────────────────────────────┐
│  Context Assembler              │
│                                 │
│  1. Get files modified by task  │
│  2. Query SOPs where applies_to │
│     matches any task file       │
│  3. Query conventions similarly │
│  4. Fit within context budget   │
└─────────────────────────────────┘
        │
        ▼
Reviewer prompt with SOP context
```

**Context priority** (when budget is tight):

| Priority | Source | Reason |
|----------|--------|--------|
| 1 | SOPs | Human-authored, authoritative |
| 2 | Prior decisions | Task-specific context |
| 3 | Conventions | Learned, supplementary |

See ADR-004 for context budget details, ADR-005 for SOP query patterns, ADR-006 for document ingestion.

### Execution Workflow Configuration

Uses semstreams workflow processor with `publish_agent` action type.

New workflow: `configs/workflows/plan-and-execute.json`

```json
{
  "id": "plan-and-execute",
  "name": "Plan and Execute Workflow",
  "description": "Developer→Reviewer loop with two-layer validation",
  "version": "1.0.0",
  "enabled": true,
  "trigger": {
    "subject": "workflow.trigger.plan-and-execute"
  },
  "steps": [
    {
      "name": "developer",
      "action": {
        "type": "publish_agent",
        "subject": "agent.task.development",
        "role": "developer",
        "model": "${trigger.payload.model}",
        "prompt": "Implement the following task.\n\nTask: ${trigger.payload.task_description}\n\nAcceptance Criteria:\n${trigger.payload.acceptance_criteria}\n\nConstraints:\n${trigger.payload.constraints}"
      },
      "on_success": "structural_gates",
      "on_fail": "developer_failed",
      "timeout": "10m"
    },
    {
      "name": "structural_gates",
      "action": {
        "type": "call",
        "subject": "workflow.gate.structural",
        "payload": {
          "slug": "${trigger.payload.slug}",
          "preset": "${trigger.payload.gate_preset:-go}",
          "overrides": "${trigger.payload.gate_overrides:-[]}"
        }
      },
      "on_success": "assemble_context",
      "on_fail": "retry_developer_gates",
      "timeout": "5m"
    },
    {
      "name": "assemble_context",
      "action": {
        "type": "call",
        "subject": "workflow.context.assemble",
        "payload": {
          "task_id": "${trigger.payload.task_id}",
          "files": "${steps.developer.output.files_modified}"
        }
      },
      "on_success": "reviewer",
      "on_fail": "reviewer",
      "timeout": "30s"
    },
    {
      "name": "reviewer",
      "action": {
        "type": "publish_agent",
        "subject": "agent.task.review",
        "role": "reviewer",
        "model": "${trigger.payload.model}",
        "prompt": "Review the following implementation for production readiness.\n\n## Applicable SOPs\n\n${steps.assemble_context.output.sops}\n\n## Conventions\n\n${steps.assemble_context.output.conventions}\n\n## Task\n\n${trigger.payload.task_description}\n\n## Implementation\n\n${steps.developer.output.result}\n\nYou MUST evaluate each SOP and provide structured output.\n\nOutput JSON with:\n- verdict: \"approved\" or \"rejected\"\n- rejection_type: if rejected, one of fixable|misscoped|architectural|too_big\n- sop_review: array of {sop_id, status, evidence, violations} for each applicable SOP\n- confidence: 0.0-1.0\n- feedback: summary with specific details\n- patterns: (optional) new patterns to remember"
      },
      "on_success": "validate_review",
      "on_fail": "reviewer_failed",
      "timeout": "5m"
    },
    {
      "name": "validate_review",
      "action": {
        "type": "call",
        "subject": "workflow.review.validate",
        "payload": {
          "task_id": "${trigger.payload.task_id}",
          "review_output": "${steps.reviewer.output}",
          "expected_sops": "${steps.assemble_context.output.sop_ids}",
          "confidence_threshold": 0.7
        }
      },
      "on_success": "verdict_check",
      "on_fail": "review_invalid"
    },
    {
      "name": "review_invalid",
      "action": {
        "type": "publish",
        "subject": "user.signal.escalate",
        "payload": {
          "task_id": "${trigger.payload.task_id}",
          "reason": "Review validation failed: ${steps.validate_review.output.reason}",
          "details": "${steps.validate_review.output.details}"
        }
      },
      "on_success": "complete"
    },
    {
      "name": "verdict_check",
      "condition": {
        "field": "${steps.reviewer.output.verdict}",
        "operator": "eq",
        "value": "approved"
      },
      "action": {
        "type": "publish",
        "subject": "workflow.task.complete",
        "payload": {
          "task_id": "${trigger.payload.task_id}",
          "status": "approved",
          "patterns": "${steps.reviewer.output.patterns}"
        }
      },
      "on_success": "complete",
      "on_fail": "categorize_rejection"
    },
    {
      "name": "categorize_rejection",
      "condition": {
        "field": "${steps.reviewer.output.rejection_type}",
        "operator": "eq",
        "value": "fixable"
      },
      "action": {
        "type": "publish",
        "subject": "workflow.events",
        "payload": {"event": "rejection_categorized", "type": "fixable"}
      },
      "on_success": "retry_developer_feedback",
      "on_fail": "check_misscoped"
    },
    {
      "name": "check_misscoped",
      "condition": {
        "field": "${steps.reviewer.output.rejection_type}",
        "operator": "eq",
        "value": "misscoped"
      },
      "action": {
        "type": "publish",
        "subject": "workflow.trigger.plan-refinement",
        "payload": {
          "original_task_id": "${trigger.payload.task_id}",
          "feedback": "${steps.reviewer.output.feedback}",
          "plan_slug": "${trigger.payload.slug}"
        }
      },
      "on_success": "complete",
      "on_fail": "check_architectural"
    },
    {
      "name": "check_architectural",
      "condition": {
        "field": "${steps.reviewer.output.rejection_type}",
        "operator": "eq",
        "value": "architectural"
      },
      "action": {
        "type": "publish",
        "subject": "workflow.trigger.design-refinement",
        "payload": {
          "original_task_id": "${trigger.payload.task_id}",
          "feedback": "${steps.reviewer.output.feedback}",
          "plan_slug": "${trigger.payload.slug}"
        }
      },
      "on_success": "complete",
      "on_fail": "check_too_big"
    },
    {
      "name": "check_too_big",
      "condition": {
        "field": "${steps.reviewer.output.rejection_type}",
        "operator": "eq",
        "value": "too_big"
      },
      "action": {
        "type": "publish",
        "subject": "workflow.trigger.task-decomposition",
        "payload": {
          "original_task_id": "${trigger.payload.task_id}",
          "feedback": "${steps.reviewer.output.feedback}",
          "plan_slug": "${trigger.payload.slug}"
        }
      },
      "on_success": "complete",
      "on_fail": "unknown_rejection"
    },
    {
      "name": "unknown_rejection",
      "action": {
        "type": "publish",
        "subject": "user.signal.escalate",
        "payload": {
          "task_id": "${trigger.payload.task_id}",
          "reason": "Unknown rejection type: ${steps.reviewer.output.rejection_type}",
          "feedback": "${steps.reviewer.output.feedback}"
        }
      },
      "on_success": "complete"
    },
    {
      "name": "retry_developer_gates",
      "condition": {
        "field": "${execution.iteration}",
        "operator": "lt",
        "value": 3
      },
      "action": {
        "type": "publish_agent",
        "subject": "agent.task.development",
        "role": "developer",
        "model": "${trigger.payload.model}",
        "prompt": "Structural gate checks failed. Fix the following issues:\n\n${steps.structural_gates.output.feedback}\n\nOriginal task: ${trigger.payload.task_description}"
      },
      "on_success": "structural_gates",
      "on_fail": "max_retries_exceeded"
    },
    {
      "name": "retry_developer_feedback",
      "condition": {
        "field": "${execution.iteration}",
        "operator": "lt",
        "value": 3
      },
      "action": {
        "type": "publish_agent",
        "subject": "agent.task.development",
        "role": "developer",
        "model": "${trigger.payload.model}",
        "prompt": "Reviewer rejected implementation. Address the feedback:\n\n${steps.reviewer.output.feedback}\n\nOriginal task: ${trigger.payload.task_description}"
      },
      "on_success": "structural_gates",
      "on_fail": "max_retries_exceeded"
    },
    {
      "name": "max_retries_exceeded",
      "action": {
        "type": "publish",
        "subject": "user.signal.escalate",
        "payload": {
          "task_id": "${trigger.payload.task_id}",
          "reason": "Max retries exceeded after ${execution.iteration} attempts",
          "last_feedback": "${steps.reviewer.output.feedback}"
        }
      },
      "on_success": "complete"
    },
    {
      "name": "developer_failed",
      "action": {
        "type": "publish",
        "subject": "user.signal.error",
        "payload": {
          "task_id": "${trigger.payload.task_id}",
          "error": "Developer agent failed: ${steps.developer.error}"
        }
      },
      "on_success": "complete"
    },
    {
      "name": "reviewer_failed",
      "action": {
        "type": "publish",
        "subject": "user.signal.error",
        "payload": {
          "task_id": "${trigger.payload.task_id}",
          "error": "Reviewer agent failed: ${steps.reviewer.error}"
        }
      },
      "on_success": "complete"
    },
    {
      "name": "complete",
      "action": {
        "type": "publish",
        "subject": "workflow.events",
        "payload": {
          "event": "task_execution_complete",
          "task_id": "${trigger.payload.task_id}",
          "iterations": "${execution.iteration}"
        }
      }
    }
  ],
  "timeout": "30m",
  "max_iterations": 3
}
```

**Key semstreams features used:**
- `publish_agent`: Spawns agentic loop with role/model/prompt
- `call`: Request/response for gate validation and review verification
- `${steps.*.output.*}`: Step output interpolation
- `${execution.iteration}`: Retry tracking
- `condition`: Conditional branching on reviewer verdict
- **Two-layer validation**: `structural_gates` → `reviewer` → `validate_review`
- **Iterative routing**: Rejection types trigger sibling workflows for plan/design refinement
- **Workflow escape**: `workflow.trigger.*` subjects allow back-edges to planning

### Deprecation Path

| Command | Status | Message |
|---------|--------|---------|
| `/propose` | Deprecated | "Use /explore or /plan instead" |
| `/design` | Deprecated | "Design is now part of /plan" |
| `/spec` | Deprecated | "Specs are now part of /plan" |

Old commands continue working during transition but emit warnings.

## Consequences

### Positive

- **Reduced ceremony** - One plan document instead of four
- **Exploration phase** - Scratchpad before commitment
- **Adversarial quality** - Developer/reviewer tension catches issues
- **Execution capability** - Tasks actually get implemented
- **Clear roles** - Each optimizes for different objective

### Negative

- **Learning curve** - Users familiar with old commands
- **Role complexity** - Must maintain two prompt sets
- **Migration period** - Both old and new commands active

### Risks

| Risk | Mitigation |
|------|------------|
| Developer/reviewer collusion | Different models, different prompts |
| Old command habits | Clear deprecation warnings, documentation |
| Exploration never commits | UI shows uncommitted plans prominently |

## Files

### New Files

| File | Purpose |
|------|---------|
| `commands/explore.go` | Create uncommitted plan |
| `commands/plan.go` | Create committed plan |
| `commands/promote.go` | Promote exploration to plan |
| `commands/auto.go` | Full automation mode |
| `commands/execute.go` | Trigger plan-and-execute workflow |
| `workflow/prompts/developer.go` | Developer role prompt |
| `workflow/prompts/reviewer.go` | Reviewer role prompt |
| `processor/structural-gates/` | Two-layer gate validation (preset system) |
| `processor/context-assembler/` | SOP/convention context assembly component |
| `processor/review-validator/` | Validate reviewer output integrity |
| `configs/workflows/plan-and-execute.json` | Execution workflow |
| `configs/workflows/plan-refinement.json` | Triggered when task is misscoped |
| `configs/workflows/design-refinement.json` | Triggered when architectural issue found |
| `configs/workflows/task-decomposition.json` | Triggered when task too big |
| `schemas/structural-gates.v1.json` | Structural gates schema |
| `schemas/review-validator.v1.json` | Review validator schema |
| `configs/gates/go.yaml` | Built-in Go gate preset |

### Modified Files

| File | Change |
|------|--------|
| `workflow/types.go` | Plan struct with committed field |
| `workflow/entity.go` | PlanEntityID generator |
| `workflow/structure.go` | CreatePlan, LoadPlan, PromotePlan |
| `commands/propose.go` | Deprecation warning |
| `commands/design.go` | Deprecation warning |
| `commands/spec.go` | Deprecation warning |

## Vocabulary Support

The workflow uses predicates from `vocabulary/source/` for SOP entity references in the reviewer output schema.

**SOP Entity IDs:**

SOP entity IDs follow the pattern `source.doc.sops.{slug}` where slug is derived from the filename.

| Document Path | Entity ID |
|---------------|-----------|
| `.semspec/sources/docs/sops/error-handling.md` | `source.doc.sops.error-handling` |
| `.semspec/sources/docs/sops/test-coverage.md` | `source.doc.sops.test-coverage` |

**Predicates Used in Review:**

| Predicate | Usage in Workflow |
|-----------|-------------------|
| `source.doc.category` | Filter for `sop` category during context assembly |
| `source.doc.applies_to` | Match against task files to find applicable SOPs |
| `source.doc.severity` | Determine if violation blocks approval (error) or warns (warning) |
| `source.doc.requirements` | Key checkpoints always included in reviewer context |

**SOP Review Output:**

The `sop_id` field in reviewer output matches entity IDs using the vocabulary's entity format. This enables the `validate_review` step to verify all applicable SOPs were checked.

See `vocabulary/source/` for the full predicate catalog.

## Related

- ADR-004: Validation Layers and Context Management (gates, risk monitor, budgets)
- ADR-005: SOPs and Conventions as Knowledge Sources (reviewer context, SOP query patterns)
- ADR-006: Sources and Knowledge Ingestion (SOP document ingestion, chunking)
- `vocabulary/source/` (predicate definitions for SOP entities)
- `docs/spec/semspec-workflow-refactor-spec.md` (full specification)
- semstreams ADR-011: Workflow Processor (action types, execution model)
- semstreams ADR-018: Agentic Workflow Orchestration (decoupling pattern)
- Strategic Corporal essay (quality from role tension)
