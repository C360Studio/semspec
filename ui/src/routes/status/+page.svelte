<script lang="ts">
	/**
	 * /status — Ops glass for active runs.
	 *
	 * Answers "is this thing working?" for a 30-minute Gemini run without
	 * requiring a drill-down into a specific plan.
	 *
	 * All data comes from $derived.by() joining the layout's load data
	 * (plans, loops) with the globally-connected stores (activityStore,
	 * feedStore). No fetches, no SSE subscriptions, no setInterval here
	 * — the layout already owns all of that. This page is pure derived state.
	 *
	 * Widgets:
	 *   1. Run banner — one per implementing plan: slug · stage · execution
	 *      summary · active agent count.
	 *   2. Agent strip — one card per active loop across implementing plans:
	 *      role · turn counter · idle seconds.
	 *   3. Activity pulse — last event age + window counters (created/
	 *      updated/completed).
	 *   4. Health dots — activity SSE only; Feed SSE is plan-scoped and
	 *      intentionally not opened here.
	 */
	import { activityStore } from '$lib/stores/activity.svelte';
	import type { PageData } from './$types';
	import type { PlanWithStatus, ActiveLoop } from '$lib/types/plan';
	import type { ActivityEvent } from '$lib/types';

	interface Props {
		data: PageData;
	}

	let { data }: Props = $props();

	// ── Tick for real-time idle-second display ─────────────────────────────
	// This is a presentation clock, not a data fetch. It makes the "idle Xs"
	// counters count up live instead of freezing at first paint.
	let now = $state(Date.now());
	$effect(() => {
		const id = setInterval(() => {
			now = Date.now();
		}, 1000);
		return () => clearInterval(id);
	});

	// ── Derived: implementing plans only ──────────────────────────────────
	// statusPlans comes from the page's own +page.ts client-side load,
	// which Playwright stubs can intercept. Falls back to the layout's plans
	// for production use (where they're always the same source).
	const implementingPlans = $derived.by((): PlanWithStatus[] => {
		const plans = (data.statusPlans ?? data.plans ?? []) as PlanWithStatus[];
		return plans.filter(
			(p) => p.stage === 'implementing' || p.stage === 'executing'
		);
	});

	const hasRuns = $derived(implementingPlans.length > 0);

	// ── Derived: all active loops across implementing plans ───────────────
	// "Active" means the loop appears in a plan's active_loops array. We pool
	// them into a flat list for the agent strip; each entry carries the plan
	// slug for context.
	interface AgentRow {
		loop: ActiveLoop;
		planSlug: string;
		idleMs: number | null;
	}

	const allAgentRows = $derived.by((): AgentRow[] => {
		void now; // clock dep so idle seconds update live
		const rows: AgentRow[] = [];
		for (const plan of implementingPlans) {
			for (const loop of plan.active_loops ?? []) {
				const lastSeen = activityStore.loopLastSeen.get(loop.loop_id);
				const idleMs = lastSeen !== undefined ? now - lastSeen : null;
				rows.push({ loop, planSlug: plan.slug, idleMs });
			}
		}
		return rows;
	});

	// ── Derived: activity pulse ───────────────────────────────────────────
	// Find the most-recent loopLastSeen across the entire store to answer
	// "when did ANYTHING last happen?"
	const lastActivityMs = $derived.by((): number | null => {
		void now; // clock dep
		let max: number | null = null;
		for (const ts of activityStore.loopLastSeen.values()) {
			if (max === null || ts > max) max = ts;
		}
		return max;
	});

	const lastActivityAgo = $derived.by((): string => {
		if (lastActivityMs === null) return 'no recent activity';
		const s = Math.floor((now - lastActivityMs) / 1000);
		if (s < 1) return 'just now';
		if (s < 60) return `${s}s ago`;
		const m = Math.floor(s / 60);
		return m < 60 ? `${m}m ago` : `${Math.floor(m / 60)}h ago`;
	});

	// Window counters from activityStore.recent (last N events as configured).
	const activityWindowCounts = $derived.by(() => {
		const events: ActivityEvent[] = activityStore.recent;
		let created = 0;
		let updated = 0;
		let completed = 0;
		for (const e of events) {
			if (e.type === 'loop_created') created++;
			else if (e.type === 'loop_updated') updated++;
			else if (e.type === 'loop_deleted') completed++;
		}
		return { created, updated, completed };
	});

	// ── Helpers ───────────────────────────────────────────────────────────

	const STALE_THRESHOLD_MS = 90 * 1000;

	function idleLabel(idleMs: number | null): string {
		if (idleMs === null) return 'starting';
		const s = Math.floor(idleMs / 1000);
		if (s < 1) return 'live';
		if (s < 60) return `${s}s ago`;
		const m = Math.floor(s / 60);
		return m < 60 ? `${m}m ago` : `${Math.floor(m / 60)}h ago`;
	}

	function elapsedLabel(ms: number): string {
		const s = Math.floor(ms / 1000);
		if (s < 60) return `${s}s`;
		const m = Math.floor(s / 60);
		if (m < 60) return `${m}m`;
		const h = Math.floor(m / 60);
		return m % 60 === 0 ? `${h}h` : `${h}h ${m % 60}m`;
	}
