## Smoke 6 — `hybrid-gpt5 mavlink-hard` · 2026-06-02

A real-LLM end-to-end run against the OpenSensorHub MAVSDK driver
fixture (`test/e2e/fixtures/osh-driver-mavsdk`). First multi-Story
multi-Requirement plan executed since ADR-043 (the per-Story pipeline)
landed.

**Verdict: 1 of 5 requirements completed. Plan terminal-rejected at
the convergence gate.**

This pack is preserved as a progression artifact. It is deliberately
honest: it captures the run as it actually happened — the wins, the
three bugs the run surfaced, and the exact line of evidence that
motivated each fix.

---

### What the run did right

- **End-to-end planning pipeline produced real artifacts** — see
  `planning/` (BMAD: plan.md, requirements.md, architecture.md,
  scenarios.md) and `openspec/` (proposal, design, tasks, 5
  per-capability spec.md files). All filled with substantive content,
  not stubs.
- **The Sarah/Bob/Winston per-Story chain worked.** The planner (Mary)
  identified 5 capabilities. The architect (Winston) populated
  components + implementation_files + harness profile selection
  (`mavlink.px4-sitl.mavsdk-smoke`). The story-preparer (Sarah)
  authored 5 stories with file ownership. The scenario-generator
  (Bob) emitted 17 scenarios tagged with BDD tier (@unit / @integration),
  bound to the harness profile for @integration scenarios.
- **Requirement 1 ran clean and produced real code.** The MAVSDK
  server lifecycle requirement executed 15 git commits across 9
  worktree branches and produced 25 Java files (15 main, 5 test —
  see `generated-code/src/`). That code is real domain work:
  `MavsdkServerLifecycle`, `MavlinkSystemConnection`, `OgcDataStreamMapper`,
  `MavsdkActionControl`, `MavlinkHeartbeat`. Not placeholders.
- **The autonomous recovery chain (ADR-037) fired twice and worked.**
  When requirement 1's TDD pipeline wedged at task-level validation
  budget, the recovery agent emitted a `requirement_change`
  PlanDecision, the plan-decision-handler auto-accepted, and the
  executor resumed. Two complete cycles ran (`03:01:04` and
  `04:01:49` UTC). See `forensics/recovery-and-rejection-trail.log`.

### What the run surfaced (three bugs, in order of discovery)

