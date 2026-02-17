# Semspec Components

> **When to use components vs workflows?** See [Architecture: Components vs Workflows](architecture.md#components-vs-workflows) for the decision framework.

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

### Tool Output Format

Tool results include structured output for agent consumption:

```json
{
  "type": "tool.result",
  "payload": {
    "call_id": "abc123",
    "tool": "file_read",
    "success": true,
    "result": "file contents...",
    "error": null
  }
}
```

For async operations, results are delivered via Server-Sent Events (SSE) on the `/events` endpoint when using the HTTP gateway. The agent loop automatically correlates results with pending tool calls.

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
| `*.plan.*` | `semspec:Plan` |
| `*.constitution.*` | `semspec:Constitution` |

---

## planner

**Purpose**: Generates Goal/Context/Scope for plans using LLM based on the plan title.

**Location**: `processor/planner/`

### Configuration

```json
{
  "stream_name": "WORKFLOWS",
  "consumer_name": "planner",
  "trigger_subject": "workflow.trigger.planner",
  "default_capability": "planning"
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `stream_name` | string | `WORKFLOWS` | JetStream stream name |
| `consumer_name` | string | `planner` | Durable consumer name |
| `trigger_subject` | string | `workflow.trigger.planner` | Subject to consume triggers from |
| `default_capability` | string | `planning` | Default model capability |

### Behavior

1. **Subscribes**: Consumes from `workflow.trigger.planner` on WORKFLOWS stream
2. **Loads Plan**: Reads existing plan from `.semspec/plans/{slug}/plan.json`
3. **Generates Content**: Calls LLM with planner system prompt
4. **Parses Response**: Extracts JSON for Goal/Context/Scope from LLM output
5. **Saves Plan**: Updates plan.json with generated content
6. **Publishes Result**: Sends completion to `workflow.result.planner.{slug}`

### LLM Response Format

The component expects LLM to return JSON (possibly wrapped in markdown code blocks):

```json
{
  "goal": "Clear statement of what the plan accomplishes",
  "context": "Current state and relevant background",
  "scope": {
    "include": ["files/areas to modify"],
    "exclude": ["files/areas to avoid"],
    "do_not_touch": ["critical files to preserve"]
  }
}
```

### NATS Subjects

| Subject | Direction | Description |
|---------|-----------|-------------|
| `workflow.trigger.planner` | Input | Plan generation triggers |
| `workflow.result.planner.<slug>` | Output | Completion notifications |

---

## explorer

**Purpose**: Generates exploration content (Goal/Context/Questions) using LLM for uncommitted explorations.

**Location**: `processor/explorer/`

### Configuration

```json
{
  "stream_name": "WORKFLOWS",
  "consumer_name": "explorer",
  "trigger_subject": "workflow.trigger.explorer",
  "default_capability": "planning"
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `stream_name` | string | `WORKFLOWS` | JetStream stream name |
| `consumer_name` | string | `explorer` | Durable consumer name |
| `trigger_subject` | string | `workflow.trigger.explorer` | Subject to consume triggers from |
| `default_capability` | string | `planning` | Default model capability |

### Behavior

1. **Subscribes**: Consumes from `workflow.trigger.explorer` on WORKFLOWS stream
2. **Loads Plan**: Reads existing exploration from `.semspec/plans/{slug}/plan.json`
3. **Generates Content**: Calls LLM with explorer system prompt
4. **Parses Response**: Extracts JSON for Goal/Context/Questions from LLM output
5. **Saves Exploration**: Updates plan.json with generated content
6. **Publishes Result**: Sends completion to `workflow.result.explorer.{slug}`

### LLM Response Format

The component expects LLM to return JSON (possibly wrapped in markdown code blocks):

```json
{
  "goal": "What the exploration aims to understand",
  "context": "Current understanding and background",
  "questions": [
    "Clarifying question 1?",
    "Clarifying question 2?"
  ]
}
```

### NATS Subjects

| Subject | Direction | Description |
|---------|-----------|-------------|
| `workflow.trigger.explorer` | Input | Exploration triggers |
| `workflow.result.explorer.<slug>` | Output | Completion notifications |

---

## task-generator

**Purpose**: Generates tasks with BDD acceptance criteria from plans using LLM.

**Location**: `processor/task-generator/`

### Configuration

```json
{
  "stream_name": "WORKFLOWS",
  "consumer_name": "task-generator",
  "trigger_subject": "workflow.trigger.tasks",
  "default_capability": "planning"
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `stream_name` | string | `WORKFLOWS` | JetStream stream name |
| `consumer_name` | string | `task-generator` | Durable consumer name |
| `trigger_subject` | string | `workflow.trigger.tasks` | Subject to consume triggers from |
| `default_capability` | string | `planning` | Default model capability |

### Behavior

1. **Subscribes**: Consumes from `workflow.trigger.tasks` on WORKFLOWS stream
2. **Loads Plan**: Reads plan from `.semspec/plans/{slug}/plan.json`
3. **Generates Tasks**: Calls LLM with task generation prompt including plan Goal/Context
4. **Parses Response**: Extracts JSON array of tasks with acceptance criteria
5. **Saves Tasks**: Writes to `.semspec/plans/{slug}/tasks.json`
6. **Publishes Result**: Sends completion to `workflow.result.tasks.{slug}`

### LLM Response Format

The component expects LLM to return JSON (possibly wrapped in markdown code blocks):

```json
{
  "tasks": [
    {
      "id": "1",
      "title": "Task title",
      "description": "What needs to be done",
      "acceptance_criteria": [
        "GIVEN context WHEN action THEN result"
      ],
      "dependencies": []
    }
  ]
}
```

### NATS Subjects

| Subject | Direction | Description |
|---------|-----------|-------------|
| `workflow.trigger.tasks` | Input | Task generation triggers |
| `workflow.result.tasks.<slug>` | Output | Completion notifications |

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

## workflow-validator

**Purpose**: Request/reply service for validating workflow documents against their type requirements. Ensures plans and tasks meet content requirements before workflow progression.

**Location**: `processor/workflow-validator/`

### Configuration

```json
{
  "base_dir": ".",
  "timeout_secs": 30
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `base_dir` | string | `SEMSPEC_REPO_PATH` or cwd | Base directory for document paths |
| `timeout_secs` | int | `30` | Request timeout in seconds |

### Request Format

```json
{
  "slug": "add-auth-refresh",
  "document": "plan",
  "content": "...",
  "path": ".semspec/plans/add-auth-refresh/plan.json"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `slug` | string | Change identifier |
| `document` | string | Document type: `plan`, `tasks` |
| `content` | string | Document content (if provided directly) |
| `path` | string | Path to document file (if reading from disk) |

Either `content` or `path` must be provided.

### Response Format

```json
{
  "valid": true,
  "document": "plan",
  "errors": [],
  "warnings": ["Consider adding acceptance criteria"]
}
```

### Behavior

1. **Receives Request**: Via NATS request/reply on `workflow.validate.*`
2. **Resolves Content**: From `content` field or reads from `path`
3. **Validates Structure**: Checks document against type-specific requirements
4. **Returns Result**: Synchronous response with validation status

### NATS Subjects

| Subject | Direction | Description |
|---------|-----------|-------------|
| `workflow.validate.*` | Input | Validation requests (wildcard for document type) |
| `workflow.validation.events` | Output | Optional validation event notifications |

### Security

- **Path validation**: Document paths validated to stay within `base_dir`
- **Path traversal protection**: Blocks attempts to read outside repository

### Integration

Used by workflow-processor during step transitions to validate document content before progressing to next workflow state.

---

## workflow-documents

**Purpose**: Output component that subscribes to workflow document messages and writes them as files to the `.semspec/plans/{slug}/` directory.

**Location**: `output/workflow-documents/`

### Configuration

```json
{
  "base_dir": ".",
  "ports": {
    "inputs": [{
      "name": "documents_in",
      "type": "jetstream",
      "subject": "output.workflow.documents",
      "stream_name": "WORKFLOWS"
    }],
    "outputs": [{
      "name": "documents_written",
      "type": "nats",
      "subject": "workflow.documents.written"
    }]
  }
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `base_dir` | string | `SEMSPEC_REPO_PATH` or cwd | Base directory for document output |
| `ports` | PortConfig | (see above) | Input/output port configuration |

### Document Output Message

```json
{
  "type": "workflow.output.document",
  "payload": {
    "slug": "add-auth-refresh",
    "document": "proposal",
    "content": "{ ... JSON content ... }",
    "entity_id": "semspec.proposal.add-auth-refresh"
  }
}
```

### Behavior

1. **Consumes Messages**: From `output.workflow.documents` JetStream subject
2. **Transforms Content**: Converts JSON content to markdown based on document type
3. **Writes File**: Creates `.semspec/plans/{slug}/{document}.json`
4. **Publishes Notification**: Sends `workflow.documents.written` event

### Document Types

| Type | Output File | Transformation |
|------|-------------|----------------|
| `plan` | `plan.json` | Goal/context/scope |
| `tasks` | `tasks.json` | BDD task checklist with acceptance criteria |

### NATS Subjects

| Subject | Direction | Description |
|---------|-----------|-------------|
| `output.workflow.documents` | Input | Document output messages (JetStream) |
| `workflow.documents.written` | Output | File written notifications |

### File Structure

```
.semspec/
└── plans/
    └── {slug}/
        ├── plan.json
        ├── metadata.json
        └── tasks.json
```

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
