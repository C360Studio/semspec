# ADR-025: Reactive Execution Model via Semsage Patterns
## Architecture Decision Record

**Status:** Proposed  
**Date:** 2026-03-05  
**Supercedes:** N/A  
**Depends on:** ADR-024 (Graph Architecture Refactor — Requirements → Scenarios → Tasks topology)  
**Informed by:** semsage prototype (C360Studio/semsage), OpenSage paper (Li et al., arXiv:2602.16891), Ian Blenke's SageAgent

---

## Context

SemSpec's current execution model is linear: the planner LLM generates a static structure (Requirements → Scenarios → Tasks → Phases) during a planning step, and agents execute that predetermined structure sequentially. This was a pragmatic early decision — easier to model, easier to reason about. The graph refactor (ADR-024) corrected the data topology. This ADR addresses the execution model.

A parallel prototype — semsage — was built to explore how the OpenSage paper's self-programming agent concepts translate to semstreams primitives. The prototype revealed something significant: **the semstreams runtime is already sufficient for fully reactive agent execution**. No custom workflow engine is needed. NATS subjects, JetStream subscriptions, the knowledge graph, and the governance filter chain compose into a complete reactive execution substrate.

The core insight from semsage: **agents are just another consumer of semstreams flows**. Tools are reactive definitions. Agent spawning composes flows by instantiating child loops. Task decomposition produces dependency graphs. The knowledge graph is shared state. Workflow shape is an emergent property of graph topology and agent decomposition — not a prescribed structure imposed at planning time.

This ADR defines how to migrate SemSpec's execution layer from static planning + linear execution to reactive decomposition + dynamic DAG execution, using semsage's proven patterns as the implementation blueprint.

---

## Decision

**Replace static Task generation during planning with reactive Task decomposition during execution.**

Planning produces Requirements and Scenarios only. Tasks are never generated upfront. At execution time, agents call `decompose_task` against unmet Scenarios, producing Tasks dynamically as a dependency graph (DAG). `spawn_agent` executes DAG nodes reactively. The knowledge graph records Tasks as they are created and completed, not before.

Phases become a retrospective organizational view over what actually executed — not a prescriptive execution schedule.

---

## The Problem with Static Planning

In the current model (even after ADR-001), the planning step asks an LLM to decompose Requirements into Scenarios and then into Tasks before any execution has begun. This creates several structural problems:

**Premature decomposition.** The LLM is making implementation decisions before agents have examined the codebase, discovered blockers, or encountered the actual complexity of the work. Tasks generated upfront are frequently wrong in scope, sequence, or even relevance.

**Brittle change handling.** When a PlanDecision mutates a Requirement, Tasks generated from the old Scenario become dirty. But the correct replacement Tasks still can't be known until an agent actually decomposes against the new Scenario at execution time. Static regeneration just repeats the same premature decomposition problem.

**Context window waste.** Small local models (qwen 14b class) are asked to reason about implementation detail during planning, consuming context on decisions that would be better made at the moment of execution when full codebase context is available.

**False sequential ordering.** Static task lists impose ordering that may not reflect actual dependencies. Real dependency graphs emerge from what agents discover during decomposition, not from what a planner predicts upfront.

---

## Target Execution Model

### Core Primitives (from semsage)

**`decompose_task`**  
Given an unmet Scenario, returns a DAG of subtasks as structured JSON. The calling agent decides whether to spawn DAG nodes individually or hand the DAG to an automated execution workflow. Decomposition happens at execution time against actual codebase context — not during planning against hypothetical context.

```
Input:  Scenario node (given/when/then + graph context)
Output: DAG { nodes: Task[], edges: Dependency[] }
```

The DAG is written to the graph immediately. Tasks become graph entities as they are discovered, not as they are planned.

**`spawn_agent`**  
Creates a child agentic loop for a specific Task. The parent publishes a TaskMessage to `agent.task.*`, subscribes to `agent.complete.{childLoopID}` before publishing (avoiding races), then blocks until the child completes or times out. The result is returned as a normal ToolResult.

Agent hierarchy is tracked as graph triples (`agentic.loop.spawned`) — lightweight entity references with relationship edges, queryable via existing graph infrastructure. No separate KV bucket needed.

```
Input:  Task node + agent role + context
Output: ToolResult { success, artifacts, graph_mutations }
```

**`create_tool`**  
Lets an agent compose existing semstreams processors into named reactive definitions at runtime. Validates all referenced processors exist, builds a `reactive.Definition`, registers it as a callable tool scoped to the current agent tree. This is what enables emergent agent behavior — agents reshape their own tool surface based on what they discover they need.

**`query_agent_tree`**  
Queries the agent hierarchy via the graph query infrastructure. Tree traversal, parent/child relationships, loop state. Agents use this to understand what work is in flight, what has completed, and what failed.

---

### Execution Flow