1. **`plan.scope.create` drifted from `Story.FilesOwned`** — Sarah
   declared files like `MavsdkServerManager.java` per-Story but the
   plan's `Scope.Create` set didn't include them. The reviewer
   correctly flagged this at R2 review but the regen reroute
   couldn't fix it because the planner persona doesn't author files
   anymore (that's Sarah's job in ADR-043). Fix shipped as
   [PR #66](https://github.com/C360Studio/semspec/pull/66): plan-manager
   auto-derives `Scope.Create` as the union of `Story.FilesOwned` on
   every Sarah mutation. **Already merged.**

2. **Winston produced `integrations[i].name ≠ upstream_resolutions[j].name`** —
   Rule 7a from the architect persona's prompt requires bidirectional
   name pairing, but the rule lived only as prose and gemini-pro
   renamed one side or skipped resolutions entirely under retry
   feedback. Smoke 6 burned 3 plan-reviewer rounds on this. Fix
   shipped as [PR #67](https://github.com/C360Studio/semspec/pull/67):
   a new structural validator catches the mismatch pre-publish, plus
   prompt clarification. **Already merged.**

3. **`topoSortStoryIDs` rejected cross-requirement `Story.DependsOn`** —
   This bug only fires on multi-Requirement plans. Sarah's authoring
   was correct (story 2.1 truly needs story 1.1's output); the design
   intent was sound. But the topo-sort over one requirement's story
   slice treated every cross-req reference as "unknown story" and
   errored out. After requirement 1 completed, the orchestrator
   dispatched requirements 2/3/5 (which all referenced story 1.1 in
   their cross-req DependsOn) and they failed immediately at
   synthesis. Fix open as [PR #68](https://github.com/C360Studio/semspec/pull/68):
   silently drop unknown story refs in topo-sort — cross-req
   ordering is already enforced upstream by `Requirement.DependsOn`
   at the scenario-orchestrator. **Pending merge.**

### What ADR-043 did NOT break (verified mid-run)

The architecture pivot kept all the existing review and retry
machinery intact:

- Plan-reviewer revision cap fired at round=2 iteration=3 max=3
  (smoke 5's wall — smoke 6 cleared it thanks to fixes #66 + #67)
- Per-task TDD cycle budget exhausted naturally at task level
  (the trigger for the recovery chain)
- Recovery PlanDecisions auto-accepted twice; executor resumed twice
- Plan auto-rejected on convergence only after the recovery budget
  (`MaxRecoveryRestarts=2` in `hybrid-gpt5` config) was exhausted

See `forensics/recovery-and-rejection-trail.log` for timestamps.

### Where the time went (timeline)

```
21:45:53 CDT  smoke launched
22:08         plan reaches `implementing`, 5 reqs dispatch in parallel
22:54:46      requirement 1 begins execution (no upstream blockers)
23:01:04      recovery cycle #1 fires (TDD budget exhausted on a node)
23:01:04      auto-accept, executor resumes
23:46:30      recovery cycle #2 fires (same node, second exhaustion)
23:46:30      auto-accept, executor resumes
00:24:02      requirement 1's 3rd TDD exhaustion → recovery budget exhausted
              → terminal-failed
00:24:02      orchestrator dispatches reqs 2/3/5 (which were waiting on req 1)
00:24:02      reqs 2/3/5 fail at synthesis (cross-req Story.DependsOn bug)
00:24:02      req 4 marked blocked-by-failure (depends on req 3)
00:24:02      plan auto-rejected on convergence (1 complete / 4 failed)
00:55:22      Playwright 180min cap reached, auto-teardown
```

Wallclock total ≈ 3h 10min. Cost estimate ≈ $15–25 (gemini-flash +
gemini-pro + gpt-5.5 reviewer).

### Layout of this pack

```
docs/runs/smoke-6-mavlink-hard-2026-06-02/
├── README.md                       (this file)
├── planning/                       BMAD persona outputs
│   ├── plan.md                     Mary's goal+context+scope
│   ├── architecture.md             Winston's tech-spec + harness profile
│   ├── requirements.md             John's intent+scenarios outlines
│   └── scenarios.md                Bob's BDD scenarios with tier tags
├── openspec/                       Spec-as-code projection (ADR-040)
│   ├── proposal.md
│   ├── design.md
│   ├── tasks.md                    per-capability checklist
│   ├── .openspec.yaml
│   └── specs/{capability}/spec.md  one per capability (5 total)
├── generated-code/                 What requirement 1 actually produced
│   └── src/                        15 main + 5 test Java files
├── bug-evidence/                   Why the run rejected
│   ├── plan-final-rejected.json    full plan state at termination
│   └── reviewer-rejections.json    13 reviewer verdicts across the run
└── forensics/                      Time-series proof
    ├── heartbeats-first-60min.log  watch CLI 10s heartbeat
    ├── heartbeats-extended.log     watch CLI extended (post-60min)
    └── recovery-and-rejection-trail.log
                                    timestamped recovery cycles + rejection
```

### What this pack will be compared against

The next mavlink-hard run, captured at `docs/runs/smoke-N-...`,
should show:

- All 5 requirements reach completion (PR #68 lets reqs 2/3/5 past
  synthesis).
- More cumulative generated code (every req's worth, not just req 1).
- No reviewer-revision-cap hits at R2 (fixes #66 + #67 already
  merged, the upstream causes are gone).
- Recovery chain may or may not fire — that's load-dependent. When
  it does fire, it should converge within the budget.

Honest progression: this run is the BEFORE picture. We will not
re-run mavlink-hard until #68 merges. When we do, the comparison
will be a 1:1 swap of these directories.