</script>

<svelte:head>
	<title>Status — Semspec</title>
</svelte:head>

<div class="status-page">
	<header class="page-header">
		<h1 class="page-title">Run Status</h1>
		<p class="page-subtitle">Live view across all active execution runs</p>
	</header>

	<!-- ── Health dots ─────────────────────────────────────────────────── -->
	<section class="health-section" aria-label="Stream health">
		<div class="health-row">
			<span
				class="health-dot"
				class:connected={activityStore.connected}
				data-testid="status-health-dot"
				title="Activity SSE — global loop ticks"
				aria-label="Activity stream {activityStore.connected ? 'connected' : 'disconnected'}"
			></span>
			<span class="health-label">Activity</span>
			<!--
				Feed SSE is plan-scoped (connected per plan detail page). It's
				intentionally NOT open on /status because no plan is selected
				here. Surfacing a disconnected dot would misrepresent health;
				we only show streams this route actually depends on.
			-->
		</div>
	</section>

	<!-- ── Activity pulse ──────────────────────────────────────────────── -->
	<section class="pulse-section" aria-label="Activity pulse">
		<div class="pulse-row">
			<span class="pulse-label">Last event</span>
			<span class="pulse-value">{lastActivityAgo}</span>
			<span class="pulse-divider" aria-hidden="true">·</span>
			<span class="pulse-counter" title="Loop creates in window">{activityWindowCounts.created} created</span>
			<span class="pulse-counter" title="Loop updates in window">{activityWindowCounts.updated} ticks</span>
			<span class="pulse-counter" title="Loop completions in window">{activityWindowCounts.completed} done</span>
		</div>
	</section>

	<!-- ── Empty state ─────────────────────────────────────────────────── -->
	{#if !hasRuns}
		<div class="empty-state" data-testid="status-empty">
			<span class="empty-icon" aria-hidden="true">○</span>
			<p class="empty-title">No runs in flight</p>
			<p class="empty-body">
				Plans in the <em>implementing</em> stage will appear here with live agent activity.
			</p>
		</div>
	{:else}
		<!-- ── Run banners ──────────────────────────────────────────────── -->
		<section class="runs-section" aria-label="Active runs">
			{#each implementingPlans as plan (plan.slug)}
				{@const summary = plan.execution_summary}
				{@const activeLoopCount = (plan.active_loops ?? []).length}
				{@const approvedAt = plan.approved_at ?? plan.created_at}
				{@const elapsed = approvedAt ? elapsedLabel(now - Date.parse(approvedAt)) : ''}
				<div class="run-banner" data-testid="status-run-banner" aria-label="Run: {plan.slug}">
					<div class="banner-header">
						<a class="banner-slug" href="/plans/{plan.slug}">{plan.slug}</a>
						<span class="banner-stage" data-stage={plan.stage}>{plan.stage}</span>
						{#if elapsed}
							<span class="banner-elapsed" title="Elapsed since last update">{elapsed}</span>
						{/if}
					</div>

					{#if summary}
						<div class="banner-summary">
							<span class="summary-item" title="Completed requirements">
								{summary.completed} done
							</span>
							<span class="summary-sep" aria-hidden="true">·</span>
							<span
								class="summary-item"
								class:has-failed={summary.failed > 0}
								data-status={summary.failed > 0 ? 'failed' : undefined}
								title="Failed requirements"
							>
								{summary.failed} failed
							</span>
							<span class="summary-sep" aria-hidden="true">·</span>
							<span class="summary-item" title="Pending requirements">
								{summary.pending} pending
							</span>
							<span class="summary-sep" aria-hidden="true">·</span>
							<span class="summary-item summary-total" title="Total requirements">
								{summary.total} total
							</span>
							<span class="summary-sep" aria-hidden="true">·</span>
							<span class="summary-item" title="Active agent loops">
								{activeLoopCount} of {summary.total} agents active
							</span>
						</div>
					{/if}
				</div>
			{/each}
		</section>

		<!-- ── Agent strip ──────────────────────────────────────────────── -->
		{#if allAgentRows.length > 0}
			<section class="agents-section" aria-label="Active agents">
				<h2 class="section-heading">Agents</h2>
				<div class="agent-grid">
					{#each allAgentRows as row (row.loop.loop_id)}
						{@const isStale = row.idleMs !== null && row.idleMs > STALE_THRESHOLD_MS}
						<div
							class="agent-card"
							class:stale={isStale}
							data-testid="status-agent-card"
							aria-label="{row.loop.role ?? 'agent'} — {row.planSlug}"
						>
							<div class="agent-header">
								<span class="agent-dot" class:pulse={!isStale} aria-hidden="true"></span>
								<span class="agent-role">{row.loop.role ?? 'agent'}</span>
								<span class="agent-plan" title="Plan">{row.planSlug}</span>
							</div>
							<div class="agent-meta">
								{#if row.loop.iterations !== undefined && row.loop.max_iterations}
									<span class="agent-turns" title="Turn counter">
										turn {row.loop.iterations}/{row.loop.max_iterations}
									</span>
								{:else if row.loop.iterations !== undefined}
									<span class="agent-turns">turn {row.loop.iterations}</span>
								{/if}
								<span class="agent-idle" class:stale-text={isStale} title="Time since last activity tick">
									{idleLabel(row.idleMs)}
								</span>
							</div>
							{#if isStale}
								<div class="agent-warning" role="alert" aria-live="polite">
									Idle &gt; {Math.floor(STALE_THRESHOLD_MS / 1000)}s — may be stuck
								</div>
							{/if}
						</div>
					{/each}
				</div>
			</section>
		{/if}
	{/if}
</div>

<style>
	.status-page {
		max-width: 960px;
		margin: 0 auto;
		padding: var(--space-6, 1.5rem) var(--space-4, 1rem);
		display: flex;
		flex-direction: column;
		gap: var(--space-5, 1.25rem);
	}

	/* ── Page header ─────────────────────────────────────── */
	.page-header {
		border-bottom: 1px solid var(--color-border);
		padding-bottom: var(--space-4, 1rem);
	}

	.page-title {
		font-size: var(--font-size-2xl, 1.5rem);
		font-weight: var(--font-weight-semibold, 600);
		color: var(--color-text-primary);
		margin: 0 0 var(--space-1, 0.25rem);
	}

	.page-subtitle {
		font-size: var(--font-size-sm, 0.875rem);
		color: var(--color-text-muted);
		margin: 0;
	}

	/* ── Health dots ─────────────────────────────────────── */
	.health-section {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg, 8px);
		padding: var(--space-3, 0.75rem) var(--space-4, 1rem);
	}

	.health-row {
		display: flex;
		align-items: center;
		gap: var(--space-3, 0.75rem);
		flex-wrap: wrap;
	}

	.health-dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--color-error);
		flex-shrink: 0;
		transition: background 0.3s;
	}

	.health-dot.connected {
		background: var(--color-success);
	}

	.health-label {
		font-size: var(--font-size-xs, 0.75rem);
		color: var(--color-text-muted);
	}

	/* ── Activity pulse ──────────────────────────────────── */
	.pulse-section {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg, 8px);
		padding: var(--space-3, 0.75rem) var(--space-4, 1rem);
	}

	.pulse-row {
		display: flex;
		align-items: center;
		gap: var(--space-3, 0.75rem);
		font-size: var(--font-size-xs, 0.75rem);
		flex-wrap: wrap;
	}

	.pulse-label {
		color: var(--color-text-muted);
	}

	.pulse-value {
		font-family: var(--font-family-mono);
		color: var(--color-text-primary);
	}

	.pulse-divider {
		color: var(--color-text-muted);
	}

	.pulse-counter {
		font-family: var(--font-family-mono);
		color: var(--color-text-secondary);
		padding: 1px 6px;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-sm, 4px);
	}

	/* ── Empty state ─────────────────────────────────────── */
	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		padding: var(--space-8, 2rem);
		text-align: center;
		color: var(--color-text-muted);
		border: 1px dashed var(--color-border);
		border-radius: var(--radius-lg, 8px);
		gap: var(--space-3, 0.75rem);
	}

	.empty-icon {
		font-size: 2.5rem;
		opacity: 0.3;
	}

	.empty-title {
		font-size: var(--font-size-lg, 1.125rem);
		font-weight: var(--font-weight-semibold, 600);
		color: var(--color-text-secondary);
		margin: 0;
	}

	.empty-body {
		font-size: var(--font-size-sm, 0.875rem);
		max-width: 360px;
		margin: 0;
	}

	/* ── Run banners ─────────────────────────────────────── */
	.runs-section {
		display: flex;
		flex-direction: column;
		gap: var(--space-3, 0.75rem);
	}

	.run-banner {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-left: 3px solid var(--color-accent);
		border-radius: var(--radius-lg, 8px);
		padding: var(--space-4, 1rem);
	}

	.banner-header {
		display: flex;
		align-items: center;
		gap: var(--space-3, 0.75rem);
		margin-bottom: var(--space-2, 0.5rem);
		flex-wrap: wrap;
	}

	.banner-slug {
		font-size: var(--font-size-base, 1rem);
		font-weight: var(--font-weight-semibold, 600);
		color: var(--color-accent);
		text-decoration: none;
		font-family: var(--font-family-mono);
	}

	.banner-slug:hover {
		text-decoration: underline;
	}

	.banner-stage {
		font-size: var(--font-size-xs, 0.75rem);
		color: var(--color-text-muted);
		padding: 2px 8px;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-full, 9999px);
	}

	.banner-stage[data-stage='implementing'],
	.banner-stage[data-stage='executing'] {
		color: var(--color-accent);
		background: var(--color-accent-muted);
	}

	.banner-elapsed {
		font-size: var(--font-size-xs, 0.75rem);
		font-family: var(--font-family-mono);
		color: var(--color-text-muted);
		margin-left: auto;
	}

	.banner-summary {
		display: flex;
		align-items: center;
		gap: var(--space-2, 0.5rem);
		font-size: var(--font-size-xs, 0.75rem);
		flex-wrap: wrap;
	}

	.summary-item {
		font-family: var(--font-family-mono);
		color: var(--color-text-secondary);
	}

	.summary-item.has-failed {
		color: var(--color-error);
		font-weight: var(--font-weight-semibold, 600);
	}

	.summary-total {
		color: var(--color-text-muted);
	}

	.summary-sep {
		color: var(--color-text-muted);
	}

	/* ── Section heading ─────────────────────────────────── */
	.section-heading {
		font-size: var(--font-size-sm, 0.875rem);
		font-weight: var(--font-weight-semibold, 600);
		color: var(--color-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.08em;
		margin: 0 0 var(--space-3, 0.75rem);
	}

	/* ── Agent strip ─────────────────────────────────────── */
	.agents-section {
		display: flex;
		flex-direction: column;
	}

	.agent-grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
		gap: var(--space-3, 0.75rem);
	}

	.agent-card {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg, 8px);
		padding: var(--space-3, 0.75rem);
		display: flex;
		flex-direction: column;
		gap: var(--space-2, 0.5rem);
	}

	.agent-card.stale {
		border-color: var(--color-warning);
	}

	.agent-header {
		display: flex;
		align-items: center;
		gap: var(--space-2, 0.5rem);
	}

	.agent-dot {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: var(--color-accent);
		flex-shrink: 0;
	}

	.agent-card.stale .agent-dot {
		background: var(--color-warning);
	}

	.agent-dot.pulse {
		animation: agent-pulse 1.8s ease-in-out infinite;
	}

	@keyframes agent-pulse {
		0%,
		100% {
			opacity: 1;
			transform: scale(1);
		}
		50% {
			opacity: 0.4;
			transform: scale(1.4);
		}
	}

	@media (prefers-reduced-motion: reduce) {
		.agent-dot.pulse {
			animation: none;
		}
	}

	.agent-role {
		font-weight: var(--font-weight-medium, 500);
		font-size: var(--font-size-sm, 0.875rem);
		color: var(--color-text-primary);
	}

	.agent-plan {
		font-size: var(--font-size-xs, 0.75rem);
		color: var(--color-text-muted);
		font-family: var(--font-family-mono);
		margin-left: auto;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		max-width: 80px;
	}

	.agent-meta {
		display: flex;
		align-items: center;
		gap: var(--space-2, 0.5rem);
		font-size: var(--font-size-xs, 0.75rem);
	}

	.agent-turns {
		font-family: var(--font-family-mono);
		color: var(--color-text-secondary);
		padding: 1px 5px;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-sm, 4px);
	}

	.agent-idle {
		font-family: var(--font-family-mono);
		color: var(--color-text-muted);
	}

	.agent-idle.stale-text {
		color: var(--color-warning);
		font-weight: var(--font-weight-semibold, 600);
	}

	.agent-warning {
		font-size: var(--font-size-xs, 0.75rem);
		color: var(--color-warning);
		padding: 2px 6px;
		background: var(--color-warning-muted);
		border-radius: var(--radius-sm, 4px);
	}
</style>
