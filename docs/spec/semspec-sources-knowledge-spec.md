# SemSpec Sources & Knowledge Management

**Status**: Spec  
**Slug**: sources-knowledge-management  
**Date**: 2025-02-11  
**Priority**: High  
**Companion to**: `ui-project-management-redesign`

---

## Problem Statement

SemSpec currently requires manual JSON configuration to add repositories and has no mechanism for adding reference documents. The AST indexer's `watch_paths` are defined statically in `semspec.json` or per-environment config files (`configs/semspec.json`, `configs/e2e.json`, etc.). There's no way to:

1. Add a new repository through the UI
2. Upload or link reference documents (API specs, datasheets, protocol docs)
3. See what knowledge sources SemSpec has access to and their indexing status
4. Remove or reconfigure sources without editing JSON and restarting

This is a blocker for customer adoption. The Open Sensor Hub (OSH) use case requires adding external repos (OSH core, driver repos) and reference documents (sensor datasheets, protocol specs, hardware documentation) so agents have the context they need to generate drivers. A developer shouldn't need to hand-edit config files to get their project into SemSpec.

---

## Design Principles

1. **Sources are first-class entities** — Repos and docs live in the knowledge graph, not just in config files
2. **Indexing is observable** — Users see what's indexed, how many entities were extracted, when it last ran, and if anything failed
3. **Git-native for repos** — Clone via URL, track branches, pull on interval or on-demand
4. **Document-type aware** — Different doc types get different ingestion strategies (markdown parsed for structure, PDFs extracted for text, API specs parsed for endpoints)
5. **Incremental** — Adding a source doesn't require restart; the system picks it up dynamically

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│  UI: /sources                                                │
│  ┌──────────────┐  ┌──────────────┐                         │
│  │ Repositories  │  │  Documents   │                         │
│  └──────┬───────┘  └──────┬───────┘                         │
│         │                  │                                  │
│         ▼                  ▼                                  │
│  POST /api/sources/repos   POST /api/sources/docs            │
│         │                  │                                  │
├─────────┼──────────────────┼─────────────────────────────────┤
│         │   SERVICE MANAGER (Go)                              │
│         ▼                  ▼                                  │
│  ┌──────────────┐  ┌──────────────┐                         │
│  │ Source        │  │ Source        │                         │
│  │ Manager      │  │ Manager      │                         │
│  │ (git clone,  │  │ (file store, │                         │
│  │  watch_path  │  │  parse,      │                         │
│  │  config)     │  │  chunk)      │                         │
│  └──────┬───────┘  └──────┬───────┘                         │
│         │                  │                                  │
│         ▼                  ▼                                  │
│  ┌─────────────────────────────────┐                         │
│  │  graph.ingest.entity            │  (existing pipeline)    │
│  │  → graph-ingest → graph-index   │                         │
│  │  → ENTITY_STATES KV             │                         │
│  └─────────────────────────────────┘                         │
│         │                                                     │
│         ▼                                                     │
│  AST indexer reconfigured          Document entities in graph │
│  with new watch_paths              queryable by agents        │
└─────────────────────────────────────────────────────────────┘
```

---

## Data Model

### Source (base type)

All sources share common metadata, stored in a new `SOURCES` KV bucket and published as graph entities.

```go
// source/types.go
package source

type SourceType string

const (
    SourceTypeRepo     SourceType = "repository"
    SourceTypeDocument SourceType = "document"
)

type SourceStatus string

const (
    SourceStatusPending  SourceStatus = "pending"   // Added, not yet indexed
    SourceStatusIndexing SourceStatus = "indexing"   // Currently being indexed
    SourceStatusReady    SourceStatus = "ready"      // Indexed and available
    SourceStatusError    SourceStatus = "error"      // Indexing failed
    SourceStatusStale    SourceStatus = "stale"      // Needs re-index
)

