# ADR-031: GitHub Issue Watcher + PR Submitter

**Status:** Proposed
**Date:** 2026-04-02
**Authors:** Coby, Claude

## Context

Semspec currently accepts plan input via HTTP API (`POST /plan-api/plans`). We want to close the
loop with GitHub: watch an issue queue for structured requests, run the full plan→execute pipeline,
and submit a PR back. The issue queue is assumed public and potentially adversarial — gating is
required.

### Existing infrastructure

| What | Where | Status |
|------|-------|--------|
| `GitHubMetadata` struct | `workflow/types.go:218` (`PlanRecord`) | Exists but not on `Plan` |
| `ScenarioBranch` plumbing | scenario-orchestrator → requirement-executor → execution-manager | End-to-end, directs worktree merges to a named branch |
| Per-requirement branches | `semspec/requirement-<id>`, created by requirement-executor | Active |
| `Plan-Slug` trailers | Merge commits | Active |
| `RequirementExecutionRequest` | `workflow/payloads/types.go:840` | No branch field yet |

### Industry patterns (research summary)

- **Sweep AI**: "Sweep:" title prefix, minimal structure, auto-runs tests, iterative PRs
- **Copilot Workspace**: natural language specs, generates plan shown in PR, human review gate
- **Dependabot**: consistent PR format, responds to comment commands, auto-merge for low-risk
- **GitHub Issue Forms**: `.yml` templates with required fields, dropdowns, checkboxes — parsed into
  consistent heading-delimited markdown
- **Security**: label-based gating is the dominant pattern; two-stage workflows (triage vs
  processing) for public repos; `pull_request_target` for safe metadata ops

## Decision

Two new components with a shared `github/` package. Polling-based (no public endpoint). Disabled by
default — opt-in via config.

### 1. `github-watcher` (input component)

Polls GitHub issues, validates against whitelist, publishes plan creation requests via NATS.

**Polling, not webhook** for MVP — avoids requiring a public endpoint. Configurable interval
(default 60s), uses `since` parameter for efficient queries, respects `X-RateLimit-*` headers.

**Security model:**

| Gate | Default | Purpose |
|------|---------|---------|
| Label requirement | `semspec` label, required | Maintainers control what gets processed |
| Contributor whitelist | empty (disabled) | Restrict to known GitHub usernames |
| Body size limit | 10KB | Anti-spam |
| Rate limit | 10 plans/hour | Prevent pipeline flooding |
| No code execution | always | Issue body is text input to planner, never executed |

**Issue template** for target repo (`.github/ISSUE_TEMPLATE/semspec.yml`):

```yaml
name: Semspec Request
description: Submit a development request for semspec to implement
labels: ["semspec"]
body:
  - type: textarea
    id: description
    attributes:
      label: Description
      description: What should be built or changed?
    validations:
      required: true
  - type: textarea
    id: scope
    attributes:
      label: Scope
      description: File patterns or directories to focus on (optional)
      placeholder: "src/api/**, tests/integration/**"
  - type: textarea
    id: constraints
    attributes:
      label: Constraints
      description: Any requirements or constraints (optional)
  - type: dropdown
    id: priority
    attributes:
      label: Priority
      options:
        - Normal
        - High
        - Low
      default: 0
```

GitHub renders this as a structured form. Responses become heading-delimited markdown in the issue
body, making parsing deterministic (no LLM needed).

**Flow:**

```
GitHub Issues API (poll)
  → validate (label + whitelist + size)
  → parse body (heading-delimited sections)
  → publish GitHubPlanCreationRequest to workflow.trigger.github-plan-create (JetStream)
  → record in GITHUB_ISSUES KV bucket (dedup)
  → post acknowledgment comment on issue
```

### 2. `github-submitter` (output component)

Watches for plan completion, pushes branch, creates PR, updates issue.

**Branch strategy — PR as merge mechanism:**

For GitHub-originated plans, code must NOT merge directly to main. The PR is the review/merge gate.
This requires a plan-level branch:

```
StatusImplementing (plan-manager)
  → create branch semspec/<issue>-<slug> from HEAD
  → store on Plan.GitHub.PlanBranch
  ↓
scenario-orchestrator reads PlanBranch from PLAN_STATES
  → passes to RequirementExecutionRequest
  ↓
requirement-executor creates sub-branches from plan branch
  → task worktrees merge to requirement branch
  → requirement branch merges to plan branch
  ↓
StatusAwaitingReview (plan-manager, after rollup)
  → github-submitter pushes plan branch
  → creates PR against default branch
  → posts comment on source issue
  → polls PR reviews for feedback loop (section 4)
  ↓
PR merged → plan-manager transitions to StatusComplete
```

**PR body format:**

