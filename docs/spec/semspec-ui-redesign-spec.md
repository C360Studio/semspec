# SemSpec UI Redesign: From Chat-First to Project-First

**Status**: Spec  
**Slug**: ui-project-management-redesign  
**Date**: 2025-02-11  
**Priority**: High

---

## Problem Statement

The SemSpec UI was shaped by SpecKit and OpenSpec chat-based artifacts. It currently presents as a "log viewer with chat" — a chat view as the primary route (`/`) with a loop sidebar, dashboard with stats cards, and a tasks page that's secondary to everything else.

This fundamentally misrepresents what SemSpec does. SemSpec is a **spec-driven autonomous development platform**. It runs mostly in full-auto once configured. The human's job is to:

1. See what needs to be worked on (proposals, specs, tasks)
2. See what agents are currently working on and their progress
3. Approve work, answer agent questions, and unblock flows
4. Review completed work

The current UI answers none of these questions at a glance. You open the app and see a chat box. There's no sense of "what is happening across my project right now."

### What's Wrong Specifically

1. **Chat is the home view** — Should be a tool within a project workspace, not the workspace itself
2. **The spec-driven workflow pipeline is invisible** — Changes go through `proposal → design → spec → tasks` but there's no visual representation of where each change sits in that pipeline
3. **No assignment/ownership visibility** — Tasks link to loops via `task.loop`, loops have roles and models, but there's no view showing who/what is working on what
4. **The dashboard is monitoring, not managing** — Stats cards and tables show numbers but don't help you take action
5. **No "needs my attention" aggregation** — Pending approvals, agent questions, failed validations, and blocked tasks are scattered across views

---

## Design Principles

1. **Project board as home, chat as contextual** — The default view should show work items and their status, not a conversation
2. **Pipeline-first for changes** — Every change slug should show its progression through the workflow stages visually
3. **Agents are team members** — Show agent activity the way GitHub shows CI status or assignees — inline with the work items
4. **Action-oriented** — Every view should make it obvious what the human needs to do next
5. **Same workspace for humans and agents** — The UI visualizes what's in the knowledge graph; agents query the same data

---

## Architecture Changes

### Navigation Restructure

**Current sidebar nav:**
```
Chat (/) — PRIMARY
Dashboard (/dashboard)
Tasks (/tasks)
History (/history)
Settings (/settings)
```

**New sidebar nav:**
```
Board (/) — PRIMARY — project board with changes and their pipeline status
Changes (/changes) — detailed change management with pipeline view
Activity (/activity) — real-time agent activity feed + chat
History (/history) — past sessions, completed work
Settings (/settings)
```

Update `Sidebar.svelte` nav items:

```typescript
const navItems = [
  { path: '/', icon: 'kanban', label: 'Board' },
  { path: '/changes', icon: 'git-pull-request', label: 'Changes' },
  { path: '/activity', icon: 'activity', label: 'Activity' },
  { path: '/history', icon: 'history', label: 'History' },
  { path: '/settings', icon: 'settings', label: 'Settings' },
];
```

The sidebar should also show:
- **Attention badge** on Board: count of items needing human action (pending approvals + unanswered questions + failed tasks)
- **Active loops count** on Activity: number of currently executing loops
- **System health indicator** in footer (keep existing)

### New File Structure