type Source struct {
    ID          string       `json:"id"`           // e.g., "source.repo.open-sensor-hub"
    Name        string       `json:"name"`         // Display name
    Type        SourceType   `json:"type"`
    Status      SourceStatus `json:"status"`
    AddedBy     string       `json:"added_by"`
    AddedAt     time.Time    `json:"added_at"`
    LastIndexed *time.Time   `json:"last_indexed,omitempty"`
    EntityCount int          `json:"entity_count"` // Entities in graph from this source
    Error       string       `json:"error,omitempty"`
}
```

### Repository Source

```go
type RepoSource struct {
    Source                          // Embedded base

    // Git config
    URL        string   `json:"url"`          // Clone URL (https or ssh)
    Branch     string   `json:"branch"`       // Branch to track (default: main)
    LocalPath  string   `json:"local_path"`   // Where cloned on disk
    
    // AST indexer config (maps to WatchPathConfig)
    Org        string   `json:"org"`          // Org for entity IDs
    Project    string   `json:"project"`      // Project name for entity IDs
    Languages  []string `json:"languages"`    // Languages to parse
    Excludes   []string `json:"excludes"`     // Directories to skip
    
    // Sync config
    AutoPull      bool   `json:"auto_pull"`       // Pull on interval
    PullInterval  string `json:"pull_interval"`   // e.g., "5m", "1h"
    LastCommitSHA string `json:"last_commit_sha,omitempty"`
}
```

### Document Source

```go
type DocumentSource struct {
    Source                          // Embedded base

    // Document info
    Filename    string `json:"filename"`           // Original filename
    MimeType    string `json:"mime_type"`          // e.g., "text/markdown", "application/pdf"
    StoragePath string `json:"storage_path"`       // Path in .semspec/sources/docs/
    SizeBytes   int64  `json:"size_bytes"`
    
    // Ingestion config
    ChunkStrategy string `json:"chunk_strategy"`   // "heading", "paragraph", "fixed", "none"
    ChunkSize     int    `json:"chunk_size"`        // For fixed strategy
    
    // Optional URL source (for re-fetch)
    SourceURL string `json:"source_url,omitempty"` // If fetched from URL
}
```

### Graph Entity Representation

Sources become entities in the knowledge graph with vocabulary predicates:

```
Entity ID: source.repo.{slug}
Triples:
  - source.type = "repository"
  - source.name = "Open Sensor Hub"
  - source.url = "https://github.com/opensensorhub/osh-core"
  - source.status = "ready"
  - source.languages = ["java", "typescript"]
  - source.entity_count = 847
  - source.last_indexed = "2025-02-11T10:00:00Z"

Entity ID: source.doc.{slug}
Triples:
  - source.type = "document"
  - source.name = "SensorML Protocol Spec"
  - source.mime_type = "application/pdf"
  - source.status = "ready"
  - source.entity_count = 23
```

This means agents can query "what sources do I have?" via the graph tools they already use (`workflow_query_graph`, `workflow_get_codebase_summary`).

---

## Filesystem Layout

```
.semspec/
├── sources/
│   ├── sources.json          # Source registry (all sources metadata)
│   ├── repos/
│   │   ├── open-sensor-hub/  # Cloned repo
│   │   │   └── ...           # Git working tree
│   │   └── osh-drivers/
│   │       └── ...
│   └── docs/
│       ├── sensorml-spec.pdf
│       ├── osh-api-reference.md
│       └── bmp280-datasheet.pdf
├── changes/
│   └── ...
└── constitution.md
```

The `sources.json` registry stores all source metadata so it survives restarts without needing the graph to be up. The graph entities are the authoritative queryable store; the JSON file is the bootstrap record.

---

## Backend: Source Manager

### New Package: `source/`

A new Go package that manages the source lifecycle. This is NOT a semstreams component — it's a library used by the service manager HTTP handlers, similar to how `workflow.Manager` works today.

```go
// source/manager.go
type Manager struct {
    repoRoot    string
    natsClient  *natsclient.Client
    logger      *slog.Logger
}

