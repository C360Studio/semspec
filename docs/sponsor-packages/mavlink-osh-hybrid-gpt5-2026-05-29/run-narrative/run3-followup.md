# Run #3 — Tuned-Budget Follow-up (Same Day)

After run #2 hit the 1h-per-requirement hard cap, four config knobs
were retuned and run #3 was fired the same session to test the
hypothesis that "the dev role is fine, the budgets were too small."

**Result: 6/8 passed, same headline as run #2. Budget tuning DID
improve internal state, but the bottleneck turned out to be
requirement granularity, not budget.**

## Config knobs applied (vs. run #2)

| Knob | Run #2 | Run #3 |
|---|---|---|
| `requirement-executor.timeout_seconds` | 3600 (1h) | 7200 (2h) |
| `requirement-executor.max_recovery_restarts` | 1 | 2 |
| `requirement-executor.recovery_timeout_seconds` | 60 | 120 |
| Playwright `EXECUTION_TIMEOUT` | 7200000 (120min) | 10800000 (180min) |

## Pre-execution cascade — faster

| Phase | Run #2 | Run #3 |
|---|---|---|
| review → reviewed | 9s | 9s |
| requirements gen | 15s | 12s |
| architecture gen | 108s | 126s |
| scenarios cascade (incl. ADR-029 retry) | 270s | 183s |
| **Total to `implementing`** | ~12 min | **~6.5 min** |

## Catalog selection — same picks (3/3 reproducibility)

Architect on run #3 again selected:

```
harness_profiles=[mavlink.px4-sitl.mavsdk-smoke, mavlink.raw-mavlink-direct]
```

Bit-identical to runs #1 and #2. The catalog-backed selection is now
proven reproducible across three runs of the same prompt on the same
config (with a config knob shuffle between #2 and #3).

Notably this run produced **2 actors + 2 integrations** in the
architecture (vs. 2 actors + 1 integration on runs #1+#2). The
architect went deeper on integration-target specificity this time.

## Verdict tally — cleaner than run #2

| Run | Total verdicts | Approved | Fixable rejected | Eventually converged | Final state |
|---|---|---|---|---|---|
| Run #2 | 13 | 8 | 5 | All | stage=rejected (timeout) |
| Run #3 | 16 | 9 | 7 | All (no terminal unrecoverable) | stage=implementing (timeout) |

Run #3 produced MORE verdicts (16 vs 13) — meaning more node-level
work got done — and reached an officially-`completed` requirement in
plan-manager (`execution_summary.completed: 1`). Run #2 never
incremented that counter (also had `completed: 0` at terminal).

## Recovery cascade observations

Run #2 fired the cascade with 1 cycle allowed (`max_recovery_restarts: 1`).
Run #3 allowed 2 — and the system used **3 plan_decisions** by the
end, suggesting more cascade activity. Specific timing of the cascade
chain on run #3 is captured in `evidence/run3/watch.log`.

## Why we still hit the wall

The three actual requirements in run #3's plan (`evidence/run3/hydrated/requirements.md`):

1. **Configure project dependencies for MAVSDK and OSH** — completed
   (reached `execution_summary.completed: 1` at ~30 min in)
2. **Document MAVSDK coverage matrix and tradeoffs** — partially done
3. **Implement MAVSDK driver, generic MAVLink fallback, and tests** —
   this is the meta-requirement bundling three major scopes into one

Requirement 3 alone is "implement the driver + a fallback + tests for
both." Each of those is a multi-cycle effort. Bundled into one
requirement with `max_tdd_cycles=5` + `max_recovery_restarts=2`, the
requirement-executor exhausts its budgets without converging.

## What this tells the sponsor

**Catalog selection is solid (3/3). Dev role works (9 approved
verdicts, 7 fixable rejections all converged). Recovery cascade
fires correctly when budgets exhaust. The bottleneck is requirement
decomposition granularity: when a single requirement bundles
"implement + fallback + tests" the budget envelope is too small
regardless of how high you tune the wallclock.**

## Recommendation for run #4 (not run yet)

Instead of tuning budgets further, **steer the requirement-generator
toward smaller chunks**. Concretely:

- Add a system-prompt constraint that each requirement should fit
  within a single ~30min TDD cycle of dev + review work.
- Or: require the planner to split any requirement that names more
  than one major deliverable ("X, Y, and tests" → 3 requirements).
- Or: cap requirements at N file-edit fragments (run #2 had 18+
  fragments per node, which is a strong overshoot signal).

Run #4 would be the experiment to validate the granularity
hypothesis. Estimated wallclock for 5-7 smaller requirements: ~120
min total, all reaching `complete`.
