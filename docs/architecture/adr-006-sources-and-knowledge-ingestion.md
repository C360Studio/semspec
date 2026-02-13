# ADR-006: Sources and Knowledge Ingestion

## Status

Proposed (depends on ADR-005)

## Context

Semspec agents need context from multiple sources:
- **Repositories** - Codebase structure, patterns, existing implementations
- **Documents** - SOPs, specs, datasheets, API docs
- **SOPs** - Project standards that inform reviewer judgment

Currently:
- Repositories require manual JSON configuration
- No mechanism for document ingestion
- No way to add sources through UI
- SOPs would require frontmatter (limiting for existing PDFs)

### The Frontmatter Problem

ADR-005 proposed SOP frontmatter:
```markdown
---
category: sop
applies_to: "*.go"
severity: error
---
```

But most organizations have:
- Existing PDFs without metadata
- Documents they can't/won't modify
- Standards spread across multiple formats

**Requiring frontmatter is a non-starter.**

## Decision

### LLM-Based Document Analysis

Documents are analyzed by an LLM to extract metadata:

```
Document uploaded
       │
       ▼
┌─────────────────────────────────┐
│  LLM Document Analyzer          │
│                                 │
│  Prompts:                       │
│  - What type of document?       │
│  - What code does it apply to?  │
│  - What's the severity?         │
│  - Extract key requirements     │
└─────────────────────────────────┘
       │
       ▼
Entity with extracted metadata
```

**No frontmatter required.** The LLM infers:

| Field | LLM Extracts From |
|-------|-------------------|
| `category` | "This appears to be a coding standard" → `sop` |
| `applies_to` | "Discusses Go error handling" → `*.go` |
| `severity` | "Uses MUST, REQUIRED language" → `error` |
| `requirements` | Bullet-pointed key rules |

### Document Analysis Prompt

```markdown
Analyze this document and extract:

1. **Category**: What type of document is this?
   - sop: Coding standard, style guide, best practice
   - spec: Technical specification, API doc
   - datasheet: Hardware/sensor documentation
   - reference: General reference material

2. **Applies To**: What files/code does this apply to?
   - File patterns: *.go, auth/*, database/*
   - Languages: Go, TypeScript, SQL
   - Components: API handlers, database layer

3. **Severity** (for SOPs only):
   - error: MUST/REQUIRED - violations are blocking
   - warning: SHOULD - violations should be fixed
   - info: MAY/CONSIDER - best practice suggestions

4. **Requirements**: Extract key rules/requirements as a list.

Output JSON:
{
  "category": "sop",
  "applies_to": ["*.go"],
  "severity": "error",
  "summary": "Error handling standards for Go code",
  "requirements": [
    "Wrap errors with fmt.Errorf and %w",
    "Include context identifiers in error messages",
    "Never swallow errors - handle or propagate"
  ]
}
```

### Source Types

From `sources-knowledge-spec.md`:

**Repository Sources:**
```go
type RepoSource struct {
    Source
    URL           string   `json:"url"`
    Branch        string   `json:"branch"`
    LocalPath     string   `json:"local_path"`
    Languages     []string `json:"languages"`
    AutoPull      bool     `json:"auto_pull"`
    PullInterval  string   `json:"pull_interval"`
}
```

**Document Sources:**
```go
type DocumentSource struct {
    Source
    Filename      string `json:"filename"`
    MimeType      string `json:"mime_type"`
    StoragePath   string `json:"storage_path"`

    // LLM-extracted metadata
    Category      string   `json:"category"`       // sop, spec, datasheet, reference
    AppliesTo     []string `json:"applies_to"`     // File patterns
    Severity      string   `json:"severity"`       // error, warning, info
    Summary       string   `json:"summary"`        // One-line description
    Requirements  []string `json:"requirements"`   // Extracted key points
}
```

### Ingestion Pipeline

```
1. Document uploaded → stored in .semspec/sources/docs/

2. Parse based on mime type:
   - PDF: Extract text via pdftotext
   - Markdown: Parse structure
   - Other: Extract raw text

3. LLM analysis:
   - Send first ~4K tokens to analyzer
   - Extract category, applies_to, severity, requirements
   - Store metadata in document entity

4. Chunk for search:
   - Split document into chunks (see Chunking Strategy below)
   - Each chunk references parent via code.structure.belongs

5. Publish to graph:
   - Parent entity with metadata (no full content)
   - Chunk entities with content sections
```