// Repository operations
func (m *Manager) AddRepo(ctx context.Context, req AddRepoRequest) (*RepoSource, error)
func (m *Manager) RemoveRepo(ctx context.Context, id string) error
func (m *Manager) PullRepo(ctx context.Context, id string) error
func (m *Manager) ListRepos(ctx context.Context) ([]RepoSource, error)
func (m *Manager) GetRepo(ctx context.Context, id string) (*RepoSource, error)

// Document operations
func (m *Manager) AddDocument(ctx context.Context, req AddDocRequest) (*DocumentSource, error)
func (m *Manager) RemoveDocument(ctx context.Context, id string) error
func (m *Manager) ListDocuments(ctx context.Context) ([]DocumentSource, error)
func (m *Manager) GetDocument(ctx context.Context, id string) (*DocumentSource, error)

// General
func (m *Manager) ListSources(ctx context.Context) ([]Source, error)
func (m *Manager) GetSource(ctx context.Context, id string) (*Source, error)
func (m *Manager) ReindexSource(ctx context.Context, id string) error
```

### AddRepo Flow

1. **Validate** — Check URL is reachable, branch exists
2. **Clone** — `git clone --branch {branch} --depth 1 {url} .semspec/sources/repos/{slug}/`
3. **Register** — Write to `sources.json` with status `pending`
4. **Configure AST Indexer** — Dynamically add a `WatchPathConfig` to the running AST indexer (see below)
5. **Trigger Index** — Publish a reindex signal or call the indexer directly
6. **Update Status** — Once indexing completes, update status to `ready` with entity count
7. **Publish to Graph** — Publish source entity to `graph.ingest.entity`

### AddDocument Flow

1. **Receive File** — Accept multipart upload or URL fetch
2. **Store** — Save to `.semspec/sources/docs/{filename}`
3. **Register** — Write to `sources.json` with status `pending`
4. **Parse & Chunk** — Based on mime type:
   - `text/markdown` (.md): Parse headings as sections, each section becomes an entity
   - `application/pdf` (.pdf): Extract text (use `pdftotext` or Go PDF library), chunk by page or heading
   - `text/plain` (.txt): Chunk by paragraph or fixed size
   - `application/json` / `application/yaml`: Parse structure, create entities for top-level keys
   - `application/openapi+json`: Parse OpenAPI spec, create entities for each endpoint
5. **Ingest** — Publish document chunk entities to `graph.ingest.entity`
6. **Update Status** — Set to `ready` with entity count

### Document Entity Format

Each chunk becomes a graph entity:

```
Entity ID: source.doc.{doc-slug}.chunk.{n}
Triples:
  - source.doc.content = "The BMP280 sensor communicates via I2C at address 0x76..."
  - source.doc.section = "Communication Protocol"
  - source.doc.page = 12
  - source.doc.source = "source.doc.bmp280-datasheet"  (back-reference)
  - dc.terms.title = "BMP280 Datasheet - Communication Protocol"
```

This makes document content queryable through the existing graph tools. When an agent runs `workflow_query_graph` looking for "BMP280 I2C protocol", it finds these entities.

### Dynamic AST Indexer Reconfiguration

The AST indexer currently reads `watch_paths` from config at startup. For dynamic repo addition, we need one of:

**Option A: Hot Reconfiguration (Preferred)**
Add a method to the AST indexer component that accepts new watch paths at runtime:

```go
// processor/ast-indexer/component.go
func (c *Component) AddWatchPath(config WatchPathConfig) error {
    // Validate config
    // Create new pathWatcher
    // Add to c.watchers
    // Trigger initial index of new path
    // Start file watcher for new path
}

func (c *Component) RemoveWatchPath(root string) error {
    // Stop file watcher
    // Remove from c.watchers
    // Optionally: remove entities from graph (or mark stale)
}
```

Expose via NATS request-reply:
```
Subject: ast-indexer.config.add-path
Subject: ast-indexer.config.remove-path
```

**Option B: Config File + Restart Signal**
Write updated config to a file and send a restart signal to the component. Simpler but disruptive.

**Recommendation**: Option A. The AST indexer already manages multiple watchers; adding/removing one at runtime is a natural extension. The NATS request-reply pattern is consistent with how other components communicate.

---

## HTTP API Endpoints

### Sources (general)

```
GET  /api/sources
     Returns: Source[] (all sources, repos and docs)
     Query: ?type=repository|document&status=ready|error

