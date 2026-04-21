/**
 * UI truth-test harness.
 *
 * Pattern: build a deterministic backend state (plans / loops / health), stub
 * the endpoints returning it, load the page, assert what the DOM renders
 * vs that known state. Not a mock of the backend — a fixture for one
 * assertion. Every UI-truth bug in the pile (see project_ui_truth_bug_pile.md)
 * should have at least one test here so the lie can't regress.
 *
 * Rules:
 *   - Only use this helper for t0 tests. t1/t2 live stacks assert against
 *     real traffic; use helpers/api.ts for those.
 *   - Every fixture builder fills sensible defaults so specs only set the
 *     fields the assertion cares about. Explicit intent, tight diffs.
 *   - Every stub returns a Promise that Playwright awaits — never synchronous
 *     body assembly inside the route callback (race-prone).
 *   - SSE endpoints return a 200 with no body so the EventSource opens but
 *     never pushes; the UI must render from the load function alone.
 *     If a spec needs SSE traffic, add a dedicated streamer below.
 */

import type { Page, Route } from '@playwright/test';

// ---------------------------------------------------------------------------
// Fixture types — loose shapes that match the JSON wire format, not the full
// TypeScript types. Specs override fields; unspecified fields get defaults.
// ---------------------------------------------------------------------------

export interface ActiveLoopFixture {
	loop_id: string;
	role: string;
	state: string;
	iterations?: number;
	max_iterations?: number;
	current_task_id?: string;
}

export interface PlanFixtureInput {
	slug: string;
	stage: string;
	approved?: boolean;
	active_loops?: ActiveLoopFixture[];
	execution_summary?: {
		completed: number;
		failed: number;
		pending: number;
		total: number;
	};
	review_verdict?: string;
	review_summary?: string;
	goal?: string;
	title?: string;
	description?: string;
}

export interface LoopFixtureInput {
	loop_id: string;
	state: string;
	workflow_step?: string;
	iterations?: number;
	max_iterations?: number;
	task_id?: string;
	workflow_slug?: string;
	created_at?: string;
}

// ---------------------------------------------------------------------------
// Fixture builders — sensible defaults everywhere so specs are one-line.
// ---------------------------------------------------------------------------

export function planFixture(input: PlanFixtureInput): Record<string, unknown> {
	const now = new Date().toISOString();
	return {
		slug: input.slug,
		title: input.title ?? input.slug,
		description: input.description ?? 'Fixture plan',
		goal: input.goal ?? 'Fixture goal',
		stage: input.stage,
		approved: input.approved ?? false,
		created_at: now,
		updated_at: now,
		active_loops: input.active_loops ?? [],
		review_verdict: input.review_verdict ?? '',
		review_summary: input.review_summary ?? '',
		...(input.execution_summary ? { execution_summary: input.execution_summary } : {})
	};
}

export function loopFixture(input: LoopFixtureInput): Record<string, unknown> {
	return {
		loop_id: input.loop_id,
		task_id: input.task_id ?? `task-${input.loop_id}`,
		user_id: '',
		channel_type: 'e2e',
		channel_id: 'e2e-fixture',
		state: input.state,
		iterations: input.iterations ?? 0,
		max_iterations: input.max_iterations ?? 50,
		created_at: input.created_at ?? new Date().toISOString(),
		workflow_slug: input.workflow_slug ?? 'semspec-planning',
		workflow_step: input.workflow_step ?? 'develop',
		metadata: {}
	};
}

// ---------------------------------------------------------------------------
// Endpoint stubs — each intercepts one URL pattern. Unstubbed endpoints fall
// through to the server so specs can combine real + mocked behavior.
// ---------------------------------------------------------------------------

async function fulfillJSON(route: Route, body: unknown, status = 200) {
	await route.fulfill({
		status,
		contentType: 'application/json',
		body: JSON.stringify(body)
	});
}

async function fulfillEmptyStream(route: Route) {
	// Open the SSE stream and leave it idle. The UI's EventSource connects;
	// no events arrive. Tests read load-function state only. This is
	// deliberate — UI must render usefully from initial data, not depend on
	// SSE arriving before first paint.
	//
	// Caveat: page.route() intercepts browser-side fetches only. SvelteKit's
	// initial SSR render in +layout.server.ts fetches from the real backend
	// BEFORE stubs can intercept (stubs register before page.goto, but the
	// route callback fires when the browser hits the endpoint — SSR has
	// already happened server-side by then). Client-side re-fetches
	// (invalidate(), page.ts loads, SSE handlers) are intercepted correctly.
	// If a spec asserts fixture state on first-paint DOM, make sure the
	// component depends on a client-re-fetchable signal, not the SSR payload.
	await route.fulfill({
		status: 200,
		contentType: 'text/event-stream',
		body: ': connected\n\n'
	});
}

export async function stubPlans(page: Page, plans: Record<string, unknown>[]): Promise<void> {
	await page.route('**/plan-manager/plans', async (route) => {
		if (route.request().method() !== 'GET') {
			await route.fallback();
			return;
		}
		await fulfillJSON(route, plans);
	});
}

export async function stubLoops(page: Page, loops: Record<string, unknown>[]): Promise<void> {
	await page.route('**/agentic-dispatch/loops', async (route) => {
		if (route.request().method() !== 'GET') {
			await route.fallback();
			return;
		}
		await fulfillJSON(route, loops);
	});
}

export async function stubHealth(
	page: Page,
	health: { healthy: boolean; components?: unknown[] } = { healthy: true, components: [] }
): Promise<void> {
	await page.route('**/agentic-dispatch/health', async (route) => {
		if (route.request().method() !== 'GET') {
			await route.fallback();
			return;
		}
		await fulfillJSON(route, health);
	});
}

