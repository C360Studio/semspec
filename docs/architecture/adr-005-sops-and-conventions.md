# ADR-005: SOPs and Conventions as Knowledge Sources

## Status

Proposed

## Context

The reviewer role (ADR-003) needs context to make judgments. Two key sources:

1. **SOPs** - Project standards written by humans (static, authoritative)
2. **Conventions** - Patterns learned from approved code (dynamic, accumulated)

### The Problem

Without SOPs and conventions, the reviewer operates blind:
- No project-specific standards to check against
- No memory of what patterns were previously approved
- Each review starts from zero

### Key Insight: Documents, Not Schemas

SOPs should be **natural language documents**, not mechanical rule schemas:

| Approach | Pros | Cons |
|----------|------|------|
| Rule schemas (regex, AST patterns) | Precise, automatable | Brittle, hard to maintain, no exceptions |
| Natural language docs | Flexible, human-readable, exceptions possible | Requires LLM judgment |

The reviewer is an LLM. It can read and reason about documents.

## Decision

### SOPs as Documents

SOPs live in `.semspec/sources/docs/sops/`:

```
.semspec/
└── sources/
    └── docs/
        └── sops/
            ├── error-handling.md
            ├── test-quality.md
            ├── naming-conventions.md
            └── security.md
```

### SOP Document Format

SOPs can be any document format (markdown, PDF, etc.). **Frontmatter is optional** — ADR-006's LLM-based analysis extracts metadata automatically.

Example SOP document:

```markdown
# Error Handling

All errors must include context about what operation failed.

## Requirements

1. Wrap errors with `fmt.Errorf("operation: %w", err)`
2. Include relevant identifiers (user ID, request ID, etc.)
3. Don't swallow errors - either handle or propagate

## Examples

Good:
```go
if err != nil {
    return fmt.Errorf("refresh token for user %s: %w", userID, err)
}
```

Bad:
```go
if err != nil {
    log.Println(err)  // Error swallowed, not propagated
}
```

## Exceptions

- In main() or goroutine entry points, logging and exiting is acceptable
- Test helpers may use t.Fatal() instead of error propagation
```

### Metadata Extraction (via ADR-006)

The LLM analyzer extracts:

| Field | Extracted From |
|-------|----------------|
| `category` | Document type inference → `sop` |
| `applies_to` | Content analysis → `*.go` |
| `severity` | Language analysis (MUST/SHOULD/MAY) → `error`/`warning`/`info` |
| `requirements` | Bullet-pointed key rules |

### Optional Frontmatter Override

For explicit control, frontmatter is still supported:

```yaml
---
category: sop
applies_to: "*.go"
severity: error
---
```

If frontmatter present → use it directly. If no frontmatter → LLM extraction.

### SOP Ingestion

SOPs are ingested via ADR-006's document ingestion pipeline:

1. **Upload**: Document added to `.semspec/sources/docs/`
2. **Analyze**: LLM extracts category, applies_to, severity, requirements
3. **Chunk**: Content split for search
4. **Publish**: Entities created in graph

See ADR-006 for full ingestion pipeline details.

**Entity format:**
```
Entity: source.doc.sops.{slug}
Triples:
  - source.doc.category = "sop"           # LLM extracted
  - source.doc.applies_to = "*.go"        # LLM extracted
  - source.doc.severity = "error"         # LLM extracted
  - source.doc.requirements = [...]       # LLM extracted
  - dc.terms.title = "Error Handling"
```

**Stable SOP IDs:**

SOP entities have deterministic IDs for reviewer reference:

| Document Path | Entity ID |
|---------------|-----------|
| `.semspec/sources/docs/sops/error-handling.md` | `source.doc.sops.error-handling` |
| `.semspec/sources/docs/sops/test-quality.md` | `source.doc.sops.test-quality` |
| `.semspec/sources/docs/sops/security.md` | `source.doc.sops.security` |

The slug is derived from the filename (without extension), sanitized to `[a-z0-9-]`.

### SOP Severity and Review Behavior

