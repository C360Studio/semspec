# Model Configuration

Semspec routes tasks to different LLM models based on capability.
This guide explains how to configure models for your setup.

## Capability System

Semspec defines 5 capabilities:

| Capability   | Description                     | Default Fallback   |
| ------------ | ------------------------------- | ------------------ |
| `planning`   | Architecture, design decisions  | qwen3 → qwen       |
| `writing`    | Proposals, specs, documentation | qwen3 → qwen       |
| `coding`     | Code generation, implementation | qwen → qwen3       |
| `reviewing`  | Code review, quality analysis   | qwen3 → qwen       |
| `fast`       | Quick responses, classification | qwen3-fast → qwen  |

When a task requires a capability, semspec:

1. Tries preferred models (Claude, if API key set)
2. Falls back to local Ollama models

## Configuration Reference

### Model Registry

The model registry in `configs/semspec.json` defines capabilities, endpoints,
and defaults:

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

### Endpoint Fields

| Field        | Description                              |
| ------------ | ---------------------------------------- |
| `provider`   | `ollama` or `anthropic`                  |
| `url`        | Ollama API URL (not needed for anthropic)|
| `model`      | Actual model name to send to provider    |
| `max_tokens` | Context window size                      |

## Adding a New Model

1. Pull the model:

   ```bash
   ollama pull mistral:7b
   ```

2. Add endpoint to `configs/semspec.json`:

   ```json
   "endpoints": {
     "mistral": {
       "provider": "ollama",
       "url": "http://localhost:11434/v1",
       "model": "mistral:7b",
       "max_tokens": 32768
     }
   }
   ```

3. Add to capability fallbacks:

   ```json
   "capabilities": {
     "fast": {
       "fallback": ["mistral", "qwen3-fast"]
     }
   }
   ```

4. Add model limits for context management:

   ```json
   "model_limits": {
     "mistral": {
       "max_tokens": 32768,
       "output_tokens": 4096
     }
   }
   ```

## Recommended Setups

### Local-Only (No API Keys)

```bash
ollama pull qwen2.5-coder:14b
ollama pull qwen3:14b
ollama pull qwen3:1.7b
```

All capabilities fall back to local models automatically.

### Hybrid (Claude + Local Fallback)

Set `ANTHROPIC_API_KEY` for primary, local models for fallback:

- Primary tasks use Claude (better quality)
- Falls back to Ollama if API unavailable

### Development (Minimal Resources)

```bash
ollama pull qwen2.5-coder:7b  # 4.7GB, fits 16GB RAM
```

Use single model for all capabilities:

```json
"defaults": { "model": "qwen-small" }
```

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
ollama pull qwen2.5-coder:7b   # Instead of 14b
ollama pull qwen3:8b           # Instead of 14b
```

## Related Documentation

| Document                               | Description                   |
| -------------------------------------- | ----------------------------- |
| [Getting Started](02-getting-started.md)  | Quick setup guide             |
| [Question Routing](06-question-routing.md)| How questions route to models |
| [Components](04-components.md)            | Component configuration       |
