# Workflow System

This document describes the LLM-driven workflow system in semspec, including capability-based model selection, autonomous workflow orchestration, and document validation with auto-retry.

## Overview

The workflow system enables structured document generation through a series of steps:

```
/propose → proposal.md → /design → design.md → /spec → spec.md → /tasks → tasks.md
```

Each step generates a markdown document stored in `.semspec/changes/{slug}/`.

## Architecture

Semspec uses **semstreams' workflow-processor** for multi-step orchestration. The workflow definition lives in `configs/workflows/document-generation.json`.

```
┌─────────────────────────────────────────────────────────────┐
│                       SEMSPEC                                │
├─────────────────────────────────────────────────────────────┤
│ Commands:          /propose, /design, /spec, /tasks          │
│                           │                                  │
│                           ▼                                  │
│ Workflow Trigger:  workflow.trigger.document-generation      │
│                           │                                  │
├───────────────────────────┼─────────────────────────────────┤
│                    SEMSTREAMS                                │
│                           │                                  │
│                           ▼                                  │
│ Workflow Processor: Executes document-generation.json        │
│                    - generate_proposal step                  │
│                    - check_auto_continue_design step         │
│                    - generate_design step (if auto)          │
│                    - ... more steps                          │
│                    - generation_failed step (on error)       │
│                           │                                  │
│                           ▼                                  │
│ Agentic Loop:       Executes LLM calls                       │
│                    - Model selection                         │
│                    - Tool execution                          │
│                    - Completion/failure events               │
└─────────────────────────────────────────────────────────────┘
```

**Key benefits of using semstreams' workflow-processor:**
- Built-in failure handling via `on_fail` steps
- Conditional step chaining (`check_auto_continue_*` steps)
- Persisted execution state for recovery
- Variable interpolation (`${trigger.payload.*}`)

## Capability-Based Model Selection

Instead of specifying models directly, workflow commands use semantic capabilities that map to appropriate models.

### Capabilities

| Capability | Description | Default Model |
|------------|-------------|---------------|
| `planning` | High-level reasoning, architecture decisions | claude-opus |
| `writing` | Documentation, proposals, specifications | claude-sonnet |
| `coding` | Code generation, implementation | claude-sonnet |
| `reviewing` | Code review, quality analysis | claude-sonnet |
| `fast` | Quick responses, simple tasks | claude-haiku |

### Role-to-Capability Mapping

| Role | Default Capability |
|------|-------------------|
| proposal-writer | writing |
| design-writer | planning |
| spec-writer | writing |
| tasks-writer | planning |

### Usage

```bash
# Default (uses role's default capability)
/propose Add user authentication
# → writing capability → claude-sonnet

# Explicit capability
/propose Add auth --capability planning
# → planning capability → claude-opus

# Short alias
/design my-feature --cap fast
# → fast capability → claude-haiku

# Direct model override (power user)
/propose Add auth --model qwen
# → bypasses registry, uses qwen directly
```

### Configuration

Configure the model registry in `configs/semspec.json`:

```json
{
  "model_registry": {
    "capabilities": {
      "planning": {
        "description": "High-level reasoning, architecture decisions",
        "preferred": ["claude-opus", "claude-sonnet"],
        "fallback": ["qwen", "llama3.2"]
      },
      "writing": {
        "description": "Documentation, proposals, specifications",
        "preferred": ["claude-sonnet"],
        "fallback": ["claude-haiku", "qwen"]
      }
    },
    "endpoints": {
      "claude-sonnet": {
        "provider": "anthropic",
        "model": "claude-sonnet-4-20250514",
        "max_tokens": 200000
      },
      "qwen": {
        "provider": "ollama",
        "url": "http://localhost:11434/v1",
        "model": "qwen2.5-coder:14b",
        "max_tokens": 128000
      }
    },
    "defaults": {
      "model": "qwen"
    }
  }
}
```

### Fallback Chain

When the primary model fails, the system tries fallback models in order:

```
claude-opus (unavailable) → claude-sonnet → qwen → llama3.2
```

## Workflow Modes

The workflow supports two modes: interactive and autonomous.

### Interactive Mode (default)

```bash
/propose Add auth
# Generates proposal.md, then waits for user review
/design add-auth
# Generates design.md, then waits for user review
```

### Autonomous Mode

```bash
/propose Add auth --auto
# Generates all documents automatically:
# proposal.md → design.md → spec.md → tasks.md
```

### How It Works

```
1. User runs: /propose Add auth --auto
2. ProposeCommand publishes to workflow.trigger.document-generation
3. Semstreams workflow-processor executes document-generation.json
4. generate_proposal step runs agentic-loop to create proposal.md
5. check_auto_continue_design step evaluates ${trigger.payload.auto}
6. If auto=true, generate_design step runs
7. Process repeats for design → spec → tasks
8. If any step fails, generation_failed step notifies user
9. Final notification sent on workflow completion
```

### Workflow Configuration

The workflow is defined in `configs/workflows/document-generation.json`. This JSON file uses semstreams' workflow-processor schema with:

- **Steps**: Each document generation step with LLM prompts
- **Conditional steps**: `check_auto_continue_*` to control chaining
- **Failure handling**: `on_fail` steps for error notification
- **Variable interpolation**: `${trigger.payload.*}` for dynamic values

## Document Validation

Generated documents are validated before proceeding to the next step.

### Document Type Requirements

#### Proposal (`proposal.md`)

| Section | Min Length | Description |
|---------|-----------|-------------|
| Title | - | `# heading` |
| Why | 50 chars | Rationale for the change |
| What Changes | 50 chars | List of modifications |
| Impact | 30 chars | Affected areas |

#### Design (`design.md`)