GET  /api/sources/{id}
     Returns: Source (single source with full detail)
```

### Repositories

```
POST /api/sources/repos
     Body: {
       url: string,           // Required: git clone URL
       branch?: string,       // Default: "main"
       name?: string,         // Default: derived from URL
       org?: string,          // Default: from platform config
       project?: string,      // Default: derived from repo name
       languages?: string[],  // Default: auto-detect from repo
       excludes?: string[],   // Default: standard excludes
       auto_pull?: boolean,   // Default: true
       pull_interval?: string  // Default: "5m"
     }
     Returns: RepoSource (with status: "pending")
     
     Flow: Clones repo, configures indexer, triggers index.
     Returns immediately — indexing happens async.
     SSE events report progress.

DELETE /api/sources/repos/{id}
     Removes repo, stops watching, optionally removes entities from graph.

POST /api/sources/repos/{id}/pull
     Triggers a git pull and re-index.

POST /api/sources/repos/{id}/reindex
     Triggers a full re-index without pulling.
```

### Documents

```
POST /api/sources/docs
     Content-Type: multipart/form-data
     Fields:
       file: File,              // The document file
       name?: string,           // Display name (default: filename)
       chunk_strategy?: string, // "heading" | "paragraph" | "fixed" | "none"
       chunk_size?: number      // For fixed strategy (default: 1000 tokens)
     Returns: DocumentSource (with status: "pending")

POST /api/sources/docs/url
     Body: {
       url: string,            // URL to fetch document from
       name?: string,
       chunk_strategy?: string,
       chunk_size?: number
     }
     Returns: DocumentSource (with status: "pending")

DELETE /api/sources/docs/{id}
     Removes document, removes entities from graph.

POST /api/sources/docs/{id}/reindex
     Re-parses and re-ingests the document.