**Planning step (unchanged in scope, reduced in output):**
```
User approves Plan
  → LLM generates Requirements   (intent — the "what")
  → LLM generates Scenarios       (behavioral contracts — the "how it must behave")
  → Planning complete
```
No Tasks are generated. Phases are not generated. The plan is Requirements + Scenarios only.

**Execution step (new):**
```
Execution begins
  → Orchestrator agent queries graph for unmet Scenarios
  → For each unmet Scenario:
      → Agent calls decompose_task(scenario)
      → DAG written to graph as Task nodes + Dependency edges
      → Agent calls spawn_agent for each ready Task node (no unmet dependencies)
      → Child agents execute, write results to graph, publish agent.complete.*
      → Parent unblocks, checks DAG for newly ready nodes
      → Repeat until all Scenario Tasks complete
      → Scenario marked passing/failing based on execution results
  → When all Scenarios for a Requirement are passing:
      → Requirement marked satisfied
  → When all Requirements satisfied:
      → Plan marked complete
```

**Parallel execution is natural:**  
If an LLM response contains three `spawn_agent` calls, the existing parallel tool-call dispatch in semstreams runs all three children concurrently without additional coordination logic. Parallelism emerges from the DAG topology, not from explicit orchestration code.

**Phases as retrospective views:**  
After execution, the UI organizes completed Tasks into Phase groupings based on which Requirement they satisfied and when they ran. Phases are derived from graph query, not prescribed upfront. This preserves the Board view the UI already has without requiring static phase planning.

---

### Agent Topology: Vertical and Horizontal

Following OpenSage's taxonomy, two topologies emerge naturally from `spawn_agent`:

**Vertical topology** — complex Tasks are decomposed into sequential subtasks by specialized sub-agents. A parent decomposes a Scenario into a DAG; child agents handle individual nodes; a synthesis agent integrates results. This is the default pattern.

**Horizontal topology** — multiple agents simultaneously execute the same Task using distinct approaches, with results integrated via an ensemble mechanism. Useful for adversarial review (existing SemSpec pattern), validation, or Tasks with high uncertainty where multiple attempts should be compared.

Both topologies are expressed as different DAG shapes. The execution engine doesn't distinguish them — it just follows edges.

---

### PlanDecision Integration

PlanDecision becomes significantly more powerful under reactive execution:

When a PlanDecision is accepted:
1. Target Requirements are mutated
2. Affected Scenarios are updated or replaced
3. Any Tasks that were executing against old Scenarios receive a cancellation signal via `agent.signal.*`
4. Running child loops for those Tasks are terminated gracefully
5. The affected Scenarios re-enter the execution queue as unmet
6. `decompose_task` runs fresh against the new Scenario with current codebase context
7. New Tasks are created; execution resumes

This is change handling that actually works — not dirty flags on stale Tasks, but genuine re-decomposition against new intent with fresh context.

---

## Governance: Trust Nothing in Execution

The semsage `create_tool` capability — agents composing new tools at runtime — is powerful and dangerous. SemSpec's existing governance philosophy applies here without compromise.

**The governance filter chain intercepts every operation.** This is non-negotiable. An agent that can create new tools and spawn child agents without governance is an agent that can escape its intent constraints.

Specific governance rules for reactive execution:

**`create_tool` governance:**  
New tool definitions must be validated against the governance filter chain before registration. The filter chain checks that all referenced processors are approved, that the tool's declared scope matches the agent's role, and that the tool does not access graph nodes outside the agent's task context.

**`spawn_agent` governance:**  
Child agent roles must be declared in the parent's spawn call. The governance chain validates that the requested role is appropriate for the Task being executed. A planning agent cannot spawn an execution agent for work outside the current Scenario scope.

**`decompose_task` governance:**  
Decomposition output (the DAG) passes through the governance filter before being written to the graph. Tasks that reference resources or capabilities outside the Scenario's declared scope are rejected.

This preserves the "trust intent in planning, trust nothing in execution" philosophy. Planning (Requirements + Scenarios) is where intent is trusted. Execution is where every action is validated.

---

## Impact on Existing Components

### What changes

**Planning processor:**  
Remove Task generation from the planning LLM prompt. Planning output is Requirements + Scenarios only. The planning step becomes faster and produces better quality output because the LLM isn't making premature implementation decisions.

**Task node:**  
Tasks are now created by `decompose_task` at execution time, not by the planner. The Task schema from ADR-001 is unchanged — but the creation source changes. Tasks gain two new fields: `dag_id` (which decomposition run produced this task) and `parent_loop_id` (which agent loop owns this task).

**Phase node:**  
Phases are no longer generated during planning. Phase assignment becomes a graph query result — the UI groups Tasks by their parent Scenario's parent Requirement, ordered by completion time. The Board view remains unchanged visually.

**Execution orchestrator:**  
The current linear task executor is replaced by the reactive loop: query unmet Scenarios → decompose → spawn → complete → repeat. This is the largest implementation change.

