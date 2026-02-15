<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import ModeIndicator from '$lib/components/board/ModeIndicator.svelte';
	import PipelineIndicator from '$lib/components/board/PipelineIndicator.svelte';
	import { plansStore } from '$lib/stores/plans.svelte';
	import { derivePlanPipeline, type PlanStage } from '$lib/types/plan';
	import { onMount } from 'svelte';

	let stageFilter = $state<string>('all');
	let sortBy = $state<'updated' | 'created'>('created');

	onMount(() => {
		plansStore.fetch();
	});

	const filteredPlans = $derived.by(() => {
		let plans = plansStore.all;

		// Filter by stage
		if (stageFilter !== 'all') {
			if (stageFilter === 'draft') {
				plans = plans.filter((p) => !p.approved);
			} else if (stageFilter === 'approved') {
				plans = plans.filter((p) => p.approved);
			} else {
				plans = plans.filter((p) => p.stage === stageFilter);
			}
		}

		// Sort creates new array
		return plans.slice().sort((a, b) => {
			const dateA = new Date(sortBy === 'created' ? a.created_at : (a.approved_at || a.created_at));
			const dateB = new Date(sortBy === 'created' ? b.created_at : (b.approved_at || b.created_at));
			return dateB.getTime() - dateA.getTime();
		});
	});

	function formatRelativeTime(dateString: string): string {
		const date = new Date(dateString);
		const now = new Date();
		const diffMs = now.getTime() - date.getTime();
		const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));
		const diffHours = Math.floor(diffMs / (1000 * 60 * 60));
		const diffMinutes = Math.floor(diffMs / (1000 * 60));

		if (diffDays > 0) return `${diffDays} day${diffDays > 1 ? 's' : ''} ago`;
		if (diffHours > 0) return `${diffHours} hour${diffHours > 1 ? 's' : ''} ago`;
		if (diffMinutes > 0) return `${diffMinutes} minute${diffMinutes > 1 ? 's' : ''} ago`;
		return 'just now';
	}

	function getStageLabel(stage: PlanStage): string {
		switch (stage) {
			case 'draft':
				return 'Draft';
			case 'planning':
				return 'Planning';
			case 'tasks':
				return 'Ready';
			case 'executing':
				return 'Executing';
			case 'complete':
				return 'Complete';
			case 'failed':
				return 'Failed';
		}
	}
</script>

<svelte:head>
	<title>Plans - Semspec</title>
</svelte:head>

