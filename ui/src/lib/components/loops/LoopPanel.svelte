<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import LoopCard from './LoopCard.svelte';
	import { loopsStore } from '$lib/stores/loops.svelte';
	import { activityStore } from '$lib/stores/activity.svelte';

	let collapsed = $state(false);
	let refreshInterval: ReturnType<typeof setInterval> | null = null;

	// Get latest activity for each loop
	function getLatestActivity(loopId: string) {
		return activityStore.recent.filter(a => a.loop_id === loopId).at(-1);
	}

	// Handlers for loop actions
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

	// Faster refresh when panel is open
	$effect(() => {
		if (!collapsed) {
			refreshInterval = setInterval(() => {
				loopsStore.fetch();
			}, 2000);
		}

		return () => {
			if (refreshInterval) {
				clearInterval(refreshInterval);
				refreshInterval = null;
			}
		};
	});
</script>

<aside class="loop-panel" class:collapsed>
	<button class="panel-toggle" onclick={() => collapsed = !collapsed} title={collapsed ? 'Expand' : 'Collapse'}>
		<Icon name={collapsed ? 'chevron-left' : 'chevron-right'} size={16} />
	</button>

	{#if !collapsed}
		<div class="panel-header">
			<h2>Active Loops</h2>
			<span class="loop-count">{loopsStore.active.length}</span>
		</div>

		<div class="panel-content">
			{#if loopsStore.loading && loopsStore.all.length === 0}
				<div class="loading-state">
					<Icon name="loader" size={20} />
					<span>Loading loops...</span>
				</div>
			{:else if loopsStore.active.length === 0}
				<div class="empty-state">
					<Icon name="inbox" size={24} />
					<span>No active loops</span>
					<p class="empty-hint">Start a workflow with /propose</p>
				</div>
			{:else}
				<div class="loop-list">
					{#each loopsStore.active as loop (loop.loop_id)}
						<LoopCard
							{loop}
							latestActivity={getLatestActivity(loop.loop_id)}
							onPause={() => handlePause(loop.loop_id)}
							onResume={() => handleResume(loop.loop_id)}
							onCancel={() => handleCancel(loop.loop_id)}
						/>
					{/each}
				</div>
			{/if}

			{#if loopsStore.paused.length > 0}
				<div class="section-divider">
					<span>Paused ({loopsStore.paused.length})</span>
				</div>
				<div class="loop-list">
					{#each loopsStore.paused as loop (loop.loop_id)}
						<LoopCard
							{loop}
							latestActivity={getLatestActivity(loop.loop_id)}
							onResume={() => handleResume(loop.loop_id)}
							onCancel={() => handleCancel(loop.loop_id)}
						/>
					{/each}
				</div>
			{/if}
		</div>

		<div class="panel-footer">
			<div class="connection-status" class:connected={activityStore.connected}>
				<span class="status-dot"></span>
				<span>{activityStore.connected ? 'Live' : 'Connecting...'}</span>
			</div>
		</div>
	{/if}
</aside>

<style>
	.loop-panel {
		width: var(--loop-panel-width, 280px);
		height: 100%;
		background: var(--color-bg-secondary);
		border-left: 1px solid var(--color-border);
		display: flex;
		flex-direction: column;
		flex-shrink: 0;
		position: relative;
		transition: width var(--transition-base);
	}

	.loop-panel.collapsed {
		width: 40px;
	}

	.panel-toggle {
		position: absolute;
		left: -12px;
		top: 50%;
		transform: translateY(-50%);
		width: 24px;
		height: 24px;
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-full);
		display: flex;
		align-items: center;
		justify-content: center;
		cursor: pointer;
		color: var(--color-text-muted);
		z-index: 10;
	}

	.panel-toggle:hover {
		background: var(--color-bg-elevated);
		color: var(--color-text-primary);
	}

	.panel-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		padding: var(--space-4);
		border-bottom: 1px solid var(--color-border);
	}

	.panel-header h2 {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0;
	}

	.loop-count {
		background: var(--color-accent-muted);
		color: var(--color-accent);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		padding: 2px 8px;
		border-radius: var(--radius-full);
	}

	.panel-content {
		flex: 1;
		overflow-y: auto;
		padding: var(--space-3);
	}

	.loop-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}

	.loading-state,
	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: var(--space-2);
		padding: var(--space-6);
		color: var(--color-text-muted);
		text-align: center;
	}

	.empty-hint {
		font-size: var(--font-size-xs);
		margin: 0;
	}

	.section-divider {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		margin: var(--space-4) 0 var(--space-2);
		color: var(--color-text-muted);
		font-size: var(--font-size-xs);
	}

	.section-divider::before,
	.section-divider::after {
		content: '';
		flex: 1;
		height: 1px;
		background: var(--color-border);
	}

	.panel-footer {
		padding: var(--space-3);
		border-top: 1px solid var(--color-border);
	}

	.connection-status {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.status-dot {
		width: 6px;
		height: 6px;
		border-radius: var(--radius-full);
		background: var(--color-error);
	}

	.connection-status.connected .status-dot {
		background: var(--color-success);
	}
</style>
