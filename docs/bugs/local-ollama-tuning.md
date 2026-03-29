# Task: Local Ollama stack tuning for 32GB RAM

**Priority**: Medium — needed before local E2E runs
**Status**: OPEN

## Current State

Installed models:
- qwen3-coder:30b (18.6GB — too big for 32GB concurrent)
- qwen3:14b (9.3GB — tool support FIXED)
- qwen2.5-coder:7b (4.7GB — tool support, primary coding model)
- qwen3:1.7b (1.4GB — fast tasks, not in local config)
- llama3.2 (2.0GB), nomic-embed-text (0.3GB), deepseek-r1:14b (9.0GB)

## Changes Needed

### 1. Add qwen3-fast endpoint to e2e-local.json
```json
"qwen3-fast": {
  "provider": "ollama",
  "url": "${LLM_API_URL:-http://host.docker.internal:11434/v1}",
  "model": "qwen3:1.7b",
  "max_tokens": 32768,
  "supports_tools": true,
  "stream": true
}
```

### 2. Add fast capability mapping
Graph-query NLQ classifier and community summarization should use 1.7b, not 7b/14b.
```json
"fast": {
  "description": "Quick classification, NLQ routing",
  "preferred": ["qwen3-fast"]
}
```

### 3. Add semembed to docker-compose.e2e-llm.yml (optional)
For semantic embeddings beyond BM25. Not strictly needed for easy scenario
since BM25 works fine for small codebases.

### 4. Concurrency concern
With 32GB RAM, concurrent model loads will OOM:
- qwen3:14b (9.3GB) + qwen2.5-coder:7b (4.7GB) = 14GB
- Add qwen3:1.7b (1.4GB) = 15.4GB
- OS + containers + NATS etc = ~4GB
- Total: ~20GB — should fit

If 14b + 7b load simultaneously it'll be tight. Ollama manages model
swapping but concurrent requests to different models will be slow.

## Files
- `configs/e2e-local.json` — model registry endpoints + capabilities
- `ui/docker-compose.e2e-llm.yml` — semembed service (optional)
