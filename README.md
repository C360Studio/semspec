# Semspec

Semspec is a semantic development agent built on SemStreams. It provides an AI-powered assistant for software engineering tasks with persistent memory and multi-agent capabilities.

## Features

- **Embedded NATS**: Runs with embedded NATS server for easy local development
- **File Operations**: Read, write, and list files within your repository
- **Git Operations**: Check status, create branches, and commit changes with conventional commit validation
- **Entity Storage**: Persistent storage for proposals, tasks, and results
- **REPL Mode**: Interactive command-line interface
- **One-Shot Mode**: Execute single tasks from the command line

## Installation

### Prerequisites

- Go 1.25 or later
- Ollama (for LLM inference)

### Build from Source

```bash
git clone https://github.com/c360/semspec.git
cd semspec
go build -o semspec ./cmd/semspec
```

## Quick Start

1. Start Ollama:
   ```bash
   ollama serve
   ```

2. Pull the default model:
   ```bash
   ollama pull qwen2.5-coder:32b
   ```

3. Run semspec in your project directory:
   ```bash
   cd /path/to/your/project
   semspec
   ```

## Usage

### REPL Mode

Start semspec without arguments for interactive mode:

```bash
semspec
```

Available commands:
- `/help` - Show available commands
- `/status` - Show current status
- `/tools` - List available tools
- `/config` - Show current configuration
- `quit` or `exit` - Exit the REPL

### One-Shot Mode

Execute a single task and exit:

```bash
semspec "Create a new function to handle user authentication"
```

### Command-Line Flags

```
--config    Path to config file
--nats-url  NATS server URL (default: embedded)
--version   Show version information
--help      Show help
```

## Configuration

Semspec uses a layered configuration system:

1. Default configuration
2. User configuration: `~/.config/semspec/config.yaml`
3. Project configuration: `semspec.yaml` (searched in current and parent directories)

### Example Configuration

```yaml
# semspec.yaml
model:
  default: qwen2.5-coder:32b
  endpoint: http://localhost:11434/v1
  temperature: 0.2
  timeout: 5m

repo:
  path: ""  # Auto-detected from git

nats:
  url: ""        # Empty for embedded
  embedded: true

tools:
  allowlist: []  # Empty allows all tools
```

### Configuration Options

| Option | Description | Default |
|--------|-------------|---------|
| `model.default` | Default LLM model | `qwen2.5-coder:32b` |
| `model.endpoint` | Ollama API endpoint | `http://localhost:11434/v1` |
| `model.temperature` | Generation temperature (0.0-1.0) | `0.2` |
| `model.timeout` | Request timeout | `5m` |
| `repo.path` | Repository root path | Auto-detected from git |
| `nats.url` | External NATS URL | Empty (use embedded) |
| `nats.embedded` | Use embedded NATS | `true` |
| `tools.allowlist` | Allowed tool names | Empty (allow all) |

## Available Tools

### File Operations

| Tool | Description |
|------|-------------|
| `file_read` | Read contents of a file |
| `file_write` | Write contents to a file |
| `file_list` | List files in a directory |

### Git Operations

| Tool | Description |
|------|-------------|
| `git_status` | Get git repository status |
| `git_branch` | Create or switch branches |
| `git_commit` | Commit changes with conventional commit format |

## Architecture

Semspec is built on SemStreams, leveraging its:

- **NATS messaging**: For component communication
- **JetStream**: For persistent storage via KV buckets
- **Agentic components**: CLI input, router, model, tools, and loop

### Entity Storage

Semspec stores entities in NATS KV buckets:

- `SEMSPEC_PROPOSALS`: Proposal entities
- `SEMSPEC_TASKS`: Task entities
- `SEMSPEC_RESULTS`: Result entities

## Development

### Running Tests

```bash
go test ./...
```

### Building

```bash
go build ./cmd/semspec
```

## License

MIT
