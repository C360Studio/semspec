# UI Architecture

This document captures the data flow, state management, and testing patterns for the Semspec UI. It
is grounded in the actual codebase and answers the questions that come up most often when adding new
pages or components.

## Contents

1. [Data Flow Architecture](#data-flow-architecture)
2. [When to Use Stores](#when-to-use-stores)
3. [Reactivity Anti-Patterns](#reactivity-anti-patterns)
4. [SSE Integration Pattern](#sse-integration-pattern)
5. [Two-Panel Layout](#two-panel-layout)
6. [E2E Test Tiers](#e2e-test-tiers)
7. [API Endpoint Conventions](#api-endpoint-conventions)
8. [Svelte 5 Patterns](#svelte-5-patterns)

---

## Data Flow Architecture

Three distinct mechanisms move data into the UI. Each has a specific responsibility — mixing them
is a common source of bugs.

```
┌──────────────────────────────────────────────────────────────┐
│  SvelteKit load functions                                     │
│  (+layout.server.ts, +page.ts)                                │
│                                                               │
│  • Run on server (SSR) and on navigation in the browser      │
│  • Fetch from backend via HTTP                               │
│  • Return data as props to the route component               │
│  • Re-run when invalidate('app:plans') is called             │
└──────────────────────────┬───────────────────────────────────┘
                           │ data prop
                           ▼
┌──────────────────────────────────────────────────────────────┐
│  Route components (+layout.svelte, +page.svelte)              │
│                                                               │
│  • Receive data via $props()                                  │
│  • Pass sub-slices down to child components as props         │
│  • Never re-fetch what the load function already provided    │
└──────────────────────────┬───────────────────────────────────┘
                           │ $props()
                           ▼
┌──────────────────────────────────────────────────────────────┐
│  Child components                                             │
│                                                               │
│  • Accept typed props; no store imports for request-response │
│  • Local $state() for on-demand fetches (expand/collapse)    │
└──────────────────────────────────────────────────────────────┘

  Separately, running alongside the above:

┌──────────────────────────────────────────────────────────────┐
│  SSE stores (activityStore, questionsStore)                   │
│                                                               │
│  • Wrap a persistent EventSource connection                   │
│  • Push events into $state() arrays for live display         │
│  • Trigger invalidate() when load function data goes stale   │
│  • Connected once in +layout.svelte, live for app lifetime   │
└──────────────────────────────────────────────────────────────┘
```

### Layout load: the root fetch

`ui/src/routes/+layout.server.ts` fetches plans, loops, and system health in parallel on every
navigation. The `depends()` calls register invalidation keys so any component can call
`invalidate('app:plans')` to re-run this fetch without a full page reload.

```typescript
// +layout.server.ts
export const load: LayoutServerLoad = async ({ fetch, depends }) => {
    depends('app:plans');
    depends('app:loops');
    depends('app:system');

    const [plans, loops, system] = await Promise.all([
        fetch('/plan-manager/plans')
            .then((r) => (r.ok ? (r.json() as Promise<PlanWithStatus[]>) : []))
            .catch(() => [] as PlanWithStatus[]),
        // ...
    ]);

    return { plans, loops, system };
};
```

### Page load: per-route fetch

`ui/src/routes/plans/[slug]/+page.ts` also declares `depends('app:plans')`, meaning the same
`invalidate('app:plans')` call re-runs both the layout load and the page load together. Plan detail
data (tasks, requirements, scenarios, trajectory items, reviews) is fetched in parallel using
`Promise.all`. The backend may return JSON `null` for empty collections; always null-coalesce:

```typescript
.then((r) => (r.ok ? r.json().then((d: Requirement[] | null) => d ?? []) : []))
```

### Data cascade through the component tree

The layout passes `data.plans` to `LeftPanel`; the page receives `data.plan`, `data.requirements`,
and so on. Child components receive exactly the slice they need via `$props()`. No component below
the route level should re-fetch data that the load function already provided.

```svelte
<!-- +layout.svelte: passes plans slice to the sidebar -->
<LeftPanel plans={data.plans ?? []} {activeLoopCount} />

<!-- +page.svelte: $derived keeps plan in sync with SSR data -->
const plan = $derived(data.plan);
const requirements = $derived(data.requirements);
```

---

## When to Use Stores

The decision is binary: does the data come from a **persistent connection** or from a
**request-response**?

### Justified: SSE-backed stores

`activityStore` and `questionsStore` wrap a persistent `EventSource`. They exist because the
connection must outlive any single page and because events arrive unpredictably.

- `activityStore` — `ui/src/lib/stores/activity.svelte.ts`
- `questionsStore` — `ui/src/lib/stores/questions.svelte.ts`

Both are connected once in `+layout.svelte` inside an `$effect` that cleans up on unmount.

### Not justified: caching load function data

A store that fetches `/plan-manager/plans` and caches the result duplicates the load function. It
introduces a second source of truth, makes invalidation ambiguous, and breaks SSR. Use a load
function instead.

### Not justified: polling + invalidate

```svelte
<!-- WRONG — polling is not event-driven -->
$effect(() => {
    const id = setInterval(() => invalidate('app:plans'), 5000);
    return () => clearInterval(id);
});
```

The correct pattern is to call `invalidate('app:plans')` exactly once per relevant SSE event. See
the [SSE Integration Pattern](#sse-integration-pattern) section.

### Rule of thumb

| Data source | Pattern |
|-------------|---------|
| Persistent connection (SSE, WebSocket) | Store with `$state()` |
| Request-response (REST, GraphQL) | Load function + `$props()` |
| On-demand expand/collapse in a single component | Local `$state()` in that component |

---

## Reactivity Anti-Patterns

### `setInterval` + `invalidate()`

Polling treats every user as if they are watching a live workflow. It hammers the backend
constantly, even when nothing has changed. Replace with SSE-triggered invalidation — see below.

### `$effect` that polls for data

```svelte
<!-- WRONG — polling in effect -->
$effect(() => {
    const fetch = () => loadSomeData();
    const id = setInterval(fetch, 3000);
    return () => clearInterval(id);
});
```

This has the same problem. If data changes are observable via SSE, subscribe to the event instead.
If they are not, use a manual refresh button.

### `onMount` + fetch for data already in the load function

```svelte
<!-- WRONG — duplicates what the load function already did -->
onMount(async () => {
    plan = await fetch(`/plan-manager/plans/${slug}`).then(r => r.json());
});
```

`onMount` is for browser-only side effects: setting `document.body.classList`, initialising a
canvas library, or managing focus. It is not a data-loading hook. If you find yourself fetching in
`onMount`, move it to a load function.

### Stores that wrap `fetch` calls

```typescript
// WRONG — a store is not a cache for HTTP responses
class PlanStore {
    plans = $state<Plan[]>([]);
    async load() {
        this.plans = await fetch('/plan-manager/plans').then(r => r.json());
    }
}
```

This duplicates load function logic and breaks invalidation. The load function already handles
caching, SSR, and re-running on `invalidate()`.

---

## SSE Integration Pattern

### How `activityStore.onEvent()` works

`ActivityStore` maintains a `Set<ActivityCallback>`. Calling `onEvent(fn)` adds `fn` to the set
and returns an unsubscribe function. Every incoming SSE event calls every callback synchronously.

```typescript
// activity.svelte.ts (simplified)
onEvent(callback: ActivityCallback): () => void {
    this.callbacks.add(callback);
    return () => this.callbacks.delete(callback);
}

private addEvent(event: ActivityEvent): void {
    this.recent = [...this.recent.slice(-(this.maxEvents - 1)), event];
    for (const callback of this.callbacks) {
        callback(event);
    }
}
```

### Subscribing in a page component

The plan detail page subscribes to `activityStore` in an `$effect`. When a `loop_updated` or
`loop_completed` event arrives for this plan's slug, it calls `invalidate('app:plans')` once. The
load function re-runs and the page receives fresh data.

```svelte
<!-- plans/[slug]/+page.svelte -->
$effect(() => {
    const currentSlug = slug; // capture reactively so Svelte tracks the dependency
    const unsubscribe = activityStore.onEvent((event) => {
        if (event.type !== 'loop_updated' && event.type !== 'loop_completed') return;
        if (!event.data || !currentSlug) return;
        try {
            const loopData = JSON.parse(event.data);
            if (loopData.workflow_slug === currentSlug) {
                invalidate('app:plans');
            }
        } catch {
            // event.data wasn't JSON — ignore
        }
    });
    return unsubscribe;
});
```

Key points:

- The `$effect` returns the `unsubscribe` function. Svelte calls it automatically when the
  component is destroyed.
- `invalidate('app:plans')` is called at most once per event that matches the slug. There is no
  polling.
- `loopData.workflow_slug` is the field on `LoopInfo` that identifies which plan the loop belongs
  to.

### The `questionsStore` pattern

`questionsStore` follows the same structure as `activityStore` but adds an initial REST fetch on
`connect()` to populate the list before SSE events arrive. SSE events then mutate the `$state`
array in place via `addQuestion()` / `updateQuestion()`. See
`ui/src/lib/stores/questions.svelte.ts` for the full implementation.

### SSE connection lifecycle

Both stores are connected in `+layout.svelte` inside an `$effect` guarded by
`typeof window === 'undefined'`. The `$effect` cleanup function disconnects both when the layout
is torn down.

```svelte
<!-- +layout.svelte -->
$effect(() => {
    if (typeof window === 'undefined') return;

    activityStore.connect();
    questionsStore.connect();

    const unsubscribe = activityStore.onEvent((event) => {
        messagesStore.handleActivityEvent(event);
    });

    return () => {
        activityStore.disconnect();
        questionsStore.disconnect();
        unsubscribe();
    };
});
```

`ActivityStore.connect()` creates an `EventSource` pointing at `/agentic-dispatch/activity`. On
error it closes and schedules a reconnect after 3 seconds.

---

## Two-Panel Layout

The app shell uses `ThreePanelLayout` with `hideRight={true}`, giving a persistent left sidebar and
a full-width center area.

```
┌─────────────────────┬──────────────────────────────────────────┐
│                     │                                          │
│  Left panel         │  Center panel                            │
│  Plan list + nav    │  Mode-switched content                   │
│  (260px fixed)      │  (Doc / Graph / Files)                   │
│                     │                                          │
└─────────────────────┴──────────────────────────────────────────┘
```

```svelte
<!-- +layout.svelte -->
<ThreePanelLayout
    id="app-shell"
    leftOpen={true}
    hideRight={true}
    leftWidth={260}
>
    {#snippet leftPanel()}
        <LeftPanel plans={data.plans ?? []} {activeLoopCount} />
    {/snippet}
    {#snippet centerPanel()}
        <main class="content">
            {@render children()}
        </main>
    {/snippet}
    {#snippet rightPanel()}{/snippet}
</ThreePanelLayout>
```

The right panel slot is kept empty. When a narrow sidebar was present it could not display enough
context to be useful; the center panel is now responsible for all content modes.

### Plan detail view modes

The plan detail page (`plans/[slug]/+page.svelte`) switches the center panel between three modes
via a `viewMode` state variable:

| Mode | Content | When visible |
|------|---------|--------------|
| `doc` | Goal, context, scope, requirements, reviews, timeline | Always (default) |
| `graph` | `SigmaCanvas` with plan entity neighborhood | Always |
| `files` | `PlanWorkspace` file tree | Only when `plan.approved === true` |

```svelte
type ViewMode = 'doc' | 'graph' | 'files';
let viewMode = $state<ViewMode>('doc');
```

The graph view lazy-loads entities via `graphStore.loadInitialGraph(planGraphAdapter)` on first
activation. A cleanup `$effect` clears plan-scoped graph entities when navigating away, preventing
stale data from appearing in the global `/entities` explorer.

---

## E2E Test Tiers

`playwright.config.ts` defines three test projects with explicit dependency and parallelism rules.

### Tier structure

| Project | Key | Parallelism | LLM | Dependencies |
|---------|-----|-------------|-----|--------------|
| Stateless UI | `t0` | `fullyParallel: true` | None | None |
| Mock LLM journey | `t1` | `fullyParallel: false` | Mock | `t0` |
| Real LLM journey | `t2` | `fullyParallel: false` | Real | `t0` |

**T0** tests verify that pages render, navigation works, and the UI handles backend errors
gracefully. They make no LLM calls, run on Chromium only, and are safe to parallelise.

**T1** tests drive a complete plan lifecycle using the mock LLM server. Each spec file covers one
journey (e.g., `plan-journey.spec.ts`, `plan-rejection-journey.spec.ts`). Serial execution is
required because mock LLM fixtures are consumed sequentially — if two journeys run in parallel they
race to consume the same fixture files.

**T2** tests run the same journeys against a real LLM provider. Provider is swappable via
environment variable.

### One plan per journey

Each T1 journey creates exactly one plan and drives it to completion. This eliminates fixture
contention: no two journeys ever consume mock responses at the same time, and fixture numbering
(`model.1.json`, `model.2.json`, …) maps cleanly to sequential LLM calls within a single run.

### Playwright config extract

```typescript
// playwright.config.ts
projects: [
    {
        name: 't0',
        testIgnore: [...T1_SPECS, ...T2_SPECS],
        use: { ...devices['Desktop Chrome'] },
        fullyParallel: true,
    },
    {
        name: 't1',
        testMatch: T1_SPECS,
        use: { ...devices['Desktop Chrome'] },
        fullyParallel: false,
        dependencies: ['t0'],
    },
    {
        name: 't2',
        testMatch: T2_SPECS,
        use: { ...devices['Desktop Chrome'] },
        fullyParallel: false,
        dependencies: ['t0'],
    },
],
```

### Hydration detection

Svelte 5 `$state()` and `$derived()` are not available until SvelteKit hydration completes. The
layout's `onMount` adds `document.body.classList.add('hydrated')` as a signal. Tests call
`waitForHydration(page)` before interacting with reactive components.

---

## API Endpoint Conventions

### URL structure

Backend components expose HTTP endpoints under their component name as the URL prefix:

| Component name | URL prefix | Example endpoint |
|----------------|-----------|-----------------|
| `plan-manager` | `/plan-manager` | `GET /plan-manager/plans/:slug` |
| `project-manager` | `/project-manager` | `GET /project-manager/status` |
| `agentic-dispatch` | `/agentic-dispatch` | `GET /agentic-dispatch/activity` (SSE) |
| `agentic-loop` | `/agentic-loop` | `GET /agentic-loop/trajectories` |

This naming comes directly from the semstreams component registration. Each component owns its
prefix; there is no shared API router.

### Caddy reverse proxy

In production and E2E, Caddy sits in front of both the Go backend and the SvelteKit node server:

```
browser → Caddy :8080
  /plan-manager/*      → semspec Go binary :8080
  /agentic-dispatch/*  → semspec Go binary :8080
  /project-manager/*   → semspec Go binary :8080
  /agentic-loop/*      → semspec Go binary :8080
  /graphql             → graph-gateway :8082
  /* (everything else) → SvelteKit node server :3000
```

The frontend uses relative URLs everywhere (`/plan-manager/plans`). Caddy routes them without the
UI needing to know the backend's address. See `ui/Caddyfile` for the production config and
`ui/Caddyfile.e2e` for the E2E variant.

### SSR fetch rewriting

During SSR the SvelteKit node server has no Caddy in front of it. `ui/src/hooks.server.ts`
intercepts `fetch` calls whose path starts with a known API prefix and rewrites them to
`BACKEND_URL` (default `http://semspec:8080`):

```typescript
// hooks.server.ts
const BACKEND_URL = env.BACKEND_URL || 'http://semspec:8080';

const API_PREFIXES = [
    '/plan-manager',
    '/agentic-dispatch',
    '/project-manager',
    '/message-logger',
    '/agentic-loop',
    '/graphql',
    // ...
];

export const handleFetch: HandleFetch = async ({ request, fetch }) => {
    const url = new URL(request.url);
    for (const prefix of API_PREFIXES) {
        if (url.pathname.startsWith(prefix)) {
            return fetch(`${BACKEND_URL}${url.pathname}${url.search}`, request);
        }
    }
    return fetch(request);
};
```

When adding a new API prefix, add it to both `hooks.server.ts` and `Caddyfile`.

### Null coalescing on API responses

The Go backend can return JSON `null` for empty collections instead of `[]`. Every collection fetch
must null-coalesce:

```typescript
.then((r) => (r.ok ? r.json().then((d: Requirement[] | null) => d ?? []) : []))
```

Omitting the `?? []` produces TypeScript errors downstream and runtime failures on `.map()` /
`.filter()` calls.

---

## Svelte 5 Patterns

### Runes quick reference

| Rune | Purpose | Notes |
|------|---------|-------|
| `$state()` | Reactive local state | Replaces `let` for values that drive the DOM |
| `$derived()` | Computed from other state | Runs synchronously; no async |
| `$derived.by(() => …)` | Multi-step derived with a closure | For derived values needing intermediate variables |
| `$effect()` | Side effects with cleanup | Use `return () => cleanup()` for teardown |
| `$props()` | Component props | Destructure at the top of `<script>` |
| `$bindable()` | Two-way bindable prop | Only when parent needs to write the value back |

### `$effect` with cleanup

The `return` value of an `$effect` callback is called when the effect is torn down — either because
the component is destroyed or because reactive dependencies changed and the effect is re-running.
This is the correct way to unsubscribe from SSE, clear timers, or destroy third-party libraries.

```svelte
$effect(() => {
    const unsubscribe = activityStore.onEvent((event) => {
        // handle event
    });
    return unsubscribe; // called on cleanup
});
```

### Local state for on-demand fetches

Not all data belongs in a load function. Data that is loaded conditionally (for example, when the
user expands a section) belongs in local component state:

```svelte
<script>
    let detail = $state<Detail | null>(null);
    let loading = $state(false);

    async function loadDetail(id: string) {
        loading = true;
        try {
            detail = await fetchDetail(id);
        } finally {
            loading = false;
        }
    }
</script>
```

This avoids loading data the user may never see and keeps the load function lean.

### Event handlers as properties

Svelte 5 uses event handler properties, not `on:event` directives:

```svelte
<!-- Svelte 5 -->
<button onclick={handleClick}>Click me</button>
<input oninput={handleInput} />
```

Callback props follow the same pattern. Name them with `on` prefix by convention:

```svelte
<!-- Parent -->
<MyComponent onSelect={handleSelect} />

<!-- MyComponent -->
<script>
    let { onSelect } = $props();
</script>
<button onclick={() => onSelect(item)}>Select</button>
```

### `$derived` for plan pipeline data

The plan detail page uses `$derived` to keep computed values in sync with the `data` prop without
writing any manual update logic:

```svelte
const plan = $derived(data.plan);
const pipeline = $derived(plan ? derivePlanPipeline(plan) : null);
const hasRequirements = $derived(requirements.length > 0);
const hasScenarios = $derived(Object.values(scenariosByReq).some((s) => s.length > 0));
```

When `invalidate('app:plans')` triggers a re-run of the load function, `data.plan` updates, and
all `$derived` values update with it automatically.

### User-overridable defaults

A pattern used in the plan detail page for the Reviews section: a `$derived` provides the default
based on the plan stage, and a nullable `$state` lets the user override it. The override resets
when the plan enters a decisive stage:

```svelte
const reviewsDefaultExpanded = $derived(
    plan ? REVIEW_FOCUS_STAGES.has(plan.stage) : false
);

let reviewsUserToggle = $state<boolean | null>(null);

const reviewsExpanded = $derived(
    reviewsUserToggle !== null ? reviewsUserToggle : reviewsDefaultExpanded
);

$effect(() => {
    if (plan && EXECUTING_STAGES.has(plan.stage)) {
        reviewsUserToggle = null; // reset override on decisive stage change
    }
});
```

---

## Related Documentation

| Document | Description |
|----------|-------------|
| [Architecture](03-architecture.md) | System architecture, component registration, NATS subjects |
| [Execution Pipeline](11-execution-pipeline.md) | NATS subjects, consumers, payload types |
| [Plan API](12-plan-api.md) | Plan, requirement, scenario, and change proposal HTTP API |
| [Observability](08-observability.md) | Trajectory tracking and token metrics |
| `ui/CLAUDE.md` | UI build commands, E2E testing, Svelte 5 gotchas |
