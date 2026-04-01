# ADR-027: Always-On Agent Teams & Institutional Knowledge Infrastructure

**Status:** Superseded
**Superseded by:** Role-scoped lessons learned system (2026-03-31)
**Date:** 2026-03-28
**Authors:** Coby, Claude
**Depends on:** ADR-025 (Reactive Execution Model), ADR-026 (Auto-Cascade)
**Context:** Agent teams, peer review, and knowledge feedback are 80% built but never wired end-to-end. Teams are off by default behind a triple gate nobody sets.

> **Supersession note:** This ADR was never implemented. The agent team model
> (named agents, benching, Q1/Q2/Q3 scoring, red team challenges) was replaced
> by a simpler role-scoped lessons learned system. Five roles (planner,
> plan-reviewer, developer, reviewer, architect) replaced individual agent
> identities. Error categories and role-scoped lessons replaced team knowledge
> and agent scoring. ~4400 lines of agent/team/review infrastructure were
> deleted from `agentgraph`. See ADR-030 for how personas now attach to roles
> via config rather than agent entities.

---

## Problem Statement

Semspec's key differentiator is a **persistent knowledge graph that eliminates context loss**. Agent teams with named identities, peer reviews, and shared lessons are the human-facing expression of this — agents that learn from their mistakes and share knowledge with teammates.

The infrastructure exists but is dead:

1. **ErrorTrends never populated.** `TaskContext.ErrorTrends` is defined (`prompt/context.go:78`). `GetAgentErrorTrends()` exists and is tested (`agentgraph/graph.go:1316`). The prompt fragment renders it beautifully (`prompt/domain/software.go:1162`). But `buildAssemblyContext()` (`execution-manager/component.go:1632`) never calls `GetAgentErrorTrends()`. Agents never see their recurring error patterns.

2. **Teams are triple-gated off by default.** `teamsEnabled()` (`component.go:492`) requires `Teams != nil && Teams.Enabled && len(Roster) >= 2`. The default config has no `teams` section. Every installation runs in "solo mode" — no persistent agent identity, no team knowledge, no competitive dynamics.

3. **Error category guidance not injected on retries.** When a builder is retried after rejection, `checkAgentBenching()` (`component.go:1261`) classifies the feedback into error categories and increments counts, but the category's prescriptive `Guidance` text from `configs/error_categories.json` is never surfaced in the retry prompt. The agent only sees raw reviewer feedback.

4. **Insights only from rejections.** `extractTeamInsights()` (`team_knowledge.go:53`) creates lessons only when `feedback != ""`, which only happens on rejection. Approved work with high ratings — equally valuable as a positive signal — is discarded.

5. **No review history visibility.** Reviews are written via `RecordReview()` but there is no read path — no `ListReviews()`, no HTTP endpoints. The data accumulates invisibly.

6. **Insight eviction is FIFO.** `maxTeamInsights = 50` with oldest-first eviction. Universally valuable lessons get dropped while recent task-specific noise accumulates.

### Impact

Today, every agent starts every task with zero institutional memory. The reviewer's feedback from the previous task is lost. The team's accumulated lessons are invisible. Error patterns repeat across agents because nobody tells them. This is the opposite of semspec's promise.

---

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Default teams mode | Always-on when agentHelper available | Teams are a core feature, not opt-in. Kill switch (`Teams.Enabled: false`) remains for debugging |
| Default roster | Auto-generate "alpha" + "bravo" from configured model | Meaningful blue/red dynamics out of the box without explicit config |
| Error trend threshold | 0 (surface from first occurrence) | In an always-on system, even a single error is a signal worth surfacing |
| Retry guidance | Append matched category guidance to feedback | Most direct path — `exec.Feedback` already flows into the prompt |
| Approval insights | Extract positive patterns from approved work | Balanced learning — agents should know what works, not just what fails |
| Insight eviction | Usage-count-based, not FIFO | Insights that are never relevant to any prompt should be dropped first |
| Kill switch | `Teams.Enabled: false` explicitly disables | Escape hatch for debugging; nil/missing config means teams ON |

---

## Current Architecture (As-Built)

### Entity Model

