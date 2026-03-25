# Model Registry for Dual DGX Spark Cluster

Proposal for running semspec on a local two-node NVIDIA DGX Spark cluster with
nemotron-3-super as the primary model and two smaller models for throughput.

## Hardware

Two networked DGX Sparks:

| Spec | Per Unit | Cluster |
|------|----------|---------|
| Memory | 128 GB unified (LPDDR5x) | 256 GB |
| Bandwidth | 273 GB/s | 273 GB/s per unit |
| GPU | GB10 Blackwell, 6,144 CUDA cores | 12,288 CUDA cores |
| Interconnect | ConnectX-7, 200 Gbps RDMA | Direct connect |
| Power | 240 W peak | 480 W peak |
| Tensor Ops | 1 PETAFLOP sparse FP4 | 2 PETAFLOPS |

Key constraint: **memory bandwidth** (273 GB/s vs ~900 GB/s on datacenter GPUs). Token
generation speed is bound by how fast weights are read — decode will be slower than an H100,
but the 128 GB capacity per unit is massive for the form factor.

## How Semspec Uses Models

Semspec is a multi-agent development tool that decomposes work into a pipeline of specialized
stages. Each stage has different computational requirements:

```
plan → requirements → scenarios → decompose → TDD pipeline → review → rollup
```

### Pipeline Stages and Their Needs

| Stage | What Happens | Model Need |
|---|---|---|
| **Planning** | Architect decomposes a goal into requirements and BDD scenarios | Deep reasoning, large context (full codebase + SOPs), quality over speed |
| **Decomposition** | Agent inspects live codebase, calls `decompose_task` to produce a TaskDAG | Reasoning + reliable tool calling, sees current file state |
| **Tester** | Write failing tests from scenario acceptance criteria | Code generation + tool calling (`bash`), moderate context |
| **Builder** | Implement code until tests pass | Fast code generation + iterative tool calling, highest token volume |
| **Validator** | Lint, type checks, structural validation | No LLM — runs deterministic shell commands from `checklist.json` |
| **Reviewer** | Adversarial code review with structured verdict | Careful reasoning, resists approval bias, accuracy over speed |
| **Requirement Review** | Review full changeset against all scenarios for a requirement | Large context (all files changed), per-scenario verdicts |
| **Routing/Classification** | Question routing, SLA decisions, task classification | Speed over quality, small token budgets |

### Why Multiple Models

Running everything on one model wastes resources in both directions:

- A 120B model doing classification burns 10x the compute needed for a yes/no decision
- A 4B model doing architectural planning hallucinates scope and misses dependencies
- The builder stage generates the most tokens (10-20 bash tool calls per task, 3+ retry
  iterations) — using the largest model here creates a throughput bottleneck

With models on separate Spark units, planning and coding run concurrently instead of queuing.

## Proposed Three-Model Registry

### Model 1: Nemotron-3-Super (Spark 1)

**Role:** Planning, decomposition, all review stages

| Spec | Value |
|------|-------|
| Parameters | 120B total / 12B active per forward pass (Mamba-Transformer MoE) |
| Context | 256K (capable of 1M, but 256K balances VRAM and practical need) |
| Quantization | Q4_K_M (native NVFP4 training — 4-bit is first-class, not afterthought) |
| VRAM (weights) | ~72 GB |
| KV cache (256K) | ~30 GB |
| Tool calling | Full OpenAI-compatible, explicitly trained for agentic workflows |
| Speculative decoding | Built-in, 2-3x speedup on structured output (no separate draft model) |

**Why nemotron for planning/review:**
- Purpose-built for agentic reasoning — 85.6% PinchBench (best open model for agents)
- 1M context validated at 91.75% RULER accuracy (even at 256K, this is robust)
- Hybrid Mamba-Transformer architecture gives O(n) context processing instead of O(n²)
- MoE means only 12B parameters active per token despite 120B total capacity
- Semspec's context-builder caps at 32K tokens anyway — 256K handles the full agent
  conversation history, tool call logs, and multi-turn retry context easily

**Why NOT nemotron for building:**
- At 273 GB/s bandwidth, reading 72 GB of weights per token generation is slow
- The builder stage is iterative: write code → run tests → read output → fix → repeat
- 10-20 tool calls per task × 3+ retry iterations = hundreds of generated tokens per task
- A 7 GB model generates tokens ~10x faster on the same hardware

### Model 2: Qwen3 8B (Spark 2)

**Role:** TDD pipeline — tester and builder stages

