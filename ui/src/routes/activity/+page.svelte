<script lang="ts">
	import ActivityFeed from '$lib/components/activity/ActivityFeed.svelte';
	import ChatPanel from '$lib/components/activity/ChatPanel.svelte';
	import QuestionQueue from '$lib/components/activity/QuestionQueue.svelte';
	import Icon from '$lib/components/shared/Icon.svelte';
	import AgentBadge from '$lib/components/board/AgentBadge.svelte';
	import { AgentTimeline } from '$lib/components/timeline';
	import { loopsStore } from '$lib/stores/loops.svelte';
	import { plansStore } from '$lib/stores/plans.svelte';
	import { onMount } from 'svelte';
	import { browser } from '$app/environment';

	type ViewMode = 'feed' | 'timeline';
	let viewMode = $state<ViewMode>('feed');
	let mounted = $state(false);

	function setViewMode(mode: ViewMode) {
		viewMode = mode;
	}

	onMount(() => {
		mounted = true;
		plansStore.fetch();
	});

	const activeLoops = $derived(loopsStore.active);
	const pausedLoops = $derived(loopsStore.paused);

	// Combine all loops for timeline
	const allLoopsForTimeline = $derived([...loopsStore.all].map((loop) => {
		// Try to find role from plan's active loops
		for (const plan of plansStore.all) {
			const activeLoop = plan.active_loops.find((l) => l.loop_id === loop.loop_id);
			if (activeLoop) {
				return { ...loop, role: activeLoop.role };
			}
		}
		return loop;
	}));

	// Find which plan a loop belongs to
	function getPlanForLoop(loopId: string) {
		for (const plan of plansStore.all) {
			const loop = plan.active_loops.find((l) => l.loop_id === loopId);
			if (loop) {
				return { plan, loop };
			}
		}
		return null;
	}

	async function handlePause(loopId: string) {
		await loopsStore.sendSignal(loopId, 'pause');
		await loopsStore.fetch();
	}

	async function handleResume(loopId: string) {
		await loopsStore.sendSignal(loopId, 'resume');
		await loopsStore.fetch();
	}

	async function handleCancel(loopId: string) {
		await loopsStore.sendSignal(loopId, 'cancel');
		await loopsStore.fetch();
	}
</script>

<svelte:head>
	<title>Activity - Semspec</title>
</svelte:head>

