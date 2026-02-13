# SemSpec Workflow Refactor Spec

**Status**: Draft — ready for review
**Origin**: Exploration session 2025-02-12
**Scope**: Pipeline simplification, execution validation architecture, UI integration

---

## Problem Statement

SemSpec's current workflow pipeline (`/propose → /design → /spec → /tasks → /implement`) creates unnecessary friction that discourages adoption — particularly for solo developers and exploratory work. The pipeline was influenced by SpecKit and OpenSpec's spec-driven patterns, which prioritize upfront planning rigor. In practice, this front-loads ceremony where it adds marginal value while under-investing in execution-time validation where quality actually gets determined.

Empirical evidence from real usage: a simple two-role pattern (developer + reviewer) with a clear plan file consistently outperforms elaborate multi-step specification pipelines. Quality comes from the review loop, not the planning documents.

---

## Design Philosophy

**Inspired by Marine Corps Planning Process:**

- **Speed to action.** Minimize steps between intent and execution.
- **Clear intent over detailed prescription.** The plan communicates what, why, and enough how. Agents adapt at point of execution.
- **Adaptation expected at the point of execution.** You can't spec every detail because agents encounter things you didn't predict.

**Core insight:** The plan trusts intent. The execution trusts nothing.

The equivalent of the five-paragraph order: situation (what exists), mission (what and why), execution (approach), constraints (scope boundaries, what not to touch), coordination (dependencies, entity references). One document. Scales with complexity.

---

## Pipeline Changes

### Current Pipeline (Remove)

```
/propose → proposal.md → /design → design.md → /spec → spec.md → /tasks → tasks.md
```

Four documents, four generation-and-validation cycles. Proposal, design, and spec capture overlapping information at different zoom levels. For ~90% of changes, the distinctions are artificial.

### New Pipeline

```
explore? → plan → tasks → [developer → reviewer] × n
```

**Three steps to work.** Explore if you need to think. Plan when you're ready to commit. Then tasks and execution.

### Explore Phase (Optional)

Exploration is an uncommitted plan. It accumulates the same information — decisions, sketches, scope, resolved questions — but without a commitment signal. The artifact is an **exploration journal**, which is a view rendered from graph entities, not a source of truth.

Journal structure:
- **Decisions** — what was decided and why, alternatives considered
- **Resolved questions** — things investigated and answered during exploration
- **Sketch** — rough approach, pseudocode, flow descriptions (not implementation-ready)
- **Scope boundaries** — what's in, what's explicitly out
- **Entity references** — code and spec entities touched or discussed

**Promote action** (`/promote`): Converts exploration to committed plan. This is a status change plus light reframing (past-tense to future-tense), not a transformation. Decisions become requirements, sketch becomes approach, scope stays scope.

**Key principle:** Explore and plan are the same entity type. The distinction is a `committed: boolean` field. The UI renders uncommitted plans as explorations and committed ones as pipeline items.

### Plan Phase

Merges proposal + design + spec into a single document. Scales with complexity — a small fix gets three paragraphs, an architecture change gets thorough treatment. Maps to the five-paragraph order:

```markdown
# Plan: [title]

## Situation
What exists now. Current state of affected code/systems.
Entity references to existing code, specs, prior decisions.

## Mission
What we're doing and why. Clear success criteria.

## Execution
Approach and sketch. How we intend to solve it.
Architectural decisions with rationale.

## Constraints
- IN: what's in scope
- OUT: what's explicitly excluded
- DO NOT TOUCH: files/systems/patterns that must not be modified

## Coordination
Dependencies on other work. Entity references.
Any sequencing requirements.
```

**Validation lenses** replace separate document generation passes. A single plan goes through multiple validation checks:
- Does it address architecture implications?
- Is it implementable as discrete tasks?
- Does it conflict with existing specs or decisions in the graph?
- Are constraints specific enough to be mechanically checkable?

### Task Generation

