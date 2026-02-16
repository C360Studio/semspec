# ADR-007: Web UI Only, No CLI Mode

## Status

Accepted

## Context

Semspec originally supported two interaction modes:
- **Service Mode**: Long-lived service with HTTP endpoints and Web UI
- **CLI Mode**: Interactive command-line interface via `./semspec cli`

After implementing both and using them in practice, we discovered fundamental UX problems with the CLI approach for async agent workflows.

### The Core Problem

Semspec workflows are inherently asynchronous:
1. User sends a command (e.g., `/plan Add auth`)
2. Command dispatches work to agent loops running in the background
3. Results arrive later via NATS messages
4. Agents may ask clarifying questions that need answers
5. Multiple steps may chain together (plan → tasks → execute)

This async model creates problems for traditional CLI:

| Issue | CLI Experience | Web UI Experience |
|-------|---------------|-------------------|
| **No feedback loop** | Command returns immediately, user sees nothing | SSE stream shows real-time progress |
| **Questions can't interrupt** | User must poll `/questions` repeatedly | Questions appear inline, answers flow naturally |
| **Results are delayed** | User must poll `/changes` or `/status` | Updates push to UI as they happen |
| **Multi-step workflows** | User manually chains commands | Autonomous mode shows each step live |

### What We Tried

1. **Polling-based CLI**: Required users to repeatedly run status commands
2. **Blocking CLI**: Waited for completion, but couldn't handle questions
3. **Hybrid approaches**: Added complexity without solving the core issue

None of these provided a good user experience for async workflows.

## Decision

**Remove CLI mode entirely. Semspec is Web UI only.**

The Web UI solves all the async UX problems:
- **SSE event stream** pushes activity, questions, and results in real-time
- **Questions appear inline** and can be answered without switching context
- **Progress is visible** as agents work through multi-step workflows
- **Single interface** to maintain and document

## Consequences

### Positive

- **Simpler codebase**: Removed ~300 lines of CLI-specific code
- **Better UX**: Users get real-time feedback on async operations
- **Easier testing**: One interface to E2E test, not two
- **Clearer docs**: No need to explain when to use CLI vs Web UI

### Negative

- **No terminal workflow**: Power users who prefer terminal must use HTTP API directly
- **Requires browser**: Can't interact with semspec from headless environments

### Mitigations

For automation and scripting:
- HTTP API remains fully functional (`POST /agentic-dispatch/message`)
- `curl` can send commands and poll for results
- CI/CD can integrate via HTTP endpoints

For terminal preference:
- SSE can be consumed via `curl` for live streaming
- Consider adding a thin TUI client in the future if demand exists

## Alternatives Considered

### Keep CLI with Polling

User runs command, then polls for results. Rejected because:
- Poor UX (constant manual polling)
- Easy to miss questions that timeout
- Doesn't scale to multi-step workflows

### CLI with Background Watcher

Separate process watches for events and prints them. Rejected because:
- Complex to implement correctly
- Process management issues
- Still can't handle interactive questions well

### WebSocket-based CLI

CLI maintains WebSocket connection for push updates. Rejected because:
- Essentially reimplementing the Web UI in terminal
- High implementation cost for marginal benefit
- Terminal rendering of rich content is limited

## References

- [ADR-003: Workflow Refactor](adr-003-workflow-refactor.md) - Defines the async workflow model
- [How Semspec Works](../how-it-works.md) - Explains the async message flow