```
src/
├── lib/
│   ├── api/
│   │   ├── client.ts            # (existing)
│   │   ├── changes.ts           # NEW - Change/workflow API
│   │   ├── mock.ts              # (update mocks)
│   │   └── graphql.ts           # (existing)
│   │
│   ├── stores/
│   │   ├── activity.svelte.ts   # (existing - SSE stream)
│   │   ├── loops.svelte.ts      # (existing)
│   │   ├── changes.svelte.ts    # NEW - Changes state
│   │   ├── messages.svelte.ts   # (existing - scoped to activity view)
│   │   ├── attention.svelte.ts  # NEW - Aggregated attention items
│   │   └── system.svelte.ts     # (existing)
│   │
│   ├── components/
│   │   ├── board/               # NEW
│   │   │   ├── BoardView.svelte
│   │   │   ├── ChangeCard.svelte
│   │   │   ├── AttentionBanner.svelte
│   │   │   ├── PipelineIndicator.svelte
│   │   │   └── AgentBadge.svelte
│   │   │
│   │   ├── changes/             # NEW
│   │   │   ├── ChangeDetail.svelte
│   │   │   ├── PipelineView.svelte
│   │   │   ├── DocumentPanel.svelte
│   │   │   ├── TaskList.svelte
│   │   │   └── ChangeTimeline.svelte
│   │   │
│   │   ├── activity/            # REFACTORED from chat/
│   │   │   ├── ActivityFeed.svelte
│   │   │   ├── ChatPanel.svelte
│   │   │   ├── MessageInput.svelte
│   │   │   ├── MessageList.svelte
│   │   │   ├── LoopDetail.svelte
│   │   │   └── QuestionQueue.svelte
│   │   │
│   │   ├── shared/
│   │   │   ├── Sidebar.svelte   # (update nav)
│   │   │   ├── Header.svelte    # (existing)
│   │   │   ├── Modal.svelte     # (existing)
│   │   │   ├── StatusBadge.svelte # NEW
│   │   │   └── Icon.svelte      # (existing)
│   │   │
│   │   ├── dashboard/           # REMOVE (absorbed into Board)
│   │   └── tasks/               # REMOVE (absorbed into Changes)
│
├── routes/
│   ├── +layout.svelte           # (update)
│   ├── +page.svelte             # Board view (was Chat)
│   ├── changes/
│   │   ├── +page.svelte         # Changes list
│   │   └── [slug]/
│   │       └── +page.svelte     # Change detail with pipeline
│   ├── activity/
│   │   └── +page.svelte         # Activity + Chat + Q&A
│   ├── history/
│   │   └── +page.svelte         # (existing, keep)
│   └── settings/
│       └── +page.svelte         # (existing, keep)
```

---

## View Specifications

### 1. Board View (`/` — Home)

This replaces the chat view as the primary landing page. It answers: "What's the state of my project right now and what needs my attention?"

#### Layout

```
┌─────────────────────────────────────────────────────────────┐
│  ⚠ 3 items need your attention                    [View All]│
│  • Approve spec for "add-auth" • Answer question from       │
│    architect-agent • Review failed task in "refactor-db"     │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  Active Changes                              [+ New Change]  │
│                                                              │
│  ┌─────────────────────────────────────────────────────┐    │
│  │ add-user-authentication          Status: Implementing│    │
│  │ ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐               │    │
│  │ │✓ prop│ │✓ dsgn│ │✓ spec│ │● task│  3/7 tasks     │    │
│  │ └──────┘ └──────┘ └──────┘ └──────┘               │    │
│  │ 🤖 implementer (qwen) working on task 4.1          │    │
│  │ 🤖 reviewer (claude) idle — waiting for 4.1        │    │
│  └─────────────────────────────────────────────────────┘    │
│                                                              │
│  ┌─────────────────────────────────────────────────────┐    │
│  │ refactor-database-layer          Status: Reviewed    │    │
│  │ ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐               │    │
│  │ │✓ prop│ │✓ dsgn│ │✓ spec│ │○ task│  awaiting      │    │
│  │ └──────┘ └──────┘ └──────┘ └──────┘  approval      │    │
│  │ ⚠ Needs approval to generate tasks    [Approve]     │    │
│  └─────────────────────────────────────────────────────┘    │
│                                                              │
│  ┌─────────────────────────────────────────────────────┐    │
│  │ fix-login-redirect                   Status: Drafted │    │
│  │ ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐               │    │
│  │ │✓ prop│ │○ dsgn│ │○ spec│ │○ task│                │    │
│  │ └──────┘ └──────┘ └──────┘ └──────┘               │    │
│  │ 🤖 design-writer (claude) generating design...      │    │
│  └─────────────────────────────────────────────────────┘    │
│                                                              │
├─────────────────────────────────────────────────────────────┤
│  System: ● Connected  │  Loops: 2 active  │  Models: 3 ok  │
└─────────────────────────────────────────────────────────────┘
```

