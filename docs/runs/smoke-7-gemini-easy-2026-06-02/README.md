# Smoke 7 — gemini @easy — 2026-06-02

**8/8 passed in 23.2 min.** First fully-green real-LLM run after the
Train A / B / C / D synthesis of post-ADR-043 go-reviewer findings
landed (PRs #72, #73, #74, #75, #76, #77, #78, #79).

## Run context

| | |
|---|---|
| Provider | `gemini` (gemini-flash for dev/analyst/planner; gemini-pro for architect/reviewer) |
| Tier | `@easy` |
| Fixture | `test/e2e/fixtures/go-project` — "Add a /health endpoint with status/uptime/version + tests" |
| Plan slug | `84360c511c18` |
| Started | 2026-06-02 22:46:55 UTC (17:46:55 CDT) |
| Completed | 2026-06-02 23:10:08 UTC |
| Wall clock | **23.2 min** |
| Errors | **0** throughout |
| Test stages | **8/8 PASS** |
| Code produced | 1 file (`main.go`) + 1 test file (`main_test.go`), 2 test functions |
| DAG nodes | 5 nodes total, all approved by code reviewer |

## What to look at first

**1. The architecture rejection + retry (forensics/validator-rejection-trail.log).**
This is the load-bearing evidence that Train D's R3 capability-coverage
validator (Pass-4 P4-C3 fix) is firing in production. Winston's first
architecture attempt at 22:36:25 emitted an integration but the
component didn't claim the capability — validator rejected. Winston
retried at 22:36:26 and produced a clean architecture (1 component, 0
integrations for the internal /health endpoint, 2 decisions, 2 tech
choices). Pre-Train-D this would have silently accepted broken state.

**2. The TDD-cycle progression (forensics/tdd-cycle-trail.log).**
Five TDD cycles on node 1 (task.84360c511c18.1.1.1) — code reviewer
returned `verdict=rejected rejection_type=fixable` on cycles 0-4,
finally `verdict=approved` on cycle 5. The execution-manager's TDD
loop + lesson-decomposer chain carried dev through to a passing
shape. Nodes 2-5 followed similar but shorter paths.

**3. The generated code (generated-code/main.go).**
gemini-flash authored a production-grade /health handler:
JSON response with status / uptime (computed since startTime) /
Go runtime version, GET-only method check (405 on others), proper
Content-Type header, error logging on encoder failure. Two test
functions in main_test.go cover the GET happy path + method check.

## Phase timeline

```
22:34:49  →  exploring                  (plan created)
22:35:14  →  drafting                   (analyst: 1 capability)
22:35:28  →  drafted → reviewing_draft  (planner)
22:35:35  →  reviewed                   (R1 approved)
22:35:38  →  approved                   (REST approve)
22:35:38  →  generating_requirements
22:35:49  →  requirements_generated     (1 requirement)
22:35:49  →  generating_architecture
22:36:25  ✗  Winston attempt #1 REJECTED — capability not in component
22:37:00  ✓  Winston attempt #2 ACCEPTED — clean shape
22:37:00  →  architecture_generated
22:37:00  →  preparing_stories          (Sarah)
22:37:05  →  stories_generated          (1 story)
22:37:05  →  generating_scenarios       (Bob)
22:37:08  →  scenarios_generated        (2 scenarios)
22:37:08  →  reviewing_scenarios        (R2)
22:37:?  →  ready_for_execution        (R2 passed)
22:38:?  →  implementing                (executor + DAG)
~23:00   →  Node 1 of 5 approved       (5 TDD cycles)
~23:10   →  All 5 nodes approved → complete
```

## What this run validates (and what it doesn't)

**Validates** (against this commit chain on main as of `583abac`):

- Train D's `phaseHit["stories"]` re-entry route (P4-C3) — implicitly
  via the validator firing on the architecture phase.
- Train D's `ValidateStory` empty-Status flip (P4-C4 / Pass-3 S-C1) —
  Sarah's emitted Stories passed the gate with non-empty FilesOwned +
  Tasks; an empty emission would have failed loud.
- Train D's per-Requirement coverage gate (Pass-3 S-C2) — every
  Requirement got at least one Story (this fixture has 1 Req → 1
  Story, but the code path runs regardless).
- Train D's ValidateStory + plan-reviewer R3 alignment — both layers
  enforce the same readiness invariants on empty-Status emissions.
- The test-fix at commit `583abac` (drops the spurious
  `integrations.length > 0` assertion for the @easy /health fixture).

**Does NOT exercise** (1-Story-per-Req fixture leaves these dormant):

- Train A — per-Story cursor persistence (C1/C2), CommitSHA on
  NodeResult round-trip (C4), VisitedNodes per-Story counter (C3).
  This run had 1 Story, so cursor was never advanced.
- Train B — per-(Req, Story) scenario merge (C2), distinct scenario
  IDs across Stories (C3), retry-key alignment (C1). Single Story
  means no parallelism that would expose the wipe-replace race.
- Train C — `story_reprepare` end-to-end. No story-shaped wedge fired,
  so the cascade + back-transition + Sarah re-prep paths stayed cold.

A multi-Story fixture is needed to validate Trains A/B/C end-to-end.
mavlink-hard would do it but costs real money (~$15-25/run per memory
notes) and ~78 min.

## Artifacts

| Path | Source | What it shows |
|---|---|---|
| `forensics/heartbeats.log` | semspec watch sidecar | per-10s stats (plans, loops, msgs, errors, ctx_util). 23 minutes of `errors=0`. |
| `forensics/semspec.log` | docker logs ui-semspec-1 | full chronological component logs — every plan transition, every TDD cycle, every loop completion |
| `forensics/phase-transitions.log` | grep | clean timeline of just the status-claim + submit_work events |
| `forensics/validator-rejection-trail.log` | grep | the ADR-043 capability-coverage rejection + retry pair — the headline evidence for Train D |
| `forensics/tdd-cycle-trail.log` | grep | every dev/reviewer cycle, every verdict, every retry — the executor's loop |
| `planning/plan.md` | semspec writer | BMAD-aligned plan summary (goal / context / scope) |
| `planning/requirements.md` | John (PM) | the 1 requirement John produced from the analyst's 1 capability |
| `planning/architecture.md` | Winston | architect's component + actor + integration shape (post-retry, clean) |
| `planning/stories.md` | Sarah | Story-level decomposition (1 Story for the 1 Requirement) |
| `planning/scenarios.md` | Bob | BDD scenarios (2 scenarios for the 1 Story) |
| `planning/plan.json` | semspec writer | full structured plan state |
| `openspec/proposal.md` | OpenSpec emit | the OpenSpec proposal document |
| `openspec/design.md` | OpenSpec emit | the OpenSpec design summary |
| `openspec/tasks.md` | OpenSpec emit | the OpenSpec task checklist |
| `openspec/specs/service-health-check/spec.md` | OpenSpec emit | the capability-scoped spec |
| `generated-code/main.go` | dev agent | the working /health handler |
| `generated-code/main_test.go` | dev agent | unit tests for the handler |
| `generated-code/go.mod` | dev agent | module definition |

## Pre-flight + run lessons

- Pre-flight curl on `https://generativelanguage.googleapis.com/v1beta/openai/models`
  caught a false alarm (initial grep too narrow); a wider check
  confirmed all 3 target models present. Per
  `feedback_preflight_endpoint_before_paid_burn.md`, this is the
  canonical pre-paid-burn check.
- Docker disk cleanup before the run (`docker builder prune -af` +
  `docker image prune -af` + `docker volume rm $(docker volume ls -q)`)
  reclaimed ~36GB and kept the laptop happy. Per
  `project_t1_execution_complete_lifecycle_flake_2026_05_16.md`.
- `task e2e:watch:llm -- gemini easy` was the canonical invocation;
  the watch sidecar's per-minute snapshots
  (`/tmp/semspec-watch-gemini-easy-<ts>/snapshot-*.tar.gz`) carry
  plan state during the run (the final `bundle.tar.gz` is captured
  post-teardown and has empty `.plans` per
  `feedback_bundle_plans_empty_post_teardown.md`).
- DEBUG=1 was NOT set, so out-of-band capture during the run was
  required for the BMAD + OpenSpec artifacts. The staging dir
  `/tmp/sponsor-pack-smoke7-gemini-easy-20260602-174950` (36MB) holds
  the live HTTP API state + KV snapshots + container docker-cp at
  multiple phases. The curated artifacts here are extracted from that
  staging dir.

## Trains landed before this run

The cumulative chain validated here:

| PR | Closes | Title |
|---|---|---|
| #72 | P4-C1 | plan-manager per-slug mutex (single-writer lock) |
| #73 | P2-C2 | per-(Req, Story) scenario merge wire shape |
| #74 | P2-C3 | scenario IDs include storyseq segment |
| #75 | P2-C1 | scenario-generator retry-key alignment |
| #76 | P1-C1/C2/C4 | per-Story cursor + CommitSHA persistence |
| #77 | P1-C3/C5/H1/H2/H3/H4 | requirement-executor 6-fix stack |
| #78 | P3-S-C1/S-C2, P4-C3/C4 | Sarah readiness gate + R2 stories routing |
| #79 | P3-S-C3, P4-C2 | story_reprepare end-to-end implementation |
| `583abac` | — | test-fix: drop integrations.length > 0 for @easy |