Tasks are generated from the plan with **context-aware sizing**. Each task carries enough embedded context to be executed without reading the full plan (borrowed from BMAD's epic sharding, implemented better via graph).

Task sizing considers the executing model's context window. A task that would push the agent into context compaction territory gets broken smaller. Five small clean completions beat one rushed messy one.

Each task includes:
- Clear description of what to do
- Relevant context from the plan (extracted from graph, not copy-pasted)
- **Testable acceptance criteria** — not "implement token refresh" but specific conditions that can be mechanically verified
- Scope constraints inherited from the plan's constraint section
- Entity references (files to modify, files to not modify)

### Execution: Developer → Reviewer Loop

The core execution engine. Two roles, adversarial perspectives.

**Developer role:**
- Write access to files and git
- Optimizes for task completion
- Receives plan intent + task context + acceptance criteria

**Reviewer role:**
- Read-only access
- Optimizes for "would I trust this in production"
- Catches: lifecycle issues, test gaming, architectural drift, subtle quality problems
- On rejection: provides specific feedback, developer retries

This matches the proven pattern: general-purpose developer and reviewer roles that work regardless of repo or task. The plan provides task-specific context, the roles provide consistent quality standards.

---

## Execution Validation Architecture

Three layers, each handling what it's good at.

### Layer 1: SOP Checks (Automatic, Silent, Always On)

The "why we all wear the same uniform" rules. Mechanical quality gates that run after every task completion, before the next task starts. No tokens spent. The agent doesn't experience friction — it experiences feedback.

**Default SOP checks:**
- Compilation/build passes
- Existing tests still pass
- Agent stayed within files scoped by the task
- "Do not touch" constraints from plan respected
- No obvious antipatterns (deleted error handling, removed tests, new undeclared dependencies)
- Lint/format compliance

**On violation:** Task kicked back with specific feedback. Not "try again" but "you modified session.go which is out of scope per plan constraints — revert and find another approach."

**Extensible per-project:** Teams add project-specific SOPs. "All API endpoints must have error handling." "No direct database queries outside the data layer." These are graph entities — queryable, versionable.

### Layer 2: Reviewer Role (Judgment-Level Validation)

Where tokens are well spent. The reviewer catches things SOPs can't express as rules:
- Lifecycle/resource management quality
- Test quality (gaming detection — tests that test implementation not behavior)
- Architectural coherence with existing patterns
- Subtle issues requiring reasoning

The graph makes the reviewer smarter over time — it knows project conventions, past decisions, what patterns have been approved before.

### Layer 3: Runtime Risk Monitors (Dynamic, Condition-Triggered)

Continuous signals evaluated during execution. Not a pre-execution risk checklist (avoids ORM-as-checkbox problem). These watch observed behavior and escalate when conditions arise.

**Signals:**
- Agent on 3rd+ retry for same task → plan's approach may be wrong → escalate to human
- Context usage above 70% → quality about to degrade → pause and checkpoint
- Changes rippling beyond task's stated scope → flag for review
- Two tasks have conflicting changes to same file → halt and coordinate
- Task completion time significantly exceeds estimate → investigate

**Graduated responses:** Log it → warn agent → pause and ask human → halt execution. Most tasks complete without triggering any monitor. When they fire, they fire before damage is done.

**Key difference from ORM:** These are continuous and based on observed behavior, not predicted risk. You're not asking "what might go wrong" and hoping you guessed right. You're watching what's actually happening.

---

## Context Management and Task Sizing

The single biggest source of quality degradation in agent execution is context pressure. When an agent approaches its context limit, it rushes, cuts corners, skips error handling, and games tests — not because it's "lazy" but because the model's attention is being competed for by accumulated conversation history, tool results, and file contents. The system must treat context as a finite resource and manage it explicitly, not hope the agent handles it gracefully.

### Context Budget Model

Every task execution operates within a **context budget** — a calculated allocation based on the executing model's capabilities and the task's requirements.

```
Total Context Window (model-specific)
├── System Prompt + Role Config          (fixed, ~2-4K tokens)
├── Plan Context (mission/constraints)   (fixed per plan, ~1-3K tokens)
├── Task Context (assembled from graph)  (variable, estimated pre-execution)
├── Working Memory (conversation + tool results accumulate here)
│   ├── Agent reasoning
│   ├── Tool call requests
│   ├── Tool results (file contents, graph queries, etc.)
│   └── Iterative refinement
├── Output Reserve (space for final file writes)  (estimated from task scope)
└── Safety Margin (15-20% buffer)        (never allocated)
```

**Pre-execution budget calculation:**

```
available_working_memory = model_context_window
    - system_prompt_tokens
    - plan_context_tokens
    - task_context_tokens
    - estimated_output_tokens
    - safety_margin (20% of total)

if available_working_memory < minimum_working_threshold:
    → task is too large, must be decomposed further
```

The `minimum_working_threshold` is the floor below which an agent can't do useful work — enough room for at least 3-4 tool call round trips with file contents. Model-specific but roughly 8-12K tokens for most coding tasks.

### Context-Aware Task Generation

Task decomposition is not just about logical work units — it's about context-feasible work units. The task generator must account for:

**File scope estimation.** Each file the task needs to read or modify has a token cost. The graph knows file sizes (from AST indexing). If a task says "modify auth/token.go and auth/session.go and gateway/auth.go," the task generator can estimate the token cost of loading those files and compare against the budget.

**Dependency context.** If a task depends on the output of a prior task, that output becomes context. A task that follows a file-creation task needs to reference the new file — budget for it.

**Acceptance criteria complexity.** More criteria = more reasoning tokens spent on verification during execution. A task with 8 acceptance criteria burns more working memory than one with 3.

**Task sizing rules:**

1. **File count heuristic:** A task touching more than 3-4 files is probably too large for a single loop. The agent will load them all, burn context, then rush the implementation of the last files.

2. **Token budget check:** Estimated file tokens (files to read + files to modify) should not exceed 40% of available working memory. The remaining 60% is for reasoning, tool calls, and iterative refinement.

3. **Complexity scaling:** Simple tasks (add a function, fix a bug) need less working memory than complex tasks (refactor a module, implement a new pattern). The task generator should tag complexity and adjust budgets.

4. **Decomposition trigger:** When estimated context exceeds budget, the task generator splits automatically. Not arbitrarily — along logical boundaries from the plan's execution section. "Implement token refresh" becomes "implement token generation," "implement token validation," "implement refresh endpoint," "update middleware."

### Task Context Assembly

Each task carries assembled context from the graph — not the entire plan, not raw file dumps, but curated information relevant to this specific task. This is where the knowledge graph earns its keep versus BMAD's static copy-paste epic sharding.

**Context assembly per task:**

```
Task Context Package:
├── Mission Summary         (2-3 sentences from plan, not full plan)
├── Relevant Constraints    (only constraints affecting THIS task's files)
├── Acceptance Criteria     (for this task only)
├── File Summaries          (AST signatures for files to modify, not full contents)
│   └── The agent reads full files via tools — summaries orient it first
├── Prior Task Outputs      (if dependent — what was created/modified and key decisions)
├── Relevant Decisions      (from exploration/plan that affect this task's approach)
└── Convention Patterns     (from graph: "in this repo, error handling looks like X")
```

**Key principle:** Give the agent enough to orient, let it pull details via tools. Don't pre-load everything into the prompt. File summaries (function signatures, struct definitions) are cheaper than full file contents and sufficient for the agent to know what it's working with before it reads specific sections.

**Graph queries for context assembly:**

- `"what files does this task scope include?"` → file entities with sizes
- `"what conventions apply to these files?"` → pattern entities from prior approvals
- `"what constraints from the plan affect these files?"` → constraint entities linked to code entities
- `"what did the prior task produce?"` → result entity with artifacts list

### Runtime Context Monitoring

The agentic-loop already tracks iterations and has KV state in AGENT_LOOPS. Extend this with context pressure tracking:

**Metrics tracked per loop:**

| Metric | Source | Purpose |
|--------|--------|---------|
| `context_tokens_used` | agentic-model response metadata | Running total of context consumption |
| `context_utilization` | tokens_used / model_context_window | Percentage — primary pressure indicator |
| `tokens_per_iteration` | delta between iterations | Rate of context growth |
| `tool_result_tokens` | agentic-tools result sizes | Largest contributor to context bloat |
| `iterations_remaining_estimate` | budget / avg_tokens_per_iteration | Predictive: will we finish? |

**Published to:** `agent.monitor.context.{loop_id}` on each iteration

**Pressure thresholds and responses:**

| Utilization | Signal | Response |
|-------------|--------|----------|
| < 50% | Green | Normal operation |
| 50-65% | Yellow | Log warning, no action. Agent has room. |
| 65-75% | Orange | Alert: context pressure rising. If task is < 50% complete (by acceptance criteria), consider pausing and checkpointing. |
| 75-85% | Red | **Checkpoint and evaluate.** Save current progress (any files written, decisions made). Assess: can the remaining work fit? If not, pause the loop, create a continuation task from remaining acceptance criteria, start fresh loop. |
| > 85% | Critical | **Force checkpoint.** Commit whatever is done. Do NOT let the agent continue — quality collapse is imminent. Remaining work becomes a new task with clean context. |

### Context Checkpointing

When context pressure forces a mid-task pause, the system must preserve progress without losing work:

1. **Save artifacts:** Any files created or modified are committed to a checkpoint branch
2. **Extract decisions:** Parse the trajectory for decisions made during execution (approach chosen, alternatives rejected)
3. **Create continuation task:** Remaining acceptance criteria become a new task. The continuation task's context package includes:
   - Summary of what was completed (not full conversation — compressed)
   - Files created/modified (as references, not contents)
   - Decisions made (so the next loop doesn't re-evaluate)
   - Remaining acceptance criteria
4. **Fresh loop:** Continuation task starts a new loop with clean context. The agent picks up from checkpoint, not from scratch.

**This is the critical difference from what happens today.** Currently, when an agent hits context limits it either compacts (lossy, degrades quality) or rushes to finish (cuts corners). With explicit checkpointing, the system makes a clean break and restarts with full capacity. Five clean 60%-capacity loops beat one degraded 100%-capacity loop.

### Model-Specific Context Profiles

The model registry (already used for capability-based selection) should include context profiles:

```json
{
  "model": "qwen2.5-coder:32b",
  "context_window": 32768,
  "effective_context": 28000,
  "sweet_spot": 16000,
  "system_prompt_overhead": 2500,
  "recommended_max_file_tokens": 8000,
  "tokens_per_iteration_estimate": 1500
}
```

- `effective_context`: Usable tokens after accounting for model-specific overhead and quality degradation at the edges
- `sweet_spot`: The utilization level where the model performs best. Many models degrade well before hitting their theoretical limit.
- `tokens_per_iteration_estimate`: Average tokens consumed per tool-call round trip. Used for predictive monitoring.

Task generation uses `sweet_spot` as the target, not `context_window`. This builds in quality headroom by default.

### Integration with SOP and Reviewer

Context management feeds into the validation layers:

**SOP integration:** If a task was completed above 75% context utilization, the SOP check adds extra scrutiny — run tests twice, check for common pressure-induced shortcuts (missing error handling, hardcoded values, incomplete implementations).

**Reviewer integration:** The reviewer receives context utilization metadata with the task output. High-pressure completions get flagged: "This task completed at 82% context utilization — check for quality shortcuts." The reviewer knows to look harder.

**Risk monitor integration:** The runtime context monitor IS one of the risk monitors described in the execution validation architecture. It publishes escalation events to `agent.monitor.*` which the risk monitor layer consumes.

---

## Agent Architecture Philosophy: The Strategic Corporal

SemSpec's agent architecture is modeled on Marine Corps small unit leadership principles — specifically General Krulak's "Strategic Corporal" concept and the role of the Staff Non-Commissioned Officer (SNCO) in mission execution.

### The Problem These Concepts Solve

Traditional multi-agent frameworks (BMAD, SpecKit) treat agents as role-players executing a script. The analyst hands to the PM who hands to the architect who hands to the developer. It's a relay race with handoff ceremonies. When the agent on the ground encounters something unexpected — a function signature that doesn't match the spec, a dependency structured differently than assumed, a test framework that needs a different approach — it either halts and waits for re-planning or blindly follows the spec into a wall.

Real execution is the three-block war. The agent encounters humanitarian aid (straightforward code addition), peacekeeping (refactoring existing code to coexist with new code), and combat (debugging an unexpected failure) in the same task. It can't call battalion for permission to adapt. It must act within intent.

### Three Roles, Three Leadership Levels

#### The Strategic Corporal (Developer Agent)

The developer agent is the lowest-level leader, closest to the code, making decisions with outsized consequences. What makes it a *strategic* corporal rather than a loose cannon:

- **Carries commander's intent.** The plan's mission and constraints are embedded in its context — not as rigid instructions but as intent it can reason against when adapting.
- **Trained on SOPs.** Project conventions, coding patterns, error handling standards from the knowledge graph. These aren't suggestions — they're muscle memory. The agent applies them without being told, the same way a Marine wears the uniform without being ordered to each morning.
- **Operates within boundaries.** Scope constraints and "do not touch" lists are hard limits. The agent has tactical freedom within them and zero freedom outside them.
- **Empowered to adapt.** When the terrain doesn't match the map — a file is structured differently than expected, an API has changed, a dependency behaves unexpectedly — the developer agent adapts its approach while maintaining fidelity to the mission. It doesn't halt for re-planning on tactical decisions.
- **Accountable for results.** Every action is captured in the trajectory. Every decision is reviewable. Autonomy comes with traceability.

**Design implication:** The developer agent's prompt carries intent and constraints, not step-by-step instructions. It has write access to files and git. It is expected to encounter the unexpected and handle it.

#### The SNCO (Task Orchestrator / Workflow Processor)

The Staff NCO is the one who actually makes the machine work. They translate the officer's intent into actionable assignments. They know every Marine in the platoon — not just their MOS but their strengths, weaknesses, how they perform under pressure, and what tasks they should never be given.

The task orchestrator is the SNCO:

- **Knows the models.** Not just theoretical capabilities but effective performance profiles. Which model handles complex refactoring well? Which one rushes on simple tasks? Which one is reliable but slow? Where does each model degrade?
- **Tasks by capability, not just availability.** Assigns the right model to the right task based on the task's estimated complexity, file scope, context budget requirements, and the model's performance history.
- **Sizes tasks to fit.** Doesn't send a fire team on a squad-sized mission. Doesn't waste a squad on a fire team task. Task decomposition accounts for the executing model's sweet spot, not its theoretical maximum.
- **Monitors execution.** The SNCO doesn't micromanage, but they know when something is going wrong. Context pressure rising, retry count climbing, scope creeping — these are the signals that trigger intervention before failure.
- **Provides corrective feedback routing.** When the reviewer rejects work, the SNCO routes specific corrective guidance back to the developer agent. Not "try again" but "your error handling on the refresh endpoint is missing, add it following the pattern in auth/token.go."

**Design implication:** The workflow processor and task generator are the SNCO layer. They own model selection, task sizing, context budget allocation, and runtime monitoring. They are the bridge between the plan (officer's intent) and execution (corporal on the ground).

#### The Platoon Leader (Reviewer Agent)

The commissioned officer who sets the standard and makes the pass/fail judgment call. The PL doesn't dig the fighting position — they inspect it and decide whether it meets the standard. They bring a different perspective from the enlisted Marines doing the work, and that separation of concerns is the point. The PL's authority is to accept or reject, not to execute.

The reviewer agent:

- **Officer authority.** The reviewer makes the pass/fail judgment. This is a qualitatively different function from execution — not just a senior developer checking code, but a separate authority with a different objective. The developer optimizes for "task complete." The reviewer optimizes for "would I trust this in production." These are deliberately adversarial perspectives.
- **Trained eye, different vantage point.** The PL has been through TBS, IOC, and field exercises. They know what right looks like from the standards perspective, not just the execution perspective. The reviewer's context includes project conventions from the graph, patterns from previously approved work, and known failure patterns from past rejections. It gets smarter over time.
- **Inspects, doesn't execute.** Read-only access. The reviewer cannot modify code. It can only pass or fail with feedback. This enforces the separation of evaluation from generation — the same way the PL doesn't grab the E-tool and start digging.
- **Directs through the SNCO.** The PL's feedback flows through the orchestrator (SNCO), who translates it into actionable corrective guidance for the developer. "The lieutenant says your defensive position is inadequate" becomes "add concertina wire on the left side and move your LP/OP 50 meters forward." In SemSpec terms: reviewer says "error handling is insufficient on the refresh endpoint" and the orchestrator routes that back with specific context — "add error handling following the pattern in auth/token.go lines 45-60, covering expired token, invalid token, and revoked token cases."

**Design implication:** The reviewer role has constrained tool access (read-only), a validation-focused prompt, and access to graph-sourced conventions. Rejection feedback is structured, routed through the orchestrator for context enrichment, and delivered to the developer agent as actionable corrective guidance — not vague criticism.

**The SNCO-PL relationship is critical.** The orchestrator (SNCO) and reviewer (PL) work as a pair. The SNCO knows the Marines and manages execution. The PL sets standards and judges results. The SNCO translates the PL's judgment into practical guidance. Neither role works well without the other — an SNCO without a PL has no quality standard, a PL without an SNCO has no connection to the ground truth of execution.

### The T/O: Model Registry as Table of Organization

The Marine Corps Table of Organization doesn't just list billets — it defines the capabilities, equipment, and structure of a unit. The model registry serves the same function:

**Capability profile** (MOS — what they're qualified to do):
- Code generation, review, planning, structured output
- Already exists in SemSpec's capability-based selection with preferred/fallback chains

**Capacity profile** (physical fitness, load-bearing — how much they can handle):
- Context window, effective context, sweet spot
- Tokens-per-iteration rate, degradation curve
- Maximum recommended file scope per task
- Feeds directly into context-aware task sizing

**Performance profile** (service record — how well they actually perform):
- Historical pass rate from reviewer (per task type)
- Common failure patterns (what they get wrong)
- Retry frequency and causes
- Average tokens-to-completion (efficiency)
- Accumulates in the graph over time — the training flywheel

**SNCO-level task assignment** uses all three profiles:

```
Given:
  - Task: implement refresh endpoint
  - Estimated complexity: moderate
  - Files in scope: 3 (auth/token.go, auth/refresh.go, middleware/auth.go)
  - Estimated file tokens: ~4,200
  - Required capabilities: [code_generation, go]

Assignment logic (SNCO):
  1. Filter models by capability: [qwen-32b, deepseek-33b, codellama-34b]
  2. Check capacity: task needs ~12K working memory
     - qwen-32b sweet_spot: 16K ✓ (comfortable)
     - deepseek-33b sweet_spot: 12K ⚠ (tight)
     - codellama-34b sweet_spot: 10K ✗ (insufficient)
  3. Check performance history:
     - qwen-32b: 87% first-pass approval on similar tasks
     - deepseek-33b: 72% first-pass approval, tends to skip error handling
  4. Assign: qwen-32b
```

This is the difference between a green lieutenant assigning by MOS code alone ("who can do Go development?") and an experienced Staff Sergeant who knows their Marines ("who can do *this* Go task well, within budget, with high confidence?").

### Scaling the Unit

The model extends naturally:

- **Squad level** (single plan, multiple tasks): One SNCO orchestrating several fire teams (developer loops) with one PL inspecting output. This is the default SemSpec execution model.
- **Platoon level** (multiple plans, parallel execution): Multiple squads coordinating across plans, with a platoon-level deconfliction layer (the Company Commander / human) preventing file conflicts between concurrent tasks. Future multi-agent coordination.
- **Company level** (multiple projects): Multiple platoons operating on separate repositories or major features, sharing an S-2 (intelligence/knowledge graph) for cross-project awareness. Future enterprise feature.

The architecture doesn't change at each level — it scales the same pattern. More SNCOs, more fire teams, same principles: intent-driven autonomy, capability-based assignment, execution-time validation, adversarial review.

---

## Trust Boundary Model

The pipeline doesn't have two "modes." It has configurable governance based on trust context.

**High trust** (solo dev, known collaborator, feature branch):
- Explore is a conversation that feeds the graph
- Plan can be minimal
- SOP checks and reviewer still run (quality is non-negotiable)
- Risk monitors have higher thresholds before escalating
- Approvals are optional

**Low trust** (external PR, gov/compliance, production release):
- Full pipeline enforced: explore → plan → tasks → execute
- Plan requires explicit approval before task generation
- Stricter SOP rules
- Lower escalation thresholds on risk monitors
- Approval gates at plan and task-completion stages

**Configuration is per-context, not per-user.** Same person might use exploration mode on a feature branch and governed mode when preparing a release.

---

## UI Integration

Builds on the board view spec from the prior UI refactor session.

### Board View Updates

The board shows whatever's in the graph. Two card types:

**ChangeCard** (committed plans in pipeline):
- Pipeline indicator: `✓ plan → ● tasks → ○ execute`
- Agent assignment badges
- Attention items (approvals needed, questions, failures)
- This is the existing spec from the UI refactor

**ExplorationCard** (uncommitted plans):
- No pipeline indicator (no pipeline yet)
- Summary: decisions captured, entities referenced, time since last activity
- Example: `"token refresh exploration — 3 decisions, 2 code entities — 45 min ago"`
- Promote action available

### New Routes

- `/explore/[id]` — Exploration workspace: conversation + knowledge sidebar (decisions, entities, questions)
- `/changes/[slug]` — Committed change detail with pipeline view (existing spec)

### Promote Flow

From exploration workspace or exploration card: `/promote` → system generates plan from accumulated graph entities → plan appears as committed change in board view → enters governed pipeline if trust context requires it.

---

## Workflow Processor Changes

### Document Generation Workflow (Replace)

Current `document-generation.json` has steps for: generate_proposal, validate_proposal, generate_design, validate_design, generate_spec, validate_spec, generate_tasks.

Replace with `plan-and-execute.json`:

1. `generate_plan` — single document generation from intent/exploration context
2. `validate_plan` — runs multiple validation lenses (architecture, implementability, constraint specificity, graph conflicts)
3. `publish_plan_to_graph` — plan entity with relationships
4. `generate_tasks` — context-aware task decomposition with acceptance criteria
5. `validate_tasks` — tasks are properly scoped, acceptance criteria are testable, context is embedded
6. `execute_tasks` — for each task: developer generates → SOP checks → reviewer validates → next task or retry

### New Components Needed

**SOP Validator** — subscribes to task completion events, runs mechanical checks, publishes pass/fail
- Subject: `workflow.sop.validate`
- Configurable rule set per project (stored as graph entities)

**Risk Monitor** — continuous process watching agent loop state
- Subject: `agent.monitor.*`
- Reads from AGENT_LOOPS KV for context usage, retry counts
- Publishes escalation events to `user.signal.*` when thresholds crossed

**Plan Validator** — replaces separate proposal/design/spec validators
- Subject: `workflow.validate.plan`
- Runs configurable validation lenses
- Returns structured feedback, not just pass/fail

### Slash Commands (Updated)

| Command | Action |
|---------|--------|
| `/explore [topic]` | Start exploration — creates uncommitted plan entity, opens conversational workspace |
| `/plan [title]` | Create committed plan directly (skip exploration) |
| `/promote` | Convert current exploration to committed plan |
| `/tasks [slug]` | Generate tasks from plan |
| `/execute [slug]` | Begin developer→reviewer execution loop |
| `/auto [topic]` | Full auto: plan → tasks → execute (high-trust shortcut) |
| `/status` | Show current pipeline state |
| `/approve [slug]` | Approve plan or completed tasks (governed mode) |

### Migration

- Existing proposals in graph get `committed: true` and map to plan entities
- Existing design/spec documents archived but available via graph query
- Document-generation.json deprecated, plan-and-execute.json takes over
- Slash commands `/propose`, `/design`, `/spec` show deprecation message pointing to new commands

---

## Dogfooding: SemSpec Manages SemSpec

External contributions to the semspec/semstreams repos go through the governed pipeline:

1. Contributor opens issue or PR
2. SemSpec bot runs `/explore` to understand the change, asks clarifying questions
3. Bot generates plan from accumulated context
4. Plan routed to maintainer for approval (attention banner in UI)
5. On approval: tasks generated, execution begins with full SOP and reviewer validation
6. Maintainer reviews completed work, not intermediate artifacts

This protects maintainer time while ensuring contribution quality. The full pipeline isn't friction when it's protecting your time from external PRs.

---

## Import Paths

The plan entity can be populated from external sources:

- **Claude Code plan files** — `/import plan.md` parses into graph entities (decisions, scope, approach)
- **GitHub issues** — structured intent that maps to plan fields
- **Conversation exports** — exploration journals from claude.ai sessions (like this one)
- **Slack threads** — unstructured context parsed into decisions and questions

All produce the same graph entities. The plan document is a rendered view regardless of source.

---

## Success Criteria

1. **Two steps to work for solo dev:** `/plan "add token refresh"` → `/auto` and execution begins
2. **Exploration captures knowledge:** Decisions, questions, sketches accumulate in graph without ceremony
3. **Promote is cheap:** Exploration → plan requires no re-work, just commitment signal
4. **SOP catches mechanical failures:** Compilation, test regression, scope violations caught automatically without tokens
5. **Reviewer catches quality issues:** Test gaming, lifecycle problems, architectural drift caught by adversarial review
6. **Risk monitors prevent waste:** Context compaction, infinite retries, scope creep detected and escalated before damage
7. **Governed mode works for teams:** External PRs go through approval gates, plan review, full validation
8. **UI reflects graph state:** Board shows explorations and changes, no separate "modes" — just different entity states rendered appropriately

---

## What This Does NOT Change

- SemStreams infrastructure (NATS, graph storage, message routing)
- Agentic components architecture (loop, model, tools)
- Capability-based model selection
- Knowledge graph schema (additive changes only)
- Git integration patterns
- Tool executor implementations
- SSE/real-time infrastructure
