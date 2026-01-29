## 1. Project Setup

- [x] 1.1 Initialize Go module (`go mod init github.com/c360/semspec`)
- [x] 1.2 Add SemStreams as dependency (`go get github.com/c360/semstreams`)
- [x] 1.3 Add cobra for CLI (`go get github.com/spf13/cobra`)
- [x] 1.4 Create directory structure: `cmd/semspec/`, `config/`, `tools/`, `storage/`

## 2. Configuration System

- [x] 2.1 Define config structs in `config/config.go` (model, repo, NATS settings)
- [x] 2.2 Implement YAML loading with layered config (user → project)
- [x] 2.3 Implement config file discovery (search parent directories for `semspec.yaml`)
- [x] 2.4 Add git root auto-detection for repo path
- [x] 2.5 Add default config creation on first run
- [x] 2.6 Write config tests

## 3. Tool Executors - File Operations

- [x] 3.1 Create `tools/file/executor.go` implementing `agentic.ToolExecutor`
- [x] 3.2 Implement `file_read` tool with path validation
- [x] 3.3 Implement `file_write` tool with parent directory creation
- [x] 3.4 Implement `file_list` tool with glob pattern support
- [x] 3.5 Add path validation helper (reject traversal outside repo root)
- [x] 3.6 Write file tools tests

## 4. Tool Executors - Git Operations

- [x] 4.1 Create `tools/git/executor.go` implementing `agentic.ToolExecutor`
- [x] 4.2 Implement `git_status` tool
- [x] 4.3 Implement `git_branch` tool (create, switch)
- [x] 4.4 Implement `git_commit` tool with auto-stage option
- [x] 4.5 Add conventional commit message validation
- [x] 4.6 Write git tools tests

## 5. Entity Storage

- [x] 5.1 Create `storage/entity.go` with generic KV operations
- [x] 5.2 Implement proposal entity storage (SEMSPEC_PROPOSALS bucket)
- [x] 5.3 Implement task entity storage (SEMSPEC_TASKS bucket)
- [x] 5.4 Implement result entity storage (SEMSPEC_RESULTS bucket)
- [x] 5.5 Add entity ID generation and parsing utilities
- [x] 5.6 Write entity storage tests

## 6. CLI Binary

- [x] 6.1 Create `cmd/semspec/main.go` with cobra root command
- [x] 6.2 Implement embedded NATS startup (with external URL override)
- [x] 6.3 Implement component initialization (cli-input, router, agentic-*)
- [x] 6.4 Implement tool executor registration with agentic-tools
- [x] 6.5 Implement graceful shutdown with timeout
- [x] 6.6 Add one-shot mode (`semspec "task"`)
- [x] 6.7 Add `--config` and `--nats-url` flags
- [x] 6.8 Add Ollama connectivity check with helpful error message

## 7. Integration Testing

- [x] 7.1 Create integration test that starts semspec and submits a simple task
- [x] 7.2 Test file tool execution end-to-end
- [x] 7.3 Test git tool execution end-to-end
- [x] 7.4 Test Ctrl+C cancellation flow
- [x] 7.5 Test one-shot mode exit codes

## 8. Documentation

- [x] 8.1 Create README.md with setup instructions
- [x] 8.2 Document configuration options in README
- [x] 8.3 Add example `semspec.yaml` configuration file