```
Team (workflow/team.go)
├── ID, Name, Status (active/benched/retired)
├── MemberIDs []string
├── SharedKnowledge []TeamInsight
├── TeamStats (Q1/Q2/Q3/Overall averages)
├── RedTeamStats (critique quality metrics)
└── ErrorCounts map[ErrorCategory]int

Agent (workflow/agent.go)
├── ID, Name, Role, Model
├── Status (available/busy/benched/retired)
├── ErrorCounts map[ErrorCategory]int
└── ReviewStats (Q1/Q2/Q3/Overall averages)

TeamInsight (workflow/team.go)
├── ID, Source, ScenarioID
├── Summary (max 200 chars)
├── CategoryIDs []string
├── Skill (tester/builder/reviewer/red-team)
└── CreatedAt
```

### Selection Algorithms

**Agent selection** (`agentgraph/graph.go:534` — `SelectAgent`):
- Filter by role + `AgentAvailable` status
- Sort: lowest `TotalErrorCount` → highest `OverallAvg` (tie-break)
- Auto-create if no available agents and `nextModel` provided

**Blue team** (`SelectBlueTeam`): lowest errors → highest stats
**Red team** (`SelectRedTeam`): highest `RedTeamStats.OverallAvg` → lowest errors (best critics first)

### Benching

- **Agent**: any single error category reaches threshold (default: 3) → `AgentBenched`
- **Team**: majority (>= len/2+1) of members benched → `TeamBenched`
- Benched entities excluded from selection

### Error Categories (7 defined)

| ID | Label | Signals | Guidance |
|----|-------|---------|----------|
| `missing_tests` | Missing Tests | "No test file created...", "Acceptance criteria not covered..." | Create test files alongside implementation |
| `wrong_pattern` | Wrong Pattern | "Using shared memory where channels...", "Bypassing DI..." | Review ADRs, follow nearest existing file |
| `sop_violation` | SOP Violation | "SOP rule explicitly referenced...", "Required checklist item skipped..." | SOPs are non-negotiable constraints |
| `incomplete_implementation` | Incomplete Implementation | "TODO or placeholder left...", "Function stub..." | All acceptance criteria must be fully addressed |
| `edge_case_missed` | Edge Case Missed | "No nil or zero-value guard...", "Empty collection..." | Ask: what happens when nil, empty, zero? |
| `api_contract_mismatch` | API Contract Mismatch | "Function signature differs...", "JSON field names..." | Cross-reference against API contract |
| `scope_violation` | Scope Violation | "Files modified outside scope...", "Unrelated refactoring..." | Only modify files in task scope |

Categories are defined in `configs/error_categories.json`, loaded into `workflow.ErrorCategoryRegistry`, and matched via case-insensitive substring scanning of reviewer feedback.

### Prompt Assembly Pipeline

```
Graph (ENTITY_STATES)
  → agentgraph.GetAgentErrorTrends()     ← NEVER CALLED (bug)
  → agentgraph.GetTeam().FilterInsights()
  → prompt.AssemblyContext { TaskContext.ErrorTrends, TeamKnowledge }
  → Fragment conditions check len() > 0
  → ContentFunc renders into system prompt
  → Formatted per provider (XML for Anthropic, Markdown for OpenAI/Ollama)
```

### Prompt Fragments (already built, waiting for data)

**Error trends** (`software.go:1162`):
```
RECURRING ISSUES — Your recent reviews flagged these patterns. You MUST address ALL of the following:

- Missing Tests (2 occurrences): Every acceptance criterion must have a corresponding test assertion.
```

**Team knowledge** (`software.go:1182`):
```
TEAM KNOWLEDGE — Lessons from previous tasks:

- [AVOID][builder] Test file not created alongside auth middleware.
- [NOTE][builder] Channel-based worker pool pattern approved 5/5.
```

**Team context** (`team_knowledge.go:16`):
```
TEAM CONTEXT

You are a member of Team alpha. All teams are working toward the shared goal...
The team with the highest combined score earns the coveted Team Trophy.
```

### The Dead Wiring (What's Broken)