#### Components

**AttentionBanner.svelte** — Top banner aggregating items needing human action:
- Pending approvals (changes in `reviewed` status awaiting `/approve`)
- Unanswered agent questions (from question routing system)
- Failed tasks/loops
- Blocked tasks (dependencies unmet)
- Items link directly to the relevant change or activity

**ChangeCard.svelte** — Card for each active change showing:
- Title and slug
- Overall status badge (from `workflow.Status`: created, drafted, reviewed, approved, implementing, complete)
- **Pipeline indicator**: 4-step visual showing `proposal → design → spec → tasks` with states:
  - `○` not started
  - `●` in progress (agent currently generating)
  - `✓` complete
  - `✗` failed (validation failed)
- Task progress (if tasks exist): "3/7 tasks complete"
- Active agents: Show which loops are currently executing against this change, with role and model
- Action buttons when applicable: [Approve], [Review], [Continue]
- GitHub sync status if linked (issue number)
- Click anywhere on card → navigates to `/changes/[slug]`

**PipelineIndicator.svelte** — Reusable 4-step pipeline visualization:
```typescript
interface Props {
  proposal: 'none' | 'generating' | 'complete' | 'failed';
  design: 'none' | 'generating' | 'complete' | 'failed';
  spec: 'none' | 'generating' | 'complete' | 'failed';
  tasks: 'none' | 'generating' | 'complete' | 'failed';
}
```

Render as connected steps with color-coded indicators. Use CSS transitions for state changes. Keep compact — this needs to work inside a card.

**AgentBadge.svelte** — Shows an active agent/loop working on this change:
```typescript
interface Props {
  role: string;      // e.g., "implementer", "design-writer"
  model: string;     // e.g., "qwen", "claude"
  state: string;     // loop state
  iteration: number;
  maxIterations: number;
}
```

Small inline badge with robot icon, role name, model name, and a tiny progress indicator.

#### Data Requirements

The Board view needs a new API endpoint or composite fetch:

```typescript
// New endpoint needed on backend
// GET /api/changes - returns all active changes with their status
interface ChangeWithStatus {
  slug: string;
  title: string;
  status: string;           // workflow.Status
  author: string;
  created_at: string;
  updated_at: string;
  files: {
    has_proposal: boolean;
    has_design: boolean;
    has_spec: boolean;
    has_tasks: boolean;
  };
  github?: {
    epic_number: number;
    epic_url: string;
    repository: string;
    task_issues: Record<string, number>;
  };
  // Enriched by joining with loop data
  active_loops: {
    loop_id: string;
    role: string;
    model: string;
    state: string;
    iterations: number;
    max_iterations: number;
    workflow_step: string;
  }[];
  // Task completion stats (parsed from tasks.md or graph)
  task_stats?: {
    total: number;
    completed: number;
    failed: number;
    in_progress: number;
  };
}
```

**If the backend endpoint doesn't exist yet**, compose it client-side:
1. Fetch changes via the workflow filesystem (needs a new simple endpoint: `GET /api/workflow/changes`)
2. Fetch active loops via existing `GET /agentic-dispatch/loops`
3. Join on `workflow_slug` field in LoopInfo
4. Create a `changes.svelte.ts` store that does this composition

**Attention items store** (`attention.svelte.ts`):
```typescript
interface AttentionItem {
  type: 'approval_needed' | 'question_pending' | 'task_failed' | 'task_blocked';
  change_slug?: string;
  loop_id?: string;
  title: string;
  description: string;
  action_url: string;     // Route to navigate to
  created_at: string;
}
```

Derive attention items from:
- Changes with status `reviewed` → approval needed
- Loops in state `paused` with pending user signals → question pending
- Loops in state `failed` → task failed
- Tasks with status `blocked` → task blocked