```

### SSE Events for Source Operations

Extend the existing `/stream/activity` SSE endpoint with source-related events:

```typescript
type SourceEvent = {
  type: 'source.clone.started'
      | 'source.clone.completed'
      | 'source.clone.failed'
      | 'source.index.started'
      | 'source.index.progress'    // { entities_found: number }
      | 'source.index.completed'   // { entity_count: number }
      | 'source.index.failed'
      | 'source.pull.completed'
      | 'source.doc.parsed'
      | 'source.doc.ingested';
  source_id: string;
  source_name: string;
  timestamp: string;
  data?: Record<string, unknown>;
};
```

---

## UI: Sources View

### Navigation

Add to the sidebar nav (from the UI redesign spec):

```typescript
const navItems = [
  { path: '/', icon: 'kanban', label: 'Board' },
  { path: '/changes', icon: 'git-pull-request', label: 'Changes' },
  { path: '/sources', icon: 'database', label: 'Sources' },  // NEW
  { path: '/activity', icon: 'activity', label: 'Activity' },
  { path: '/history', icon: 'history', label: 'History' },
  { path: '/settings', icon: 'settings', label: 'Settings' },
];
```

### File Structure

```
src/
├── lib/
│   ├── api/
│   │   └── sources.ts          # NEW - Sources API client
│   ├── stores/
│   │   └── sources.svelte.ts   # NEW - Sources state
│   └── components/
│       └── sources/             # NEW
│           ├── SourcesList.svelte
│           ├── RepoCard.svelte
│           ├── DocCard.svelte
│           ├── AddRepoModal.svelte
│           ├── AddDocModal.svelte
│           ├── IndexingProgress.svelte
│           └── LanguageBadge.svelte
├── routes/
│   └── sources/
│       └── +page.svelte         # Sources view
```

### Sources View Layout (`/sources`)

```
┌──────────────────────────────────────────────────────────────┐
│  Sources                         [+ Add Repo] [+ Add Doc]    │
│                                                               │
│  ┌─ Repositories ────────────────────────────────────────┐   │
│  │                                                        │   │
│  │  ┌────────────────────────────────────────────────┐   │   │
│  │  │ 📦 Open Sensor Hub                    ● Ready  │   │   │
│  │  │ github.com/opensensorhub/osh-core              │   │   │
│  │  │ Branch: main │ Java, TypeScript                │   │   │
│  │  │ 847 entities │ Last indexed: 5 min ago         │   │   │
│  │  │                    [Pull] [Reindex] [Remove]   │   │   │
│  │  └────────────────────────────────────────────────┘   │   │
│  │                                                        │   │
│  │  ┌────────────────────────────────────────────────┐   │   │
│  │  │ 📦 OSH Node Drivers               ● Indexing   │   │   │
│  │  │ github.com/opensensorhub/osh-node-drivers      │   │   │
│  │  │ Branch: develop │ TypeScript                   │   │   │
│  │  │ ████████░░ 312 entities found...               │   │   │
│  │  └────────────────────────────────────────────────┘   │   │
│  │                                                        │   │
│  └────────────────────────────────────────────────────────┘   │
│                                                               │
│  ┌─ Documents ───────────────────────────────────────────┐   │
│  │                                                        │   │
│  │  ┌────────────────────────────────────────────────┐   │   │
│  │  │ 📄 SensorML Protocol Spec           ● Ready    │   │   │
│  │  │ sensorml-spec.pdf │ 2.3 MB │ PDF               │   │   │
│  │  │ 23 chunks │ Strategy: heading                  │   │   │
│  │  │                         [Reindex] [Remove]     │   │   │
│  │  └────────────────────────────────────────────────┘   │   │
│  │                                                        │   │
│  │  ┌────────────────────────────────────────────────┐   │   │
│  │  │ 📄 BMP280 Sensor Datasheet          ● Ready    │   │   │
│  │  │ bmp280-datasheet.pdf │ 1.1 MB │ PDF            │   │   │
│  │  │ 45 chunks │ Strategy: page                     │   │   │
│  │  │                         [Reindex] [Remove]     │   │   │
│  │  └────────────────────────────────────────────────┘   │   │
│  │                                                        │   │
│  │  ┌────────────────────────────────────────────────┐   │   │
│  │  │ 📄 OSH API Reference                ● Ready    │   │   │
│  │  │ osh-api-reference.md │ 48 KB │ Markdown        │   │   │
│  │  │ 67 chunks │ Strategy: heading                  │   │   │
│  │  │                         [Reindex] [Remove]     │   │   │
│  │  └────────────────────────────────────────────────┘   │   │
│  │                                                        │   │
│  └────────────────────────────────────────────────────────┘   │
│                                                               │
│  Knowledge Graph: 1,294 total entities from 5 sources         │
└──────────────────────────────────────────────────────────────┘
```

### Add Repo Modal

```
┌──────────────────────────────────────────────┐
│  Add Repository                         [×]  │
│                                              │
│  Repository URL *                            │
│  ┌──────────────────────────────────────┐    │
│  │ https://github.com/org/repo         │    │
│  └──────────────────────────────────────┘    │
│                                              │
│  Branch                                      │
│  ┌──────────────────────────────────────┐    │
│  │ main                                 │    │
│  └──────────────────────────────────────┘    │
│                                              │
│  ▸ Advanced Settings                         │
│    Name: [auto-detected from URL          ]  │
│    Languages: [auto-detect ▾              ]  │
│    Exclude dirs: [node_modules, vendor, ..]  │
│    Auto-pull: [✓] every [5m            ]     │
│                                              │
│              [Cancel]  [Add Repository]      │
└──────────────────────────────────────────────┘
```

The modal should:
- Auto-detect repo name from URL (last path segment)
- Auto-detect languages if possible (check GitHub API or clone first, scan extensions)
- Show sensible defaults for excludes based on detected languages
- Validate URL format before enabling submit
- After submit, show the repo card immediately with "Cloning..." status
- SSE events update the card through clone → index → ready

### Add Document Modal

```
┌──────────────────────────────────────────────┐
│  Add Document                           [×]  │
│                                              │
│  ┌──────────────────────────────────────┐    │
│  │                                      │    │
│  │   Drop files here or click to browse │    │
│  │                                      │    │
│  │   PDF, Markdown, Text, JSON, YAML    │    │
│  │                                      │    │
│  └──────────────────────────────────────┘    │
│                                              │
│  — or —                                      │
│                                              │
│  Fetch from URL                              │
│  ┌──────────────────────────────────────┐    │
│  │ https://example.com/spec.pdf        │    │
│  └──────────────────────────────────────┘    │
│                                              │
│  Display Name                                │
│  ┌──────────────────────────────────────┐    │
│  │ [auto-filled from filename]         │    │
│  └──────────────────────────────────────┘    │
│                                              │
│  Chunking Strategy                           │
│  [Heading-based ▾]                           │
│    Splits on headings/sections — best for    │
│    structured docs with clear sections       │
│                                              │
│              [Cancel]  [Add Document]        │
└──────────────────────────────────────────────┘
```

The modal should:
- Support drag-and-drop file upload
- Accept multiple files at once
- Auto-detect chunking strategy from mime type (heading for markdown, page for PDF)
- Show file size and type after selection
- Validate supported file types before enabling submit

### Components

**RepoCard.svelte**:
```typescript
interface Props {
  repo: RepoSource;
  onPull: () => void;
  onReindex: () => void;
  onRemove: () => void;
}
```
- Shows: name, URL, branch, languages (as colored badges), entity count, last indexed time, status
- Status states: pending (spinner), cloning (progress), indexing (progress bar with entity count), ready (green), error (red with message), stale (yellow)
- Actions: Pull (fetch latest), Reindex (full re-parse), Remove (with confirmation)

**DocCard.svelte**:
```typescript
interface Props {
  doc: DocumentSource;
  onReindex: () => void;
  onRemove: () => void;
}
```
- Shows: name, filename, mime type icon, file size, chunk count, chunk strategy, status
- Status states: pending, parsing, ingesting, ready, error
- Actions: Reindex, Remove (with confirmation)

**IndexingProgress.svelte** — Reusable progress indicator:
```typescript
interface Props {
  status: SourceStatus;
  entityCount?: number;
  error?: string;
}
```
- Renders appropriate indicator per status: spinner, progress bar, checkmark, error icon
- Progress bar shows entity count ticking up during indexing (via SSE)

**LanguageBadge.svelte** — Small colored pill for language:
```typescript
interface Props {
  language: string;  // "go", "typescript", "java", etc.
}
```
- Each language gets a consistent color (Go: cyan, TypeScript: blue, Java: orange, etc.)

**AddRepoModal.svelte** and **AddDocModal.svelte** — Modal forms as described above.

### Stores

**sources.svelte.ts**:
```typescript
interface SourcesState {
  repos: RepoSource[];
  docs: DocumentSource[];
  loading: boolean;
  error: string | null;
}