| Component | Status | Gap |
|-----------|--------|-----|
| `teamsEnabled()` | Triple-gated OFF | `Teams != nil && Enabled && len(Roster) >= 2` — nobody sets this |
| `seedTeams()` | Guards on `teamsEnabled()` | Never runs in default config |
| `buildAssemblyContext()` ErrorTrends | Field exists, never populated | Missing call to `GetAgentErrorTrends()` |
| `buildAssemblyContext()` TeamKnowledge | Guards on `teamsEnabled() && BlueTeamID != ""` | Dead because teams never seed |
| `checkAgentBenching()` | Returns `bool` only | Matched category IDs discarded — not threaded to retry prompt |
| `extractTeamInsights()` | Rejection-only | `feedback != ""` gate ignores approvals |
| Review read path | Write-only | `RecordReview()` exists, no `ListReviews()` |
| Insight eviction | FIFO at 50 | No relevance signal |

---

## Target Architecture

### Phase 1: Wire the Dead Spots

#### 1.1 Populate ErrorTrends

In `buildAssemblyContext()` (`execution-manager/component.go:1632`), after TaskContext creation (line 1652), when `exec.AgentID != ""`:

```go
if exec.AgentID != "" && c.agentHelper != nil && c.errorCategories != nil {
    trends, err := c.agentHelper.GetAgentErrorTrendsWithThreshold(
        ctx, exec.AgentID, c.errorCategories, 0)
    if err == nil {
        for _, t := range trends {
            asmCtx.TaskContext.ErrorTrends = append(asmCtx.TaskContext.ErrorTrends,
                prompt.ErrorTrend{
                    CategoryID: t.Category.ID,
                    Label:      t.Category.Label,
                    Guidance:   t.Category.Guidance,
                    Count:      t.Count,
                })
        }
    }
}
```

Uses threshold 0 (not `DefaultErrorTrendThreshold = 1`) so first-occurrence errors surface immediately. The `GetAgentErrorTrendsWithThreshold` variant already exists for this.

**Files**: `processor/execution-manager/component.go`
**Reuses**: `agentgraph.Helper.GetAgentErrorTrendsWithThreshold()`, `prompt.ErrorTrend`

#### 1.2 Inject Error Category Guidance on Retries

Modify `checkAgentBenching()` to return matched category IDs:

```go
func (c *Component) checkAgentBenching(ctx context.Context, exec *taskExecution, feedback string) (bool, []string)
```

In `routeFixableRejection()`, use the returned category IDs to append remediation guidance:

```go
func (c *Component) routeFixableRejection(ctx context.Context, exec *taskExecution, feedback string, agentBenched bool, matchedCategories []string) {
    // ... existing agent replacement logic ...

    enriched := c.enrichFeedbackWithGuidance(feedback, matchedCategories)

    if exec.Iteration+1 < exec.MaxIterations {
        if c.feedbackNeedsTestRetry(enriched) {
            c.startTesterRetryLocked(ctx, exec, enriched)
        } else {
            c.startBuilderRetryLocked(ctx, exec, enriched)
        }
    }
}

func (c *Component) enrichFeedbackWithGuidance(feedback string, categoryIDs []string) string {
    if len(categoryIDs) == 0 || c.errorCategories == nil {
        return feedback
    }
    var sb strings.Builder
    sb.WriteString(feedback)
    sb.WriteString("\n\n--- REMEDIATION GUIDANCE ---\n")
    for _, id := range categoryIDs {
        if cat := c.errorCategories.Get(id); cat != nil {
            fmt.Fprintf(&sb, "- %s: %s\n", cat.Label, cat.Guidance)
        }
    }
    return sb.String()
}
```

**Files**: `processor/execution-manager/component.go`
**Reuses**: `workflow.ErrorCategoryRegistry.Get()`

### Phase 2: Make Teams Always-On

#### 2.1 Invert teamsEnabled Gate

```go
// Before (opt-in):
func (c *Component) teamsEnabled() bool {
    return c.config.Teams != nil && c.config.Teams.Enabled && len(c.config.Teams.Roster) >= 2
}

// After (opt-out):
func (c *Component) teamsEnabled() bool {
    if c.config.Teams != nil && !c.config.Teams.Enabled {
        return false // explicit kill switch
    }
    return c.agentHelper != nil
}
```

Applied to both `execution-manager/component.go:492` and `requirement-executor/component.go:1514`.

#### 2.2 Default Roster Generation

Add `defaultRoster()` to `execution-manager/config.go`:

```go
func defaultRoster(model string) []TeamRosterEntry {
    return []TeamRosterEntry{
        {Name: "alpha", Members: []TeamMemberEntry{
            {Role: "tester", Model: model},
            {Role: "builder", Model: model},
            {Role: "reviewer", Model: model},
        }},
        {Name: "bravo", Members: []TeamMemberEntry{
            {Role: "tester", Model: model},
            {Role: "builder", Model: model},
            {Role: "reviewer", Model: model},
        }},
    }
}
```