Severity determines how violations affect approval:

| Severity | Meaning | On Violation |
|----------|---------|--------------|
| `error` | Hard requirement | Blocks approval (reviewer must reject) |
| `warning` | Strong recommendation | Does not block, but must be noted |
| `info` | Best practice | Informational only |

**Severity extraction:** LLM analyzes document language:
- "MUST", "REQUIRED", "SHALL" → `error`
- "SHOULD", "RECOMMENDED" → `warning`
- "MAY", "OPTIONAL", "CONSIDER" → `info`

### SOP Coverage Requirements

When reviewer runs, the `validate_review` step checks coverage:

```
┌─────────────────────────────────────────┐
│ validate_review                          │
│                                          │
│ 1. Get expected SOPs from assemble_ctx   │
│ 2. Get sop_review array from reviewer    │
│ 3. Check: all expected SOPs in array?    │
│ 4. Check: all entries have evidence?     │
│ 5. Check: error-severity violations      │
│    → verdict must be "rejected"          │
│ 6. Check: confidence ≥ 0.7?              │
└──────────────────────────────────────────┘
```

**Coverage validation rules:**

| Check | On Failure |
|-------|------------|
| Missing SOP entry | Escalate: "SOP {id} not reviewed" |
| Empty evidence | Escalate: "SOP {id} has no evidence" |
| Error violation + approved | Reject: "Cannot approve with error-level violations" |
| Confidence < 0.7 | Escalate: "Low confidence review" |

This ensures the reviewer cannot:
- Skip checking applicable SOPs
- Claim "checked" without evidence
- Approve code that violates hard requirements

### SOP Query at Review Time

When reviewer receives a task, the context assembler:

1. Gets list of files modified by task
2. Queries SOPs where `applies_to` matches any modified file
3. Returns both SOP content (for prompt) and SOP IDs (for coverage validation)
4. Includes matching SOP content in reviewer context

### Context Assembler Architecture

The context assembler is a workflow step that queries the graph and builds reviewer context:

```
┌─────────────────────────────────────────────────────────────┐
│ Workflow Step: assemble_context                              │
│                                                              │
│ Input:                                                       │
│   - task_id                                                  │
│   - files_modified (from developer step)                     │
│   - budget_tokens (optional, default 8000)                   │
│                                                              │
│ Output:                                                      │
│   - sops: formatted SOP content for prompt                   │
│   - sop_ids: list of IDs for coverage validation             │
│   - conventions: formatted convention content                │
└─────────────────────────────────────────────────────────────┘
```

### Graph Query Patterns

The assembler uses GraphQL to query entities from the graph.

**Query SOP parent entities:**
```graphql
{
  entities(filter: { predicatePrefix: "source.doc.sops" }) {
    id
    triples { predicate object }
  }
}
```

This returns all entities with IDs starting with `source.doc.sops`. The assembler then filters out chunk entities (IDs containing `.chunk.`) to get only parent documents.

**Get chunks for a specific SOP:**
```graphql
{
  traverse(start: "source.doc.sops.error-handling", depth: 1, direction: INBOUND) {
    nodes {
      id
      triples { predicate object }
    }
    edges { source target predicate }
  }
}
```

Chunks have a `code.structure.belongs` relationship pointing to their parent. Using `INBOUND` traversal finds all entities that point to the parent.

### Parent + Chunks Entity Model

Large SOPs are split into parent (metadata) and chunk (content) entities:

