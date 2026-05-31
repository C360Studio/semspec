# Config Tuning Applied for Run #3

Four knobs identified from run #2 forensics, applied in the same session
**before run #3 fires**. Each change is justified by a specific
observation from the run #2 evidence.

## 1. `requirement-executor.timeout_seconds: 3600 → 7200`

**Where:** `configs/e2e-hybrid-gpt5.json`

**Observation:** Both requirements got force-killed at the 1-hour mark
(`02:42:29 reason="execution timed out after 1h0m0s"`). But
requirement 1 had actually emitted `workflow_step=requirement
outcome=success` at 02:09:43 — about 70 minutes after it started.
The 1h cap was below the natural completion time for an OSH/Java
requirement with 18-fragment node dispatches.

**Fix:** 2h/requirement matches the `@mavlink-decode` tier shape and
gives recovery cycles room to work.

## 2. `requirement-executor.max_recovery_restarts: 1 → 2`

**Where:** `configs/e2e-hybrid-gpt5.json`

**Observation:** The Playwright spec (`plan-lifecycle-llm-mavlink-osh.spec.ts:301`)
explicitly opts into `allowRecoveryCycles: 2` to match the autonomous
QA-recovery-chain shape. But the run #2 config only allowed 1 recovery
cycle. The log shows `recovery_restart=1 max_recovery_restarts=1` at
02:09:51 — meaning the system would not have permitted a second
recovery cascade even if budget allowed.

**Fix:** Align config with spec expectation. Matches `@mavlink-decode`
config (the only other tier currently using autonomous-recovery
semantics).

## 3. `requirement-executor.recovery_timeout_seconds: 60 → 120`

**Where:** `configs/e2e-hybrid-gpt5.json`

**Observation:** The recovery-agent's LLM dispatch (gemini-pro reading
trajectory + emitting a requirement_change PlanDecision) needs to
fit inside this cap. With hybrid model latency including gpt-5.5
reviewer subroutines, 60s leaves no headroom.

**Fix:** 120s gives the recovery agent two-call budget on slow
upstream responses. Cheap insurance.

## 4. mavlink-hard taskfile budgets: 120min → 180min

**Where:** `taskfiles/e2e.yml` — both `ui:test:llm` and `watch:llm`
mavlink-hard branches.

**Observation:** With requirement timeout at 2h per req and parallel
scheduling allowing up to 2 reqs concurrent, plus 2 recovery cycles
× ~15min each, realistic upper bound is ~150-180min wallclock. Run
#2's 120min Playwright timeout would still bite even after the inner
budgets are bumped.

**Fix:** `EXECUTION_TIMEOUT=10800000` and `PLAYWRIGHT_TIMEOUT=10800000`
(180 minutes).

## How to verify the changes

```bash
python3 -c "
import json
d = json.load(open('configs/e2e-hybrid-gpt5.json'))
re = d['components']['requirement-executor']['config']
assert re['timeout_seconds'] == 7200
assert re['max_recovery_restarts'] == 2
assert re['recovery_timeout_seconds'] == 120
print('config: OK')
"
grep -c 'EXECUTION_TIMEOUT=10800000' taskfiles/e2e.yml
# expected: 2 (one each for ui:test:llm and watch:llm)
```

## How to fire run #3

```bash
WITH_EPIC=1 DEBUG=1 task e2e:watch:llm -- hybrid-gpt5 mavlink-hard
```

Same incantation as run #2 — only the budgets change.