In `seedTeams()`, when no roster is configured:

```go
func (c *Component) seedTeams() {
    if !c.teamsEnabled() {
        return
    }
    if c.agentHelper == nil {
        return
    }

    roster := c.config.Teams.Roster
    if c.config.Teams == nil || len(roster) == 0 {
        roster = defaultRoster(c.config.Model)
    }
    // ... existing seeding logic using roster ...
}
```

#### 2.3 Validation Updates

Remove the "must have 2 teams" validation error from both:
- `execution-manager/config.go:170` — `Validate()`
- `requirement-executor/config.go:125` — `Validate()`

Default generation handles the minimum roster requirement. Validation only needs to check that *explicitly provided* rosters have valid entries.

**Files**: `processor/execution-manager/component.go`, `processor/execution-manager/config.go`, `processor/requirement-executor/component.go`, `processor/requirement-executor/config.go`

#### 2.4 AgentPersona Support (ADR-030 Foundation)

ADR-030 (BMAD Alignment) depends on agent persona infrastructure shipping with always-on teams. Named agents with persistent identity are where personas attach — team knowledge + error trends give our "personas" memory that BMAD's static markdown files can't match. We add the struct and data plumbing here; ADR-030 builds the prompt vocabulary, `CategoryPersona` fragment, architecture phase, and presets on top.

**File**: `workflow/agent.go` — Add `AgentPersona` struct:

```go
type AgentPersona struct {
    DisplayName  string   `json:"display_name"`  // "Mary", "Winston" — UI + logs
    SystemPrompt string   `json:"system_prompt"` // injected at CategoryPersona (ADR-030)
    Backstory    string   `json:"backstory"`     // optional character narrative
    Traits       []string `json:"traits"`        // personality attributes
    Style        string   `json:"style"`         // communication style
}
```

Add `Persona *AgentPersona` field to `Agent` struct.

**File**: `processor/execution-manager/config.go` — Add `Persona` to `TeamMemberEntry`:

```go
type TeamMemberEntry struct {
    Role    string                  `json:"role"`
    Model   string                  `json:"model"`
    Persona *workflow.AgentPersona  `json:"persona,omitempty"`
}
```

**File**: `processor/execution-manager/component.go` — In `seedTeams()`, pass persona from roster config to agent entity. Write `agent.identity.display_name` triple when persona has a DisplayName.

**File**: `prompt/context.go` — Add `Persona *workflow.AgentPersona` to `AssemblyContext`.

**File**: `processor/execution-manager/component.go` — In `buildAssemblyContext()`, populate `asmCtx.Persona` from agent entity when persona is set.

**Note**: The `CategoryPersona` prompt fragment, `prompt.Vocabulary` struct, and BMAD preset are ADR-030's scope. We provide the data plumbing only.

**Files**: `workflow/agent.go`, `processor/execution-manager/config.go`, `processor/execution-manager/component.go`, `prompt/context.go`, `agentgraph/graph.go`

### Phase 3: Strengthen the Knowledge Loop

#### 3.1 Extract Insights from Approvals

Modify `extractTeamInsights()` to accept the verdict and create positive-pattern insights:

```go
func (c *Component) extractTeamInsights(ctx context.Context, exec *taskExecution, feedback, verdict string) {
    if c.agentHelper == nil || exec.BlueTeamID == "" {
        return
    }

    if verdict == "rejected" && feedback != "" {
        // ... existing rejection insight logic (unchanged) ...
    }

    if verdict == "approved" && feedback != "" {
        insight := workflow.TeamInsight{
            ID:         uuid.New().String(),
            Source:     "approved-pattern",
            ScenarioID: exec.TaskID,
            Summary:    truncateInsight(feedback, 200),
            Skill:      "builder",
            CreatedAt:  time.Now(),
        }
        c.agentHelper.AddTeamInsight(ctx, exec.BlueTeamID, insight)
    }

    // ... existing red team insight logic (unchanged) ...
}
```

**Files**: `processor/execution-manager/team_knowledge.go`

#### 3.2 Add Guidance to Team Knowledge Prompt

