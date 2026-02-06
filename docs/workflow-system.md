# Workflow System

This document describes the LLM-driven workflow system in semspec, including capability-based model selection, autonomous workflow orchestration, and document validation with auto-retry.

## Overview

The workflow system enables structured document generation through a series of steps:

```
/propose → proposal.md → /design → design.md → /spec → spec.md → /tasks → tasks.md
```

Each step generates a markdown document stored in `.semspec/changes/{slug}/`.

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

## Workflow Orchestrator

The workflow orchestrator enables autonomous mode where steps chain automatically.

### Interactive vs Autonomous Mode

**Interactive Mode** (default):
```bash
/propose Add auth
# Generates proposal.md, then waits for user review
/design add-auth
# Generates design.md, then waits for user review
```

**Autonomous Mode**:
```bash
/propose Add auth --auto
# Generates all documents automatically:
# proposal.md → design.md → spec.md → tasks.md
```

### How It Works

```
1. User runs: /propose Add auth --auto
2. ProposeCommand publishes task with auto_continue=true
3. agentic-loop generates proposal.md
4. Loop completion stored in KV: COMPLETE_{loop_id}
5. workflow-orchestrator watches KV, sees completion
6. Matches rule: "auto-design-after-proposal"
7. Validates proposal.md (see Validation below)
8. If valid, publishes design-writer task
9. Process repeats for design → spec → tasks
10. Final notification sent to user
```

### Rules Configuration

Rules are defined in `configs/workflow-rules.yaml`:

```yaml
version: "1.0"

rules:
  - name: auto-design-after-proposal
    description: "Trigger design step after proposal completes in auto mode"
    condition:
      kv_bucket: "AGENT_LOOPS"
      key_pattern: "COMPLETE_*"
      match:
        role: "proposal-writer"
        status: "complete"
        metadata.auto_continue: "true"
    action:
      type: publish_task
      subject: "agent.task.workflow"
      payload:
        role: "design-writer"
        workflow_slug: "$entity.metadata.workflow_slug"
        workflow_step: "design"
        auto_continue: true
        capability: "planning"

  - name: workflow-complete-notification
    description: "Notify user when autonomous workflow completes"
    condition:
      kv_bucket: "AGENT_LOOPS"
      key_pattern: "COMPLETE_*"
      match:
        role: "tasks-writer"
        status: "complete"
    action:
      type: publish_response
      subject: "user.response.$entity.metadata.channel_type.$entity.metadata.channel_id"
      content: |
        Autonomous workflow complete!
        Generated documents for: $entity.metadata.workflow_slug
```

### Variable Substitution

Rules support variable substitution from the loop state:

| Variable | Description |
|----------|-------------|
| `$entity.loop_id` | Loop identifier |
| `$entity.role` | Role that completed |
| `$entity.status` | Completion status |
| `$entity.workflow_slug` | Workflow slug |
| `$entity.workflow_step` | Workflow step |
| `$entity.metadata.<key>` | Metadata values |

### Component Configuration

```json
{
  "workflow-orchestrator": {
    "name": "workflow-orchestrator",
    "type": "processor",
    "enabled": true,
    "config": {
      "rules_path": "configs/workflow-rules.yaml",
      "loops_bucket": "AGENT_LOOPS",
      "stream_name": "AGENT",
      "validation": {
        "enabled": true,
        "max_retries": 3,
        "backoff_base_seconds": 5,
        "backoff_multiplier": 2.0
      }
    }
  }
}
```

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

## Architecture

### Message Flow

```
User Command
    ↓
ProposeCommand.Execute()
    ↓
Publish to agent.task.workflow
    ↓
agentic-loop (proposal-writer)
    ↓
LLM generates proposal.md
    ↓
Loop completes → COMPLETE_{id} in KV
    ↓
workflow-orchestrator
    ├── Validates document
    ├── If valid: matches rule → triggers next step
    └── If invalid: retries with feedback
    ↓
(repeat for design, spec, tasks)
    ↓
Final notification to user
```

### Key Components

| Component | Purpose |
|-----------|---------|
| `model/` | Capability-based model selection |
| `workflow/validation/` | Document validation and retry |
| `processor/workflow-orchestrator/` | Autonomous workflow chaining |
| `workflow/prompts/` | Role-specific LLM prompts |
| `commands/` | User-facing commands |

### KV Buckets

| Bucket | Purpose |
|--------|---------|
| `AGENT_LOOPS` | Loop completion states |
| `AGENT_TRAJECTORIES` | Conversation history |

### JetStream Subjects

| Subject | Purpose |
|---------|---------|
| `agent.task.workflow` | Workflow task requests |
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

2. Update `stepToDocumentType()` in workflow-orchestrator

### Adding a New Workflow Rule

Add to `configs/workflow-rules.yaml`:
```yaml
- name: custom-rule
  description: "Custom workflow rule"
  condition:
    kv_bucket: "AGENT_LOOPS"
    key_pattern: "COMPLETE_*"
    match:
      role: "custom-role"
      status: "complete"
  action:
    type: publish_task
    subject: "agent.task.workflow"
    payload:
      role: "next-role"
      workflow_slug: "$entity.metadata.workflow_slug"
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

### Orchestrator Not Triggering

1. Check rules are loaded:
```bash
# Look for "Loaded workflow rules" in logs
docker logs semspec 2>&1 | grep "workflow rules"
```

2. Verify KV entry exists:
```bash
curl http://localhost:8080/message-logger/kv/AGENT_LOOPS
```

3. Check rule matching:
```bash
# Look for "Rule matched" in logs
docker logs semspec 2>&1 | grep "Rule matched"
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