```
Parent Entity: source.doc.sops.error-handling
├── source.doc.category = "sop"
├── source.doc.applies_to = "*.go"
├── source.doc.severity = "error"
├── source.doc.summary = "Error handling standards..."
├── source.doc.requirements = ["Wrap errors...", "Include context...", ...]
├── dc.terms.title = "Error Handling"
└── (NO full content - too large)

Chunk Entity: source.doc.sops.error-handling.chunk.1
├── source.doc.content = "## Requirements\n\n1. Wrap errors..."
├── source.doc.section = "Requirements"
└── code.structure.belongs = "source.doc.sops.error-handling"

Chunk Entity: source.doc.sops.error-handling.chunk.2
├── source.doc.content = "## Examples\n\nGood:\n```go..."
├── source.doc.section = "Examples"
└── code.structure.belongs = "source.doc.sops.error-handling"
```

**Benefits:**
- Parent entity is small and fast to query
- `requirements` array always fits in context (key points)
- Chunks can be selectively included based on budget
- Section info helps prioritize relevant chunks

### Context Budget Allocation (Equal, All-or-Nothing)

All applicable SOPs MUST fit in context with full detail. No truncation.

```
Total budget: 4000 tokens (reserved in task sizing, see ADR-004)
Matched SOPs: 3

Step 1: Calculate total SOP content needed
  - error-handling: ~800 tokens (metadata + all chunks)
  - test-quality: ~600 tokens
  - naming: ~400 tokens
  Total: 1800 tokens

Step 2: Check: Does total fit in budget?
  - 1800 < 4000 → OK, include all SOPs with full content
  - If total > budget → FAIL (task too large, trigger decomposition)
```

**All-or-nothing rule:**

| Condition | Action |
|-----------|--------|
| All SOPs fit | Include full content for all SOPs |
| Any SOP doesn't fit | Task is too large → decompose |
| Never | Truncate SOPs based on severity |

**Why NOT severity-based allocation:**
- Severity-based truncation signals "warnings matter less"
- Agents may deprioritize warning/info SOPs
- All SOPs should be evaluated equally
- Severity ONLY affects validation: error violations → hard reject

### Assembler Implementation

```go
type ContextAssembler struct {
    graphClient *GraphQLClient
}

type AssembleRequest struct {
    TaskID       string
    Files        []string
    BudgetTokens int
}

type AssembleResult struct {
    SOPs        string   // Formatted for prompt
    SOPIDs      []string // For coverage validation
    Conventions string   // Formatted for prompt
}

func (ca *ContextAssembler) Assemble(ctx context.Context, req AssembleRequest) (*AssembleResult, error) {
    // 1. Query SOP parent entities (fail fast on graph errors)
    sopParents, err := ca.querySOPParents(ctx)
    if err != nil {
        return nil, fmt.Errorf("query SOP parents: %w", err)
    }

    // 2. Filter by applies_to matching task files
    matchingSOPs := ca.filterByAppliesTo(sopParents, req.Files)

    // 3. For each matching SOP, get chunks (fail fast on graph errors)
    for i := range matchingSOPs {
        chunks, err := ca.getChunks(ctx, matchingSOPs[i].ID)
        if err != nil {
            return nil, fmt.Errorf("get chunks for %s: %w", matchingSOPs[i].ID, err)
        }
        matchingSOPs[i].Chunks = chunks
    }

    // 4. Query conventions (fail fast on graph errors)
    conventions, err := ca.queryConventions(ctx)
    if err != nil {
        return nil, fmt.Errorf("query conventions: %w", err)
    }
    matchingConventions := ca.filterByAppliesTo(conventions, req.Files)

    // 5. Check all-or-nothing SOP budget (4000 tokens reserved in ADR-004)
    budget := req.BudgetTokens
    if budget == 0 {
        budget = 4000 // default from ADR-004 sop_context_reserve
    }

    sopContent, sopIDs, sopTokens := ca.assembleAllSOPs(matchingSOPs)
    if sopTokens > budget {
        return nil, fmt.Errorf("SOP content (%d tokens) exceeds budget (%d tokens) - task too large, needs decomposition", sopTokens, budget)
    }

    // Conventions get remaining budget (optional, can be empty)
    remainingBudget := budget - sopTokens
    convContent := ca.assembleConventions(matchingConventions, remainingBudget)

    return &AssembleResult{
        SOPs:        sopContent,
        SOPIDs:      sopIDs,
        Conventions: convContent,
    }, nil
}

