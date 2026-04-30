# Diagnostic-Bundle Seed Fixtures

Captured 2026-04-29 from local-Ollama @easy runs. Each subdirectory is the
diagnostic dump for one run. ADR-034 (`docs/adr/ADR-034-watch-cli-and-diagnostic-bundles.md`)
references these as the canonical fixtures for the detector library that
will live under `pkg/health/`.

Preserved here verbatim from `/tmp/local-easy-*` before they aged out. The
huge `semspec-final.log` / `semspec-pretimeout.log` files (8.5M and 6.5M
respectively) are intentionally **dropped** from the in-repo copy — their
signal density is low relative to the structured JSON dumps already
captured. If a future detector needs raw container logs, re-run with the
same dump scripts.

## What's in each dump

Standard files that show up across runs:

| File | Source |
|---|---|
| `agent-loops-*.json` | `GET /message-logger/kv/AGENT_LOOPS` |
| `messages-*.json` | `GET /message-logger/entries?limit=N` |
| `metrics-*.txt` | `GET /metrics` (Prometheus exposition format) |
| `plan-states-*.json` | `GET /message-logger/kv/PLAN_STATES` |
| `ollama-*.txt` | `ollama ps` + `ollama show <model>` output |
| `iter-timeline.txt` | per-iteration grep'd timestamps from semspec.log |
| `last-request.json` | trailing `agent.request` body before timeout |
| `responses.json` | trailing `agent.response` payloads |
| `run.log` | playwright/e2e harness output |

## Fixture → detector shape mapping

Per ADR-034 §"Evidence to seed implementation":

### `local-easy-0734/`
**Shape: `WedgeZoneApproaching` + `OllamaCrash`.**
30b-on-Apple-Silicon wedge: Ollama crashed mid-loop after iter 5 hit
~14K prompt tokens. The `pretimeout` snapshots show context utilization
climbing past 0.5 through iter 3-5; the `final` snapshots are post-crash.
Diff `metrics-pretimeout.txt` vs `metrics-final.txt` for the
`loop_context_utilization` and `request_tokens_in` deltas the
WedgeZoneApproaching detector should match on.

### `local-easy-v105-0822/`
**Shape: `JSONInText`.**
qwen2.5-coder:14b emitting `{"name":"graph_summary"}` as text content
with `finish_reason=stop` instead of using the function-calling channel.
Just `run.log` was captured for this one — sufficient to build the
detector but the detector should also match the equivalent shape in any
fuller dump's `messages-*.json` `agent.response` entries.

### `local-easy-v106-0833/`
**Shape: `EntityIDAsPath` (cargo-cult variant).**
qwen3:14b@temp0.6 cargo-culting entity IDs as bash paths over four
iterations. Look for `agent.request` payloads where the LLM's prior
turn put a graph-style entity ID where bash expected a filesystem path.
Often paired with `JSONInText` — graph_summary results land as text and
the model treats the resulting tokens as paths.

### `local-easy-v107-0850/`
Initial run.log only. Captured before the wedge re-manifested; less
useful as a fixture but kept as a control (a partial run that didn't
fail interestingly).

### `local-easy-v107-rerun-0930/`
**Shape: `ThinkingSpiral` + `BlindRetry`.**
qwen3:14b architecture-gen exhausting 3 retries with `finish_reason=stop`
+ no `tool_calls` + `completion_tokens=915` each. The retry loop did
not inject failure context (BlindRetry), so each attempt was identical
to the previous, which is why all three exhausted with the same shape.
Detector pair: ThinkingSpiral matches the per-attempt `finish_reason +
high completion_tokens + no tool_calls` shape; BlindRetry matches the
no-feedback-injection across retries shape.

## Caveats

- Captured against semstreams beta.24 + qwen3:14b/qwen2.5-coder:14b.
  ADR-033 shipped substantial changes after these dumps; subsequent
  ladder runs will yield additional fixtures. New shapes go alongside
  these as `local-easy-<date>-<shape>/` subdirectories.
- No prompt-content redaction has been applied — these dumps are
  internal-only test data. ADR-034's `--redact-prompts` flag affects
  *exported* bundles, not the in-repo fixtures.
- `feedback_e2e_active_monitoring.md` and
  `feedback_dump_logs_before_teardown.md` describe the bash that
  produced these dumps. ADR-034's `semspec watch --bundle` will replace
  that bash with structured Go code; the bundle schema is what the
  detectors will run against.
