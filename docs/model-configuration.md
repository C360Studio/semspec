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
| `max_tokens` | Context window size |
| `api_key_env` | Environment variable for the API key (e.g., `GEMINI_API_KEY`) |

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
