# semstreams ask: `scratchpad` tool

## Summary

Add a built-in `scratchpad` tool to `processor/agentic-tools` that gives
agents a free-form one-shot reasoning channel before a strict structured
commit. Complements the existing `write_todos` tool (cross-iteration list
state) by serving single-dispatch reasoning runway where iteration
tracking is the wrong shape.

## Motivation

Mid-tier models in semspec's matrix (qwen3-MoE on OpenRouter, mid-tier
vLLM, llama-3.3-70b dense) struggle when forced to emit a complex
decomposed artifact under strict `response_format` + strict tool-args
constraints simultaneously. The reasoning quality crimp on
constrained-decode whole-response output is well-documented and matches
the empirical pattern in semspec's testing log — particularly for
"one-shot" generator dispatches (planner, requirement-generator,
scenario-generator, architecture-generator) where no per-call retry
loop absorbs reasoning failures.

The existing `write_todos` tool (beta.59) covers the multi-iteration
case beautifully: Builder TDD loops, decomposer multi-step exploration,
agents whose plan needs to survive context compaction. But its shape
(`{id, content, status}` enum, full-list-replace semantics) is mismatched
to the one-shot decomposition use case. An agent doing a single-dispatch
plan emission would have to hack write_todos with a single
`content="my draft: ..."` item that gets marked completed — that misuses
the tool's intent and adds schema overhead the model doesn't need.

A separate `scratchpad` tool, kept deliberately minimal, fits the gap:
forcing function for "think before commit" without the iteration-list
ceremony.

## Design

Minimal tool definition, intentionally narrow:

```go
const ScratchpadToolName = "scratchpad"

func (e *ScratchpadExecutor) ListTools() []agentic.ToolDefinition {
    return []agentic.ToolDefinition{{
        Name: ScratchpadToolName,
        Description: "Free-form reasoning space before committing structured output. " +
            "Use this to think through decomposition, list constraints you must satisfy, " +
            "or note edge cases you considered — content is recorded in the trajectory " +
            "but not interpreted by any consumer. Call this BEFORE the strict commit " +
            "tool when the task involves decomposition or multi-step planning.",
        Parameters: map[string]any{
            "type":                 "object",
            "required":             []string{"text"},
            "additionalProperties": false,
            "properties": map[string]any{
                "text": map[string]any{
                    "type":        "string",
                    "description": "Your reasoning. Free-form prose; no schema, no length limit, no interpretation. " +
                        "Lands in the trajectory for audit and recovery.",
                },
            },
        },
        Strict: true,
    }}
}
```

### Semantics

- **Single text argument.** No id, no status enum, no list — just prose.
- **Returns void.** Tool result is empty (or a confirmation echo). No
  side effects beyond trajectory.
- **No persistence required.** Unlike `write_todos`, scratchpad text
  does not need to land on the loop entity's triples. It is enough that
  it appears in the conversation trajectory and in the loop's tool-call
  history.
- **Per-call, not cumulative.** Each call is independent. The agent can
  call it once or many times; the framework does not coordinate them.

### Forcing function

With `ToolChoice` set to `required` on the dispatch and the agent's
persona instructing "call `scratchpad` first, then your commit tool,"
weaker models get the explicit reasoning runway they otherwise skip.

### Audit / recovery value

Scratchpad calls appear in the trajectory alongside other tool calls.
Recovery agents inspecting a wedged dispatch can see whether the model
drafted before committing, whether the draft was coherent, and whether
the final commit followed the draft or diverged from it. This is the
auditable equivalent of provider-native thinking modes — but works on
ANY provider that supports tool calling.

## Why not extend `write_todos`?

`write_todos` has a deliberately narrow shape designed for cross-iteration
task tracking: stable IDs, status enum, full-list-replace. Extending it
with a "just dump text" mode would muddy its purpose and force every
caller to either use IDs they don't need or special-case "single todo
with status=completed = scratchpad."

Two tools, two purposes, both small:

- `write_todos` — agent's internal task list across iterations
- `scratchpad` — agent's pre-commit reasoning space within a single
  dispatch

## Relation to the L1–L4 framework

(See semspec `docs/structured-output-levels.md` for context.)

The `scratchpad` tool turns L3 into something closer to L4 without
provider-native thinking modes:

- L3 alone: model can emit prose before the tool_call, but weak models
  often don't.
- L3 + `scratchpad` (forced via ToolChoice): the prose space becomes
  explicit, auditable, and tool-disciplined.
- L4 (thinking mode): provider-private scratchpad, opaque to the
  trajectory.

The `scratchpad` tool is provider-portable L4-equivalent for the cases
where provider thinking is unavailable, slow, or expensive — and where
the auditable trail is itself valuable.

## Out of scope

- Persistence across iterations (that is `write_todos`'s job).
- Multi-text-channel scratchpad (e.g. separate slots for "constraints"
  vs "plan"). Start with one channel; add structure only when single-text
  proves insufficient in real LLM testing.
- Read-back during the same dispatch (the conversation history already
  carries it as a tool-call result).

## Implementation notes (suggested)

- New file: `processor/agentic-tools/scratchpad.go`
- New test: `processor/agentic-tools/scratchpad_test.go`
- Register alongside `write_todos` in the tool registry.
- No graph mutation, no NATS publish — pure trajectory tool.
- Compatible with existing `ToolChoice` semantics so callers can opt in
  via `tool_choice = {mode: "required", name: "scratchpad"}` for the
  first call, then unlock the commit tool.

## Adoption plan in semspec (post-ship)

Once shipped:
1. Bump semstreams version.
2. Wire `scratchpad` into the available-tool palette for
   `RoleRequirementGenerator`, `RoleScenarioGenerator`,
   `RoleArchitect`, `RolePlanner`.
3. Update personas to instruct pre-commit draft.
4. Measure: generator failure rate on mid-tier providers vs current.

## Open questions

- Should the tool's `Strict` mode actually be `false`? The text field is
  unconstrained anyway, and strict mode imposes provider strictness on
  what is effectively a single-string envelope. Leaning false to keep
  the tool's strictness exactly matched to its surface area.
- Should there be a max text length? Trajectories already handle large
  payloads; probably no limit needed at the tool level.