| Spec | Value |
|------|-------|
| Parameters | 8B dense |
| Context | 128K |
| Quantization | Q4_K_M |
| VRAM | ~7 GB |
| Tool calling | Excellent (Hermes format), reliable bash/submit_work/ask_question |
| HumanEval | 76.0% (highest among sub-8B models) |

**Why Qwen3 8B for coding:**
- Best code generation quality under 8B parameters
- 128K context is more than enough for file-level implementation tasks
- Fast decode at 7 GB — the iterative build-test-fix loop stays responsive
- Reliable tool calling means fewer wasted iterations from format errors
- Lives on Spark 2, so building runs concurrently with planning on Spark 1

### Model 3: Qwen3 4B (Spark 2)

**Role:** Fast tasks — classification, routing, question answering

| Spec | Value |
|------|-------|
| Parameters | 4B dense |
| Context | 128K |
| Quantization | Q4_K_M |
| VRAM | ~4 GB |
| Tool calling | Reliable (4B is the practical minimum for format adherence) |

**Why Qwen3 4B for routing:**
- Sub-200ms latency for classification decisions
- 4B is the floor for reliable tool calling — below this, models struggle with JSON format
- Handles `ask_question` and `submit_work` terminal signals without hallucinating parameters
- Tiny memory footprint leaves Spark 2 headroom for the coding model's KV cache

## Memory Budget

| | Spark 1 (128 GB) | Spark 2 (128 GB) |
|---|---|---|
| Nemotron-3-Super Q4 weights | ~72 GB | — |
| Nemotron KV cache (256K) | ~30 GB | — |
| Qwen3 8B Q4 weights | — | ~7 GB |
| Qwen3 4B Q4 weights | — | ~4 GB |
| KV caches (128K each) | — | ~8 GB |
| OS + inference framework | ~10 GB | ~10 GB |
| **Remaining** | **~16 GB** | **~99 GB** |

Spark 2 has enormous headroom. Options for using it:
- Upgrade coding model to **Qwen3.5 9B** (262K context, ~6 GB Q4) for better quality
- Add a dedicated review model (Llama 3.3 8B) if nemotron review throughput is insufficient
- Run an embedding model for graph search similarity

## Capability Mapping

```json
{
  "model_registry": {
    "capabilities": {
      "planning": {
        "description": "Architecture decisions, requirement decomposition, scenario generation",
        "preferred": ["nemotron"],
        "fallback": ["qwen3-8b"]
      },
      "coding": {
        "description": "Code generation, test writing, implementation",
        "preferred": ["qwen3-8b"],
        "fallback": ["nemotron"]
      },
      "reviewing": {
        "description": "Code review, requirement review, plan review",
        "preferred": ["nemotron"],
        "fallback": ["qwen3-8b"]
      },
      "writing": {
        "description": "Documentation, proposals, specifications",
        "preferred": ["nemotron"],
        "fallback": ["qwen3-8b"]
      },
      "fast": {
        "description": "Classification, routing, quick decisions",
        "preferred": ["qwen3-4b"],
        "fallback": ["qwen3-8b"]
      }
    },
    "endpoints": {
      "nemotron": {
        "provider": "ollama",
        "url": "http://spark1:11434/v1",
        "model": "nemotron-3-super:q4_k_m",
        "max_tokens": 262144,
        "supports_tools": true,
        "tool_format": "openai"
      },
      "qwen3-8b": {
        "provider": "ollama",
        "url": "http://spark2:11434/v1",
        "model": "qwen3:8b",
        "max_tokens": 131072,
        "supports_tools": true,
        "tool_format": "openai"
      },
      "qwen3-4b": {
        "provider": "ollama",
        "url": "http://spark2:11434/v1",
        "model": "qwen3:4b",
        "max_tokens": 131072,
        "supports_tools": true,
        "tool_format": "openai"
      }
    },
    "defaults": {
      "model": "qwen3-8b"
    }
  }
}
```

## Deployment Architecture