---

### 2. Changes View (`/changes` and `/changes/[slug]`)

#### Changes List (`/changes`)

A filterable list of all changes (active + archived). Think GitHub Issues list.

```
┌──────────────────────────────────────────────────────────────┐
│  Changes                                      [+ New Change] │
│                                                               │
│  Filter: [All ▾] [Status ▾] [Sort: Updated ▾]              │
│                                                               │
│  ┌─ add-user-authentication ──────────── implementing ──┐    │
│  │  Created 3 days ago by coby                          │    │
│  │  ✓prop ✓dsgn ✓spec ●tasks  │  3/7 tasks  │ GH #42  │    │
│  └──────────────────────────────────────────────────────┘    │
│                                                               │
│  ┌─ refactor-database-layer ──────────── reviewed ──────┐    │
│  │  Created 1 day ago by coby                           │    │
│  │  ✓prop ✓dsgn ✓spec ○tasks  │  awaiting approval     │    │
│  └──────────────────────────────────────────────────────┘    │
│                                                               │
│  ┌─ fix-login-redirect ──────────────── drafted ────────┐    │
│  │  Created 2 hours ago by coby                         │    │
│  │  ✓prop ○dsgn ○spec ○tasks  │  design in progress    │    │
│  └──────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────┘
```

#### Change Detail (`/changes/[slug]`)

This is the deep view for a single change. Think GitHub Issue detail crossed with a CI pipeline view.

```
┌──────────────────────────────────────────────────────────────┐
│  ← Back to Changes                                           │
│                                                               │
│  add-user-authentication                                     │
│  Status: implementing  │  Author: coby  │  GH #42           │
│                                                               │
│  ═══✓═══════✓═══════✓═══════●═══                            │
│  Proposal   Design    Spec     Tasks                         │
│                                                               │
├──────────────────────────┬───────────────────────────────────┤
│                          │                                    │
│  Documents               │  Tasks                    4/7      │
│                          │                                    │
│  ▸ proposal.md    ✓      │  ✓ 1.1 Create user model          │
│  ▸ design.md      ✓      │  ✓ 1.2 Add migration              │
│  ▸ spec.md        ✓      │  ✓ 2.1 JWT token service          │
│  ▸ tasks.md       ✓      │  ● 2.2 Login endpoint             │
│                          │    🤖 implementer (qwen) iter 3/10 │
│  [View Document]         │  ○ 2.3 Logout endpoint             │
│                          │    ⏳ blocked by 2.2               │
│                          │  ○ 3.1 Integration tests           │
│                          │  ○ 3.2 Documentation               │
│                          │                                    │
├──────────────────────────┴───────────────────────────────────┤
│                                                               │
│  Timeline                                                    │
│                                                               │
│  ● 10:42  Task 2.2 started — implementer (qwen) loop abc123 │
│  ✓ 10:38  Task 2.1 completed — 847 tokens                   │
│  ✓ 10:15  Spec approved by coby                              │
│  ● 10:12  Spec generated — spec-writer (claude)              │
│  ✓ 09:45  Design approved by coby                            │
│  ...                                                          │
└──────────────────────────────────────────────────────────────┘
```

#### Components

**PipelineView.svelte** — Expanded pipeline with clickable stages:
- Each stage is clickable to view/preview the document
- Shows which agent generated each document and when
- Failed stages show the validation error with a retry button

**DocumentPanel.svelte** — Left panel listing the 4 workflow documents:
- Shows existence status (✓ exists, ○ not yet, ✗ failed)
- Click to view document content in a modal or slide-out panel
- Render the markdown content
- Show generation metadata (model used, tokens, timestamp)

**TaskList.svelte** — Right panel showing parsed tasks from tasks.md:
- Each task shows: ID, description, status (pending/in_progress/complete/failed/blocked)
- Active tasks show the assigned loop with agent badge
- Blocked tasks show which predecessor they're waiting on
- Failed tasks show error summary with retry action
- Task status should update via SSE when loops complete

