# Workflow System

This document describes the LLM-driven workflow system in semspec, including capability-based model selection, the plan-and-execute adversarial loop, and specialized processing components.

## Overview

Semspec uses two complementary patterns for LLM-driven processing:

1. **Components** - Single-shot processors that call LLM, parse structured output, and persist to files
2. **Workflows** - Multi-step orchestration for coordinating multiple agents

See [Architecture: Components vs Workflows](architecture.md#components-vs-workflows) for when to use each pattern.

## Current Workflow: Plan and Execute (ADR-003)

The `plan-and-execute` workflow implements an adversarial developer/reviewer loop:

```
┌─────────────┐     ┌─────────────┐     ┌─────────────────────┐
│  developer  │────▶│  reviewer   │────▶│  verdict_check      │
│  (implement)│     │  (evaluate) │     │                     │
└─────────────┘     └─────────────┘     └──────────┬──────────┘
                                                   │
                    ┌──────────────────────────────┼──────────────────┐
                    │                              │                  │
                    ▼                              ▼                  ▼
            ┌───────────────┐            ┌───────────────┐    ┌───────────────┐
            │   approved    │            │   fixable     │    │  misscoped/   │
            │   → complete  │            │   → retry     │    │  too_big      │
            └───────────────┘            └───────────────┘    │  → escalate   │
                                                              └───────────────┘
```

**Workflow definition**: `configs/workflows/plan-and-execute.json`

**Key features:**
- Adversarial loop: developer implements, reviewer evaluates
- Conditional routing based on verdict (approved, fixable, misscoped, too_big)
- Retry with feedback for fixable issues (max 3 iterations)
- Escalation to user for architectural or scoping issues
- Built-in failure handling via `on_fail` steps

## Specialized Processing Components

For single-shot LLM operations that require structured output parsing, semspec uses dedicated components instead of workflow steps. This is because agentic-loop returns raw text and cannot parse structured JSON responses.

| Component | Trigger | Processing | Output |
|-----------|---------|------------|--------|
| `planner` | `/plan <title>` | LLM → Goal/Context/Scope | `plan.json` |
| `explorer` | `/plan <topic>` (explore mode) | LLM → Goal/Context/Questions | `plan.json` |
| `task-generator` | `/execute <slug>` (auto) | LLM → BDD tasks | `tasks.json` |

Each component:
1. Subscribes to `workflow.trigger.<name>` subject
2. Calls LLM with domain-specific prompts
3. Parses JSON from markdown-wrapped responses
4. Validates required fields
5. Saves to filesystem via `workflow.Manager`
6. Publishes completion to `workflow.result.<name>.<slug>`

See [Components](04-components.md) for detailed documentation of each component.

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
| planner | planning |
| developer | coding |
| reviewer | reviewing |
| task-generator | planning |

### Usage

```bash
# Default (uses role's default capability)
/plan Add user authentication
# → planning capability → claude-opus

# Direct model override (power user)
/plan Add auth --model qwen
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

## Planning Workflow

The planning workflow uses specialized components for LLM-assisted content generation.

### Interactive Mode (default)

```bash
/plan Add auth
# Creates plan stub, triggers planner component
# LLM generates Goal/Context/Scope and saves to plan.json

/explore authentication options
# Creates exploration stub, triggers explorer component
# LLM generates Goal/Context/Questions for refinement
```

### Manual Mode

```bash
/plan Add auth -m
# Creates plan stub only, no LLM processing
# User manually edits plan.json

/explore authentication options -m
# Creates exploration record only, no LLM processing
```

### How It Works

```
1. User runs: /plan Add auth
2. PlanCommand creates plan stub in .semspec/plans/<slug>/plan.json
3. PlanCommand publishes to workflow.trigger.planner
4. Planner component receives message:
   a. Calls LLM with planner prompt
   b. Parses JSON response (Goal/Context/Scope)
   c. Merges with existing plan
   d. Saves updated plan.json
   e. Publishes completion to workflow.result.planner.<slug>
5. User can then run /tasks <slug> --generate to create tasks
```

### Workflow Configuration

The plan-and-execute workflow is defined in `configs/workflows/plan-and-execute.json`. This JSON file uses semstreams' workflow-processor schema with:

- **Steps**: Developer and reviewer agent steps
- **Conditional routing**: Verdict-based routing (approved, fixable, misscoped, too_big)
- **Retry logic**: Up to 3 iterations for fixable issues
- **Failure handling**: `on_fail` steps for error notification
- **Variable interpolation**: `${trigger.payload.*}` for dynamic values

## Document Validation

Generated documents are validated before proceeding to the next step.

### Document Type Requirements

#### Plan (`plan.json`)

| Field | Required | Description |
|-------|----------|-------------|
| `goal` | yes | What to achieve |
| `context` | yes | Relevant background |
| `scope` | yes | Boundaries of the change |

#### Tasks (`tasks.json`)

| Field | Required | Description |
|-------|----------|-------------|
| BDD scenarios | yes | GIVEN/WHEN/THEN format |
| Acceptance criteria | yes | Per-task acceptance criteria |

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

### Planning Message Flow

```
User Command (/plan "Add auth")
    ↓
PlanCommand.Execute()
    ├── Creates plan stub in .semspec/plans/<slug>/plan.json
    └── Publishes to workflow.trigger.planner
    ↓
[SEMSPEC] planner component
    ├── Calls LLM with planner prompt
    ├── Parses JSON response (Goal/Context/Scope)
    ├── Saves to plan.json
    └── Publishes to workflow.result.planner.<slug>
    ↓
User notified of completion
```

### Execution Message Flow (Plan-and-Execute)

```
User Command (/execute <slug>)
    ↓
ExecuteCommand.Execute()
    └── Publishes to workflow.trigger.plan-and-execute
    ↓
[SEMSTREAMS] workflow-processor
    ↓
Executes plan-and-execute.json steps
    ├── developer → agentic-loop → implementation
    ├── reviewer → agentic-loop → evaluation
    ├── verdict_check → conditional routing
    ├── retry_developer (if fixable)
    └── escalate (if misscoped/too_big)
    ↓
Task completion or escalation to user
```

### Key Components

| Component | Purpose |
|-----------|---------|
| `commands/` | User-facing commands (/plan, /approve, /execute, etc.) |
| `processor/planner/` | LLM-based Goal/Context/Scope generation |
| `processor/explorer/` | LLM-based exploration with questions |
| `processor/task-generator/` | LLM-based task generation |
| `workflow/` | Workflow types, prompts, and validation |
| `model/` | Capability-based model selection |
| `configs/workflows/` | Workflow JSON definitions |

**Semstreams components used:**
- `workflow-processor` — Multi-step workflow execution (plan-and-execute)
- `agentic-loop` — Generic LLM execution with tool use

**Semspec components (specialized processors):**
- `planner` — Parses LLM output into plan.json
- `explorer` — Parses LLM output into exploration records
- `task-generator` — Parses LLM output into tasks.json

### NATS Subjects

| Subject | Purpose |
|---------|---------|
| `workflow.trigger.planner` | Triggers planner component |
| `workflow.trigger.explorer` | Triggers explorer component |
| `workflow.trigger.tasks` | Triggers task-generator component |
| `workflow.trigger.plan-and-execute` | Triggers plan-and-execute workflow |
| `workflow.result.planner.<slug>` | Planner completion notification |
| `workflow.result.explorer.<slug>` | Explorer completion notification |
| `workflow.result.tasks.<slug>` | Task-generator completion notification |
| `user.response.>` | User notifications |
| `user.signal.escalate` | Escalation events from workflow |

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

### Adding a New Processing Component

For single-shot LLM operations that need structured output parsing, create a new component following the planner pattern:

1. Create component directory:
```
processor/<name>/
├── component.go   # Main processing logic
├── config.go      # Configuration
└── factory.go     # Registration
```

2. Implement the processing pattern:
```go
// 1. Subscribe to trigger subject
consumer, _ := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
    Durable:       "my-component",
    FilterSubject: "workflow.trigger.my-component",
})

