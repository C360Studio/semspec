# Model Configuration

Semspec routes tasks to different LLM models based on capability.
This guide explains how to configure models for your setup.

## Capability System

Semspec defines 10 capabilities. Each capability has a preferred chain (cloud) and a fallback
chain (local). When a task requires a capability, semspec tries preferred models first, then
falls back to local Ollama models.

| Capability | Description | Preferred | Fallback |
| ---------- | ----------- | --------- | -------- |
| `planning` | High-level reasoning, architecture decisions | claude-opus → claude-sonnet | qwen3 → qwen |
| `plan_review` | Strategic plan assessment, SOP compliance | claude-opus → claude-sonnet | qwen3 → qwen |
| `architecture` | Technology choices, component boundaries | claude-opus → claude-sonnet | qwen3 → qwen |
| `requirement_generation` | Requirement decomposition from plans | claude-sonnet | qwen3 → qwen |
| `scenario_generation` | BDD scenario generation from requirements | claude-sonnet | qwen3 → qwen |
| `coding` | Code generation, implementation | claude-sonnet | qwen → qwen3 |
| `reviewing` | Code review, quality analysis | claude-sonnet | qwen3 → qwen |
| `qa` | QA rollup review, integration validation | claude-sonnet | qwen3 → qwen |
| `writing` | Documentation, proposals, specifications | claude-sonnet | qwen3 → qwen |
| `fast` | Quick responses, simple tasks | claude-haiku | qwen3-fast → qwen |

## Configured Endpoints

These are the model endpoints defined in `configs/semspec.json`:

| Endpoint | Provider | Actual Model |
| -------- | -------- | ------------ |
| `claude-opus` | anthropic | claude-opus-4-6 |
| `claude-sonnet` | anthropic | claude-sonnet-4-6 |
| `claude-haiku` | anthropic | claude-haiku-4-5-20251001 |
| `qwen` | ollama | qwen3-coder:30b |
| `qwen3` | ollama | qwen3:14b |
| `qwen3-fast` | ollama | qwen3:1.7b |
| `ollama-coder` | ollama | qwen2.5-coder:7b |

## Configuration Reference

Models are configured in `configs/semspec.json` under `model_registry`:

```json
{
  "model_registry": {
    "capabilities": {
      "coding": {
        "description": "Code generation, implementation",
        "preferred": ["claude-sonnet"],
        "fallback": ["qwen", "qwen3"]
      }
    },
    "endpoints": {
      "qwen": {
        "provider": "ollama",
        "url": "${LLM_API_URL:-http://localhost:11434}/v1",
        "model": "qwen3-coder:30b",
        "max_tokens": 128000
      }
    },
    "defaults": {
      "model": "qwen"
    }
  }
}
```

### Endpoint Fields

| Field | Description |
| ----- | ----------- |
| `provider` | `ollama`, `anthropic`, or `openai` (OpenAI-compatible — works with Gemini, OpenRouter, vLLM) |
| `url` | API URL (not needed for anthropic) |
| `model` | Actual model name sent to the provider |
| `max_tokens` | Model's context window size (see below) |
| `max_concurrent` | Max concurrent requests to this endpoint. Set to 1-2 for local Ollama |
| `request_timeout` | Per-request HTTP timeout (e.g., `"300s"`, `"5m"`). Leave unset for slow local models |
| `reasoning_effort` | Thinking depth: `"low"`, `"medium"`, `"high"` (Gemini, o3, etc.) |
| `api_key_env` | Environment variable for the API key (e.g., `GEMINI_API_KEY`) |
| `supports_tools` | Whether the endpoint supports tool/function calling |

### Endpoint Name Constraints

Endpoint names (the JSON keys under `endpoints`) are used as the final segment of the
`{org}.{platform}.agent.model-registry.endpoint.{name}` entity ID written to the graph.
Dots are reserved as segment separators, so endpoint names must not contain dots.

Use dashes instead: `llama3-2`, not `llama3.2`. The `model` field inside the endpoint is
sent to the provider as-is and may contain dots (e.g. `"model": "llama3.2"`).

