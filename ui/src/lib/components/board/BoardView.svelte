<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import AttentionBanner from './AttentionBanner.svelte';
	import PlanKanban from './PlanKanban.svelte';
	import { goto } from '$app/navigation';
	import type { PlanWithStatus } from '$lib/types/plan';
	import type { Loop } from '$lib/types';

	interface Props {
		plans: PlanWithStatus[];
		loops: Loop[];
	}

	let { plans, loops }: Props = $props();

	function handleNewPlan() {
		goto('/plans/new');
	}
</script>

<div class="board-view">
	<AttentionBanner {plans} {loops} tasksByPlan={{}} />

	<div class="board-header">
		<h1>Plans</h1>
		<button class="new-plan-btn" onclick={handleNewPlan}>
			<Icon name="plus" size={16} />
			<span>New Plan</span>
		</button>
	</div>

	{#if plans.length === 0}
		<div class="empty-state">
			<Icon name="inbox" size={48} />
			<h2>No plans yet</h2>
			<p>Create a plan to describe what you'd like to build.</p>
			<a href="/plans/new" class="start-btn">Create Your First Plan</a>
		</div>
	{:else}
		<PlanKanban {plans} />
	{/if}
</div>

<style>
	.board-view {
		height: 100%;
		display: flex;
		flex-direction: column;
		padding: var(--space-4) var(--space-6);
	}

	.board-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: var(--space-4);
		flex-shrink: 0;
	}

	.board-header h1 {
		font-size: var(--font-size-xl);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0;
	}

	.new-plan-btn {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-4);
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

	.empty-state h2 {
		margin: 0;
		color: var(--color-text-primary);
	}

	.empty-state p {
		margin: 0;
		font-size: var(--font-size-sm);
	}

	.start-btn {
		padding: var(--space-2) var(--space-4);
		background: var(--color-accent);
		color: white;
		border-radius: var(--radius-md);
		text-decoration: none;
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
	}
</style>
