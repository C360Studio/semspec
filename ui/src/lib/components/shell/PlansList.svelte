<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import PipelineIndicator from '$lib/components/board/PipelineIndicator.svelte';
	import { derivePlanPipeline, type PlanWithStatus } from '$lib/types/plan';

	type PlanFilter = 'all' | 'active' | 'draft' | 'complete';

	interface Props {
		plans: PlanWithStatus[];
	}

	let { plans }: Props = $props();

	let planFilter = $state<PlanFilter>('all');

	const filteredPlans = $derived.by(() => {
		let filtered = plans;
		const f = planFilter;

		if (f === 'active') {
			filtered = filtered.filter(
				(p) => p.approved && !['complete', 'failed', 'archived'].includes(p.stage)
			);
		} else if (f === 'draft') {
			filtered = filtered.filter((p) => !p.approved);
		} else if (f === 'complete') {
			filtered = filtered.filter((p) => ['complete', 'failed'].includes(p.stage));
		}

		return filtered.sort(
			(a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
		);
	});

	const filters: { value: typeof planFilter; label: string }[] = [
		{ value: 'all', label: 'All' },
		{ value: 'active', label: 'Active' },
		{ value: 'draft', label: 'Drafts' },
		{ value: 'complete', label: 'Done' }
	];

	// New Plan button is now a link — no handler needed

	function getStageLabel(stage: string): string {
		const labels: Record<string, string> = {
			draft: 'Draft',
			drafting: 'Draft',
			planning: 'Planning',
			ready_for_approval: 'Review',
			reviewed: 'Reviewed',
			needs_changes: 'Changes',
			approved: 'Approved',
			requirements_generated: 'Reqs',
			scenarios_generated: 'Scenarios',
			ready_for_execution: 'Ready',
			implementing: 'Running',
			executing: 'Running',
			reviewing_rollup: 'Reviewing',
			complete: 'Done',
			failed: 'Failed'
		};
		return labels[stage] ?? stage;
	}
</script>

<div class="plans-list">
	<div class="list-header">
		<div class="filter-chips" role="radiogroup" aria-label="Filter plans">
			{#each filters as f}
				<button
					class="chip"
					class:active={planFilter === f.value}
					role="radio"
					aria-checked={planFilter === f.value}
					onclick={() => (planFilter = f.value)}
				>
					{f.label}
				</button>
			{/each}
		</div>
		<a href="/plans/new" class="new-plan-btn" title="New Plan">
			<Icon name="plus" size={14} />
		</a>
	</div>

	{#if filteredPlans.length === 0}
		<div class="empty">
			<Icon name="inbox" size={24} />
			<span>No plans</span>
		</div>
	{:else}
		<div class="list-items">
			{#each filteredPlans as plan (plan.slug)}
				{@const pipeline = derivePlanPipeline(plan)}
				{@const loopCount = (plan.active_loops ?? []).length}
				<a href="/plans/{plan.slug}" class="plan-item" class:draft={!plan.approved}>
					<div class="item-top">
						<span class="slug">{plan.slug}</span>
						<span class="stage" data-stage={plan.stage}>{getStageLabel(plan.stage)}</span>
					</div>
					{#if plan.approved}
						<PipelineIndicator
							plan={pipeline.plan}
							requirements={pipeline.requirements}
							execute={pipeline.execute}
							compact
						/>
					{/if}
					{#if loopCount > 0}
						<div class="loops-indicator">
							<Icon name="activity" size={10} />
							<span>{loopCount} loop{loopCount !== 1 ? 's' : ''}</span>
						</div>
					{/if}
				</a>
			{/each}
		</div>
	{/if}
</div>

<style>
	.plans-list {
		display: flex;
		flex-direction: column;
		height: 100%;
		overflow: hidden;
	}

	.list-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		border-bottom: 1px solid var(--color-border);
	}

	.filter-chips {
		display: flex;
		gap: 2px;
		flex: 1;
	}

	.chip {
		padding: 2px var(--space-2);
		font-size: var(--font-size-xs);
		border: none;
		background: none;
		color: var(--color-text-muted);
		border-radius: var(--radius-sm);
		cursor: pointer;
	}

	.chip:hover {
		background: var(--color-bg-tertiary);
	}

	.chip.active {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.new-plan-btn {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 24px;
		height: 24px;
		padding: 0;
		border: 1px solid var(--color-border);
		background: var(--color-bg-secondary);
		color: var(--color-text-muted);
		border-radius: var(--radius-sm);
		cursor: pointer;
	}

	.new-plan-btn:hover {
		background: var(--color-accent);
		color: var(--color-bg-primary);
		border-color: var(--color-accent);
	}

	.list-items {
		flex: 1;
		overflow-y: auto;
		padding: var(--space-2);
	}

	.plan-item {
		display: block;
		padding: var(--space-2) var(--space-3);
		border-radius: var(--radius-md);
		text-decoration: none;
		color: inherit;
		transition: background var(--transition-fast);
	}

	.plan-item:hover {
		background: var(--color-bg-tertiary);
		text-decoration: none;
	}

	.plan-item.draft {
		opacity: 0.7;
	}

	.item-top {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		margin-bottom: 2px;
	}

	.slug {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.stage {
		font-size: 10px;
		padding: 1px var(--space-1);
		border-radius: var(--radius-sm);
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
		white-space: nowrap;
	}

	.stage[data-stage='implementing'],
	.stage[data-stage='executing'] {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.stage[data-stage='complete'] {
		background: var(--color-success-muted, rgba(34, 197, 94, 0.15));
		color: var(--color-success);
	}

	.stage[data-stage='failed'] {
		background: var(--color-error-muted, rgba(239, 68, 68, 0.15));
		color: var(--color-error);
	}

	.loops-indicator {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: 10px;
		color: var(--color-accent);
		margin-top: 2px;
	}

	.empty {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-8) var(--space-4);
		color: var(--color-text-muted);
		font-size: var(--font-size-sm);
	}
</style>