// Provides:
//   - repos: RepoSource[] (sorted by name)
//   - docs: DocumentSource[] (sorted by name)
//   - totalEntityCount: number (sum across all sources)
//   - sourceById(id): Source | undefined
//   - addRepo(req): Promise<RepoSource>
//   - addDocument(file, opts): Promise<DocumentSource>
//   - removeSource(id): Promise<void>
//   - pullRepo(id): Promise<void>
//   - reindex(id): Promise<void>
//
// Subscribes to SSE events:
//   - source.* events update individual source status in place
//   - source.index.progress updates entity count live
```

---

## Supported Document Types

| Extension | MIME Type | Parse Strategy | Chunking |
|-----------|-----------|---------------|----------|
| `.md` | text/markdown | Parse markdown AST | By heading (h1/h2/h3 sections) |
| `.pdf` | application/pdf | Extract text via `pdftotext` | By page or by detected headings |
| `.txt` | text/plain | Raw text | By paragraph or fixed size |
| `.json` | application/json | Parse JSON structure | By top-level key |
| `.yaml`/`.yml` | application/yaml | Parse YAML structure | By top-level key |
| `.html` | text/html | Strip tags, extract text | By heading |
| `.rst` | text/x-rst | Parse reStructuredText | By heading |

### Future (not in v1)

| Extension | MIME Type | Notes |
|-----------|-----------|-------|
| `.docx` | Office doc | Requires pandoc or similar |
| `.csv` | Tabular | Row-based entities |
| OpenAPI spec | JSON/YAML | Endpoint-per-entity parsing |
| `.proto` | Protobuf | Service/message definitions |

---

## Integration with Board View

The Board view (from the UI redesign spec) should show a subtle sources summary in the footer or a collapsible section:

```
Knowledge: 5 sources │ 1,294 entities │ 2 repos │ 3 docs │ All indexed ✓
```

If any source is in error or stale state, show a warning:

```
⚠ 1 source needs attention: "osh-drivers" indexing failed    [View Sources]
```

This ties into the attention system — a failed source should appear as an attention item on the Board.

---

## Implementation Order

### Phase 1: Source Manager + Repo Support (Backend)

1. Create `source/` package with `Manager`, `Source`, `RepoSource` types
2. Implement `sources.json` registry read/write
3. Implement `AddRepo` — git clone + register
4. Implement dynamic AST indexer reconfiguration (NATS request-reply)
5. Add HTTP endpoints: `GET /api/sources`, `POST /api/sources/repos`, `DELETE /api/sources/repos/{id}`
6. Add SSE events for source operations
7. **Test**: Add a repo via API, see it get cloned and indexed

### Phase 2: Document Support (Backend)

1. Add `DocumentSource` type
2. Implement file upload handling (multipart)
3. Implement markdown parser/chunker
4. Implement PDF text extraction + chunker
5. Implement entity publishing for document chunks
6. Add HTTP endpoints: `POST /api/sources/docs`, `DELETE /api/sources/docs/{id}`
7. **Test**: Upload a PDF, see chunks appear as graph entities

### Phase 3: Sources UI

1. Create `sources.svelte.ts` store with mock data
2. Build `RepoCard.svelte` and `DocCard.svelte`
3. Build `AddRepoModal.svelte` and `AddDocModal.svelte`
4. Build `IndexingProgress.svelte` and `LanguageBadge.svelte`
5. Build `SourcesList.svelte` and `/sources/+page.svelte`
6. Wire SSE events for live status updates
7. Add sources summary to Board view footer
8. **Test**: Full flow — add repo via UI, watch it clone and index, see entities

### Phase 4: Polish + Backend Wiring

1. Switch from mock to real API
2. Add auto-pull scheduling (background goroutine in source manager)
3. Add language auto-detection for repos
4. Add URL-based document fetching
5. Add source entity count to graph (query graph for count per source prefix)
6. Add sources to attention system (failed sources show on Board)

---

## What This Does NOT Change

- AST indexer core logic — only adds dynamic reconfiguration
- Graph ingestion pipeline — documents use the same `graph.ingest.entity` subject
- Existing `semspec.json` config — static watch_paths still work, sources are additive
- Workflow system — changes/proposals/specs are unaffected
- The UI redesign spec — this is a companion, adds one nav item and minor Board integration

---

## OSH Customer Scenario

With both specs implemented, the Open Sensor Hub workflow looks like:

1. Open SemSpec UI → Board view shows empty project
2. Go to Sources → Add Repo → paste `https://github.com/opensensorhub/osh-core`
3. Watch it clone and index (847 Java entities appear in graph)
4. Add Repo → paste the OSH Node drivers repo
5. Add Document → upload BMP280 sensor datasheet PDF
6. Add Document → upload SensorML protocol spec
7. Go to Board → `/propose "Add BMP280 barometric pressure driver for OSH Node"`
8. Agent now has full context: OSH codebase structure, existing driver patterns, sensor datasheet, protocol spec
9. Pipeline runs: proposal → design → spec → tasks — all informed by the indexed knowledge
10. Developer reviews and approves on the Board, agents implement with real codebase awareness
