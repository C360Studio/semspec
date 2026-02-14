<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import AttentionBanner from './AttentionBanner.svelte';
	import PlanCard from './PlanCard.svelte';
	import { plansStore } from '$lib/stores/plans.svelte';
	import { loopsStore } from '$lib/stores/loops.svelte';
	import { systemStore } from '$lib/stores/system.svelte';
	import { onMount } from 'svelte';

	onMount(() => {
		plansStore.fetch();
	});

	const activePlans = $derived(plansStore.active);
	const activeLoopsCount = $derived(loopsStore.active.length);
	const isHealthy = $derived(systemStore.healthy);
</script>

<div class="board-view">
	<AttentionBanner />

	<div class="board-header">
		<h1>Active Plans</h1>
		<a href="/activity" class="new-plan-link">
			<Icon name="plus" size={16} />
			<span>New Plan</span>
		</a>
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
			<p>Use <code>/propose</code> in the activity view to start a new plan.</p>
			<a href="/activity" class="start-link">Go to Activity</a>
		</div>
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
		max-width: 1200px;
		margin: 0 auto;
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

	.new-plan-link {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-accent);
		color: var(--color-bg-primary);
		border-radius: var(--radius-md);
		text-decoration: none;
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		transition: opacity var(--transition-fast);
	}

	.new-plan-link:hover {
		opacity: 0.9;
		text-decoration: none;
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
		max-width: 300px;
	}

	.empty-state code {
		padding: 2px 6px;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-sm);
		font-family: var(--font-mono);
	}

	.start-link {
		padding: var(--space-2) var(--space-4);
		background: var(--color-accent);
		color: var(--color-bg-primary);
		border-radius: var(--radius-md);
		text-decoration: none;
		font-weight: var(--font-weight-medium);
	}

	.start-link:hover {
		text-decoration: none;
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