**ChangeTimeline.svelte** — Bottom panel showing chronological activity:
- Document generation events
- Approval/rejection events
- Task start/complete/fail events
- Agent questions and answers
- GitHub sync events
- Each entry links to relevant loop trajectory if applicable

#### Data Requirements

Change detail requires:
1. `GET /api/workflow/changes/{slug}` — Change metadata + file flags
2. `GET /api/workflow/changes/{slug}/tasks` — Parsed tasks with status (new endpoint, or parse tasks.md client-side)
3. `GET /agentic-dispatch/loops?workflow_slug={slug}` — All loops for this change (filter existing endpoint)
4. Activity events filtered by change slug (SSE with client-side filter, or parameterized endpoint)

---

### 3. Activity View (`/activity`)

This absorbs the current chat view and dashboard activity feed into a unified real-time view. Think "mission control" — you watch agents work and can intervene.

#### Layout

```
┌───────────────────────────────────┬─────────────────────────┐
│                                   │                          │
│  Activity Feed                    │  Chat / Commands         │
│                                   │                          │
│  ● 10:42 [add-auth]              │  /propose fix redirect   │
│    implementer started task 2.2   │                          │
│    Model: qwen │ Loop: abc123     │  > Creating proposal     │
│                                   │    for fix-login-redirect│
│  ✓ 10:38 [add-auth]              │    ...                   │
│    Task 2.1 completed             │                          │
│    847 tokens │ 2.3s              │  Last response:          │
│                                   │  Proposal created at     │
│  ? 10:35 [refactor-db]           │  .semspec/changes/...    │
│    architect-agent asks:          │                          │
│    "Should I split the users      │                          │
│    table or use partitioning?"    │                          │
│    [Answer] [Skip]                │                          │
│                                   │                          │
│  ● 10:30 [add-auth]              │                          │
│    implementer tool call:         │                          │
│    file_write auth/jwt.go         │                          │
│                                   │                          │
│  ⚠ 10:28 [fix-redirect]          │                          │
│    design-writer validation       │                          │
│    failed: missing API section    │                          │
│    Retrying (attempt 2/3)...      │                          │
│                                   │                          │
├───────────────────────────────────┤  ┌───────────────────┐  │
│  Active Loops              2 of 3 │  │ Type a command... │  │
│                                   │  └───────────────────┘  │
│  abc123 implementer qwen ███░░ 3/10│                         │
│  def456 design-writer claude ██░ 2/5│                         │
└───────────────────────────────────┴─────────────────────────┘
```

#### Components

