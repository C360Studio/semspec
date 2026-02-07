# Question Routing

Semspec includes a knowledge gap resolution protocol that allows agents to ask questions when they encounter uncertainty, route those questions to appropriate answerers, and integrate answers back into the workflow.

## Why This Matters

During document generation or code analysis, LLMs sometimes encounter gaps in their knowledge:

- "Does the API return pagination info in headers or body?"
- "What's the preferred error handling pattern for this team?"
- "Should this use the existing auth flow or a new one?"

Without a structured way to surface these gaps, the LLM either:
1. Guesses (potentially incorrectly)
2. Produces incomplete output
3. Asks inline, breaking the document structure

The question routing system captures these gaps as structured questions, routes them to the right answerer (another agent, a team, or a human), and blocks the workflow until answered.

## Commands

### `/ask <topic> <question>`

Ask a question that will be routed based on topic.

```bash
/ask api.semstreams "Does LoopInfo include workflow_slug?"
/ask architecture.auth "Should refresh tokens be stored in cookies or localStorage?"
/ask requirements.scope "Is mobile support in scope for v1?"
```

Topics are hierarchical (dot-separated). The routing system matches topics against patterns in `configs/answerers.yaml` to determine who should answer.

### `/questions [status|id]`

List questions or view a specific question.

```bash
/questions              # List pending questions
/questions pending      # Same as above
/questions answered     # List answered questions
/questions timeout      # List timed-out questions
/questions all          # List all questions
/questions q-abc123     # Show details for a specific question
```

### `/answer <id> <response>`

Answer a pending question.

```bash
/answer q-abc123 "Yes, LoopInfo includes workflow_slug as an optional field."
```

## Gap Detection

LLMs can signal knowledge gaps using XML blocks in their output:

```xml
<gap>
  <topic>api.semstreams</topic>
  <question>Does LoopInfo include workflow_slug?</question>
  <context>Need to know available fields for state tracking</context>
  <urgency>high</urgency>
</gap>
```

The gap parser (`workflow/gap/parser.go`) extracts these blocks and converts them to Question payloads.

### Gap Fields

| Field | Required | Description |
|-------|----------|-------------|
| `topic` | Yes | Hierarchical topic for routing (e.g., `api.semstreams`) |
| `question` | Yes | The actual question |
| `context` | No | Additional context for the answerer |
| `urgency` | No | `low`, `normal`, `high`, or `blocking` (default: `normal`) |

When gaps are detected:
1. Gap blocks are removed from the output
2. Questions are created in the QUESTIONS KV bucket
3. Questions are routed based on topic patterns
4. Workflow can optionally block until answered

## Routing Configuration

Route configuration lives in `configs/answerers.yaml`:

```yaml
version: "1"

routes:
  # API questions → semstreams team (human)
  - pattern: "api.semstreams.*"
    answerer: team/semstreams
    sla: 4h
    notify: slack://semstreams-team
    escalate_to: human/tech-lead

  # Architecture questions → architect agent (auto-answer)
  - pattern: "architecture.*"
    answerer: agent/architect
    capability: planning
    sla: 1h
    escalate_to: team/architecture

  # Security questions → security agent, escalate to team
  - pattern: "security.*"
    answerer: agent/security-reviewer
    capability: reviewing
    sla: 30m
    escalate_to: team/security
    notify: slack://security-alerts

  # Requirements → human requester
  - pattern: "requirements.*"
    answerer: human/requester
    sla: 24h
    escalate_to: team/product

  # Knowledge questions → web search tool
  - pattern: "knowledge.*"
    answerer: tool/web-search
    sla: 5m

# Default when no pattern matches
default:
  answerer: human/requester
  sla: 24h
```

### Route Fields

| Field | Description |
|-------|-------------|
| `pattern` | Glob pattern for topics (`*` = single level, `**` = multi-level) |
| `answerer` | Who handles the question (format: `type/name`) |
| `capability` | Model capability for agent answerers (`planning`, `reviewing`, etc.) |
| `sla` | Maximum time to answer before escalation (e.g., `4h`, `30m`, `24h`) |
| `escalate_to` | Next answerer if SLA exceeded |
| `notify` | Notification channel (e.g., `slack://channel-name`) |

### Pattern Matching

| Pattern | Matches |
|---------|---------|
| `api.*` | `api.auth`, `api.users`, etc. |
| `api.semstreams.*` | `api.semstreams.loop`, `api.semstreams.model` |
| `architecture.**` | `architecture.db`, `architecture.auth.refresh` |
| `*` | Any single-level topic |
| `**` | Any topic |

## Answerer Types

### `agent/` - LLM Agents

Routes to an LLM agent for automatic answering.

```yaml
answerer: agent/architect
capability: planning
```

The `question-answerer` processor picks up the task and generates an answer using the configured model capability.

### `team/` - Human Teams

Routes to a team (e.g., Slack channel, email group).

```yaml
answerer: team/security
notify: slack://security-alerts
```

