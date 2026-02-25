# CLAUDE.md - Semspec UI

This file provides guidance to Claude Code when working with the UI codebase.

## Project Overview

Svelte 5 + SvelteKit frontend for Semspec, connecting to the Go backend via HTTP and SSE.

## Build Commands

```bash
npm run dev          # Start dev server
npm run build        # Production build
npm run preview      # Preview production build
npm run check        # Svelte check + TypeScript
npm run lint         # ESLint + Prettier check
npm run test:e2e     # Run Playwright e2e tests
```

## E2E Testing Best Practices

### Playwright + Svelte 5 Patterns

**Hydration Detection:**
- Wait for `body.hydrated` class before interacting with components
- SvelteKit hydration must complete for `$state()` and `$derived()` to work
- Use `waitForHydration(page)` helper from `e2e/helpers/setup.ts`

**Input Methods for Svelte 5:**
- Use `pressSequentially()` for inputs with reactive detection (URL detection, autocomplete)
  - Svelte 5's `bind:value` + `oninput` requires real keystroke events
  - `fill()` bypasses event handlers that Svelte relies on for reactivity
- Use `fill()` only for simple form inputs without reactive behavior
- Never use arbitrary `waitForTimeout()` - wait for observable effects instead

**Observable Effects:**
- Wait for DOM changes (element appears/disappears) instead of arbitrary timeouts
- Svelte 5 `$derived()` updates are synchronous but may batch - wait for the UI effect
- Example: After typing URL, wait for suggestion chip to appear, not a fixed delay

**Mocking Strategy:**
- Use real Docker Compose stack for navigation, rendering, reactivity
- Mock API routes (`page.route()`) only for:
  - Error scenarios (500, 404, network failures)
  - Controlled success responses with known data
  - Slow response simulation
- Never mock Svelte component internals

**Test Structure:**
- Page Object Model in `e2e/pages/`
- Test data helpers in `e2e/helpers/testData.ts`
- Fixtures for file uploads in `e2e/fixtures/`

**Retry Policy:**
- Default: no retries (tests should be deterministic)
- If flaky: fix root cause (usually missing wait for observable effect)
- CI: 2 retries as safety net, not as primary fix

### Running E2E Tests

```bash
# Start e2e stack (NATS, backend, UI)
docker compose -f docker-compose.e2e.yml up -d

# Run all tests
npm run test:e2e

# Run specific test file
npx playwright test sources-chat.spec.ts

# Run with UI mode for debugging
npx playwright test --ui

# Run specific test by name
npx playwright test --grep "URL Detection"
```

### Mock LLM E2E Testing

Full-stack E2E tests with deterministic LLM responses. Uses the backend's mock LLM server with pre-defined JSON fixtures.

```bash
# Run hello-world mock LLM tests
npm run test:e2e:mock

# Run with Playwright UI mode
npm run test:e2e:mock:ui

# Run with specific scenario
MOCK_SCENARIO=hello-world-plan-rejection npm run test:e2e:mock
```

**Available Scenarios** (`test/e2e/fixtures/mock-responses/`):
| Scenario | Purpose |
|----------|---------|
| `hello-world` | Happy path - plan approved, tasks generated |
| `hello-world-plan-rejection` | Plan rejected once, then approved |
| `hello-world-task-rejection` | Tasks rejected once, then approved |
| `hello-world-double-rejection` | Both plan and tasks rejected once |
| `hello-world-plan-exhaustion` | Always rejects (escalation testing) |

**Mock LLM Assertions** (`e2e/helpers/mock-llm.ts`):
```typescript
const mockLLM = new MockLLMClient();
await mockLLM.waitForHealthy();
const stats = await mockLLM.getStats();
const requests = await mockLLM.getRequests('mock-planner');
```

### Debugging Flaky Tests

1. Check if test waits for observable effects (not arbitrary timeouts)
2. Ensure hydration is complete before interaction (`body.hydrated` class)
3. For reactive inputs, use `pressSequentially()` not `fill()` - Svelte needs real events
4. Add explicit waits for UI elements: `await expect(element).toBeVisible()`
5. Check if component steals focus (e.g., `onMount` with `.focus()`) interrupting typing

## Svelte 5 Patterns

### Runes System

```svelte
<script>
    let { data, onUpdate = $bindable() } = $props();
    let processed = $derived(transform(data));

    $effect(() => {
        // Side effects with cleanup
        return () => { /* cleanup */ };
    });
</script>
```

**Key Runes:**
- `$state()` - Reactive state declaration
- `$derived()` - Computed values that update automatically
- `$effect()` - Side effects that run when dependencies change
- `$props()` - Component props with destructuring
- `$bindable()` - Two-way bindable props

### Event Handling

- Events as properties: `{onclick}` not `on:click`
- Callback props over createEventDispatcher
- Example: `<button {onclick}>` or `<button onclick={handleClick}>`

### Reactivity Gotchas

**DOM Event Flow:**
- Svelte 5's `bind:value` syncs on native `input` events
- `oninput={handler}` requires real DOM events to fire
- Programmatic value changes (like Playwright's `fill()`) may not trigger handlers

**Focus Management:**
- Avoid `onMount(() => element.focus())` in components that appear during user input
- Focus stealing interrupts typing and breaks reactive detection
- Use `$effect` with conditions if focus is needed

**Derived Timing:**
- `$derived()` updates synchronously when dependencies change
- But DOM updates are batched - wait for visible effects in tests

## Directory Structure

```
ui/
├── src/
│   ├── lib/
│   │   ├── components/   # Svelte components
│   │   ├── stores/       # Svelte 5 stores (*.svelte.ts)
│   │   ├── types/        # TypeScript interfaces
│   │   └── api/          # API client
│   └── routes/           # SvelteKit routes
├── e2e/
│   ├── pages/            # Page Object Model classes
│   ├── helpers/          # Test utilities
│   └── fixtures/         # Test data files
└── static/               # Static assets
```
