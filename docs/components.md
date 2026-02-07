# Semspec Components

## ast-indexer

**Purpose**: Extracts code entities from Go source files and publishes them to the graph.

**Location**: `processor/ast-indexer/`

### Configuration

```json
{
  "repo_path": ".",
  "org": "semspec",
  "project": "myproject",
  "watch_enabled": true,
  "index_interval": "5m"
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `repo_path` | string | `.` | Repository path to index |
| `org` | string | required | Organization for entity IDs |
| `project` | string | required | Project name for entity IDs |
| `watch_enabled` | bool | `true` | Enable file watcher for real-time updates |
| `index_interval` | string | `5m` | Periodic full reindex interval |

### Behavior

1. **Startup**: Performs full index of all `.go` files in `repo_path`
2. **Watch mode**: If enabled, watches for file changes via fsnotify
3. **Periodic reindex**: If interval set, performs full reindex on schedule
4. **Output**: Publishes entities to `graph.ingest.entity` subject

### Entity Types Extracted

| Type | Description |
|------|-------------|
| `file` | Go source files |
| `function` | Standalone functions |
| `method` | Methods with receivers |
| `struct` | Struct types |
| `interface` | Interface types |
| `const` | Constants |
| `var` | Variables |

### Entity ID Format

```
{org}.semspec.code.{type}.{project}.{instance}
```

Example: `acme.semspec.code.function.myproject.cmd-main-go-main`

### Dependencies

Uses `processor/ast/` package:
- `parser.go` - Go AST parsing
- `entities.go` - CodeEntity type and serialization
- `watcher.go` - File system watcher with debouncing
- `predicates.go` - Vocabulary predicate constants

---

## semspec-tools

**Purpose**: Executes file and git operations for agentic workflows.

**Location**: `processor/semspec-tools/`

### Configuration

```json
{
  "repo_path": ".",
  "stream_name": "AGENT",
  "timeout": "30s",
  "heartbeat_interval": "10s"
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `repo_path` | string | `.` | Repository path for operations |
| `stream_name` | string | `AGENT` | JetStream stream name |
| `timeout` | string | `30s` | Tool execution timeout |
| `heartbeat_interval` | string | `10s` | Heartbeat send interval |
| `consumer_name_suffix` | string | (none) | Suffix for consumer uniqueness |

### Tools Provided

| Tool | Description | Parameters |
|------|-------------|------------|
| `file_read` | Read file contents | `path` (required) |
| `file_write` | Write file contents | `path`, `content` (required) |
| `file_list` | List directory | `path` (optional, defaults to repo root) |
| `git_status` | Get git status | (none) |
| `git_branch` | List/create branches | `name` (optional), `create` (optional) |
| `git_commit` | Create commit | `message` (required), `files` (optional) |

### NATS Subjects

| Subject | Direction | Description |
|---------|-----------|-------------|
| `tool.execute.file_*` | Input | File operation requests |
| `tool.execute.git_*` | Input | Git operation requests |
| `tool.result.<call_id>` | Output | Execution results |
| `tool.register.<name>` | Output | Tool advertisement |
| `tool.heartbeat.semspec` | Output | Provider health |

### Consumer Naming

Uses `semspec-tool-<name>` pattern to avoid conflicts with semstreams' `agentic-tools-<name>` consumers.

### Security

- **Path validation**: All file operations validated to stay within `repo_path`
- **Conventional commits**: Git commits validated against `type(scope)?: description` format

### Dependencies

Uses `tools/` packages:
- `tools/file/executor.go` - FileExecutor
- `tools/git/executor.go` - GitExecutor

---

## constitution

**Purpose**: Manages and enforces project constitution rules. The constitution defines project-wide constraints checked during development workflows.

**Location**: `processor/constitution/`

### Configuration

```json
{
  "project": "myproject",
  "org": "myorg",
  "file_path": ".semspec/constitution.yaml",
  "auto_reload": true,
  "enforce_mode": "warn",
  "stream_name": "AGENT"
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `project` | string | required | Project name for constitution |
| `org` | string | required | Organization for entity IDs |
| `file_path` | string | - | Path to constitution YAML/JSON file |
| `auto_reload` | bool | `true` | Watch file for changes |
| `enforce_mode` | string | `warn` | Enforcement mode: `strict`, `warn`, `off` |
| `stream_name` | string | `AGENT` | JetStream stream name |

### Constitution File Format

```yaml
version: "v1"
code_quality:
  - "All functions must have clear, descriptive names"
  - "Complex logic must include explanatory comments"
testing:
  - "All public APIs must have test coverage"
  - "Tests must include edge cases"
security:
  - "No hardcoded credentials"
  - "All user input must be validated"
architecture:
  - "Components must be loosely coupled"
  - "Follow dependency injection patterns"
```

### Behavior

1. **Loads Rules**: Reads constitution from YAML/JSON file
2. **Publishes to Graph**: Constitution entity stored in graph
3. **Handles Checks**: Processes check requests via `/check` command
4. **Reports Violations**: Returns violations (MUST rules) and warnings (SHOULD rules)

### NATS Subjects

| Subject | Direction | Description |
|---------|-----------|-------------|
| `constitution.check.request` | Input | Check requests |
| `constitution.check.result` | Output | Check results |
| `graph.ingest.entity` | Output | Constitution entity updates |

---

## rdf-export

**Purpose**: Streaming output component that subscribes to graph entity ingestion messages and serializes them to RDF formats.

**Location**: `processor/rdf-export/`

### Configuration

```json
{
  "format": "turtle",
  "profile": "minimal",
  "base_iri": "https://semspec.dev"
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `format` | string | `turtle` | RDF format: `turtle`, `ntriples`, `jsonld` |
| `profile` | string | `minimal` | Ontology profile: `minimal`, `bfo`, `cco` |
| `base_iri` | string | `https://semspec.dev` | Base IRI for entity URIs |

### Profiles

| Profile | Description |
|---------|-------------|
| `minimal` | PROV-O only - basic provenance |
| `bfo` | Adds BFO (Basic Formal Ontology) types |
| `cco` | Adds CCO (Common Core Ontologies) types |

### Behavior

1. **Subscribes**: Consumes from `graph.ingest.entity` subject
2. **Infers Types**: Adds `rdf:type` triples based on entity ID pattern
3. **Serializes**: Converts triples to requested RDF format
4. **Publishes**: Outputs to `graph.export.rdf` subject

### NATS Subjects

| Subject | Direction | Description |
|---------|-----------|-------------|
| `graph.ingest.entity` | Input | Entity ingest messages |
| `graph.export.rdf` | Output | Serialized RDF output |

### Entity Type Inference

Entity IDs are mapped to RDF types based on patterns:

| Pattern | RDF Type |
|---------|----------|
| `*.code.function.*` | `semspec:Function` |
| `*.code.struct.*` | `semspec:Struct` |
| `*.proposal.*` | `semspec:Proposal` |
| `*.constitution.*` | `semspec:Constitution` |

---

## question-answerer

**Purpose**: Answers questions using LLM agents based on topic and capability routing. Part of the knowledge gap resolution protocol.

**Location**: `processor/question-answerer/`

### Configuration

```json
{
  "stream_name": "AGENT",
  "consumer_name": "question-answerer",
  "task_subject": "agent.task.question-answerer",
  "default_capability": "reviewing"
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `stream_name` | string | `AGENT` | JetStream stream name |
| `consumer_name` | string | `question-answerer` | Durable consumer name |
| `task_subject` | string | `agent.task.question-answerer` | Subject to consume tasks from |
| `default_capability` | string | `reviewing` | Default model capability |

### Behavior

1. **Consumes Tasks**: Listens on `agent.task.question-answerer` for question-answering tasks
2. **Resolves Model**: Uses capability-based model selection (planning, reviewing, coding, etc.)
3. **Generates Answer**: Calls LLM with question context and topic
4. **Publishes Answer**: Sends answer to `question.answer.<id>` subject
5. **Updates Store**: Marks question as answered in QUESTIONS KV bucket

### NATS Subjects

| Subject | Direction | Description |
|---------|-----------|-------------|
| `agent.task.question-answerer` | Input | Question-answering tasks from router |
| `question.answer.<id>` | Output | Answer payloads |

### Dependencies

- `workflow/answerer/` — Task types and routing
- `workflow/question.go` — Question store
- `model/` — Capability-based model selection

---

## question-timeout

**Purpose**: Monitors question SLAs and triggers escalation when questions are not answered in time.

**Location**: `processor/question-timeout/`

### Configuration

```json
{
  "check_interval": "1m",
  "default_sla": "24h",
  "answerer_config_path": "configs/answerers.yaml"
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `check_interval` | duration | `1m` | How often to check for timeouts |
| `default_sla` | duration | `24h` | Default SLA when not specified in route |
| `answerer_config_path` | string | (auto-detected) | Path to answerers.yaml config |

### Behavior

1. **Periodic Check**: Runs on `check_interval` to find overdue questions
2. **SLA Evaluation**: Compares question age against route SLA (or default)
3. **Timeout Events**: Publishes `question.timeout.<id>` when SLA exceeded
4. **Escalation**: If `escalate_to` configured, reassigns question and publishes `question.escalate.<id>`
5. **Notifications**: Can trigger notifications via configured channels

### NATS Subjects

| Subject | Direction | Description |
|---------|-----------|-------------|
| `question.timeout.<id>` | Output | Timeout events |
| `question.escalate.<id>` | Output | Escalation events |

### Escalation Flow

When a question's SLA is exceeded:
1. Timeout event published
2. Question reassigned to `escalate_to` answerer
3. Escalation event published
4. Notifications sent (if configured)

### Dependencies

- `workflow/answerer/registry.go` — Route configuration with SLAs
- `workflow/question.go` — Question store

---

## Creating New Components

### Directory Structure

```
processor/<name>/
├── component.go   # Discoverable + lifecycle implementation
├── config.go      # Configuration schema
└── factory.go     # Component registration
```

### Required Interface

```go
// Must implement component.Discoverable
type Component struct { ... }

func (c *Component) Meta() component.Metadata
func (c *Component) InputPorts() []component.Port
func (c *Component) OutputPorts() []component.Port
func (c *Component) ConfigSchema() component.ConfigSchema
func (c *Component) Health() component.HealthStatus
func (c *Component) DataFlow() component.FlowMetrics

// Optional lifecycle methods
func (c *Component) Initialize() error
func (c *Component) Start(ctx context.Context) error
func (c *Component) Stop(timeout time.Duration) error
```

### Registration

```go
// factory.go
func Register(registry RegistryInterface) error {
    return registry.RegisterWithConfig(component.RegistrationConfig{
        Name:        "my-component",
        Factory:     NewComponent,
        Schema:      mySchema,
        Type:        "processor",
        Protocol:    "custom",
        Domain:      "semantic",
        Description: "My custom component",
        Version:     "0.1.0",
    })
}
```

### Wiring

1. Import in `cmd/semspec/main.go`
2. Call `mycomponent.Register(registry)`
3. Add instance config to `configs/semspec.json`