Sends a notification to the team. Humans answer via `/answer` command.

### `human/` - Individual Humans

Assigns to a specific person.

```yaml
answerer: human/requester  # Special: whoever started the workflow
answerer: human/tech-lead  # Specific person
```

### `tool/` - Automated Tools

Routes to a tool for automated answering.

```yaml
answerer: tool/web-search
```

The tool executes and returns an answer automatically.

## SLA and Escalation

Each route can specify an SLA (Service Level Agreement) - the maximum time allowed before escalation.

```yaml
sla: 4h
escalate_to: human/tech-lead
```

The `question-timeout` processor monitors pending questions and:
1. Publishes timeout events when SLA is exceeded
2. Escalates to the next answerer
3. Sends notifications if configured

### Escalation Flow

```
Question created
     │
     ▼
agent/architect (SLA: 1h)
     │
     │ (timeout)
     ▼
team/architecture (SLA: 4h)
     │
     │ (timeout)
     ▼
human/tech-lead (SLA: 24h)
```

## Components

### question-answerer

Consumes question-answering tasks and generates answers using LLM agents.

**Configuration:**
```json
{
  "stream_name": "AGENT",
  "consumer_name": "question-answerer",
  "task_subject": "agent.task.question-answerer",
  "default_capability": "reviewing"
}
```

**NATS Subjects:**

| Subject | Direction | Description |
|---------|-----------|-------------|
| `agent.task.question-answerer` | Input | Question-answering tasks |
| `question.answer.<id>` | Output | Answer payloads |

### question-timeout

Monitors question SLAs and triggers escalation.

**Configuration:**
```json
{
  "check_interval": "1m",
  "default_sla": "24h",
  "answerer_config_path": "configs/answerers.yaml"
}
```

**NATS Subjects:**

| Subject | Direction | Description |
|---------|-----------|-------------|
| `question.timeout.<id>` | Output | Timeout events |
| `question.escalate.<id>` | Output | Escalation events |

## Message Flow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  QUESTION ROUTING FLOW                                                       │
│                                                                              │
│  1. GAP DETECTION                                                           │
│     LLM output with <gap> blocks                                            │
│              │                                                              │
│              │ gap/parser.go                                                │
│              ▼                                                              │
│  2. QUESTION CREATION                                                       │
│     Question stored in QUESTIONS KV bucket                                  │
│              │                                                              │
│              │ answerer/router.go                                           │
│              ▼                                                              │
│  3. ROUTING                                                                 │
│     ┌─────────────────┬─────────────────┬─────────────────┐                │
│     │  agent/*        │  team/human/*   │  tool/*         │                │
│     │                 │                 │                 │                │
│     │  Publish task   │  Update         │  Publish task   │                │
│     │  to question-   │  assignment,    │  to tool.*      │                │
│     │  answerer       │  send notify    │                 │                │
│     └────────┬────────┴────────┬────────┴────────┬────────┘                │
│              │                 │                 │                          │
│              ▼                 ▼                 ▼                          │
│  4. ANSWERING                                                               │
│     question-answerer     /answer cmd      tool executor                   │
│     (LLM generation)      (human input)    (automated)                     │
│              │                 │                 │                          │
│              └─────────────────┴─────────────────┘                          │
│                              │                                              │
│                              ▼                                              │
│  5. ANSWER PUBLICATION                                                      │
│     question.answer.<id>                                                   │
│              │                                                              │
│              ▼                                                              │
│  6. WORKFLOW RESUME                                                         │
│     Blocked loop notified, continues with answer                           │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘

TIMEOUT PATH (parallel):

question-timeout processor
         │
         │ (check_interval)
         ▼
Check pending questions against SLA
         │
         │ (SLA exceeded)
         ▼
Publish question.timeout.<id>
         │
         │ (escalate_to configured)
         ▼
Update assignment, publish question.escalate.<id>
```

## NATS Subjects

| Subject | Transport | Direction | Purpose |
|---------|-----------|-----------|---------|
| `agent.task.question-answerer` | JetStream | Internal | Tasks for agent answering |
| `question.answer.<id>` | JetStream | Output | Answer payloads |
| `question.timeout.<id>` | JetStream | Output | Timeout events |
| `question.escalate.<id>` | JetStream | Output | Escalation events |
| `notification.<proto>.<dest>` | JetStream | Output | Team/human notifications |
| `tool.task.<name>` | JetStream | Internal | Tasks for tool answering |

## Question Lifecycle

```
pending → answered    (normal flow)
pending → timeout     (SLA exceeded, no escalation)
pending → pending     (escalated, reassigned)
```

Questions are stored in the `QUESTIONS` KV bucket with a 30-day TTL.

## Related Documentation

| Document | Description |
|----------|-------------|
| [Workflow System](workflow-system.md) | How workflows orchestrate document generation |
| [Components](components.md) | Component configuration |
| [Architecture](architecture.md) | System architecture |
