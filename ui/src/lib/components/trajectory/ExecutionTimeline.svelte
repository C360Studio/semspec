<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import LoopSummary from './LoopSummary.svelte';
	import TrajectoryEntryCard from './TrajectoryEntryCard.svelte';
	import { trajectoryStore } from '$lib/stores/trajectory.svelte';
	import { activityStore } from '$lib/stores/activity.svelte';
	import type { Loop } from '$lib/types';
	import type { PlanStage } from '$lib/types/plan';
	import type { TrajectoryListItem } from '$lib/types/trajectory';

	interface Props {
		/** All loops from the layout (filtered to this plan internally) */
		loops: Loop[];
		/** Plan slug for filtering */
		slug: string;
		/** Current plan stage for auto-expand logic */
		stage: PlanStage;
		/** Prefetched trajectory summaries from page load — seeded into store on mount */
		trajectoryItems?: TrajectoryListItem[];
	}

	let { loops, slug, stage, trajectoryItems = [] }: Props = $props();

	// ── Loop grouping ──────────────────────────────────────────────
	// Plan-phase loops: workflow_step is plan-related (planner, reviewer, generators)
	const PLAN_STEPS = new Set(['plan', 'plan-review', 'requirement-generation', 'scenario-generation']);

	const planLoops = $derived(
		loops
			.filter((l) => l.workflow_slug === slug && (PLAN_STEPS.has(l.workflow_step ?? '') || isPlanRole(l)))
			.sort(byCreatedAt)
	);

	const executionLoops = $derived(
		loops
			.filter((l) => l.workflow_slug === slug && !PLAN_STEPS.has(l.workflow_step ?? '') && !isPlanRole(l))
			.sort(byCreatedAt)
	);

	const hasAnyLoops = $derived(planLoops.length > 0 || executionLoops.length > 0);

	// ── Collapse state management ──────────────────────────────────
	// User-explicit toggles (sticky across state transitions)
	let userToggles = $state(new Map<string, boolean>());

	// Plan phase: auto-collapsed after scenarios_generated, auto-expanded while active
	const planPhaseActive = $derived(
		['drafting', 'approved', 'requirements_generated'].includes(stage) &&
		planLoops.some((l) => l.state === 'executing' || l.state === 'pending')
	);
	const planPhaseExpanded = $derived(
		userToggles.has('plan-phase')
			? userToggles.get('plan-phase')!
			: planPhaseActive || planLoops.length === 0
	);

	// Execution phase: auto-expanded when implementing/executing
	const execPhaseActive = $derived(
		['implementing', 'executing', 'reviewing_rollup'].includes(stage)
	);
	const execPhaseExpanded = $derived(
		userToggles.has('exec-phase')
			? userToggles.get('exec-phase')!
			: execPhaseActive
	);

	// Per-loop expand (Level 3 — trajectory entries): collapsed by default
	function isLoopExpanded(loopId: string): boolean {
		if (userToggles.has(loopId)) return userToggles.get(loopId)!;
		// Auto-expand the currently active loop
		const loop = loops.find((l) => l.loop_id === loopId);
		return loop?.state === 'executing';
	}

	function toggle(id: string) {
		const current = id === 'plan-phase' ? planPhaseExpanded
			: id === 'exec-phase' ? execPhaseExpanded
			: isLoopExpanded(id);
		const next = new Map(userToggles);
		next.set(id, !current);
		userToggles = next;
	}

	// ── Summary stats ──────────────────────────────────────────────
	function phaseStats(phaseLoops: Loop[]) {
		let totalTokens = 0;
		let totalDuration = 0;
		let llmCalls = 0;
		let toolCalls = 0;

		for (const loop of phaseLoops) {
			const traj = trajectoryStore.get(loop.loop_id);
			if (traj) {
				totalTokens += traj.total_tokens_in + traj.total_tokens_out;
				totalDuration += traj.duration;
				llmCalls += traj.steps.filter((s) => s.step_type === 'model_call').length;
				toolCalls += traj.steps.filter((s) => s.step_type === 'tool_call').length;
			}
		}
		return { totalTokens, totalDuration, llmCalls, toolCalls };
	}

	const planStats = $derived(phaseStats(planLoops));
	const execStats = $derived(phaseStats(executionLoops));

	// ── Trajectory fetching ────────────────────────────────────────
	// Fetch trajectories for visible loops
	$effect(() => {
		const loopsToFetch = [
			...(planPhaseExpanded ? planLoops : []),
			...(execPhaseExpanded ? executionLoops : [])
		];
		for (const loop of loopsToFetch) {
			if (!trajectoryStore.get(loop.loop_id) && !trajectoryStore.isLoading(loop.loop_id)) {
				trajectoryStore.fetch(loop.loop_id);
			}
		}
	});

	// Seed summary cache from page-load prefetch — re-runs when trajectoryItems updates
	// (e.g. after invalidate('app:plans') triggers a layout refresh)
	$effect(() => {
		trajectoryStore.seedFromList(trajectoryItems);
	});

	// Subscribe to SSE activity events and invalidate+refetch on loop updates
	$effect(() => {
		const allPlanLoopIds = new Set([
			...planLoops.map((l) => l.loop_id),
			...executionLoops.map((l) => l.loop_id)
		]);

		const unsubscribe = activityStore.onEvent((event) => {
			if (event.type !== 'loop_updated' && event.type !== 'loop_completed') return;
			const loopId = event.loop_id;
			if (!loopId || !allPlanLoopIds.has(loopId)) return;
			trajectoryStore.invalidate(loopId);
			trajectoryStore.fetch(loopId);
		});

		return unsubscribe;
	});

	// Safety-net poll (15s) for active loops — SSE handles real-time updates
	$effect(() => {
		const activeLoops = [...planLoops, ...executionLoops].filter(
			(l) => l.state === 'executing' || l.state === 'pending'
		);
		if (activeLoops.length === 0) return;

		const interval = setInterval(() => {
			for (const loop of activeLoops) {
				trajectoryStore.invalidate(loop.loop_id);
				trajectoryStore.fetch(loop.loop_id);
			}
		}, 15000);
		return () => clearInterval(interval);
	});

	// ── Helpers ────────────────────────────────────────────────────
	function isPlanRole(loop: Loop): boolean {
		const step = loop.workflow_step ?? '';
		return step.startsWith('plan') || step.includes('requirement-gen') || step.includes('scenario-gen');
	}

	function byCreatedAt(a: Loop, b: Loop): number {
		const ta = a.created_at ? new Date(a.created_at).getTime() : 0;
		const tb = b.created_at ? new Date(b.created_at).getTime() : 0;
		return ta - tb;
	}

	function formatTokens(count: number): string {
		if (count >= 1000) return `${(count / 1000).toFixed(1)}k`;
		return String(count);
	}

	function formatDuration(ms: number): string {
		if (ms < 1000) return `${ms}ms`;
		if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
		return `${(ms / 60000).toFixed(1)}m`;
	}

	function loopRole(loop: Loop): string {
		return loop.workflow_step ?? 'agent';
	}
