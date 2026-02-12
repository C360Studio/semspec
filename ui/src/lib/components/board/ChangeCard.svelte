<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import StatusBadge from '$lib/components/shared/StatusBadge.svelte';
	import PipelineIndicator from './PipelineIndicator.svelte';
	import AgentBadge from './AgentBadge.svelte';
	import { derivePipelineState, type ChangeWithStatus } from '$lib/types/changes';
	import { changesStore } from '$lib/stores/changes.svelte';
	import { attentionStore } from '$lib/stores/attention.svelte';

	interface Props {
		change: ChangeWithStatus;
	}

	let { change }: Props = $props();

	const pipeline = $derived(derivePipelineState(change.files, change.active_loops));
	const attentionItems = $derived(attentionStore.forChange(change.slug));
	const needsApproval = $derived(change.status === 'reviewed');

	async function handleApprove(e: Event) {
		e.preventDefault();
		e.stopPropagation();
		await changesStore.approve(change.slug);
	}
</script>

<a href="/changes/{change.slug}" class="change-card" class:needs-attention={attentionItems.length > 0}>
	<div class="card-header">
		<h3 class="change-title">{change.slug}</h3>
		<StatusBadge status={change.status} />
	</div>

	<div class="pipeline-row">
		<PipelineIndicator
			proposal={pipeline.proposal}
			design={pipeline.design}
			spec={pipeline.spec}
			tasks={pipeline.tasks}
		/>
		{#if change.task_stats}
			<span class="task-count">
				{change.task_stats.completed}/{change.task_stats.total} tasks
			</span>
		{/if}
	</div>

	{#if change.active_loops.length > 0}
		<div class="agents-row">
			{#each change.active_loops as loop}
				<AgentBadge
					role={loop.role}
					model={loop.model}
					state={loop.state}
					iterations={loop.iterations}
					maxIterations={loop.max_iterations}
				/>
			{/each}
		</div>
	{/if}

	{#if needsApproval}
		<div class="attention-row">
			<Icon name="alert-triangle" size={14} />
			<span>Needs approval to generate tasks</span>
			<button class="approve-btn" onclick={handleApprove}>Approve</button>
		</div>
	{/if}

	{#if change.github}
		<div class="github-row">
			<Icon name="external-link" size={12} />
			<span>GH #{change.github.epic_number}</span>
		</div>
	{/if}
</a>

<style>
	.change-card {
		display: block;
		padding: var(--space-4);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		text-decoration: none;
		color: inherit;
		transition: all var(--transition-fast);
	}

	.change-card:hover {
		border-color: var(--color-accent);
		background: var(--color-bg-tertiary);
		text-decoration: none;
	}

	.change-card:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
		border-color: var(--color-accent);
	}

	.change-card.needs-attention {
		border-left: 3px solid var(--color-warning);
	}

	.card-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: var(--space-3);
	}

	.change-title {
		font-size: var(--font-size-base);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0;
	}

	.pipeline-row {
		display: flex;
		align-items: center;
		gap: var(--space-4);
		margin-bottom: var(--space-3);
	}

	.task-count {
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	.agents-row {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		margin-bottom: var(--space-3);
	}

	.attention-row {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-warning-muted, rgba(245, 158, 11, 0.1));
		border-radius: var(--radius-md);
		font-size: var(--font-size-sm);
		color: var(--color-warning);
		margin-bottom: var(--space-2);
	}

	.approve-btn {
		margin-left: auto;
		padding: var(--space-1) var(--space-3);
		background: var(--color-warning);
		color: var(--color-bg-primary);
		border: none;
		border-radius: var(--radius-md);
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		cursor: pointer;
		transition: opacity var(--transition-fast);
	}

	.approve-btn:hover {
		opacity: 0.9;
	}

	.github-row {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}
</style>