```markdown
## Summary
Fixes #<issue-number>

<plan.Goal>

## Requirements
- [x] <requirement 1 title>
- [x] <requirement 2 title>

## Changes
<list of modified files>

---
Generated by [semspec](https://github.com/c360studio/semspec) from #<N>
```

**Failure handling:** On `StatusRejected`, post failure comment on issue with error summary and
review findings. Do NOT close the issue — the user may update and re-trigger.

### Shared package: `github/`

Thin GitHub API client using stdlib `net/http` — no `go-github` dependency. The API surface is
small: list issues, get issue, create comment, create branch ref, create PR.

### NATS subjects

| Subject | Stream | Direction | Payload |
|---------|--------|-----------|---------|
| `workflow.trigger.github-plan-create` | WORKFLOW | watcher → plan-manager | `GitHubPlanCreationRequest` |
| `github.pr.created` | WORKFLOW | submitter → observability | `GitHubPRCreatedEvent` |
| `plan.mutation.github.pr_feedback` | WORKFLOW | submitter → plan-manager | `GitHubPRFeedbackRequest` |
| `plan.mutation.review.approve` | WORKFLOW | submitter/UI → plan-manager | Generic approval (slug + reviewer) |

### KV bucket

`GITHUB_ISSUES` — tracks processed issue numbers. Key: `<owner>.<repo>.<issue-number>`, Value:
`{plan_slug, created_at, status}`. Prevents duplicate plan creation on restart or re-poll.

## Config

Both components disabled by default. No behavior change for existing users.

```json
"github-watcher": {
  "name": "github-watcher",
  "type": "processor",
  "enabled": false,
  "config": {
    "github_token": "${GITHUB_TOKEN}",
    "repository": "${GITHUB_REPOSITORY}",
    "poll_interval": "60s",
    "issue_label": "semspec",
    "require_label": true,
    "allowed_contributors": [],
    "require_contributor": false,
    "max_body_size": 10000,
    "max_plans_per_hour": 10
  }
},
"github-submitter": {
  "name": "github-submitter",
  "type": "processor",
  "enabled": false,
  "config": {
    "github_token": "${GITHUB_TOKEN}",
    "repository": "${GITHUB_REPOSITORY}",
    "remote_name": "origin",
    "branch_prefix": "semspec/",
    "draft_pr": true,
    "comment_on_transitions": true,
    "review_poll_interval": "30s",
    "max_pr_revisions": 3,
    "auto_accept_feedback": true
  }
}
```

### 3. `awaiting_review` — human gate before completion

A plan is not complete until a human approves it. A new `StatusAwaitingReview` state sits between
`reviewing_rollup` and `complete`, gated by config:

```
reviewing_rollup
  → awaiting_review   (auto_approve_review=false OR GitHub plan)
  → complete          (auto_approve_review=true, no GitHub — existing behavior)
```

**Config** (same pattern as existing plan approval gates):

```json
"plan-manager": {
  "config": {
    "auto_approve_review": true
  }
}
```

- `auto_approve_review: true` (default) — existing behavior, no gate
- `auto_approve_review: false` — hold at `awaiting_review`
- GitHub plans (`plan.GitHub != nil`) — always gate regardless of config

The gate can be satisfied through any channel:

| Channel | Approve (→ complete) | Request Changes (→ ready\_for\_execution) |
|---------|---------------------|-----------------------------------------|
| GitHub PR | PR merged | `CHANGES_REQUESTED` review |
| UI | "Approve" button | "Request Changes" button |
| HTTP | `POST /plans/{slug}/complete` | `POST /plans/{slug}/retry` with feedback |

**State machine additions:**

```
StatusAwaitingReview:
  → complete              (human approves)
  → ready_for_execution   (human requests changes — re-execute affected requirements)
  → rejected              (human rejects)
  → archived              (human shelves)
```

**New mutation: `plan.mutation.review.approve`** — generic approval, used by github-submitter on
PR merge, UI approve button, and the existing `POST /plans/{slug}/complete` endpoint (refactored
to publish this mutation when plan is in `awaiting_review`).

**What stays alive during `awaiting_review`:** Plan branch, EXECUTION\_STATES KV entries,
PLAN\_STATES metadata, PlanDecision history. No worktrees (ephemeral, created fresh from branch
HEAD on re-execution).

### 4. PR feedback loop

When a PR receives review feedback, route it back into the execution pipeline. The plan stays
alive in `awaiting_review` — no resurrection of completed plans needed.

**Flow:**

