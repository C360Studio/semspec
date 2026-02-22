<script lang="ts">
	import ActivityFeed from '$lib/components/activity/ActivityFeed.svelte';
	import ChatPanel from '$lib/components/activity/ChatPanel.svelte';
	import ChatDropZone from '$lib/components/chat/ChatDropZone.svelte';
	import QuestionQueue from '$lib/components/activity/QuestionQueue.svelte';
	import Icon from '$lib/components/shared/Icon.svelte';
	import CollapsiblePanel from '$lib/components/shared/CollapsiblePanel.svelte';
	import LoopCard from '$lib/components/loops/LoopCard.svelte';
	import { AgentTimeline } from '$lib/components/timeline';
	import { loopsStore } from '$lib/stores/loops.svelte';
	import { plansStore } from '$lib/stores/plans.svelte';
	import { projectStore } from '$lib/stores/project.svelte';
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
			const activeLoop = plan.active_loops?.find((l) => l.loop_id === loop.loop_id);
			if (activeLoop) {
				return { ...loop, role: activeLoop.role };
			}
		}
		return loop;
	}));

	// Find which plan a loop belongs to
	function getPlanForLoop(loopId: string) {
		for (const plan of plansStore.all) {
			const loop = plan.active_loops?.find((l) => l.loop_id === loopId);
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
	<!-- Feed/Timeline Panel (main content, flexible) -->
	<CollapsiblePanel id="activity-feed" title="Feed" flex={true}>
		{#snippet headerActions()}
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
		{/snippet}

		<div class="panel-body">
			{#if viewMode === 'feed'}
				<ActivityFeed />
			{:else}
				<div class="timeline-content">
					<AgentTimeline loops={allLoopsForTimeline} showLegend={true} />
				</div>
			{/if}
		</div>
	</CollapsiblePanel>

	<!-- Loops Panel -->
	<CollapsiblePanel id="activity-loops" title="Loops" width="300px" minWidth="250px">
		{#snippet headerActions()}
			<span class="loops-count">{activeLoops.length}</span>
		{/snippet}

		<div class="panel-body">
			{#if activeLoops.length === 0 && pausedLoops.length === 0}
				<div class="loops-empty">
					<p>No active loops</p>
				</div>
			{:else}
				<div class="loops-list">
					{#each activeLoops as loop (loop.loop_id)}
						{@const info = getPlanForLoop(loop.loop_id)}
						<LoopCard
							{loop}
							planSlug={info?.plan.slug}
							onPause={() => handlePause(loop.loop_id)}
							onResume={() => handleResume(loop.loop_id)}
							onCancel={() => handleCancel(loop.loop_id)}
						/>
					{/each}

					{#if pausedLoops.length > 0}
						<div class="loops-divider">Paused ({pausedLoops.length})</div>
						{#each pausedLoops as loop (loop.loop_id)}
							<LoopCard
								{loop}
								onResume={() => handleResume(loop.loop_id)}
								onCancel={() => handleCancel(loop.loop_id)}
							/>
						{/each}
					{/if}
				</div>
			{/if}
		</div>
	</CollapsiblePanel>

	<!-- Chat Panel -->
	<CollapsiblePanel id="activity-chat" title="Chat" width="400px" minWidth="300px">
		<div class="chat-content">
			<div class="questions-section">
				<QuestionQueue />
			</div>

			<div class="chat-section">
				<ChatDropZone projectId={projectStore.currentProjectId}>
					<ChatPanel />
				</ChatDropZone>
			</div>
		</div>
	</CollapsiblePanel>
</div>

<style>
	.activity-view {
		display: flex;
		gap: var(--space-4);
		height: 100%;
		padding: var(--space-4);
		background: var(--color-bg-primary);
	}

	.panel-body {
		height: 100%;
		overflow: auto;
	}

	.view-toggle {
		display: flex;
		gap: var(--space-1);
	}

	.toggle-btn {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-1) var(--space-2);
		background: transparent;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		font-size: var(--font-size-xs);
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

	.timeline-content {
		padding: var(--space-2);
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
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
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

	.chat-content {
		display: flex;
		flex-direction: column;
		height: 100%;
	}

	.questions-section {
		flex-shrink: 0;
		padding: var(--space-3);
		border-bottom: 1px solid var(--color-border);
	}

	.chat-section {
		flex: 1;
		min-height: 0;
		overflow: hidden;
	}

	/* Responsive: mobile - stack panels vertically */
	@media (max-width: 900px) {
		.activity-view {
			flex-direction: column;
		}

		.activity-view :global(.collapsible-panel) {
			width: 100% !important;
			min-width: 100% !important;
			flex: none !important;
			max-height: 50vh;
		}
	}
</style>