<div class="plans-view">
	<header class="plans-header">
		<h1>Plans</h1>
		<a href="/activity" class="new-plan-btn">
			<Icon name="plus" size={16} />
			New Plan
		</a>
	</header>

	<div class="filters">
		<div class="filter-group">
			<label for="stage-filter">Stage:</label>
			<select id="stage-filter" bind:value={stageFilter}>
				<option value="all">All</option>
				<option value="draft">Drafts</option>
				<option value="approved">Approved</option>
				<option value="tasks">Ready to Execute</option>
				<option value="executing">Executing</option>
				<option value="complete">Complete</option>
			</select>
		</div>
		<div class="filter-group">
			<label for="sort-by">Sort:</label>
			<select id="sort-by" bind:value={sortBy}>
				<option value="created">Created</option>
				<option value="updated">Updated</option>
			</select>
		</div>
	</div>

	{#if plansStore.loading}
		<div class="loading-state">
			<Icon name="loader" size={24} class="spin" />
			<span>Loading plans...</span>
		</div>
	{:else if filteredPlans.length === 0}
		<div class="empty-state">
			<Icon name="inbox" size={48} />
			<h2>No plans found</h2>
			<p>
				{#if stageFilter !== 'all'}
					No plans in "{stageFilter}" stage.
				{:else}
					Start a new plan with <code>/plan</code> in the activity view.
				{/if}
			</p>
		</div>
	{:else}
		<div class="plans-list">
			{#each filteredPlans as plan (plan.slug)}
				{@const pipeline = derivePlanPipeline(plan)}
				<a href="/plans/{plan.slug}" class="plan-row" class:draft={!plan.approved}>
					<div class="plan-main">
						<span class="plan-slug">{plan.slug}</span>
						<ModeIndicator approved={plan.approved} compact />
						<span class="plan-stage" data-stage={plan.stage}>
							{getStageLabel(plan.stage)}
						</span>
					</div>
					<div class="plan-meta">
						Created {formatRelativeTime(plan.created_at)}
						{#if plan.approved_at}
							· Approved {formatRelativeTime(plan.approved_at)}
						{/if}
					</div>
					<div class="plan-details">
						{#if plan.approved}
							<PipelineIndicator
								plan={pipeline.plan}
								tasks={pipeline.tasks}
								execute={pipeline.execute}
								compact
							/>
						{:else}
							<span class="draft-label">Pending approval...</span>
						{/if}
						{#if plan.task_stats}
							<span class="task-count">
								{plan.task_stats.completed}/{plan.task_stats.total} tasks
							</span>
						{:else if plan.stage === 'tasks'}
							<span class="task-count ready">ready to execute</span>
						{/if}
						{#if plan.github}
							<span class="github-link">
								<Icon name="external-link" size={12} />
								GH #{plan.github.epic_number}
							</span>
						{/if}
					</div>
				</a>
			{/each}
		</div>
	{/if}
</div>

<style>
	.plans-view {
		padding: var(--space-6);
		max-width: 900px;
		margin: 0 auto;
	}

	.plans-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: var(--space-4);
	}

	.plans-header h1 {
		font-size: var(--font-size-xl);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0;
	}

	.new-plan-btn {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-accent);
		color: white;
		border-radius: var(--radius-md);
		text-decoration: none;
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
	}

	.new-plan-btn:hover {
		opacity: 0.9;
		text-decoration: none;
	}

	.filters {
		display: flex;
		gap: var(--space-4);
		margin-bottom: var(--space-4);
		padding-bottom: var(--space-4);
		border-bottom: 1px solid var(--color-border);
	}

	.filter-group {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.filter-group label {
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	.filter-group select {
		padding: var(--space-1) var(--space-2);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text-primary);
		font-size: var(--font-size-sm);
	}

	.loading-state,
	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: var(--space-3);
		padding: var(--space-12) 0;
		color: var(--color-text-muted);
		text-align: center;
	}

	.empty-state h2 {
		margin: 0;
		font-size: var(--font-size-lg);
		color: var(--color-text-primary);
	}

	.empty-state p {
		margin: 0;
	}

	.empty-state code {
		padding: 2px 6px;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-sm);
	}

	.plans-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.plan-row {
		display: block;
		padding: var(--space-4);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		text-decoration: none;
		color: inherit;
		transition: all var(--transition-fast);
	}

	.plan-row:hover {
		border-color: var(--color-accent);
		text-decoration: none;
	}

	.plan-row.draft {
		border-style: dashed;
		background: var(--color-bg-primary);
	}

	.plan-row.draft:hover {
		background: var(--color-bg-secondary);
	}

	.plan-main {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		margin-bottom: var(--space-2);
	}

	.plan-slug {
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.plan-stage {
		font-size: var(--font-size-xs);
		padding: 2px var(--space-2);
		border-radius: var(--radius-sm);
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
	}

	.plan-stage[data-stage='executing'] {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.plan-stage[data-stage='complete'] {
		background: var(--color-success-muted, rgba(34, 197, 94, 0.15));
		color: var(--color-success);
	}

	.plan-stage[data-stage='failed'] {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.15));
		color: var(--color-error);
	}

	.plan-meta {
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
		margin-bottom: var(--space-3);
	}

	.plan-details {
		display: flex;
		align-items: center;
		gap: var(--space-4);
	}

	.draft-label {
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
		font-style: italic;
	}

	.task-count {
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	.task-count.ready {
		color: var(--color-success);
	}

	.github-link {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
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