export interface ActivityEventSeed {
	loop_id: string;
	type: 'loop_created' | 'loop_updated' | 'loop_deleted' | 'loop_completed';
	timestamp?: string;
	// Any extra fields land on the ActivityEvent as-is; matches the backend's
	// flexible shape without pinning us to a rigid type here.
	[key: string]: unknown;
}

/**
 * Stub the global activity SSE endpoint. By default serves an empty stream
 * (opens, never pushes) — same behaviour as before. Pass `events` to preload
 * the stream body with one `event: activity` frame per entry so the browser's
 * EventSource fires each handler and activityStore fills deterministically.
 *
 * Ends with an SSE comment line (`: keep-alive`) rather than closing the
 * stream. Without it, route.fulfill() terminates the body, the EventSource
 * fires onerror, and activityStore.connected flips to false before assertions
 * run — which would break any spec that asserts on connection state.
 */
export async function stubActivityStream(
	page: Page,
	events: ActivityEventSeed[] = []
): Promise<void> {
	if (events.length === 0) {
		await page.route('**/agentic-dispatch/activity', fulfillEmptyStream);
		return;
	}
	await page.route('**/agentic-dispatch/activity', async (route) => {
		const frames: string[] = ['event: connected', 'data: {}', ''];
		for (const ev of events) {
			frames.push('event: activity');
			frames.push(
				`data: ${JSON.stringify({
					timestamp: ev.timestamp ?? new Date().toISOString(),
					...ev
				})}`
			);
			frames.push('');
		}
		// SSE comment — holds the connection open past the last event.
		frames.push(': keep-alive');
		await route.fulfill({
			status: 200,
			contentType: 'text/event-stream',
			body: frames.join('\n') + '\n'
		});
	});
}

export async function stubPlanStream(page: Page): Promise<void> {
	await page.route('**/plan-manager/plans/*/stream', fulfillEmptyStream);
}

export async function stubExecutionStream(page: Page): Promise<void> {
	await page.route('**/execution-manager/plans/*/stream', fulfillEmptyStream);
}

// ---------------------------------------------------------------------------
// Files view (bug #7.6) — stubs for the per-plan branches + file-diff endpoints
// backing PlanWorkspace. Specs build BranchFixture rows and register a single
// route that serves them; a companion stub fulfils per-file patch requests.
// ---------------------------------------------------------------------------

export interface BranchDiffFileFixture {
	path: string;
	status?: string;
	insertions?: number;
	deletions?: number;
	binary?: boolean;
	old_path?: string;
}

export interface BranchFixture {
	requirement_id: string;
	title: string;
	branch?: string;
	stage?: string;
	base?: string;
	files?: BranchDiffFileFixture[];
	total_insertions?: number;
	total_deletions?: number;
	diff_error?: string;
}

function materializeBranch(input: BranchFixture): Record<string, unknown> {
	const files = (input.files ?? []).map((f) => ({
		path: f.path,
		status: f.status ?? 'modified',
		insertions: f.insertions ?? 0,
		deletions: f.deletions ?? 0,
		binary: f.binary ?? false,
		old_path: f.old_path ?? ''
	}));
	const totalIns =
		input.total_insertions ??
		files.reduce((s, f) => s + (typeof f.insertions === 'number' ? f.insertions : 0), 0);
	const totalDel =
		input.total_deletions ??
		files.reduce((s, f) => s + (typeof f.deletions === 'number' ? f.deletions : 0), 0);
	return {
		requirement_id: input.requirement_id,
		title: input.title,
		branch: input.branch ?? '',
		stage: input.stage ?? 'pending',
		base: input.base ?? 'main',
		files,
		total_insertions: totalIns,
		total_deletions: totalDel,
		...(input.diff_error ? { diff_error: input.diff_error } : {})
	};
}

export async function stubPlanBranches(
	page: Page,
	slug: string,
	branches: BranchFixture[]
): Promise<void> {
	await page.route(`**/plan-manager/plans/${slug}/branches`, async (route) => {
		if (route.request().method() !== 'GET') {
			await route.fallback();
			return;
		}
		await fulfillJSON(route, branches.map(materializeBranch));
	});
}

/**
 * Stub per-file diff requests. `patches` is a map from file path to the
 * unified-diff string to return. Paths not in the map receive a generic
 * placeholder so the UI has something to render.
 */
export async function stubRequirementFileDiff(
	page: Page,
	slug: string,
	patches: Record<string, string>
): Promise<void> {
	await page.route(
		new RegExp(`/plan-manager/plans/${slug}/requirements/.+/file-diff(\\?.*)?$`),
		async (route) => {
			if (route.request().method() !== 'GET') {
				await route.fallback();
				return;
			}
			const url = new URL(route.request().url());
			const requestedPath = url.searchParams.get('path') ?? '';
			const patch = patches[requestedPath] ?? `diff placeholder for ${requestedPath}`;
			await fulfillJSON(route, { patch });
		}
	);
}

// ---------------------------------------------------------------------------
// Board-scenario bundle — most /board truth-tests need the same four stubs
// with identical no-op content. One call; specs can still add overrides.
// ---------------------------------------------------------------------------

export async function stubBoardBackend(
	page: Page,
	args: {
		plans?: Record<string, unknown>[];
		loops?: Record<string, unknown>[];
		health?: { healthy: boolean; components?: unknown[] };
		activityEvents?: ActivityEventSeed[];
	} = {}
): Promise<void> {
	await stubPlans(page, args.plans ?? []);
	await stubLoops(page, args.loops ?? []);
	if (args.health !== undefined) {
		await stubHealth(page, args.health);
	} else {
		await stubHealth(page);
	}
	await stubActivityStream(page, args.activityEvents ?? []);
}