// 2. Call LLM with domain-specific prompt
func (c *Component) generate(ctx context.Context, trigger *WorkflowTriggerPayload) (*MyContent, error) {
    // Build prompt, call LLM, parse JSON response
}

// 3. Save to filesystem
func (c *Component) save(ctx context.Context, content *MyContent) error {
    // Use workflow.Manager or direct file I/O
}

// 4. Publish completion
c.natsClient.Publish(ctx, "workflow.result.my-component."+slug, result)
```

3. Register in `cmd/semspec/main.go`:
```go
mycomponent.Register(registry)
```

4. Add config to `configs/semspec.json`

See `processor/planner/` as the canonical reference implementation.

### Adding a New Workflow Step

Edit `configs/workflows/plan-and-execute.json` to add new steps. See semstreams workflow-processor documentation for the full schema.

For steps that need specialized output processing, have the workflow trigger a component:

```json
{
  "name": "generate_custom",
  "action": {
    "type": "publish",
    "subject": "workflow.trigger.my-component",
    "payload": {
      "slug": "${trigger.payload.slug}",
      "title": "${trigger.payload.title}"
    }
  },
  "on_success": "next_step",
  "on_fail": "error_handler"
}
```

## Troubleshooting

### Component Not Processing

1. Check component logs:
```bash
docker logs semspec 2>&1 | grep "planner\|explorer\|task-generator"
```

2. Verify trigger was published:
```bash
curl http://localhost:8080/message-logger/entries?limit=50 | jq '.[] | select(.subject | contains("workflow.trigger"))'
```

3. Check for processing errors:
```bash
curl http://localhost:8080/message-logger/entries?limit=50 | jq '.[] | select(.type == "error")'
```

### Plan Not Updated

If `/plan` runs but `plan.json` doesn't have Goal/Context/Scope:

1. Check planner component is registered:
```bash
docker logs semspec 2>&1 | grep "planner started"
```

2. Check LLM response parsing:
```bash
# Look for JSON parsing errors
docker logs semspec 2>&1 | grep "parse plan"
```

3. Verify plan.json exists:
```bash
cat .semspec/plans/<slug>/plan.json
```

### Workflow Not Progressing

1. Check workflow-processor logs:
```bash
docker logs semspec 2>&1 | grep "workflow-processor"
```

2. Check for agent failures:
```bash
curl http://localhost:8080/message-logger/entries?limit=50 | jq '.[] | select(.subject | contains("agent.failed"))'
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

When no LLM is configured or all models fail:

1. API keys are set (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`)
2. Ollama is running if using local models
3. Model names match configuration in `configs/semspec.json`
4. Check component-specific error messages in logs
