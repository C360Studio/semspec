# OpenAI `reasoning_effort + tools` Constraint

A blocker discovered while pre-flighting the gpt-5.5 endpoint that, if
not caught, would have wasted hours of token spend on every dev/reviewer
dispatch. Captured here so a future operator doesn't re-discover it on
their own paid run.

## The constraint

OpenAI's chat-completions endpoint (`/v1/chat/completions`) rejects
requests that combine **tools** with **reasoning_effort** on
`gpt-5.5`:

```
status: 400 Bad Request
message: "Function tools with reasoning_effort are not supported for
         gpt-5.5 in /v1/chat/completions. Please use /v1/responses
         instead."
```

Confirmed for `reasoning_effort=medium` AND `reasoning_effort=low`.
Reasoning models lie on `/v1/responses` (which semstreams beta.86
doesn't speak yet — see semstreams ADR-034 / ADR-037).

## Combined-constraint matrix (tested 2026-05-29 against api.openai.com)

| Model | plain | + tools | + tools + reasoning_effort | responses API |
|---|---|---|---|---|
| gpt-5.3-codex (and all `gpt-*-codex`) | ❌ "not a chat model" | n/a | n/a | ✅ |
| gpt-5.5-pro, gpt-5-pro, o1-pro | ❌ Responses-only | n/a | n/a | (likely ✅) |
| **gpt-5.5** | ✅ | ✅ (incl. `tool_choice: required` + json_schema response_format) | ❌ "use /v1/responses instead" | (likely ✅) |
| gpt-5.4, gpt-5.1 | ✅ | (presumed ✅) | (untested — assume same constraint) | (likely ✅) |

## What this means for hybrid-gpt5 config

For any "openai" capability slot that has `supports_tools: true`,
**omit `reasoning_effort`** from the endpoint until semstreams ships
Responses API support upstream. gemini-pro retains
`reasoning_effort: medium` — the constraint is OpenAI-side only.

## Pre-flight recipe before any paid e2e burn

The realistic shape semspec actually dispatches:

```bash
curl -s -X POST https://api.openai.com/v1/chat/completions \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "<m>",
    "messages": [{"role":"user","content":"call dummy"}],
    "max_completion_tokens": 50,
    "tools": [{"type":"function","function":{"name":"dummy",
              "parameters":{"type":"object","properties":{}}}}],
    "tool_choice": "required",
    "reasoning_effort": "medium"
  }'
```

If this returns `finish_reason: tool_calls`, you're safe. If it
returns a `400` with `"Function tools with reasoning_effort"`, omit
`reasoning_effort` from the endpoint config.

A plain `messages:[{"content":"reply ok"}]` call passes for nearly
any of these models AND HIDES the tool-mode failure — so the plain
pre-flight is necessary-but-not-sufficient.