Add `Guidance` field to `prompt.TeamLesson`:

```go
type TeamLesson struct {
    Category string
    Summary  string
    Role     string
    Guidance string // remediation text from error category def
}
```

Populate in `buildAssemblyContext()` by looking up each insight's category in `c.errorCategories`.

Update the team-knowledge fragment (`software.go:1191`) to render guidance:

```go
fmt.Fprintf(&sb, "- [%s][%s] %s", kind, lesson.Role, lesson.Summary)
if lesson.Guidance != "" {
    fmt.Fprintf(&sb, " GUIDANCE: %s", lesson.Guidance)
}
sb.WriteString("\n")
```

**Files**: `prompt/context.go`, `prompt/domain/software.go`, `processor/execution-manager/component.go`

#### 3.3 Usage-Based Insight Eviction

Add `UsedCount int` to `workflow.TeamInsight`. Increment in `FilterInsights()` each time an insight passes the filter (proxy for relevance — insights that match prompts survive longer).

Increase `maxTeamInsights` from 50 to 100. Change eviction from FIFO to lowest-UsedCount-first.

**Files**: `workflow/team.go`, `agentgraph/graph.go`

### Phase 4: Review History Visibility

#### 4.1 Query Paths

Add to `agentgraph/graph.go`:

```go
func (h *Helper) ListReviews(ctx context.Context) ([]*Review, error)
func (h *Helper) ListReviewsByAgent(ctx context.Context, agentID string) ([]*Review, error)
```

Follow the `ListAgentsByRole` pattern — scan by entity prefix, unmarshal triples, filter.

#### 4.2 HTTP Endpoints

Add to `execution-manager/http.go`:

| Endpoint | Returns |
|----------|---------|
| `GET {prefix}/agents` | All agents: ID, name, role, model, status, error counts, review averages |
| `GET {prefix}/teams` | All teams: ID, name, status, member count, insight count, team stats |
| `GET {prefix}/agents/{id}/reviews` | Reviews for agent: verdict, ratings, feedback, error categories |

**Files**: `agentgraph/graph.go`, `processor/execution-manager/http.go`

---

## What the Agent Actually Sees

After all phases, a builder retried after rejection sees in its system prompt:

```
RECURRING ISSUES — Your recent reviews flagged these patterns.
You MUST address ALL of the following:

- Missing Tests (2 occurrences): Every acceptance criterion must have a
  corresponding test assertion. Create or update test files alongside
  implementation files.
- Wrong Pattern (1 occurrence): Review the relevant ADRs and CLAUDE.md
  conventions before implementing.

TEAM KNOWLEDGE — Lessons from previous tasks:

- [AVOID][builder] Test file not created alongside auth middleware.
  GUIDANCE: Every acceptance criterion must have a test assertion.
- [AVOID][tester] Empty token edge case not covered.
  GUIDANCE: For every input parameter, ask: what happens when nil, empty, zero?
- [NOTE][builder] Channel-based worker pool pattern approved 5/5.

TEAM CONTEXT

You are a member of Team alpha. All teams are working toward the shared goal
of building an excellent project together. The team with the highest combined
score earns the coveted Team Trophy.
```

And the retry feedback includes:

```
--- REMEDIATION GUIDANCE ---
- Missing Tests: Every acceptance criterion must have a corresponding test
  assertion. Create or update test files alongside implementation files.
```

Today this agent sees: nothing. No history, no team, no patterns, no guidance.

---

## Plan-Level Red Team Support (Not in This ADR, But Designed For)

This ADR does NOT implement plan-level red team review, but the infrastructure changes must support it. The vision: after all blue team requirements complete, a red team runs an entire plan→execute cycle that red-teams the implementation — writing integration tests, e2e tests, and filling coverage gaps.

### What Already Exists

The plan state machine is already prepared:

```
implementing → reviewing_rollup → complete/rejected
```

- `StatusReviewingRollup` is defined (`workflow/types.go:51`)
- State transitions are valid (`types.go:155-161`)
- `rollupTaskIndex sync.Map` in plan-manager routes `agent.complete` events to plans (`plan-manager/component.go:48`)
- `RollupReviewContext` carries aggregated data: `PlanTitle`, `PlanGoal`, `Requirements[]`, `ScenarioOutcomes[]`, `AggregateFiles[]` (`prompt/context.go:213`)
- `RolePlanRollupReviewer` prompt role exists (`prompt/fragment.go`)
- Prompt fragments for plan-rollup-reviewer exist (`prompt/domain/software.go`)
- TODO at `execution_events.go:18`: *"Future: insert a reviewing_rollup stage here (plan-level red team writes integration tests) before completing."*
- Task-level `RedTeamChallengeResult` schema (`workflow/payloads/red_team.go`) with Issues, TestFiles, OverallScore is reusable

