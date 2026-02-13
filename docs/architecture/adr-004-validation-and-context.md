# ADR-004: Validation Layers and Context Management

## Status

Proposed (depends on ADR-003)

## Context

ADR-003 establishes the developer→reviewer execution loop. This ADR addresses:

1. **How do we catch mechanical failures?** (build breaks, test failures)
2. **How do we detect behavioral issues?** (retries, scope creep, context exhaustion)
3. **How do we manage finite context windows?** (budget, monitoring, checkpointing)
4. **What context does the reviewer need?** (SOPs, conventions, decisions)

### Problems with Current State

- No gate checks before review
- No awareness of retry patterns or escalation
- No context budget tracking
- Reviewer has no access to project conventions or SOPs

## Decision

### Two-Layer Validation

**Layer 1: Gate Checks (Mechanical, Binary)**

Shell commands run after developer completes, before reviewer. Pass/fail, no judgment.

```yaml
# .semspec/config.yaml
gates:
  - name: build
    command: "go build ./..."
    required: true
  - name: test
    command: "go test ./..."
    required: true
  - name: lint
    command: "golangci-lint run"
    required: false  # Warning only
```

If required gate fails → retry developer with error output.
If optional gate fails → warn but continue to reviewer.

**Layer 2: Risk Monitor (Behavioral, Graduated)**

New processor: `processor/risk-monitor/`

| Signal | Threshold | Response |
|--------|-----------|----------|
| Retry count | > 3 | Escalate to human |
| Context utilization | > 70% | Pause and checkpoint |
| Scope creep | Beyond task files | Flag for review |
| Conflicting changes | Same file by parallel tasks | Halt |

**Graduated responses:**
```
observe → log → warn → pause → halt → escalate
```

**NATS Subjects:**
- Input: `agent.monitor.*` (loop state changes)
- Output: `user.signal.*` (escalation events)

### Context Budget Model

Pre-execution budget calculation:

```
available_working_memory = model_context_window
    - system_prompt_tokens
    - plan_context_tokens
    - task_context_tokens
    - estimated_output_tokens
    - safety_margin (20%)

if available_working_memory < 8K:
    → task too large, decompose further
```

**Task sizing rules:**
1. File count heuristic: >3-4 files = probably too large
2. Token budget: file tokens < 40% of working memory
3. Auto-decomposition when budget exceeded

### Runtime Context Monitoring

Metrics tracked per loop:

| Metric | Description |
|--------|-------------|
| `context_tokens_used` | Running total |
| `context_utilization` | Percentage of window |
| `tokens_per_iteration` | Rate of growth |
| `iterations_remaining_estimate` | Predictive |

**Pressure thresholds:**

| Utilization | Color | Response |
|-------------|-------|----------|
| < 50% | Green | Normal |
| 50-65% | Yellow | Log warning |
| 65-75% | Orange | Consider checkpoint |
| 75-85% | Red | Checkpoint and evaluate |
| > 85% | Critical | Force checkpoint |

### Context Checkpointing

When pressure forces mid-task pause:

1. **Save artifacts** - Create checkpoint branch
2. **Extract decisions** - Query accumulated decisions from graph
3. **Create continuation** - New task with remaining criteria
4. **Fresh loop** - Clean context with checkpoint state

### Reviewer Context Assembly

The reviewer needs assembled context from the graph:

| Element | Source | Priority |
|---------|--------|----------|
| SOPs | `source.doc.sops.*` | Highest |
| Prior decisions | `prov.derived_from={plan_id}` | High |
| File summaries | AST signatures | Medium |
| Conventions | Accumulated patterns | Lower |

**Token budgeting for context:**

```
Reviewer Context Window (e.g., 32K)
├── System Prompt           ~2,000 tokens (fixed)
├── Task Output             ~variable
├── Assembled Context       ~variable (budgeted)
│   ├── SOPs (keep)
│   ├── Decisions (keep)
│   ├── File Summaries (trim)
│   └── Conventions (drop first)
├── Working Memory          ~6,000 tokens (reserved)
├── Output                  ~1,000 tokens (reserved)
└── Safety Margin           ~15%
```

Priority-based truncation when over budget.

### SOPs as Knowledge Sources

SOPs are natural language documents in `.semspec/sources/docs/sops/`:

```markdown
---
category: sop
applies_to: "*.go"
severity: error
---

# Error Handling

All errors must include context about what operation failed.

## Requirements
1. Wrap errors with `fmt.Errorf("operation: %w", err)`
2. Include relevant identifiers
...
```