### Chunking Strategy

Documents are split into chunks for efficient context assembly:

**Chunk size targets:**
- Target: ~1000 tokens per chunk
- Maximum: 1500 tokens (hard limit)
- Minimum: 200 tokens (avoid tiny fragments)

**Split boundaries (in priority order):**
1. **Section headers** (## or ###) - preferred, preserves document structure
2. **Paragraph breaks** (double newline) - natural semantic boundaries
3. **Sentence boundaries** - last resort for very long paragraphs

**Code block handling:**
- Never split inside a code block
- If a code block exceeds max chunk size, it becomes its own chunk
- Code blocks stay with their preceding explanation when possible

**Chunking algorithm:**
```
1. Parse document into sections (by ## headings)
2. For each section:
   a. If section < max_tokens: one chunk with section name
   b. If section > max_tokens: split at paragraphs, preserving code blocks
3. Assign sequential chunk numbers: .chunk.1, .chunk.2, etc.
4. Each chunk gets:
   - source.doc.content: the chunk text
   - source.doc.section: the section heading (e.g., "Requirements")
   - code.structure.belongs: parent entity ID
```

**Example chunking:**
```
Document: error-handling.md (3500 tokens total)
├── Section: Requirements (800 tokens) → chunk.1
├── Section: Examples (1200 tokens) → chunk.2
├── Section: Exceptions (400 tokens) → chunk.3
└── Section: Reference (1100 tokens) → chunk.4
```

### Entity Format

Documents use a **parent + chunks** model. The parent entity holds metadata (small, always queryable), while chunk entities hold content (loaded on demand for context assembly).

**Parent entity (metadata only):**
```
Entity ID: source.doc.sops.{slug}
Triples:
  - source.doc.type = "document"
  - source.doc.category = "sop"           # LLM extracted
  - source.doc.applies_to = ["*.go"]      # LLM extracted (array)
  - source.doc.severity = "error"         # LLM extracted
  - source.doc.summary = "Error handling standards for Go code"
  - source.doc.requirements = ["Wrap errors...", "Include context...", ...]
  - dc.terms.title = "Error Handling"
  - source.doc.chunk_count = 4            # Number of chunks
```

**Note:** Parent entity does NOT contain full document content. The `summary` and `requirements` fields are short extracts that always fit in context.

**Chunk entities (content):**
```
Entity ID: source.doc.sops.{slug}.chunk.1
Triples:
  - source.doc.content = "## Requirements\n\n1. Wrap errors..."
  - source.doc.section = "Requirements"
  - source.doc.chunk_index = 1
  - code.structure.belongs = "source.doc.sops.{slug}"

Entity ID: source.doc.sops.{slug}.chunk.2
Triples:
  - source.doc.content = "## Examples\n\nGood:\n```go..."
  - source.doc.section = "Examples"
  - source.doc.chunk_index = 2
  - code.structure.belongs = "source.doc.sops.{slug}"
```

**Relationship predicate:** `code.structure.belongs`
- Direction: chunk → parent (chunk points to its parent)
- Used for traversal: query parent, then INBOUND traverse to find chunks
- Consistent with code entity patterns (functions belong to packages)

### Querying SOPs for Reviewer Context

The context assembler queries SOPs matching task files:

```go
func (a *Assembler) GetSOPsForFiles(ctx context.Context, files []string) ([]SOP, error) {
    // Query documents where category = "sop"
    docs, err := a.graph.Query(ctx, QueryFilter{
        Predicates: []Predicate{
            {Key: "source.doc.category", Equals: "sop"},
        },
    })

    // Filter by applies_to matching any task file
    var sops []SOP
    for _, doc := range docs {
        appliesTo := doc.GetStringSlice("source.doc.applies_to")
        for _, pattern := range appliesTo {
            for _, file := range files {
                if glob.Match(pattern, file) {
                    sops = append(sops, docToSOP(doc))
                    break
                }
            }
        }
    }

    return sops, nil
}
```

### Reviewer Context Assembly

SOPs are assembled into reviewer prompt:

```markdown
## Applicable SOPs

### Error Handling (severity: error)
Applies to: *.go

**Requirements:**
- Wrap errors with fmt.Errorf and %w
- Include context identifiers in error messages
- Never swallow errors - handle or propagate

---

## Implementation to Review

[developer output here]
```

### Optional: Frontmatter Override

For users who want explicit control, frontmatter is still supported:

```markdown
---
category: sop
applies_to: "*.go"
severity: error
---

# Error Handling

...content...
```

If frontmatter present → use it directly
If no frontmatter → LLM extraction

### Integration with ADR-003 Workflow

The reviewer step includes SOP context:

```json
{
  "name": "reviewer",
  "action": {
    "type": "publish_agent",
    "subject": "agent.task.review",
    "role": "reviewer",
    "prompt": "Review implementation.\n\n${assembled_sop_context}\n\nImplementation:\n${steps.developer.output.result}"
  }
}
```

Where `assembled_sop_context` is built by the context assembler before the workflow step.

### Repository Ingestion Pipeline

While documents use LLM-based analysis, repository sources follow a different ingestion path leveraging the existing AST indexer infrastructure.

**AddRepo Flow:**

```
POST /api/sources/repos (or CLI: semspec source add <url>)
         │
         ▼
┌─────────────────────────────────┐
│ 1. Validate                     │
│    - URL reachable?             │
│    - Branch exists?             │
│    - Language supported?        │
└────────────────┬────────────────┘
                 │
                 ▼
┌─────────────────────────────────┐
│ 2. Clone                        │
│    git clone --branch {branch}  │
│    --depth 1 {url}              │
│    .semspec/sources/repos/{slug}│
└────────────────┬────────────────┘
                 │
                 ▼
┌─────────────────────────────────┐
│ 3. Register                     │
│    Write to sources.json        │
│    Status: "pending"            │
└────────────────┬────────────────┘
                 │
                 ▼
┌─────────────────────────────────┐
│ 4. Configure AST Indexer        │
│    NATS: ast-indexer.config.add-path
│    Payload: WatchPathConfig     │
└────────────────┬────────────────┘
                 │
                 ▼
┌─────────────────────────────────┐
│ 5. Index                        │
│    Status: "indexing"           │
│    Parse files → extract entities
│    Publish to graph.ingest.entity
└────────────────┬────────────────┘
                 │
                 ▼
┌─────────────────────────────────┐
│ 6. Complete                     │
│    Status: "ready"              │
│    Update entity_count          │
│    Publish source entity to graph
└─────────────────────────────────┘
```

**Dynamic AST Indexer Reconfiguration:**

The AST indexer accepts new watch paths at runtime via NATS request-reply:

```
Subject: ast-indexer.config.add-path
Payload: {
  "root": ".semspec/sources/repos/osh-core",
  "org": "opensensorhub",
  "project": "osh-core",
  "languages": ["java"],
  "exclude_patterns": ["**/test/**", "**/build/**"]
}
```

This allows adding repositories without restarting semspec.

**Repository Entity Format:**

```
Entity ID: source.repo.{slug}
Triples:
  - source.type = "repository"
  - source.name = "Open Sensor Hub Core"
  - source.repo.url = "https://github.com/opensensorhub/osh-core"
  - source.repo.branch = "master"
  - source.repo.languages = ["java"]
  - source.repo.status = "ready"
  - source.repo.entity_count = 847
  - source.repo.last_indexed = "2026-02-13T10:00:00Z"
  - source.repo.last_commit = "abc123..."
```

See `docs/spec/semspec-sources-knowledge-spec.md` for full API and UI specifications.

### Language Support

The AST indexer supports multiple languages through a pluggable parser registry.

**Currently Supported:**

| Language | Parser | File Extensions |
|----------|--------|-----------------|
| Go | Native Go AST | `.go` |
| TypeScript | Regex-based | `.ts`, `.tsx` |
| JavaScript | Regex-based | `.js`, `.jsx` |

**Parser Registry Pattern:**

Languages are registered via `ast.DefaultRegistry.Register()`:

```go
// In processor/ast/ts/parser.go
func init() {
    ast.DefaultRegistry.Register("typescript",
        ast.WithExtensions(".ts", ".tsx"),
        ast.WithFactory(func() ast.FileParser { return NewParser() }))

    ast.DefaultRegistry.Register("javascript",
        ast.WithExtensions(".js", ".jsx"),
        ast.WithFactory(func() ast.FileParser { return NewParser() }))
}
```

**Adding a New Language:**

1. Create parser package: `processor/ast/{lang}/parser.go`
2. Implement `ast.FileParser` interface:
   ```go
   type FileParser interface {
       ParseFile(ctx context.Context, path string) (*ParseResult, error)
       ParseDirectory(ctx context.Context, dir string) ([]*ParseResult, error)
   }
   ```
3. Register in `init()` with extensions
4. Import package in `processor/ast-indexer/component.go`

**Planned: Java Support**

Java is needed for the OpenSensorHub use case. Implementation approach:

| Approach | Pros | Cons |
|----------|------|------|
| Tree-sitter | Fast, battle-tested, multi-language | Requires CGO, complex setup |
| JavaParser (via subprocess) | Pure Java, mature | External dependency, IPC overhead |
| Regex-based (like TS) | Simple, no dependencies | Limited accuracy for complex syntax |

**Recommendation:** Tree-sitter via `go-tree-sitter` for production accuracy. Initially regex-based for prototype.

**Note:** Java support is implementation work, not an architectural decision. This ADR documents the extensibility mechanism; language additions are incremental.

### Source Coordination

For projects with multiple related sources (e.g., OSH core + drivers + docs), sources can be coordinated via project tagging.

**Project Tagging:**

```
Entity ID: source.repo.osh-core
Triples:
  - source.project = "opensensorhub"  # Project tag
  - source.type = "repository"
  ...

Entity ID: source.repo.osh-drivers
Triples:
  - source.project = "opensensorhub"  # Same project
  - source.type = "repository"
  ...

Entity ID: source.doc.sensorml-spec
Triples:
  - source.project = "opensensorhub"  # Same project
  - source.type = "document"
  ...
```

**Benefits:**

- Context assembler can query sources by project
- UI can group related sources
- Entity prefixes for the project can be consistent

**Context Assembly by Project:**

```go
// Query all sources for a project
sources, _ := graph.Query(ctx, QueryFilter{
    Predicates: []Predicate{
        {Key: "source.project", Equals: "opensensorhub"},
    },
})
```

**Future:** Source dependencies (e.g., "osh-drivers depends on osh-core") for build ordering.

### Ingestion Triggers

What initiates source ingestion?

**Repository Sources:**

| Trigger | Mechanism |
|---------|-----------|
| Manual add | `POST /api/sources/repos` or CLI `semspec source add <url>` |
| Auto-pull | Scheduled git pull at `pull_interval` (e.g., "5m") |
| Manual pull | `POST /api/sources/repos/{id}/pull` |
| Re-index | `POST /api/sources/repos/{id}/reindex` |

**Document Sources:**

| Trigger | Mechanism |
|---------|-----------|
| Upload | `POST /api/sources/docs` (multipart) |
| URL fetch | `POST /api/sources/docs/url` with source URL |
| Re-index | `POST /api/sources/docs/{id}/reindex` |

**Re-Analysis Triggers:**

Documents are re-analyzed when:
- File content changes (detected via hash comparison)
- User explicitly requests re-index
- LLM model changes (optional: re-analyze for improved extraction)

**File Watcher (Future):**

For development workflow, watch `.semspec/sources/` for changes:
- New file in `docs/` → auto-ingest document
- Git pull in `repos/` → auto-reindex repository

## Consequences

### Positive

- **No frontmatter required** - Works with existing PDFs
- **Smart extraction** - LLM infers scope from content
- **Unified pipeline** - Same ingestion for all document types
- **Queryable SOPs** - Graph-based lookup by file pattern
- **Flexible override** - Frontmatter still works if preferred
- **Dynamic repo addition** - No restart required to add repositories
- **Extensible language support** - Plugin architecture for new languages
- **Project coordination** - Related sources can be grouped

### Negative

- **LLM cost** - Each document needs analysis call
- **Extraction accuracy** - LLM may misclassify documents
- **Latency** - Analysis adds ingestion time
- **Java not yet supported** - Required for OpenSensorHub use case

### Mitigations

| Risk | Mitigation |
|------|------------|
| LLM misclassification | Show extracted metadata in UI, allow user correction |
| Extraction cost | Cache results, only re-analyze on document change |
| Slow ingestion | Analysis is async, show progress in UI |
| Missing Java support | Extensible parser registry allows incremental addition |

### Implementation Phases

From `semspec-sources-knowledge-spec.md`:

1. **Phase 1:** Source Manager + Repo Support (backend)
2. **Phase 2:** Document Support (backend)
3. **Phase 3:** Sources UI
4. **Phase 4:** Polish + auto-pull + language detection

## Files

### New Files

| File | Purpose |
|------|---------|
| `source/manager.go` | Source lifecycle management |
| `source/types.go` | Source, RepoSource, DocumentSource |
| `source/analyzer.go` | LLM-based document analysis |
| `source/parser/` | Parsers for PDF, markdown, etc. |
| `source/chunker/` | Document chunking (section-aware, code-block preserving) |
| `source/chunker/markdown.go` | Markdown-specific chunking with ## boundaries |
| `source/chunker/pdf.go` | PDF chunking with page/section awareness |
| `processor/source-ingester/` | Component for async ingestion |

### Modified Files

| File | Change |
|------|--------|
| `processor/ast-indexer/` | Dynamic watch path reconfiguration |
| `workflow/context/assembler.go` | Query SOPs for reviewer context |

## Vocabulary Support

The source vocabulary package (`vocabulary/source/`) provides predicates for all source entities.

### Predicate Categories

**Document predicates (`source.doc.*`):**
- `source.doc.type` - Document type identifier
- `source.doc.category` - Classification (sop, spec, datasheet, reference, api)
- `source.doc.applies_to` - File patterns (array of globs)
- `source.doc.severity` - Violation severity (error, warning, info)
- `source.doc.summary` - LLM-extracted summary
- `source.doc.requirements` - Extracted key requirements (array)
- `source.doc.content` - Chunk text content
- `source.doc.section` - Section heading for chunk
- `source.doc.chunk_index` - Chunk sequence number (1-indexed)
- `source.doc.chunk_count` - Total chunks in document
- `source.doc.mime_type` - Document MIME type
- `source.doc.file_path` - Original file path
- `source.doc.file_hash` - Content hash for staleness

**Repository predicates (`source.repo.*`):**
- `source.repo.type` - Repository type identifier
- `source.repo.url` - Git clone URL
- `source.repo.branch` - Branch name
- `source.repo.status` - Indexing status
- `source.repo.languages` - Detected languages (array)
- `source.repo.entity_count` - Number of indexed entities
- `source.repo.last_indexed` - Last indexing timestamp
- `source.repo.auto_pull` - Whether to auto-pull
- `source.repo.pull_interval` - Auto-pull interval
- `source.repo.last_commit` - Last indexed commit SHA

**Generic predicates (`source.*`):**
- `source.type` - Type discriminator (repository/document)
- `source.name` - Display name
- `source.status` - Overall status
- `source.project` - Project tag for grouping related sources
- `source.added_by` - User/agent who added
- `source.added_at` - Addition timestamp
- `source.error` - Error message if failed

### Type-Safe Enums

```go
import "github.com/c360studio/semspec/vocabulary/source"

// Document categories
source.DocCategorySOP       // "sop"
source.DocCategorySpec      // "spec"
source.DocCategoryDatasheet // "datasheet"
source.DocCategoryReference // "reference"
source.DocCategoryAPI       // "api"

// Severity levels
source.DocSeverityError   // "error" - blocks approval
source.DocSeverityWarning // "warning" - reviewer discretion
source.DocSeverityInfo    // "info" - informational only

// Source status
source.SourceStatusPending  // "pending"
source.SourceStatusIndexing // "indexing"
source.SourceStatusReady    // "ready"
source.SourceStatusError    // "error"
source.SourceStatusStale    // "stale"

// Source types
source.SourceTypeRepository // "repository"
source.SourceTypeDocument   // "document"
```

### IRI Mappings

Predicates map to standard ontologies for RDF export:

| Predicate | Standard IRI | Ontology |
|-----------|--------------|----------|
| `source.doc.category` | `dc:type` | Dublin Core |
| `source.doc.summary` | `dc:abstract` | Dublin Core |
| `source.doc.mime_type` | `dc:format` | Dublin Core |
| `source.name` | `dc:title` | Dublin Core |
| `source.added_by` | `prov:wasAttributedTo` | PROV-O |
| `source.added_at` | `prov:generatedAtTime` | PROV-O |
| `code.structure.belongs` | `bfo:part_of` | BFO |

## Dependencies

- LLM endpoint for document analysis
- PDF text extraction (pdftotext or Go library)
- Graph query capabilities
- Source vocabulary package (`vocabulary/source/`)

## Related

- ADR-003: Pipeline Simplification and Adversarial Roles (uses SOPs in reviewer)
- ADR-004: Validation Layers and Context Management (context budget)
- ADR-005: SOPs and Conventions (SOP patterns, now using LLM extraction)
- `docs/spec/semspec-sources-knowledge-spec.md` (full specification)
