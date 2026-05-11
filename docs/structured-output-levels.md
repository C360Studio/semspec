# Structural Enforcement Levels for LLM Agent Output

How much structural discipline does the wire ask of a model, and where does
the reasoning happen? This document is the anchor for that decision across
dispatch sites in semspec.

The framework names five positions (L1–L4 plus an external Pipeline) along
the autonomy-vs-structure axis. It is the reference for choosing a
configuration per role and for deciding which lever to pull when a
particular model class struggles.

## The levels

| Level | Name | Reasoning lives in | Structure enforced where | Provider availability |
|-------|------|--------------------|---------------------------|------------------------|
| **L1** | Free emission | Entire response, unconstrained | Nowhere — downstream parser or reformatter must extract | Universal |
| **L2** | Schema-constrained whole response | Every output token, masked against the schema during decode | The entire response wire | OpenAI proper, OpenRouter, vLLM, most Ollama models. NOT Anthropic; NOT Gemini-OpenAI-compat (silently ignored) |
| **L3** | Tool-use with reasoning pre-text | Pre-tool prose, autoregressive, no constraints | Tool-call args only (strict schema on args) | Universal — any provider with tool-calling |
| **L4** | Thinking-mode + tool-use | Private hidden thinking trace + visible pre-tool prose | Tool-call args only | Anthropic extended thinking, Gemini thinking, qwen3 `enable_thinking`, Nemotron `reasoning_effort`, OpenAI o-series |
| **Pipeline** | External reformatter (4-step) | Builder L1 + reformatter LLM | After-the-fact via a sub-frontier reformatter component | Architectural — provider-independent |

## Trade-offs

| Level | Reasoning quality | Latency overhead | Token cost | Operational complexity |
|-------|-------------------|------------------|------------|------------------------|
| **L1** | Maximum | None inherent; parser-retry tax when malformed | Smallest output | Lowest at dispatch; highest at parse |
| **L2** | Measurably crimped on reasoning benchmarks; worst on decomposition / planning | None | Smallest | Lowest |
| **L3** | Near-maximum (reasoning text unconstrained) | None inherent | +10–30% output (pre-tool prose) | Low |
| **L4** | Maximum — reasoning channel isolated from output | +1–2s typical; provider-dependent | +2–5× output tokens (thinking traces are billed on many providers) | Low; provider-gated |
| **Pipeline** | Maximum on Builder (L1) | +1 full LLM round-trip per cycle | +1 LLM call cost per cycle, plus retries | Highest — new component, new failure modes, new debug surface |

## Empirical evidence (testing log)

| Level | Evidence |
|-------|----------|
| **L1** | OpenRouter @easy take 22 fast-failed on llama-3.3-70b without tool discipline; take 23 hit 6/8 with cascade gap; parser-retry tax was real |
| **L2** | Generators currently run L3+L2 combined; mid-tier failures concentrate at L2-honoring providers (OpenRouter qwen3-MoE, vLLM mid-tier) |
| **L3** | Gemini @hard take 5 first-ever 9/9 PASS — Gemini-OpenAI-compat falls back to L3-only because response_format is silently ignored |
| **L4** | OpenRouter @easy take 28 — qwen3.6-27b 8/8 in 17.8m only worked with `enable_thinking=true` + 600s timeout. Nemotron 49B `thinking_off` (take 36) plan-phase clean but exec-phase same wall |
| **Pipeline** | Not yet shipped — theorized for the generator path |

## Role-to-level map (current intent)

| Role | Recommended level | Rationale |
|------|-------------------|-----------|
| **Builder (developer)** | L3 + `write_todos` + L4 where provider supports it | Has TDD-cycle retry depth, code-reviewer, req-reviewer, recovery race-closure all absorbing failures. Pipeline overhead unjustified. |
| **Decomposer** | L3 + `write_todos` | Multi-step exploration before DAG commit — `write_todos` natively fits cross-iteration tracking |
| **Generators** (planner / req-gen / scen-gen / arch-gen) | L3 + scratchpad tool (semstreams ask); drop response_format strict | One-shot dispatches, no per-call retry loop, cognitive load is decomposition not coding. Pipeline becomes the next-step escalation if L3+scratchpad insufficient |
| **Reviewers** (code-rev / req-rev / qa-rev) | L3 + L2 acceptable | Output is small structured verdict + feedback; L2 cost is low and the structure guarantee is valuable here |
| **Plan-reviewer** | L3 only | Reasoning-heavy; verdict structure better enforced via tool-args than whole-response schema |