**ActivityFeed.svelte** (refactored from existing):
- Real-time SSE-driven feed of all agent activity
- Each event tagged with the change slug it relates to (color-coded or filterable)
- Event types: loop_started, loop_completed, loop_failed, tool_call, tool_result, model_call, question_asked, approval_requested, document_generated, validation_passed, validation_failed
- Inline action buttons for questions and approvals (don't force navigation)
- Filter bar: by change slug, by event type, by agent role
- Click on a loop ID to expand trajectory detail inline

**ChatPanel.svelte** (refactored from existing chat components):
- Keeps the existing chat/command functionality
- Scoped to the right panel — it's a command interface, not the whole view
- Shows slash command palette on `/`
- Shows recent command responses
- Message history persists per session

**QuestionQueue.svelte** — Dedicated section for agent questions:
- Questions from the question routing system that need human answers
- Show context: which agent asked, what change, what they're trying to do
- Answer inline with text input
- Escalation timer (SLA from question routing)
- Answering a question sends the signal back to the waiting loop

**Active Loops** section at bottom left:
- Compact list of all currently executing loops
- Each shows: loop ID, role, model, progress bar (iterations/max)
- Click to expand loop trajectory
- Cancel/pause buttons

---

### 4. History View (`/history`) — Keep Mostly As-Is

Minor updates:
- Add change slug grouping — group past loops by which change they worked on
- Add filtering by change, by outcome (success/fail), by date range
- Show token usage summaries per change

---

## Backend API Additions Needed

The UI redesign depends on exposing workflow data that currently lives only on the filesystem. These endpoints need to be added to the Go service manager HTTP API:

### Required New Endpoints

```
GET  /api/workflow/changes
     Returns: ChangeWithStatus[] (all active changes with metadata)
     Query params: ?status=implementing&sort=updated_at

GET  /api/workflow/changes/{slug}
     Returns: ChangeWithStatus (single change with full detail)

GET  /api/workflow/changes/{slug}/documents/{type}
     Returns: { content: string, generated_at: string, model: string }
     Where type = proposal | design | spec | tasks

GET  /api/workflow/changes/{slug}/tasks
     Returns: ParsedTask[] (structured task data from tasks.md)

POST /api/workflow/changes/{slug}/approve
     Transitions change status to approved

POST /api/workflow/changes/{slug}/reject
     Transitions change status to rejected with reason
```

### Existing Endpoints to Extend

```
GET  /agentic-dispatch/loops
     Add query param: ?workflow_slug={slug}
     Add fields to LoopInfo: workflow_slug, workflow_step, role, model
     (ADR-002 already proposes these additions)

GET  /stream/activity
     Add change_slug field to SSE events where applicable
     This enables client-side filtering by change
```

### If Backend Changes Can't Happen Yet

The UI should be built with a **mock layer** that simulates these endpoints. The existing mock system (`VITE_USE_MOCKS=true`) should be extended with:

```typescript
// src/lib/api/mock-changes.ts
export const mockChanges: ChangeWithStatus[] = [
  {
    slug: 'add-user-authentication',
    title: 'Add User Authentication with JWT Tokens',
    status: 'implementing',
    author: 'coby',
    created_at: '2025-02-08T09:00:00Z',
    updated_at: '2025-02-11T10:42:00Z',
    files: {
      has_proposal: true,
      has_design: true,
      has_spec: true,
      has_tasks: true,
    },
    github: {
      epic_number: 42,
      epic_url: 'https://github.com/org/repo/issues/42',
      repository: 'org/repo',
      task_issues: { '1.1': 43, '1.2': 44, '2.1': 45 },
    },
    active_loops: [
      {
        loop_id: 'abc123',
        role: 'implementer',
        model: 'qwen',
        state: 'executing',
        iterations: 3,
        max_iterations: 10,
        workflow_step: 'tasks',
      },
    ],
    task_stats: { total: 7, completed: 3, failed: 0, in_progress: 1 },
  },
  {
    slug: 'refactor-database-layer',
    title: 'Refactor Database Layer for Connection Pooling',
    status: 'reviewed',
    author: 'coby',
    created_at: '2025-02-10T14:00:00Z',
    updated_at: '2025-02-11T09:30:00Z',
    files: {
      has_proposal: true,
      has_design: true,
      has_spec: true,
      has_tasks: false,
    },
    active_loops: [],
    task_stats: undefined,
  },
  {
    slug: 'fix-login-redirect',
    title: 'Fix Login Redirect Loop on Session Expiry',
    status: 'drafted',
    author: 'coby',
    created_at: '2025-02-11T08:00:00Z',
    updated_at: '2025-02-11T10:30:00Z',
    files: {
      has_proposal: true,
      has_design: false,
      has_spec: false,
      has_tasks: false,
    },
    active_loops: [
      {
        loop_id: 'def456',
        role: 'design-writer',
        model: 'claude',
        state: 'executing',
        iterations: 2,
        max_iterations: 5,
        workflow_step: 'design',
      },
    ],
    task_stats: undefined,
  },
];
```

---

## Stores

### changes.svelte.ts (NEW)

```typescript
import { api } from '$lib/api/client';

interface ChangesState {
  changes: ChangeWithStatus[];
  loading: boolean;
  error: string | null;
  selectedSlug: string | null;
}

// Reactive store using Svelte 5 runes
// Fetches changes list and joins with active loops
// Refreshes on SSE events related to workflow changes
// Provides derived getters:
//   - activeChanges (not archived/rejected)
//   - changesByStatus (grouped)
//   - changeBySlug(slug)
```

### attention.svelte.ts (NEW)

```typescript
// Derives attention items from changes + loops stores
// Attention item types:
//   - approval_needed: change.status === 'reviewed'
//   - question_pending: loop.state === 'paused' with pending question
//   - task_failed: loop.state === 'failed'
//   - task_blocked: task.status === 'blocked'
//
// Provides:
//   - items: AttentionItem[]
//   - count: number (for badge)
//   - byType: Record<AttentionType, AttentionItem[]>
```

---

## Implementation Order

### Phase 1: Board View + Changes Store (Do First)

1. Create `changes.svelte.ts` store with mock data
2. Create `attention.svelte.ts` derived store
3. Build `PipelineIndicator.svelte` component
4. Build `AgentBadge.svelte` component
5. Build `ChangeCard.svelte` component
6. Build `AttentionBanner.svelte` component
7. Build `BoardView.svelte` and wire to `/` route
8. Update `Sidebar.svelte` with new nav items and badges
9. **Test**: Board view renders with mock data, attention items show

### Phase 2: Changes Detail View

1. Create `/changes/+page.svelte` (changes list)
2. Create `/changes/[slug]/+page.svelte` (change detail)
3. Build `PipelineView.svelte` (expanded pipeline)
4. Build `DocumentPanel.svelte` (document list with preview)
5. Build `TaskList.svelte` (parsed tasks with status and agent assignment)
6. Build `ChangeTimeline.svelte` (chronological activity for one change)
7. **Test**: Navigate from board card → change detail, see pipeline + tasks + timeline

### Phase 3: Activity View Refactor

1. Create `/activity/+page.svelte` with split layout
2. Refactor existing chat components into `ChatPanel.svelte` (right side)
3. Refactor `ActivityFeed.svelte` (left side) with change slug tags
4. Build `QuestionQueue.svelte` for inline question answering
5. Add active loops summary section
6. **Test**: Real-time SSE events show in feed, chat still works, questions answerable inline

### Phase 4: Backend Wiring

1. Add `GET /api/workflow/changes` endpoint to Go service
2. Add `GET /api/workflow/changes/{slug}` endpoint
3. Add `GET /api/workflow/changes/{slug}/tasks` endpoint
4. Extend loop list endpoint with `workflow_slug` filter
5. Add `workflow_slug` to SSE activity events
6. Switch stores from mock to real API
7. **Test**: End-to-end with running SemSpec backend

---

## Styling Notes

- Keep existing design language (dark theme, vanilla CSS custom properties, Lucide icons)
- Pipeline indicator should use the existing color tokens:
  - `--color-success` for completed steps
  - `--color-accent` for in-progress steps
  - `--color-text-muted` for not-started steps
  - `--color-error` for failed steps
- ChangeCard should feel like a GitHub issue card — compact but information-dense
- AttentionBanner should use `--color-warning` background, be dismissible but persistent
- Agent badges should be subtle — small pill with icon + text, not visually dominant
- Keep the existing `StatusBadge` pattern for workflow status but create one if it doesn't exist

---

## What This Does NOT Change

- Settings view — keep as-is
- SSE/real-time infrastructure — keep as-is, just consume events differently
- API client architecture — keep existing pattern, add new endpoints
- Build/deployment — keep SvelteKit static adapter
- Design system tokens — keep all existing CSS custom properties
- The underlying SemSpec backend workflow — no Go changes to workflow logic (only new HTTP endpoints to expose existing data)

---

## Success Criteria

When this is complete, a human opening the SemSpec UI should:

1. **Immediately see** what changes exist and their pipeline status
2. **Know at a glance** what needs their attention (approvals, questions, failures)
3. **See which agents** are working on what, with progress
4. **Drill into any change** to see its full workflow pipeline, documents, tasks, and timeline
5. **Still be able to** send commands and chat with agents (in the Activity view)
6. **Feel like** they're managing a software project, not reading a chat log