### What This ADR Prepares

| Infrastructure | How This ADR Helps |
|---------------|-------------------|
| Team entities always exist | Phase 2 — always-on teams mean `SelectRedTeam()` always has candidates |
| Team knowledge persists across requirements | Phase 3 — insights accumulate plan-wide, available to red team context |
| Error trends populated | Phase 1.1 — red team can see recurring blue team patterns |
| Review history queryable | Phase 4 — red team can reference blue team's review history for the plan |
| Agents have identity | Phase 2.2 — red team agents are named, tracked, and learn across plans |

### What the Red Team Plan-Level Feature Will Need (Future ADR)

| Gap | Description |
|-----|-------------|
| **Dispatch trigger** | `execution_events.go:126` — transition to `reviewing_rollup` instead of `complete`, dispatch red team plan |
| **Red team plan generation** | Red team receives `RollupReviewContext` + all blue team files, generates its own requirements (integration tests, e2e tests, coverage gaps) |
| **Red team execution cycle** | Full plan→decompose→execute pipeline for red team requirements, using `requirement-executor` and `execution-manager` with red team agents |
| **Worktree coordination** | Red team executes against blue team's merged main branch — all blue team worktrees already merged at this point |
| **Red team result rollup** | Aggregate red team test results: passing tests → confidence signal, failing tests → blue team issues |
| **Knowledge feedback** | Red team findings feed back as insights to blue team's `SharedKnowledge` |
| **Plan completion gate** | `reviewing_rollup → complete` only when red team cycle finishes; `→ rejected` if critical issues found |
| **SSE visibility** | Extend plan SSE stream with `red_team_dispatched`, `red_team_requirement_updated`, `red_team_complete` events |

### Why This ADR Unblocks It

Without always-on teams, plan-level red team can't work:
- No teams exist to select as red team
- No accumulated knowledge to inform the red team's test strategy
- No error trends to guide what to test for
- No agent identity for red team members to learn from across plans

The infrastructure in this ADR is the foundation layer.

---

## Other Future Work (Not in Scope)

| Item | Description |
|------|-------------|
| **Knowledge snapshots** | Periodic filesystem backup of team insights for durability across infra teardown |
| **Insight synthesis** | LLM pass to condense 20+ insights into 5-10 meta-insights (prevent prompt bloat) |
| **Cross-project agents** | Named agents bring knowledge from project A to project B |
| **Review dashboard** | Svelte UI visualizing agent performance, team leaderboards, error trends |
| **Peer review ratings** | Reviewer-of-reviewer ratings for calibration (review quality feedback loop) |

---

## Branch Strategy

All work for this ADR will be done on the `teams` feature branch off `main`. This is a large cross-cutting change and should be validated end-to-end before merging.

```bash
git checkout -b teams
```

Commits should be phased (one per implementation step) so the branch can be reviewed incrementally. Merge to `main` only after full E2E validation passes.

---

## Implementation Sequence

| Step | Scope | Lines | Risk | Dependency |
|------|-------|-------|------|------------|
| 1.1 Populate ErrorTrends | `execution-manager/component.go` | ~15 | Low | None |
| 1.2 Retry guidance injection | `execution-manager/component.go` | ~40 | Low | None |
| 2.1 Invert teamsEnabled | `execution-manager`, `requirement-executor` | ~10 | Medium | None |
| 2.2 Default roster | `execution-manager/config.go`, `component.go` | ~30 | Medium | 2.1 |
| 2.3 Validation updates | `config.go` (both components) | ~10 | Low | 2.1 |
| 2.4 AgentPersona support | `workflow/agent.go`, `config.go`, `component.go`, `prompt/context.go` | ~40 | Low | 2.1, ADR-030 foundation |
| 3.1 Approval insights | `team_knowledge.go` | ~20 | Low | 2.1 |
| 3.2 Guidance in knowledge | `prompt/context.go`, `software.go`, `component.go` | ~15 | Low | 1.1 |
| 3.3 Usage-based eviction | `workflow/team.go`, `agentgraph/graph.go` | ~20 | Low | None |
| 4.1 ListReviews | `agentgraph/graph.go` | ~60 | Low | None |
| 4.2 HTTP endpoints | `execution-manager/http.go` | ~80 | Low | 4.1 |
| 5.1 Update hello-world fixtures | `test/e2e/fixtures/` | ~30 | Medium | 2.1 |
| 5.2 Team knowledge loop E2E | `test/e2e/scenarios/`, `test/e2e/fixtures/` | ~200 | Medium | All phases |