**NATS subject additions:**
```
agent.task.{loopId}           — task assignment to agent loop (existing)
agent.complete.{loopId}       — loop completion signal (existing)
agent.signal.{loopId}         — cancellation / pause signal (new)
agent.decompose.{scenarioId}  — decomposition request (new)
agent.dag.{dagId}             — DAG ready for execution (new)
```

### What does not change

- Requirements node type (ADR-001)
- Scenario node type (ADR-001)
- PlanDecision lifecycle (ADR-001)
- Governance filter chain
- KV bucket state management
- The knowledge graph as shared agent memory
- SvelteKit UI structure (Board view, Requirement panel)
- Three-layer validation approach
- NATS JetStream as messaging backbone

---

## Risks and Mitigations

**Risk: Decomposition quality degrades for smaller models**  
`decompose_task` asks a local model to decompose a Scenario into a valid DAG. Smaller models may produce malformed DAGs or miss dependencies.  
*Mitigation:* The governance filter validates DAG structure before writing to graph. Invalid DAGs are rejected and the agent is asked to retry with a structured prompt scaffold. The scaffold includes graph context about similar completed Scenarios to guide decomposition quality.

**Risk: Infinite spawn loops**  
An agent that calls `spawn_agent` in a loop without termination conditions could exhaust resources.  
*Mitigation:* Each agent loop has a maximum depth (configurable, default: 5 levels) enforced by the governance chain. Loops that exceed depth are terminated and their parent is notified with an error ToolResult.

**Risk: DAG cycles**  
`decompose_task` could produce a DAG with cycles, causing deadlock.  
*Mitigation:* DAG validation in the governance filter includes cycle detection (topological sort). Cyclic DAGs are rejected before being written to the graph.

**Risk: Orphaned child loops**  
A parent loop that crashes before receiving `agent.complete.*` leaves child loops running with no parent.  
*Mitigation:* Child loops have a maximum TTL (configurable, default: 30 minutes). Loops without a living parent — detected via graph query on `agentic.loop.spawned` triples — are terminated by a watchdog processor.

**Risk: `create_tool` scope creep**  
Agents composing tools at runtime could gradually accumulate capabilities beyond their intended scope.  
*Mitigation:* Tools created via `create_tool` are scoped to the current agent tree and expire when the root loop completes. They are not persisted to the global tool registry without explicit human approval. The governance filter enforces tree-scoped tool visibility.

---

## Implementation Phases

### Phase 1: Core primitives (prerequisite: ADR-024 complete)
- Implement `decompose_task` tool executor
- Implement `spawn_agent` tool executor with JetStream subscription pattern
- Implement `query_agent_tree` backed by graph triples
- Add agent hierarchy tracking as graph relationship triples (`agentic.loop.spawned`)
- Add new NATS subjects (`agent.signal.*`, `agent.decompose.*`, `agent.dag.*`)
- Governance filter extensions for spawn depth and DAG validation

### Phase 2: Execution orchestrator replacement
- Replace linear task executor with reactive Scenario loop
- Implement DAG execution workflow (`workflow/dag` — already exists in semsage)
- Update planning processor to remove Task generation
- Remove Phase generation from planning step
- Implement Phase-as-retrospective-view via graph query

### Phase 3: `create_tool` and advanced topology
- Implement `create_tool` executor with tree-scoped registration
- Implement horizontal topology (ensemble execution)
- Implement PlanDecision integration with running loop cancellation
- Watchdog processor for orphaned loops

### Phase 4: Observability
- UI: live agent tree visualization (already partially in semsage UI)
- UI: DAG view per Scenario showing decomposition and execution state
- UI: loop trajectory view (already in semsage as `/api/trajectory/loops/{id}`)
- Expose loop hierarchy via existing GraphQL endpoint

---

## Definition of Done

- [ ] `decompose_task` produces valid DAGs from Scenario nodes
- [ ] `spawn_agent` blocks correctly via JetStream subscription, no polling
- [ ] Agent hierarchy queryable as graph triples
- [ ] Governance filter validates DAG structure, spawn depth, tool scope
- [ ] Planning step produces Requirements + Scenarios only (no Tasks)
- [ ] Execution orchestrator drives reactive Scenario loop
- [ ] PlanDecision acceptance cancels affected running loops and re-queues Scenarios
- [ ] Watchdog terminates orphaned loops
- [ ] Board view renders correctly from retrospective Phase grouping
- [ ] Existing alpha tests pass
- [ ] No static task generation anywhere in the planning pipeline

---

## References

- semsage prototype: https://github.com/C360Studio/semsage
- OpenSage paper: Li et al., arXiv:2602.16891, "OpenSage: Self-programming Agent Generation Engine"
- SageAgent (Python reference implementation): https://github.com/ianblenke/sageagent
- ADR-024: SemSpec Graph Architecture Refactor (Requirements → Scenarios → Tasks topology)