</script>

{#if hasAnyLoops}
	<div class="execution-timeline">
		<!-- Plan Phase -->
		{#if planLoops.length > 0}
			<div class="phase-section">
				<button class="phase-header" onclick={() => toggle('plan-phase')}>
					<div class="phase-left">
						<Icon name={planPhaseExpanded ? 'chevron-down' : 'chevron-right'} size={14} />
						<Icon name="edit-3" size={14} />
						<span class="phase-title">Planning</span>
						<span class="phase-count">{planLoops.length} loop{planLoops.length !== 1 ? 's' : ''}</span>
					</div>
					<div class="phase-stats">
						{#if planStats.llmCalls > 0}
							<span class="stat">{planStats.llmCalls} LLM</span>
						{/if}
						{#if planStats.totalTokens > 0}
							<span class="stat">{formatTokens(planStats.totalTokens)} tok</span>
						{/if}
						{#if planStats.totalDuration > 0}
							<span class="stat">{formatDuration(planStats.totalDuration)}</span>
						{/if}
					</div>
				</button>

				{#if planPhaseExpanded}
					<div class="phase-content">
						{#each planLoops as loop (loop.loop_id)}
							<div class="loop-block">
								<button class="loop-header" onclick={() => toggle(loop.loop_id)}>
									<LoopSummary
										role={loopRole(loop)}
										state={loop.state}
										trajectory={trajectoryStore.get(loop.loop_id)}
										summary={trajectoryStore.getSummary(loop.loop_id)}
									/>
								</button>
								{#if isLoopExpanded(loop.loop_id)}
									{@const traj = trajectoryStore.get(loop.loop_id)}
									<div class="loop-entries">
										{#if trajectoryStore.isLoading(loop.loop_id) && !traj}
											<div class="loop-loading">Loading...</div>
										{:else if traj?.steps && traj.steps.length > 0}
											{#each traj.steps as entry, i (i)}
												<TrajectoryEntryCard {entry} compact />
											{/each}
										{:else}
											<div class="loop-empty">No entries yet</div>
										{/if}
									</div>
								{/if}
							</div>
						{/each}
					</div>
				{/if}
			</div>
		{/if}

		<!-- Execution Phase -->
		{#if executionLoops.length > 0}
			<div class="phase-section">
				<button class="phase-header" onclick={() => toggle('exec-phase')}>
					<div class="phase-left">
						<Icon name={execPhaseExpanded ? 'chevron-down' : 'chevron-right'} size={14} />
						<Icon name="play" size={14} />
						<span class="phase-title">Execution</span>
						<span class="phase-count">{executionLoops.length} loop{executionLoops.length !== 1 ? 's' : ''}</span>
					</div>
					<div class="phase-stats">
						{#if execStats.llmCalls > 0}
							<span class="stat">{execStats.llmCalls} LLM</span>
						{/if}
						{#if execStats.toolCalls > 0}
							<span class="stat">{execStats.toolCalls} tools</span>
						{/if}
						{#if execStats.totalTokens > 0}
							<span class="stat">{formatTokens(execStats.totalTokens)} tok</span>
						{/if}
						{#if execStats.totalDuration > 0}
							<span class="stat">{formatDuration(execStats.totalDuration)}</span>
						{/if}
					</div>
				</button>

				{#if execPhaseExpanded}
					<div class="phase-content">
						{#each executionLoops as loop (loop.loop_id)}
							<div class="loop-block">
								<button class="loop-header" onclick={() => toggle(loop.loop_id)}>
									<LoopSummary
										role={loopRole(loop)}
										state={loop.state}
										trajectory={trajectoryStore.get(loop.loop_id)}
										summary={trajectoryStore.getSummary(loop.loop_id)}
									/>
								</button>
								{#if isLoopExpanded(loop.loop_id)}
									{@const traj = trajectoryStore.get(loop.loop_id)}
									<div class="loop-entries">
										{#if trajectoryStore.isLoading(loop.loop_id) && !traj}
											<div class="loop-loading">Loading...</div>
										{:else if traj?.steps && traj.steps.length > 0}
											{#each traj.steps as entry, i (i)}
												<TrajectoryEntryCard {entry} compact />
											{/each}
										{:else}
											<div class="loop-empty">No entries yet</div>
										{/if}
									</div>
								{/if}
							</div>
						{/each}
					</div>
				{/if}
			</div>
		{/if}
	</div>
{/if}

<style>
	.execution-timeline {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}

	.phase-section {
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		overflow: hidden;
	}

	.phase-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		width: 100%;
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-tertiary);
		border: none;
		cursor: pointer;
		transition: background var(--transition-fast);
	}

	.phase-header:hover {
		background: var(--color-bg-elevated);
	}

	.phase-left {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		color: var(--color-text-secondary);
	}

	.phase-title {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.phase-count {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.phase-stats {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.stat {
		font-size: var(--font-size-xs);
		font-family: var(--font-family-mono);
		color: var(--color-text-muted);
	}

	.phase-content {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		padding: var(--space-2);
	}

	.loop-block {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.loop-header {
		display: block;
		width: 100%;
		background: none;
		border: none;
		padding: 0;
		cursor: pointer;
		text-align: left;
	}

	.loop-entries {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		padding-left: var(--space-4);
		border-left: 2px solid var(--color-border);
		margin-left: var(--space-2);
	}

	.loop-loading,
	.loop-empty {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		padding: var(--space-2);
		font-style: italic;
	}
</style>