| Section | Min Length | Description |
|---------|-----------|-------------|
| Title | - | `# Design: ...` |
| Technical Approach | 100 chars | Strategy and key decisions |
| Components Affected | 50 chars | Component change table |
| Data Flow | 30 chars | Data flow description |
| Dependencies | 20 chars | New/removed dependencies |

#### Spec (`spec.md`)

| Section | Min Length | Description |
|---------|-----------|-------------|
| Title | - | Specification title |
| Overview | 30 chars | Overview section |
| Requirements | 100 chars | Formal requirements |
| GIVEN/WHEN/THEN | - | At least one scenario |
| Constraints | 20 chars | System constraints |

#### Tasks (`tasks.md`)

| Section | Min Length | Description |
|---------|-----------|-------------|
| Title | - | Tasks title |
| Task Checkboxes | - | `- [ ] N.N` format |
| Numbered Sections | - | `## N.` format |

### Validation Warnings

The validator also checks for:
- Placeholder text (TODO, FIXME, TBD, etc.)
- Minimum document length
- Empty sections

### Auto-Retry on Validation Failure

When validation fails, the system automatically retries with feedback:

```
Loop completes → Validate document
    ↓
Valid? → Clear retry state → Continue to next step
    ↓
Invalid? → Check retry count
    ↓
Can retry? → Wait for backoff → Retry with feedback
    ↓
Max retries exceeded? → Notify user of failure
```

### Retry Configuration

| Setting | Default | Description |
|---------|---------|-------------|
| `max_retries` | 3 | Maximum retry attempts |
| `backoff_base_seconds` | 5 | Initial backoff duration |
| `backoff_multiplier` | 2.0 | Exponential multiplier |

Backoff progression: 5s → 10s → 20s

### Validation Feedback

When retrying, the LLM receives detailed feedback:

```markdown
## Validation Failed

The generated document is missing required sections or content.

### Missing or Incomplete Sections

- Why: Section too short (min 50 chars, got 10)
- What Changes: What Changes section listing modifications

### Warnings

- Contains placeholder text: TODO

Please regenerate the document addressing these issues.

Attempt 2 of 3. Please ensure all required sections are present
and meet minimum content requirements.
```

## Component Architecture

### Message Flow

```
User Command (/propose)
    ↓
ProposeCommand.Execute()
    ↓
Publish to workflow.trigger.document-generation
    ↓
[SEMSTREAMS] workflow-processor
    ↓
Executes document-generation.json steps
    ├── generate_proposal → agentic-loop → proposal.md
    ├── check_auto_continue_design → conditional
    ├── generate_design → agentic-loop → design.md
    ├── ... more steps
    └── on_fail: generation_failed → user notification
    ↓
Final notification to user
```

### Key Components

| Component | Purpose |
|-----------|---------|
| `commands/` | User-facing commands (/propose, /design, etc.) |
| `workflow/` | Workflow types, prompts, and validation |
| `model/` | Capability-based model selection |
| `configs/workflows/` | Workflow JSON definitions |

**Semstreams components used:**
- `workflow-processor` — Multi-step workflow execution
- `agentic-loop` — LLM execution with tool use

### NATS Subjects

| Subject | Purpose |
|---------|---------|
| `workflow.trigger.document-generation` | Workflow trigger from commands |
| `user.response.>` | User notifications |

## Extending the System

### Adding a New Capability

1. Add to `model/capability.go`:
```go
const CapabilityCustom Capability = "custom"
```

2. Configure in `configs/semspec.json`:
```json
"custom": {
  "description": "Custom capability",
  "preferred": ["model-a"],
  "fallback": ["model-b"]
}
```

### Adding a New Document Type

1. Add to `workflow/validation/validator.go`:
```go
const DocumentTypeCustom DocumentType = "custom"

// In NewValidator():
DocumentTypeCustom: {
    {Name: "Title", Pattern: ..., MinContent: 0},
    // ... section requirements
},
```

2. Add a step in `configs/workflows/document-generation.json` that uses the new document type

### Adding a New Workflow Step

Edit `configs/workflows/document-generation.json` to add new steps. See semstreams workflow-processor documentation for the full schema. Example step:

```json
{
  "name": "generate_custom",
  "type": "agentic",
  "config": {
    "role": "custom-writer",
    "prompt_template": "custom_prompt",
    "output_file": "${trigger.payload.slug}/custom.md"
  },
  "on_fail": "generation_failed"
}
```

## Troubleshooting

### Validation Failures

Check the generated document against requirements:
```bash
cat .semspec/changes/my-feature/proposal.md
```

Verify section headers match expected patterns (case-insensitive):
- `## Why` or `## why`
- `## What Changes` or `## What Change`

### Workflow Not Progressing

1. Check workflow-processor logs:
```bash
docker logs semspec 2>&1 | grep "workflow-processor"
```

2. Check for LLM failures:
```bash
curl http://localhost:8080/message-logger/entries?limit=50 | jq '.[] | select(.type == "error")'
```

3. Verify workflow was triggered:
```bash
# Look for workflow trigger in message log
curl http://localhost:8080/message-logger/entries?limit=50 | jq '.[] | select(.subject | contains("workflow.trigger"))'
```

### Model Selection Issues

1. Check registry configuration:
```bash
cat configs/semspec.json | jq '.model_registry'
```

2. Verify endpoint availability:
```bash
# For Ollama
curl http://localhost:11434/v1/models

# For Anthropic
# Check API key is set
```

### LLM Failures

When no LLM is configured or all models fail, the workflow-processor's `generation_failed` step will notify the user. Check:

1. API keys are set (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`)
2. Ollama is running if using local models
3. Model names match configuration in `configs/semspec.json`