```
┌─────────────────────────────┐     ┌─────────────────────────────┐
│  Spark 1                     │     │  Spark 2                     │
│                              │     │                              │
│  Ollama (http://spark1:11434)│     │  Ollama (http://spark2:11434)│
│  ├── nemotron-3-super Q4     │     │  ├── qwen3:8b Q4             │
│  │   72 GB + 30 GB KV        │     │  │   7 GB + 4 GB KV          │
│  │                           │     │  ├── qwen3:4b Q4             │
│  │   Capabilities:           │     │  │   4 GB + 4 GB KV          │
│  │   • planning              │     │  │                           │
│  │   • reviewing             │     │  │   Capabilities:           │
│  │   • writing               │     │  │   • coding                │
│  │                           │     │  │   • fast                  │
│  └───────────────────────────│     │  └───────────────────────────│
│                              │     │                              │
│  128 GB used: ~112 GB        │     │  128 GB used: ~29 GB         │
│  Free: ~16 GB                │     │  Free: ~99 GB                │
└──────────────┬───────────────┘     └──────────────┬───────────────┘
               │                                     │
               │  ConnectX-7, 200 Gbps RDMA          │
               └─────────────┬───────────────────────┘
                             │
                    ┌────────┴────────┐
                    │  Semspec        │
                    │  (either Spark  │
                    │   or separate)  │
                    │                 │
                    │  Routes by      │
                    │  capability →   │
                    │  correct Spark  │
                    └─────────────────┘
```

Each Spark runs its own Ollama instance. Semspec routes requests to the correct endpoint
based on the capability required. No tensor parallelism needed — the models are independent.

## Context Window Tuning

Nemotron-3-super supports 1M context but we recommend 256K for this cluster:

| Context | KV Cache VRAM | Total VRAM (Q4) | Practical? |
|---------|---------------|-----------------|------------|
| 64K | ~8 GB | ~80 GB | Comfortable — 48 GB free |
| 128K | ~15 GB | ~87 GB | Good balance — 41 GB free |
| 256K | ~30 GB | ~102 GB | Recommended — 26 GB free |
| 512K | ~60 GB | ~132 GB | Exceeds single Spark |
| 1M | ~120 GB | ~192 GB | Requires both Sparks for one model |

256K is the sweet spot because:
- Semspec's context-builder budget-caps at 32K tokens for assembled context
- Agent conversation history rarely exceeds 50K tokens even in long sessions
- Leaves 16 GB headroom for concurrent requests and system overhead
- 128K is a safe fallback if memory pressure appears

To configure in Ollama, set `num_ctx` in the Modelfile or via API parameter.

## Alternatives Considered

| Option | Verdict |
|---|---|
| **Nemotron at 1M context** | Consumes ~150+ GB including KV cache. No room for concurrent inference or a second model on that Spark. |
| **Nemotron for everything** | Works but creates a throughput bottleneck. Builder iterations queue behind planning. No concurrency between Sparks. |
| **Qwen3-Coder 30B (MoE, 3B active)** | ~20 GB at Q4. Could replace Qwen3 8B on Spark 2 if code quality is insufficient. Worth benchmarking. |
| **Qwen3.5 9B** | 262K context, ~6 GB Q4. Strong candidate to replace Qwen3 8B once battle-tested. |
| **Phi-4-mini 3.8B** | Good tool calling but lower HumanEval than Qwen3 4B. Backup for `fast` capability. |
| **vLLM tensor parallelism across both Sparks** | Would let nemotron use 256 GB as a single pool. More complex setup (NCCL from source with sm_121). Worth exploring if single-Spark nemotron hits OOM. |
| **Single model with batching** | LPDDR5x bandwidth makes batching less effective than on datacenter GPUs. |

## Risks

1. **Nemotron on single Spark is tight** — 72 GB weights + 30 GB KV = 102 GB of 128 GB.
   If OOM occurs: drop to 128K context (~15 GB KV savings) or use Q3_K_M quantization
   (~55 GB weights).

2. **Decode latency** — At 273 GB/s, nemotron will generate at roughly 3-4 tokens/sec
   (vs 20+ on H100). Planning calls take 30-60 seconds. Acceptable for planning/review
   (infrequent, quality-critical) but confirms why builder should use the smaller model.

3. **Tool calling format alignment** — Nemotron uses OpenAI-compatible format, Qwen3 uses
   Hermes format. Semspec's agentic-tools component normalizes this, but test thoroughly
   with each model to confirm `bash`, `decompose_task`, and `submit_work` work correctly.

4. **Thermal sustained load** — DGX Spark runs at 80-82°C GPU under sustained decode.
   Long planning sessions (multi-requirement plans) should be monitored. Semspec's async
   pipeline naturally introduces cooling gaps between stages.

## Next Steps

1. Pull nemotron-3-super on Spark 1: `ollama pull nemotron-3-super`
2. Pull qwen3:8b and qwen3:4b on Spark 2
3. Create a `configs/semspec-spark.json` with the registry config above
4. Run `task e2e:mock` against each model independently to validate tool calling
5. Benchmark decode speed on each Spark to calibrate timeout values
6. Run a real plan end-to-end and measure total pipeline latency