## Key insights

1. **The L1→L2 transition is where autonomy is lost hardest.** L2→L3
   recovers most of it; L3→L4 captures the rest. The Pipeline is the L1
   maximum-autonomy bet with architectural cost.
2. **Provider matters more than level name.** Gemini ignoring
   `response_format` means gemini @hard is effectively L3-only regardless
   of config — and that is where the only @hard pass to date came from.
   L2's "crimp" is contingent on the provider actually honoring
   constrained decoding.
3. **Thinking mode (L4) is the cheap escape hatch when available.** No new
   component, just config. The qwen3.6-27b 8/8 result is the single
   strongest data point that structural load can be offloaded without
   architecture changes.
4. **Tool-args strict (L3) is the structure boundary that survives.** It
   is where the schema actually binds without crimping reasoning. Tool
   definitions are where the structural discipline lives in an L3 world.
5. **Cognitive load is highest where retry depth is shallowest.** Builder
   has 7 TDD cycles × 2 req retries + recovery. Generators have 1 shot.
   The Generator path is where structural absorption (Pipeline OR
   scratchpad) earns its keep most clearly.

## Pragmatic sequence (Path 1)

Shipping order — each step gated on signal from the previous:

1. **Now**:
   - Per-component `attach_response_format` flag on generators (planner,
     requirement-generator, scenario-generator, architecture-generator).
     Default `nil` preserves existing behavior; explicit `false` drops L2
     to L3-only. Wired 2026-05-12.
   - Disabled in `configs/e2e-openrouter.json` so the L2 drop A/B fires
     where the constraint actually bites (Gemini ignores `response_format`
     so `e2e-gemini.json` is no-op either way).
   - Drafted semstreams ask for `scratchpad` tool (see
     `docs/asks/semstreams-scratchpad.md`).

2. **When semstreams beta.59+ scratchpad lands**:
   - Bump semstreams.
   - Wire `scratchpad` on generator dispatches; persona instructions force
     pre-commit draft.
   - Optionally wire `write_todos` on Builder + decomposer.

3. **Measure** (real-LLM runs):
   - Does generator failure rate drop on mid-tier providers?
   - Does Builder TDD efficiency improve with `write_todos`?

4. **If generators still choke after L3 + scratchpad**:
   - Build the external reformatter (Step 2 of the original 4-step
     Compiler Pipeline) targeted at generators specifically. Builder
     pipeline remains unshipped — Builder retry depth absorbs cognitive
     load better than a reformatter would.

## Config knobs

| Knob | Component(s) | Effect |
|------|--------------|--------|
| `attach_response_format` (bool, default true) | planner, requirement-generator, scenario-generator, architecture-generator | Gates L2 wire attach + matching `HasResponseFormat` prompt hint. `false` → L3-only on supporting providers |
| `enable_thinking` (bool, in `endpoint.options`) | All providers that support it | Enables L4 — provider sends a private thinking trace |
| `reasoning_effort` (string, in `endpoint.options`) | OpenAI o-series, some OpenRouter routes | L4 with a tier dial (`none`, `low`, `medium`, `high`) |

## Related code

- `tools/terminal/response_format.go` — `EndpointSupportsResponseFormatGated`,
  `ResponseFormatForEndpointGated`
- `processor/planner/config.go` — `AttachResponseFormat` field
- `processor/requirement-generator/config.go` — same
- `processor/scenario-generator/config.go` — same
- `processor/architecture-generator/config.go` — same
- `configs/e2e-openrouter.json` — opted-out for the 4 generators
- ADR-035 strict-parse discipline (related: when L1 or L2 produces
  unparseable output, no silent compensation downstream)
