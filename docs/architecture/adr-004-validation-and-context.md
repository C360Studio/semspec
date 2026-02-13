# ADR-004: Validation Layers and Context Management

## Status

Proposed (depends on ADR-003)

## Context

ADR-003 establishes the developer→reviewer execution loop. This ADR addresses:

1. **How do we catch mechanical failures?** (build breaks, test failures)
2. **How do we manage finite context windows?** (budget, task sizing)
3. **What context does the reviewer need?** (SOPs, conventions, decisions)

### Problems with Current State

- No gate checks before review
- No context budget tracking
- No task sizing validation before execution
- Reviewer has no access to project conventions or SOPs

## Scope

### Phase 1 (This ADR)

| Feature | Description |
|---------|-------------|
| Gate checks | Structural validation (build, test, lint) before reviewer |
| Context budget formula | Pre-execution task sizing validation |
| Task sizing heuristics | >3-4 files = too large, 40% budget rule |
| Reviewer context priority | SOPs > decisions > conventions |
| Reference semstreams runtime | Use existing ContextConfig for execution |

### Future Work (Deferred)

| Feature | Reason | Future ADR |
|---------|--------|------------|
| Risk Monitor processor | No integration design yet | TBD |
| Context checkpointing | Branch API undefined | TBD |
| SNCO-level assignment | Undefined concept | TBD |
| Convention learning | Move to ADR-005 | ADR-005 |
| Complex pressure thresholds | Semstreams 60% compaction sufficient | N/A |

## Decision

### Gate Checks (Layer 1)

Shell commands run after developer completes, before reviewer. Pass/fail, no judgment.

Gate configuration uses the preset system defined in ADR-003:

```yaml
# .semspec/config.yaml
gates:
  preset: go  # Uses built-in Go preset
  overrides:
    - name: coverage
      required: true
      threshold: 80
  additional:
    - name: custom-check
      command: "./scripts/check-migrations.sh"
      required: true
```

If required gate fails → retry developer with error output.
If optional gate fails → warn but continue to reviewer.

See ADR-003 for the full gate preset system and built-in presets.

### Context Budget Model

**Pre-execution budget calculation:**

Before `/execute` triggers a task (see ADR-003 Task Generation), validate it fits in context:

```
task_token_estimate = sum(file_tokens for file in task.files)
                    + task_description_tokens
                    + acceptance_criteria_tokens

available_budget = model_context_window
                 - system_prompt_overhead (~2000)
                 - plan_context (~variable)
                 - safety_margin (20%)

if task_token_estimate > available_budget * 0.40:
    → task too large, trigger task-decomposition workflow
```

**Task sizing rules:**
1. File count heuristic: >3-4 files = probably too large
2. Token budget: file tokens < 40% of available budget
3. If budget exceeded → trigger `workflow.trigger.task-decomposition` (see ADR-003)

**Token estimation:**

Use simple 4-character-per-token heuristic (same as semstreams). Sufficient for sizing validation.

### Runtime Context Management

Semstreams agentic-loop already provides runtime context management:

```go
// semstreams/processor/agentic-loop/config.go
type ContextConfig struct {
    ModelLimits      map[string]int  // e.g., "claude-sonnet": 200000
    CompactThreshold float64         // 0.60 = compact at 60%
    HeadroomTokens   int             // Reserved for responses (6400)
    ToolResultMaxAge int             // GC after N iterations
}
```

**What semstreams provides:**
- Per-model context limits in config
- Token counting (4 chars/token heuristic)
- Utilization tracking (0.0-1.0)
- Compaction at 60% threshold
- Priority-based region eviction
- Tool result garbage collection

**Semspec uses these via workflow configuration**, not custom implementation.

See semstreams ADR-015 for full context memory management details.

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

### Convention Learning (Deferred)

Convention learning (extracting patterns from approved code) is deferred to ADR-005.

The reviewer output schema in ADR-003 includes a `patterns` field for future use:

```json
{
  "patterns": [
    {
      "name": "Context timeout in handlers",
      "pattern": "All HTTP handlers use context.WithTimeout",
      "applies_to": "handlers/*.go"
    }
  ]
}
```

Implementation details in ADR-005.

### Model Registry Extension (Optional)

The existing semspec model registry has `MaxTokens` per endpoint. For task sizing, optionally add:

```json
{
  "endpoints": {
    "claude-sonnet-4-20250514": {
      "provider": "anthropic",
      "model": "claude-sonnet-4-20250514",
      "max_tokens": 200000,
      "sweet_spot": 100000
    }
  }
}
```

The `sweet_spot` field indicates recommended working context for optimal performance. Task sizing can use this instead of `max_tokens * 0.4`.

**Note:** Complex ContextProfile struct and SNCO-level assignment are deferred.

## Consequences

### Positive

- **Mechanical failures caught early** - Gates before review (uses ADR-003 preset system)
- **Tasks fit in context** - Pre-execution sizing validation
- **Reviewer has context** - SOPs, conventions, decisions (via context assembler)
- **No wheel reinvention** - Uses semstreams runtime context management

### Negative

- **Graph queries** - Context assembly adds latency
- **Task decomposition** - May require user intervention

### Risks

| Risk | Mitigation |
|------|------------|
| SOP document rot | Include in sources sync |
| Task sizing too conservative | Tunable thresholds (40% default) |

## Files

### Phase 1 Files

| File | Purpose |
|------|---------|
| `workflow/context/assembler.go` | Graph-powered SOP/convention assembly |
| `workflow/context/budget.go` | Task sizing calculation |
| `commands/execute.go` | Task sizing validation before trigger |

### Phase 1 Modified Files

| File | Change |
|------|--------|
| `model/registry.go` | Optional `sweet_spot` field |
| `configs/semspec.json` | Model sweet_spot values |

### Deferred Files

| File | Deferred To |
|------|-------------|
| `processor/risk-monitor/` | Future ADR |
| `workflow/context/checkpoint.go` | Future ADR |
| `workflow/assignment.go` | Future ADR |
| `model/performance.go` | Future ADR |

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