Startup fails fast with a clear error if any endpoint name contains a dot.

### Understanding max_tokens

`max_tokens` tells semspec how large the model's context window is. It is **not** sent to the
LLM API — it is used internally to:

- Budget system prompts (skip verbose fragments for smaller models)
- Detect context window pressure via observability metrics
- Trim tool descriptions when the model has limited context

For Ollama models, the actual context window is controlled by `num_ctx` in the model's
Modelfile, not by this config. Set `max_tokens` to match your model's effective `num_ctx`
so semspec can budget prompts correctly. For cloud providers, set it to the model's
documented context limit.

## Recommended Setups

### Local-Only (No API Keys)

```bash
ollama pull qwen3-coder:30b   # Coding tasks (default model)
ollama pull qwen3:14b          # Reasoning tasks
ollama pull qwen3:1.7b         # Fast tasks
```

All capabilities fall back to local models automatically.

### Claude (Cloud + Local Fallback)

```bash
ANTHROPIC_API_KEY=sk-ant-... docker compose up -d
```

### Gemini (Cloud + Local Fallback)

Add a Gemini endpoint to `configs/semspec.json` and set the API key:

```json
"gemini-flash": {
  "provider": "openai",
  "url": "https://generativelanguage.googleapis.com/v1beta/openai",
  "model": "gemini-2.5-flash",
  "api_key_env": "GEMINI_API_KEY",
  "max_tokens": 1000000
}
```

```bash
GEMINI_API_KEY=... docker compose up -d
```

Any OpenAI-compatible API works the same way (OpenRouter, vLLM, etc.).

### Development (Minimal Resources)

```bash
ollama pull qwen2.5-coder:7b  # 4.7GB, fits 16GB RAM
```

Use a single model for all capabilities by setting the default:

```json
"defaults": { "model": "ollama-coder" }
```

## Adding a New Model

1. Pull the model: `ollama pull <model-name>`
2. Add an endpoint to `configs/semspec.json` under `endpoints`
3. Add it to the relevant capability fallback chains
4. Restart semspec

## Troubleshooting

### Model Not Found

```text
Error: model "qwen3:14b" not found
```

Pull the model: `ollama pull qwen3:14b`

### Connection Refused

```text
Error: connection refused localhost:11434
```

Start Ollama: `ollama serve`

### Out of Memory

Reduce model size or use quantized versions:

```bash
ollama pull qwen2.5-coder:7b   # Instead of 30b
ollama pull qwen3:8b           # Instead of 14b
```

### Config Edits Don't Take Effect

The official `docker/Dockerfile` bakes `configs/semspec.json` into the image at
`/app/configs/semspec.json`. If you run the published image and edit the file on your
host, nothing changes until you either rebuild or mount the config at runtime.

Mount your host config over the baked-in one:

```yaml
services:
  semspec:
    volumes:
      - ${SEMSPEC_REPO:-.}:/workspace
      - ./configs/semspec.json:/app/configs/semspec.json:ro  # override baked config
```

Rebuild the image instead if you prefer to keep config in-image:

```bash
task local:rebuild
```

### Stale Endpoints After Renaming

Endpoint definitions are cached in the `KV_semstreams_config` KV bucket and mirrored as
entities in `KV_ENTITY_STATES`. On startup semstreams compares the `"version"` field in
`configs/semspec.json` against the version stored in KV:

- **File newer** → KV is overwritten from file (what you want).
- **Equal or KV newer** → KV wins, file edits are ignored. Look for this in logs:
  `"File version is older than KV, using KV config" hint="bump file version to update KV"`.

**Recipe for config edits:** bump `"version"` (e.g. `"1.1.0"` → `"1.1.1"`) in
`configs/semspec.json` whenever you change anything else in the file. On next boot
semstreams will push your file over the stale KV state.

If the KV also has orphan entities from an old bad endpoint name, delete them targeted
via `nats kv del` or the NATS monitor at `http://localhost:8222`. As a last resort wipe
everything:

```bash
docker compose down -v   # drops all NATS state, including KV buckets
docker compose up -d
```