func (ca *ContextAssembler) querySOPParents(ctx context.Context) ([]SOP, error) {
    query := `{
        entities(filter: { predicatePrefix: "source.doc.sops" }) {
            id
            triples { predicate object }
        }
    }`

    result, err := ca.graphClient.Query(ctx, query)
    if err != nil {
        return nil, err
    }

    var sops []SOP
    for _, entity := range result.Entities {
        // Skip chunk entities
        if strings.Contains(entity.ID, ".chunk.") {
            continue
        }
        sops = append(sops, entityToSOP(entity))
    }
    return sops, nil
}

func (ca *ContextAssembler) getChunks(ctx context.Context, parentID string) ([]Chunk, error) {
    query := fmt.Sprintf(`{
        traverse(start: "%s", depth: 1, direction: INBOUND) {
            nodes { id triples { predicate object } }
            edges { source target predicate }
        }
    }`, parentID)

    result, err := ca.graphClient.Query(ctx, query)
    if err != nil {
        return nil, err
    }

    var chunks []Chunk
    for _, edge := range result.Edges {
        if edge.Predicate == "code.structure.belongs" {
            // edge.Source is the chunk pointing to parent
            for _, node := range result.Nodes {
                if node.ID == edge.Source {
                    chunks = append(chunks, entityToChunk(node))
                }
            }
        }
    }
    return chunks, nil
}

// Glob matching semantics:
// - Case-insensitive on macOS/Windows, case-sensitive on Linux
// - No symlink following (matches path as given)
// - * matches any sequence within a path segment
// - ** (doublestar) matches zero or more path segments
// - ? matches any single character
// - Examples:
//   - "*.go" matches "main.go" but not "handlers/main.go"
//   - "**/*.go" matches "handlers/auth/middleware.go"
//   - "handlers/*.go" matches "handlers/main.go" but not "handlers/auth/main.go"

func (ca *ContextAssembler) filterByAppliesTo(sops []SOP, files []string) []SOP {
    var matched []SOP
    for _, sop := range sops {
        for _, pattern := range sop.AppliesTo {
            for _, file := range files {
                if glob.Match(pattern, file) {
                    matched = append(matched, sop)
                    goto nextSOP
                }
            }
        }
    nextSOP:
    }
    return matched
}
```

### SOP Content Assembly

The assembler includes ALL SOP content (all-or-nothing rule):

```go
// assembleAllSOPs includes full content for all matching SOPs.
// Returns formatted content, IDs, and total token count.
// Never truncates - caller must check if result fits in budget.
func (ca *ContextAssembler) assembleAllSOPs(sops []SOP) (string, []string, int) {
    var content strings.Builder
    var ids []string
    totalTokens := 0

    for _, sop := range sops {
        ids = append(ids, sop.ID)

        // Include header, summary, and requirements
        header := fmt.Sprintf("### %s (severity: %s)\n\n**Summary:** %s\n\n**Requirements:**\n",
            sop.Title, sop.Severity, sop.Summary)
        for _, req := range sop.Requirements {
            header += fmt.Sprintf("- %s\n", req)
        }
        header += "\n"
        content.WriteString(header)
        totalTokens += countTokens(header)

        // Include ALL chunks (no truncation)
        for _, chunk := range sop.Chunks {
            chunkContent := fmt.Sprintf("#### %s\n%s\n\n", chunk.Section, chunk.Content)
            content.WriteString(chunkContent)
            totalTokens += countTokens(chunkContent)
        }

        content.WriteString("---\n\n") // Separator between SOPs
    }

    return content.String(), ids, totalTokens
}

// countTokens estimates tokens using 4 chars/token heuristic (same as semstreams)
func countTokens(s string) int {
    return (len(s) + 3) / 4 // Round up
}
```

**Output format in reviewer prompt:**

```markdown
## Applicable SOPs

### Error Handling (severity: error)

**Summary:** Standards for error handling in Go code.

**Requirements:**
- Wrap errors with fmt.Errorf and %w
- Include context identifiers in error messages
- Never swallow errors - handle or propagate

#### Requirements
1. Wrap errors with `fmt.Errorf("operation: %w", err)`
2. Include relevant identifiers (user ID, request ID, etc.)
...