Indexed into graph, queried by assembler based on `applies_to` matching task files.

**Benefits:**
- Natural language, not schemas
- Adding SOPs = adding documents
- Reviewer can reason about exceptions
- Versioned in graph

### Convention Learning

When reviewer approves, patterns can be captured:

```go
// On approval, extract noted patterns
for _, pattern := range reviewResult.NotedPatterns {
    graph.Publish(Entity{
        ID: fmt.Sprintf("semspec.convention.%s.%s", project, pattern.ID),
        Triples: []Triple{
            {Predicate: "semspec.convention.name", Object: pattern.Name},
            {Predicate: "semspec.convention.applies_to", Object: pattern.AppliesTo},
        },
    })
}
```

Creates learning flywheel: approved patterns inform future reviews.

### Model Registry Context Profiles

Add to model registry:

```go
type ContextProfile struct {
    ContextWindow            int  // Theoretical max
    EffectiveContext         int  // After overhead
    SweetSpot                int  // Best performance zone
    SystemPromptOverhead     int  // Fixed cost
    RecommendedMaxFileTokens int  // Per-task limit
}
```

**SNCO-level assignment:**
1. Filter models by capability
2. Check capacity: task fits in sweet_spot
3. Check performance history
4. Assign best fit

## Consequences

### Positive

- **Mechanical failures caught early** - Gates before review
- **Behavioral issues detected** - Risk monitor escalates
- **Context exhaustion prevented** - Budget and checkpointing
- **Reviewer has context** - SOPs, conventions, decisions
- **Learning accumulates** - Conventions from approvals

### Negative

- **New component** - Risk monitor processor
- **Graph queries** - Context assembly latency
- **Checkpoint complexity** - Branch management

### Risks

| Risk | Mitigation |
|------|------------|
| Checkpoint branch proliferation | Auto-cleanup after task completion |
| SOP document rot | Include in sources sync |
| False escalation | Tunable thresholds per project |

## Files

### New Files

| File | Purpose |
|------|---------|
| `processor/risk-monitor/` | Behavioral monitoring component |
| `workflow/context/assembler.go` | Graph-powered context assembly |
| `workflow/context/budget.go` | Budget calculation |
| `workflow/context/monitor.go` | Runtime monitoring |
| `workflow/context/checkpoint.go` | Checkpointing logic |
| `workflow/context/estimator.go` | Token estimation |
| `model/performance.go` | Model performance tracking |
| `workflow/assignment.go` | SNCO model assignment |

### Modified Files

| File | Change |
|------|--------|
| `workflow/task.go` | Extended context package |
| `model/registry.go` | ContextProfile addition |
| `configs/models.yaml` | Model profiles |

## Vocabulary Support

The context assembler queries SOP entities using predicates from `vocabulary/source/`.

**Graph Queries:**

```graphql
# Query SOP parent entities (excludes chunks)
{
  entities(filter: { predicatePrefix: "source.doc.sops" }) {
    id
    triples { predicate object }
  }
}
```

**Key Predicates for Context Assembly:**

| Predicate | Query Purpose |
|-----------|---------------|
| `source.doc.category` | Filter for `sop` (vs. spec, datasheet) |
| `source.doc.applies_to` | Match against task files |
| `source.doc.severity` | Prioritize error > warning > info for budget |
| `source.doc.summary` | Always fits in context (short) |
| `source.doc.requirements` | Key checkpoints (always included) |
| `source.doc.content` | Chunk content (budget-dependent) |
| `source.doc.chunk_index` | Order chunks correctly |

**Budget Allocation by Severity:**

When multiple SOPs match and budget is constrained, the assembler allocates chunk budget proportionally:

| Severity | Chunk Budget Share |
|----------|-------------------|
| error | 50% |
| warning | 35% |
| info | 15% |

Parent metadata (summary, requirements) is always included regardless of budget.

See `vocabulary/source/` for full predicate definitions and `ADR-005` for detailed context assembly logic.

## Dependencies

- **ADR-003**: Pipeline and roles (this builds on execution loop)
- **ADR-005**: SOP query patterns and context assembly architecture
- **ADR-006**: Document ingestion and chunking
- **vocabulary/source/**: Predicate definitions for SOP entities

## Related

- ADR-003: Pipeline Simplification and Adversarial Roles
- ADR-005: SOPs and Conventions as Knowledge Sources
- ADR-006: Sources and Knowledge Ingestion
- `vocabulary/source/` (predicate definitions)
- `docs/spec/semspec-workflow-refactor-spec.md`
- `docs/spec/semspec-sources-knowledge-spec.md`