```
1. All requirements complete → reviewing_rollup → awaiting_review
2. github-submitter creates PR (plan in awaiting_review + has GitHub metadata)
3. Human reviews PR on GitHub
4. github-submitter polls PR reviews (30s interval, batches unprocessed reviews)

If CHANGES_REQUESTED:
  5. Publish GitHubPRFeedbackRequest to JetStream (batched)
  6. plan-manager (single writer):
     a. Map file-scoped comments → requirements via FilesModified reverse index
     b. Create PlanDecision(s) per affected requirement (audit trail)
     c. Append unmapped comments to plan.Context
     d. Reset affected requirement executions by ID
     e. Increment PRRevision, update LastProcessedReviewID
     f. Transition: awaiting_review → ready_for_execution
  7. Scenario orchestrator re-dispatches affected requirements
  8. Requirement executor adds commits to existing plan branch
  9. Convergence → reviewing_rollup → awaiting_review
  10. github-submitter pushes updated branch, comments on PR

If PR merged:
  5. github-submitter publishes plan.mutation.review.approve
  6. plan-manager transitions: awaiting_review → complete
```

**Comment-to-requirement mapping** uses `RequirementExecution.FilesModified` (already tracked in
`EXECUTION_STATES`). A reverse index maps file paths to requirement IDs:

| Comment type | Detection | Action |
|---|---|---|
| File-scoped inline | `comment.path` is set | Map to requirement via FilesModified |
| Suggestion block | `comment.path` + suggestion markdown | Same, include diff in rationale |
| General (no file path) | Top-level review body | Append to plan context, retry ALL |

If multiple requirements modified the same file, target all of them (conservative).

**Review state handling:**

| GitHub review state | Action |
|---|---|
| `CHANGES_REQUESTED` | Trigger feedback loop |
| `COMMENTED` | Ignore — no explicit change request |
| `APPROVED` | Wait for merge event; approved-but-not-merged is a no-op |

**Max revisions:** Default 3 (configurable `max_pr_revisions`). At cap, github-submitter stops
polling and posts a comment: "Semspec reached revision limit. Merge, close, or manually retry."
Plan stays in `awaiting_review` — human can still merge or use HTTP.

**Concurrent reviews:** All unprocessed reviews (review.id > lastProcessedReviewID) are batched
into a single `GitHubPRFeedbackRequest`. Prevents rapid-fire feedback rounds.

**New function: `resetRequirementExecutionsByID`** — resets requirement executions by ID
regardless of stage (existing retry filters by failed/error stage only). Deletes EXECUTION\_STATES
entries so scenario-orchestrator sees affected requirements as pending. ~30 lines following
existing `resetRequirementExecutions` pattern.

**New payload: `GitHubPRFeedbackRequest`**

```go
type GitHubPRFeedbackRequest struct {
    Slug     string            `json:"slug"`
    PRNumber int               `json:"pr_number"`
    ReviewID int64             `json:"review_id"`
    Reviewer string            `json:"reviewer"`
    State    string            `json:"state"`     // CHANGES_REQUESTED
    Body     string            `json:"body"`
    Comments []PRReviewComment `json:"comments"`
    TraceID  string            `json:"trace_id,omitempty"`
}

type PRReviewComment struct {
    ID       int64  `json:"id"`
    Path     string `json:"path,omitempty"`      // file path (empty for general comments)
    Line     int    `json:"line,omitempty"`
    Body     string `json:"body"`
    DiffHunk string `json:"diff_hunk,omitempty"`
}
```

**New mutation: `plan.mutation.github.pr_feedback`** — on plan-manager. Creates PlanDecisions,
resets executions, transitions `awaiting_review → ready_for_execution`.

**GitHubMetadata extensions:**

```go
PRNumber              int    `json:"pr_number,omitempty"`
PRURL                 string `json:"pr_url,omitempty"`
PRRevision            int    `json:"pr_revision,omitempty"`
LastProcessedReviewID int64  `json:"last_processed_review_id,omitempty"`
PRState               string `json:"pr_state,omitempty"`  // open, merged, closed
```

**github-submitter additions** (extends section 2 above):

1. **PR creation** at `awaiting_review` (not `complete`) for GitHub plans
2. **Review polling** — 30s per tracked plan, exits on merge/close/revision cap
3. **Feedback dispatch** — publish `GitHubPRFeedbackRequest` on `CHANGES_REQUESTED`
4. **Merge detection** — publish `plan.mutation.review.approve`
5. **Re-completion** — plan returns to `awaiting_review`, push updated branch, comment on PR

## Consequences

### Files to create

