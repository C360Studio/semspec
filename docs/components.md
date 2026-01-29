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
