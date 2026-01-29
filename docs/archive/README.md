# Archived Documentation

These documents were created during early planning phases before full semstreams integration was understood. They contain useful thinking but describe architectures that are no longer applicable.

## Why Archived

These specs were written when semspec was conceived as a standalone CLI with embedded NATS. After exploring semstreams, we discovered:

1. **Semstreams already has `input/cli`** - A full stdin REPL that publishes to NATS
2. **Semstreams has a service manager** - HTTP API + SSE for web UI integration
3. **Semspec should NOT be a CLI** - It's a tool package + web UI

## Archived Documents

| Document | Original Purpose | Why Archived |
|----------|------------------|--------------|
| `semspec-build-plan.md` | Implementation roadmap | Written with embedded NATS assumptions |
| `semspec-architecture-decisions.md` | ADRs for design choices | Describes CLI approach (now wrong) |
| `semspec-application-spec.md` | Application specification | Describes semspec as CLI binary |

## Current Architecture

See the main `CLAUDE.md` and `README.md` for the current architecture:

- **tools/**: Go package with tool executors registered with semstreams agentic-tools
- **ui/**: SvelteKit web interface talking to semstreams service manager via HTTP/SSE
- **No CLI binary**: Semstreams provides `input/cli` for terminal interaction

## Historical Value

These documents are preserved because:
- They contain useful research and design thinking
- The vocabulary work in `semspec-vocabulary-spec.md` (not archived) remains valid
- They document the evolution of architectural understanding