| File | Purpose |
|------|---------|
| `github/client.go` | Thin GitHub API client (stdlib `net/http`) |
| `github/types.go` | Issue, PR, Comment, Review request/response types |
| `github/issue_parser.go` | Parse structured issue body into plan fields |
| `github/issue_parser_test.go` | Table-driven tests for body parsing |
| `processor/github-watcher/factory.go` | Component registration |
| `processor/github-watcher/config.go` | Config struct with whitelist, poll interval, label gate |
| `processor/github-watcher/component.go` | Polling loop, validation, NATS publishing |
| `processor/github-watcher/component_test.go` | Unit tests with mock GitHub responses |
| `processor/github-submitter/factory.go` | Component registration |
| `processor/github-submitter/config.go` | Config struct with token, remote, draft PR flag, max revisions |
| `processor/github-submitter/component.go` | KV watcher, branch push, PR creation, review polling, feedback dispatch |
| `processor/github-submitter/pr_builder.go` | Construct PR body from plan data |
| `processor/github-submitter/review_poller.go` | Poll PR reviews, batch unprocessed, publish feedback |
| `processor/github-submitter/component_test.go` | Unit tests |
| `workflow/payloads/github.go` | `GitHubPlanCreationRequest`, `GitHubPRCreatedEvent`, `GitHubPRFeedbackRequest` |
| `workflow/mapping.go` | `MapFilesToRequirements` — reverse index from EXECUTION\_STATES |
| `workflow/mapping_test.go` | Table-driven tests for file→requirement mapping |

### Files to modify

| File | Change |
|------|--------|
| `workflow/types.go` | Add `StatusAwaitingReview`; add `GitHub *GitHubMetadata` to `Plan` struct; extend `GitHubMetadata` with PR tracking fields; update `CanTransitionTo` |
| `workflow/payloads/types.go` | Add `PlanBranch` to `RequirementExecutionRequest` |
| `processor/plan-manager/component.go` | Subscribe to `workflow.trigger.github-plan-create` and `plan.mutation.github.pr_feedback` and `plan.mutation.review.approve` |
| `processor/plan-manager/mutations.go` | Add `handleGitHubPRFeedback`, `handleReviewApprove` handlers |
| `processor/plan-manager/http.go` | Add `resetRequirementExecutionsByID`; refactor complete endpoint for `awaiting_review` |
| `processor/plan-manager/execution_events.go` | Convergence target: route to `awaiting_review` when config/GitHub requires it; create plan branch on `StatusImplementing` for GitHub plans |
| `processor/scenario-orchestrator/component.go` | Read plan's `PlanBranch` from PLAN_STATES, pass to `RequirementExecutionRequest` |
| `processor/requirement-executor/component.go` | Use `PlanBranch` as base for requirement branches (line ~426) |
| `cmd/semspec/main.go` | Register both new components |
| `configs/semspec.json` | Add component config sections (disabled by default); add `auto_approve_review` to plan-manager |

### Risks

| Risk | Mitigation |
|------|------------|
| Polling misses issues during downtime | `GITHUB_ISSUES` KV tracks `lastProcessedAt`; restart polls from last known timestamp |
| GitHub API rate limit (5000/hr) | Configurable poll interval, `since` parameter, exponential backoff on 403 |
| Spam issues create runaway plans | Label gate + contributor whitelist + rate limit (max N plans/hour) |
| Plan fails mid-execution | Submitter watches `StatusRejected` too; posts failure comment |
| Token rotation | Env var based; no restart needed with secret mount |
| PR feedback infinite loop | Max revision cap (default 3); post explanatory comment at cap |
| Concurrent PR reviews | Batch all unprocessed reviews into single feedback request |
| PR merged before feedback processed | Detect merged state, skip pending feedback, transition to complete |
| PR closed without merge | Stop polling, plan stays in `awaiting_review` for human decision |

### Implementation order

1. `workflow/types.go` — `StatusAwaitingReview`, transition rules, `GitHubMetadata` extensions
2. `github/` package — client, types, issue parser (standalone, fully testable)
3. `workflow/payloads/` — new payload types (`github.go`), `PlanBranch` on `RequirementExecutionRequest`
4. `workflow/mapping.go` — `MapFilesToRequirements` reverse index
5. `plan-manager` — `auto_approve_review` config, convergence routing, `handleReviewApprove`,
   `resetRequirementExecutionsByID`
6. `github-watcher` component — polling, validation, plan creation trigger
7. `plan-manager` GitHub integration — subscribe to github plan trigger, `handleGitHubPRFeedback`
8. Branch plumbing — plan-manager creates branch on `StatusImplementing`, scenario-orchestrator
   passes through, requirement-executor uses as base
9. `github-submitter` component — KV watch, push, PR creation, review polling, feedback dispatch,
   merge detection
10. Registration — main.go, semspec.json, issue template

## Future work (not in scope)

- **Webhook mode**: HTTP endpoint with `X-Hub-Signature-256` verification for real-time response
- **GitHub App**: installation tokens, bot identity (`semspec[bot]`), fine-grained permissions
- **Issue lifecycle**: status comments at each plan phase, `/semspec cancel` command in comments
- **Multi-repo**: config supports multiple repositories with independent whitelists
- **Smarter comment classification**: LLM-assisted mapping for general comments that don't
  target specific files
- **PR suggestion auto-apply**: apply GitHub suggestion blocks as direct file edits before
  re-execution
