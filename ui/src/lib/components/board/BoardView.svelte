<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import AttentionBanner from './AttentionBanner.svelte';
	import PlanCard from './PlanCard.svelte';
	import KanbanView from '$lib/components/kanban/KanbanView.svelte';
	import { plansStore } from '$lib/stores/plans.svelte';
	import { loopsStore } from '$lib/stores/loops.svelte';
	import { systemStore } from '$lib/stores/system.svelte';
	import { chatDrawerStore } from '$lib/stores/chatDrawer.svelte';
	import { kanbanStore } from '$lib/stores/kanban.svelte';
	import { onMount } from 'svelte';

	onMount(() => {
		plansStore.fetch();
	});

	const activePlans = $derived(plansStore.active);
	const activeLoopsCount = $derived(loopsStore.active.length);
	const isHealthy = $derived(systemStore.healthy);
	const isKanban = $derived(kanbanStore.viewMode === 'kanban');

	function handleNewPlan() {
		chatDrawerStore.open({ type: 'global' });
	}
</script>

<div class="board-view">
	<AttentionBanner />

	<div class="board-header">
		<h1>{isKanban ? 'Task Board' : 'Active Plans'}</h1>
		<div class="header-actions">
			<div class="view-toggle" role="radiogroup" aria-label="Board view mode">
				<button
					class="toggle-btn"
					class:active={!isKanban}
					aria-checked={!isKanban}
					role="radio"
					title="Grid view"
					onclick={() => kanbanStore.setViewMode('grid')}
				>
					<Icon name="layout-grid" size={16} />
				</button>
				<button
					class="toggle-btn"
					class:active={isKanban}
					aria-checked={isKanban}
					role="radio"
					title="Kanban view"
					onclick={() => kanbanStore.setViewMode('kanban')}
				>
					<Icon name="columns" size={16} />
				</button>
			</div>
			<button class="new-plan-btn" onclick={handleNewPlan}>
				<Icon name="plus" size={16} />
				<span>New Plan</span>
			</button>
		</div>
	</div>

	{#if plansStore.loading}
		<div class="loading-state">
			<Icon name="loader" size={24} class="spin" />
			<span>Loading plans...</span>
		</div>
	{:else if plansStore.error}
		<div class="error-state">
			<Icon name="alert-circle" size={24} />
			<span>{plansStore.error}</span>
			<button onclick={() => plansStore.fetch()}>Retry</button>
		</div>
	{:else if activePlans.length === 0}
		<div class="empty-state">
			<Icon name="inbox" size={48} />
			<h2>No active plans</h2>
			<p>Click "New Plan" above to describe what you'd like to build.</p>
			<button class="start-btn" onclick={handleNewPlan}>Create Your First Plan</button>
		</div>
	{:else if isKanban}
		<KanbanView />
	{:else}
		<div class="plans-grid">
			{#each activePlans as plan (plan.slug)}
				<PlanCard {plan} />
			{/each}
		</div>
	{/if}

	<footer class="board-footer">
		<div class="status-item">
			<div class="status-dot" class:healthy={isHealthy}></div>
			<span>{isHealthy ? 'Connected' : 'Disconnected'}</span>
		</div>
		<div class="status-item">
			<Icon name="activity" size={14} />
			<span>{activeLoopsCount} active loop{activeLoopsCount !== 1 ? 's' : ''}</span>
		</div>
	</footer>
</div>

<style>
	.board-view {
		height: 100%;
		display: flex;
		flex-direction: column;
		padding: var(--space-6);
	}

	.board-view:not(:has(.kanban-view)) {
		max-width: 1200px;
		margin: 0 auto;
	}

	/* Fallback for browsers without :has() */
	@supports not selector(:has(*)) {
		.board-view {
			max-width: 1200px;
			margin: 0 auto;
		}
	}

	.board-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: var(--space-4);
	}

	.board-header h1 {
		font-size: var(--font-size-xl);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0;
	}

	.header-actions {
		display: flex;
		align-items: center;
		gap: var(--space-3);
	}

	.view-toggle {
		display: flex;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-md);
		border: 1px solid var(--color-border);
		overflow: hidden;
	}

	.toggle-btn {
		display: flex;
		align-items: center;
		justify-content: center;
		padding: var(--space-1) var(--space-2);
		border: none;
		background: none;
		color: var(--color-text-muted);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.toggle-btn:hover {
		color: var(--color-text-primary);
		background: var(--color-bg-elevated);
	}

	.toggle-btn.active {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.new-plan-btn {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-accent);
		color: var(--color-bg-primary);
		border: none;
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
		transition: opacity var(--transition-fast);
	}

	.new-plan-btn:hover {
		opacity: 0.9;
	}

	.plans-grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(350px, 1fr));
		gap: var(--space-4);
		flex: 1;
		overflow-y: auto;
	}

	.loading-state,
	.error-state,
	.empty-state {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: var(--space-3);
		color: var(--color-text-muted);
		text-align: center;
	}

	.error-state {
		color: var(--color-error);
	}

	.error-state button {
		padding: var(--space-2) var(--space-4);
		background: var(--color-accent);
		color: var(--color-bg-primary);
		border: none;
		border-radius: var(--radius-md);
		cursor: pointer;
	}

	.empty-state h2 {
		margin: 0;
		font-size: var(--font-size-lg);
		color: var(--color-text-primary);
	}

	.empty-state p {
		margin: 0;
		max-width: 320px;
	}

	.start-btn {
		padding: var(--space-2) var(--space-4);
		background: var(--color-accent);
		color: var(--color-bg-primary);
		border: none;
		border-radius: var(--radius-md);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
		transition: opacity var(--transition-fast);
	}

	.start-btn:hover {
		opacity: 0.9;
	}

	.board-footer {
		display: flex;
		gap: var(--space-4);
		padding-top: var(--space-4);
		border-top: 1px solid var(--color-border);
		margin-top: var(--space-4);
	}

	.status-item {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	.status-dot {
		width: 8px;
		height: 8px;
		border-radius: var(--radius-full);
		background: var(--color-error);
	}

	.status-dot.healthy {
		background: var(--color-success);
	}

	:global(.spin) {
		animation: spin 1s linear infinite;
	}

	@keyframes spin {
		from {
			transform: rotate(0deg);
		}
		to {
			transform: rotate(360deg);
		}
	}
</style>