**Total**: ~540 lines of changes across ~13 files. No new packages. No new dependencies.

---

## Phase 5: E2E Coverage

### 5.1 Update Existing hello-world Fixtures

With teams always-on, the hello-world mock fixture call counts shift because:
- `seedTeams()` runs on startup (graph writes for 2 teams + 6 agents)
- `SelectBlueTeam()` runs during dispatch
- `SelectAgent()` routes through team-based selection
- Team knowledge injection queries team on every prompt build

Update mock fixture response counts and verify hello-world still passes. This is the regression gate — if hello-world breaks, the always-on change is wrong.

**Run**: `task e2e:mock -- hello-world`

### 5.2 Team Knowledge Loop E2E (New Scenario)

A Tier 2 mock-LLM scenario (`team-knowledge-loop`) that validates the full institutional knowledge feedback loop:

**Scenario flow:**
1. Create plan, trigger execution (standard setup)
2. **First builder call** → `submit_work` with code that will be rejected
3. **Reviewer call** → rejection with feedback containing `missing_tests` signals ("No test file created alongside implementation")
4. Assert: `checkAgentBenching` classified feedback into `missing_tests` category
5. Assert: agent error counts incremented in ENTITY_STATES
6. **Retry builder call** → verify prompt contains:
   - `RECURRING ISSUES` section with "Missing Tests (1 occurrence)"
   - `REMEDIATION GUIDANCE` section with category guidance text
   - `TEAM KNOWLEDGE` section (if insight was extracted from rejection)
7. **Second builder call** → `submit_work` with passing code
8. **Second reviewer call** → approval with explanation feedback
9. Assert: positive insight extracted with `Source: "approved-pattern"`
10. Assert: `GET /execution-manager/agents` returns agents with error counts
11. Assert: `GET /execution-manager/teams` returns teams with insight counts

**Mock fixture design:**
- First builder response: `submit_work` with minimal code (no test file)
- Reviewer rejection: structured JSON with verdict "rejected", feedback matching `missing_tests` signals, error_categories: ["missing_tests"]
- Second builder response: `submit_work` with code + test file
- Reviewer approval: structured JSON with verdict "approved", feedback explaining what worked

**Key assertions (what makes this test valuable):**
- Retry prompt contains error trend data (proves Phase 1.1 wiring works)
- Retry feedback contains remediation guidance (proves Phase 1.2 wiring works)
- Teams seeded without explicit config (proves Phase 2 wiring works)
- Approval generates positive insight (proves Phase 3.1 works)
- HTTP endpoints return roster data (proves Phase 4 works)

**Files:**
- `test/e2e/scenarios/team_knowledge_loop.go` — scenario implementation
- `test/e2e/fixtures/team-knowledge-loop/` — mock LLM fixture responses
- `test/e2e/e2e_test.go` — register scenario

**Run**: `task e2e:mock -- team-knowledge-loop`

---

## Verification

1. **Unit tests**: `go test ./processor/execution-manager/... ./agentgraph/... ./prompt/...`
2. **Build**: `go build ./...`
3. **E2E hello-world**: `task e2e:mock -- hello-world` — regression gate (must still pass with always-on teams)
4. **E2E team-knowledge-loop**: `task e2e:mock -- team-knowledge-loop` — validates full knowledge feedback loop
5. **E2E team-roster**: `task e2e:run -- team-roster` — validates team seeding without explicit config
6. **Manual**: Start semspec with no teams config → verify "Agent roster initialized" + team seeding in logs
7. **HTTP**: `curl http://localhost:8180/execution-manager/agents | jq` → verify roster visibility

---

## Decision

**Proposed.** Pending review.