<div class="activity-view">
	<div class="activity-left">
		<div class="view-toggle">
			{#key mounted}
				<button
					class="toggle-btn"
					class:active={viewMode === 'feed'}
					onclick={() => setViewMode('feed')}
					type="button"
				>
					<Icon name="list" size={14} />
					Feed
				</button>
				<button
					class="toggle-btn"
					class:active={viewMode === 'timeline'}
					onclick={() => setViewMode('timeline')}
					type="button"
				>
					<Icon name="activity" size={14} />
					Timeline
				</button>
			{/key}
		</div>

		{#if viewMode === 'feed'}
			<div class="feed-section">
				<ActivityFeed />
			</div>
		{:else}
			<div class="timeline-section">
				<AgentTimeline loops={allLoopsForTimeline} showLegend={true} />
			</div>
		{/if}

		<div class="loops-section">
			<div class="loops-header">
				<Icon name="activity" size={16} />
				<span>Active Loops</span>
				<span class="loops-count">{activeLoops.length}</span>
			</div>

			{#if activeLoops.length === 0 && pausedLoops.length === 0}
				<div class="loops-empty">
					<p>No active loops</p>
				</div>
			{:else}
				<div class="loops-list">
					{#each activeLoops as loop (loop.loop_id)}
						{@const info = getPlanForLoop(loop.loop_id)}
						<div class="loop-card" data-state={loop.state}>
							<div class="loop-info">
								<span class="loop-id">{loop.loop_id.slice(-8)}</span>
								{#if info}
									<a href="/plans/{info.plan.slug}" class="loop-plan">
										{info.plan.slug}
									</a>
									<AgentBadge
										role={info.loop.role}
										model={info.loop.model}
										state={info.loop.state}
										iterations={loop.iterations}
										maxIterations={loop.max_iterations}
									/>
								{:else}
									<span class="loop-progress">{loop.iterations}/{loop.max_iterations}</span>
								{/if}
							</div>
							<div class="loop-actions">
								{#if loop.state === 'executing'}
									<button
										class="loop-btn"
										onclick={() => handlePause(loop.loop_id)}
										title="Pause"
									>
										<Icon name="pause" size={12} />
									</button>
								{:else if loop.state === 'paused'}
									<button
										class="loop-btn"
										onclick={() => handleResume(loop.loop_id)}
										title="Resume"
									>
										<Icon name="play" size={12} />
									</button>
								{/if}
								<button
									class="loop-btn danger"
									onclick={() => handleCancel(loop.loop_id)}
									title="Cancel"
								>
									<Icon name="x" size={12} />
								</button>
							</div>
						</div>
					{/each}

					{#if pausedLoops.length > 0}
						<div class="loops-divider">Paused ({pausedLoops.length})</div>
						{#each pausedLoops as loop (loop.loop_id)}
							<div class="loop-card" data-state="paused">
								<div class="loop-info">
									<span class="loop-id">{loop.loop_id.slice(-8)}</span>
									<span class="loop-progress">{loop.iterations}/{loop.max_iterations}</span>
								</div>
								<div class="loop-actions">
									<button
										class="loop-btn"
										onclick={() => handleResume(loop.loop_id)}
										title="Resume"
									>
										<Icon name="play" size={12} />
									</button>
									<button
										class="loop-btn danger"
										onclick={() => handleCancel(loop.loop_id)}
										title="Cancel"
									>
										<Icon name="x" size={12} />
									</button>
								</div>
							</div>
						{/each}
					{/if}
				</div>
			{/if}
		</div>
	</div>

	<div class="activity-right">
		<div class="questions-section">
			<QuestionQueue />
		</div>

		<div class="chat-section">
			<ChatPanel />
		</div>
	</div>
</div>

<style>
	.activity-view {
		display: grid;
		grid-template-columns: 1fr 1fr;
		height: 100%;
		gap: 1px;
		background: var(--color-border);
	}

	.activity-left,
	.activity-right {
		background: var(--color-bg-primary);
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}

	.view-toggle {
		display: flex;
		gap: var(--space-1);
		padding: var(--space-3) var(--space-4);
		border-bottom: 1px solid var(--color-border);
		background: var(--color-bg-secondary);
	}

	.toggle-btn {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-3);
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.toggle-btn:hover {
		background: var(--color-bg-elevated);
		color: var(--color-text-primary);
	}

	.toggle-btn.active {
		background: var(--color-accent-muted);
		border-color: var(--color-accent);
		color: var(--color-accent);
	}

	.feed-section {
		flex: 1;
		padding: var(--space-4);
		overflow: hidden;
		min-height: 0;
	}

	.timeline-section {
		flex: 1;
		padding: var(--space-4);
		overflow-y: auto;
		min-height: 0;
	}

	.loops-section {
		flex-shrink: 0;
		border-top: 1px solid var(--color-border);
		max-height: 200px;
		overflow-y: auto;
	}

	.loops-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-3) var(--space-4);
		background: var(--color-bg-secondary);
		border-bottom: 1px solid var(--color-border);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		position: sticky;
		top: 0;
	}

	.loops-count {
		background: var(--color-accent-muted);
		color: var(--color-accent);
		padding: 1px 6px;
		border-radius: var(--radius-full);
		font-size: var(--font-size-xs);
	}

	.loops-empty {
		padding: var(--space-4);
		text-align: center;
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
	}

	.loops-empty p {
		margin: 0;
	}

	.loops-list {
		padding: var(--space-2);
	}

	.loop-card {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-secondary);
		border-radius: var(--radius-md);
		margin-bottom: var(--space-1);
		border-left: 3px solid var(--color-text-muted);
	}

	.loop-card[data-state='executing'] {
		border-left-color: var(--color-accent);
	}

	.loop-card[data-state='paused'] {
		border-left-color: var(--color-warning);
		opacity: 0.8;
	}

	.loop-info {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		flex: 1;
		min-width: 0;
	}

	.loop-id {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
		color: var(--color-text-primary);
	}

	.loop-plan {
		font-size: var(--font-size-xs);
		padding: 1px 4px;
		background: var(--color-accent-muted);
		color: var(--color-accent);
		border-radius: var(--radius-sm);
		text-decoration: none;
	}

	.loop-plan:hover {
		text-decoration: none;
		background: var(--color-accent);
		color: white;
	}

	.loop-progress {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-variant-numeric: tabular-nums;
	}

	.loop-actions {
		display: flex;
		gap: var(--space-1);
	}

	.loop-btn {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 24px;
		height: 24px;
		background: var(--color-bg-tertiary);
		border: none;
		border-radius: var(--radius-sm);
		color: var(--color-text-muted);
		cursor: pointer;
	}

	.loop-btn:hover {
		background: var(--color-bg-elevated);
		color: var(--color-text-primary);
	}

	.loop-btn.danger:hover {
		background: var(--color-error-muted);
		color: var(--color-error);
	}

	.loops-divider {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) 0;
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.loops-divider::before,
	.loops-divider::after {
		content: '';
		flex: 1;
		height: 1px;
		background: var(--color-border);
	}

	.questions-section {
		flex-shrink: 0;
		padding: var(--space-4);
		padding-bottom: 0;
	}

	.chat-section {
		flex: 1;
		padding: var(--space-4);
		overflow: hidden;
		min-height: 0;
	}
</style>