#### Examples
Good:
```go
if err != nil {
    return fmt.Errorf("refresh token for user %s: %w", userID, err)
}
```
...

---

### Test Quality (severity: warning)
...
```

### Conventions: The Learning Flywheel

Conventions are patterns **learned from approved code**. Unlike SOPs (human-authored, static), conventions accumulate over time.

```
┌─────────────┐
│  Developer  │
│  writes     │
│  code       │
└──────┬──────┘
       │
       ▼
┌─────────────┐     reject + feedback
│  Reviewer   │◄────────────────────┐
│  evaluates  │                     │
└──────┬──────┘                     │
       │ approve                    │
       ▼                            │
┌─────────────┐                     │
│  Extract    │                     │
│  patterns   │                     │
└──────┬──────┘                     │
       │
       ▼
┌─────────────┐
│  Convention │
│  stored in  │
│  graph      │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  Future     │
│  reviews    │◄─────── informs
│  see it     │
└─────────────┘
```

### Convention Entity Structure

```
Entity: semspec.convention.{project}.{id}
Triples:
  - semspec.convention.name = "Error wrapping with context"
  - semspec.convention.pattern = "Use fmt.Errorf with %w for error wrapping"
  - semspec.convention.applies_to = "*.go"
  - semspec.convention.source_task = "task-123"
  - semspec.convention.approved_by = "reviewer-model-xyz"
  - prov.generated_at = "2024-01-15T10:30:00Z"
```

### How Conventions Are Created

**Option A: Explicit Reviewer Output**

Reviewer prompt includes instruction to note patterns:

```markdown
## If Approving

When you approve, note any patterns worth remembering:

```json
{
  "verdict": "approved",
  "patterns": [
    {
      "name": "Context timeout in handlers",
      "pattern": "All HTTP handlers use context.WithTimeout",
      "applies_to": "handlers/*.go"
    }
  ]
}
```
```

**Option B: Post-Approval Analysis**

Separate pass analyzes approved code for patterns. More complex, deferred.

**Decision: Option A** - Explicit reviewer output is simpler and more transparent.

### Convention Query at Review Time

Similar to SOPs, conventions matching task files are included in reviewer context:

```go
func (a *Assembler) GetConventionsForFiles(ctx context.Context, files []string) ([]Convention, error) {
    entities, err := a.graph.QueryByPrefix(ctx, "semspec.convention.")
    if err != nil {
        return nil, err
    }

    var conventions []Convention
    for _, entity := range entities {
        appliesTo := entity.GetString("semspec.convention.applies_to")
        for _, file := range files {
            if glob.Match(appliesTo, file) {
                conventions = append(conventions, entityToConvention(entity))
                break
            }
        }
    }

    return conventions, nil
}
```

### Priority: SOPs Over Conventions

When assembling reviewer context with limited budget:

| Priority | Source | Reason |
|----------|--------|--------|
| 1 | SOPs | Human-authored, authoritative |
| 2 | Conventions | Learned, supplementary |

If budget is tight, conventions are truncated/dropped first.

### Convention Decay

Conventions that are repeatedly violated in approved code should decay:

```go
// Future enhancement: track convention violations
if approved && violatesConvention(task, convention) {
    convention.ViolationCount++
    if convention.ViolationCount > threshold {
        // Mark as stale or delete
    }
}
```

**Deferred** - Not in initial implementation.

## Consequences

### Positive

- **Human-readable standards** - SOPs are markdown, not code
- **Flexible matching** - Glob patterns, not exact paths
- **Exceptions possible** - Reviewer can reason about edge cases
- **Learning accumulates** - Good patterns become conventions
- **Transparent** - All SOPs and conventions visible in graph

### Negative

- **Requires ADR-006** - Document ingestion dependency
- **Glob matching overhead** - Every review queries SOPs/conventions
- **Convention noise** - May accumulate low-value patterns

### Risks

| Risk | Mitigation |
|------|------------|
| SOP document rot | Include in sources sync, flag stale |
| Convention explosion | Cap per-project, require minimum severity |
| Conflicting SOP/convention | SOPs always win (higher authority) |

## Files

### New Files

| File | Purpose |
|------|---------|
| `.semspec/sources/docs/sops/*.md` | SOP documents (user-created) |
| `workflow/sop/parser.go` | Parse SOP frontmatter and content |
| `workflow/sop/matcher.go` | Match SOPs to files via glob |
| `workflow/sop/coverage.go` | Validate reviewer SOP coverage |
| `workflow/convention/extractor.go` | Extract patterns from reviewer output |
| `workflow/convention/store.go` | Store conventions in graph |

### Modified Files

| File | Change |
|------|--------|
| `workflow/context/assembler.go` | Query SOPs and conventions, return IDs for coverage |
| `workflow/prompts/reviewer.go` | Include sop_review array in output format |
| `processor/review-validator/` | Validate reviewer output against SOP coverage rules |

## Vocabulary Support

The source vocabulary package (`vocabulary/source/`) provides predicates for SOP and document entities.

### Predicates Used by SOPs

| Predicate | Description | Data Type |
|-----------|-------------|-----------|
| `source.doc.category` | Document classification (sop, spec, etc.) | string |
| `source.doc.applies_to` | File patterns this doc applies to | array |
| `source.doc.severity` | Violation severity (error, warning, info) | string |
| `source.doc.summary` | LLM-extracted summary | string |
| `source.doc.requirements` | Extracted key requirements | array |
| `source.doc.content` | Chunk text content | string |
| `source.doc.section` | Section heading for chunk | string |
| `source.doc.chunk_index` | Chunk sequence number | int |
| `source.doc.chunk_count` | Total chunks in document | int |

### IRI Mappings

Key predicates map to standard ontologies:

| Predicate | Standard IRI |
|-----------|--------------|
| `source.doc.category` | `dc:type` |
| `source.doc.summary` | `dc:abstract` |
| `source.name` | `dc:title` |
| `source.added_by` | `prov:wasAttributedTo` |
| `source.added_at` | `prov:generatedAtTime` |

Parent-chunk relationships use `code.structure.belongs` which maps to BFO `part_of`.

### Using the Vocabulary

```go
import (
    "github.com/c360studio/semspec/vocabulary/source"
    "github.com/c360studio/semstreams/message"
)

// Build SOP parent entity triples
triples := []message.Triple{
    {Subject: sopID, Predicate: source.DocCategory, Object: string(source.DocCategorySOP)},
    {Subject: sopID, Predicate: source.DocAppliesTo, Object: []string{"*.go"}},
    {Subject: sopID, Predicate: source.DocSeverity, Object: string(source.DocSeverityError)},
    {Subject: sopID, Predicate: source.DocSummary, Object: extractedSummary},
    {Subject: sopID, Predicate: source.DocRequirements, Object: requirements},
}

// Build chunk entity triples
chunkTriples := []message.Triple{
    {Subject: chunkID, Predicate: source.DocContent, Object: chunkText},
    {Subject: chunkID, Predicate: source.DocSection, Object: "Requirements"},
    {Subject: chunkID, Predicate: source.DocChunkIndex, Object: 1},
    {Subject: chunkID, Predicate: semspec.CodeBelongsTo, Object: sopID},
}
```

## Dependencies

- **ADR-006**: Document ingestion infrastructure with LLM extraction
- **ADR-003**: Reviewer role definition
- **ADR-004**: Context assembly framework

## Open Questions

1. **Convention confidence**: Should conventions have a confidence score based on how many times they've been reinforced?

2. **Convention scope**: Project-wide only, or can conventions be scoped to directories?

3. **SOP inheritance**: Can SOPs inherit from other SOPs (e.g., base Go SOP + project-specific)?

## Related

- ADR-003: Pipeline Simplification and Adversarial Roles
- ADR-004: Validation Layers and Context Management
- ADR-006: Sources and Knowledge Ingestion
- `docs/spec/semspec-sources-knowledge-spec.md`
