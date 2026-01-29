## Why

Semspec needs a working foundation before we can build advanced features like the knowledge graph or training flywheel. Phase 1 delivers a minimal but functional agent that wires together existing SemStreams components (cli-input, router, agentic-*) into a cohesive CLI tool, adds the missing pieces (tool executors, entity storage), and proves the end-to-end flow works.

## What Changes

- New CLI binary (`cmd/semspec`) that orchestrates existing SemStreams components
- Tool executors for file operations and git (registered with agentic-tools)
- Entity storage for proposals, tasks, and results (using NATS KV)
- Project and model configuration system

## Capabilities

### New Capabilities

- `semspec-cli`: CLI binary that wires together SemStreams components into a runnable agent
- `tool-file-ops`: File read/write/list operations for agent tool calls
- `tool-git-ops`: Git status/commit/branch operations for agent tool calls
- `entity-storage`: KV storage for proposal, task, and result entities
- `project-config`: Project configuration and Ollama model settings

### Modified Capabilities

(none - this builds on existing SemStreams components without modifying them)

### Existing Capabilities (from SemStreams - NOT part of this change)

These already exist in `../semstreams` and will be used as-is:
- `cli-input`: Terminal input handling, Ctrl+C signals, response rendering
- `router`: Command parsing, permissions, message routing, loop tracking
- `agentic-loop`: Agent state machine orchestration
- `agentic-model`: LLM communication via OpenAI-compatible API
- `agentic-tools`: Tool execution with timeout and allowlist

## Non-goals

- **Modifying SemStreams components** - We use them as-is
- **No knowledge graph yet** - Phase 2 adds AST understanding and persistent context
- **No multi-model orchestration** - Phase 3 adds specialized roles (architect/editor split)
- **No training capture** - Phase 4 adds the feedback flywheel
- **No Slack/Discord/Web** - Phase 5 adds additional input channels

## Impact

- **New packages**: `cmd/semspec`, `tools/file`, `tools/git`, `storage/entity`, `config/`
- **Dependencies**: SemStreams (as Go module), cobra (CLI), NATS KV
- **External**: Ollama must be running with qwen2.5-coder model pulled
