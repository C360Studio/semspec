<script lang="ts">
	import Icon from '$lib/components/shared/Icon.svelte';
	import StatusBadge from '$lib/components/shared/StatusBadge.svelte';
	import PipelineIndicator from '$lib/components/board/PipelineIndicator.svelte';
	import { changesStore } from '$lib/stores/changes.svelte';
	import { derivePipelineState } from '$lib/types/changes';
	import { onMount } from 'svelte';

	let statusFilter = $state<string>('all');
	let sortBy = $state<'updated' | 'created'>('updated');

	onMount(() => {
		changesStore.fetch();
	});

	const filteredChanges = $derived.by(() => {
		let changes = changesStore.all;

		// Filter by status
		if (statusFilter !== 'all') {
			changes = changes.filter((c) => c.status === statusFilter);
		}

		// Sort creates new array
		return changes.slice().sort((a, b) => {
			const dateA = new Date(sortBy === 'updated' ? a.updated_at : a.created_at);
			const dateB = new Date(sortBy === 'updated' ? b.updated_at : b.created_at);
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
</script>

<svelte:head>
	<title>Changes - Semspec</title>
</svelte:head>

<div class="changes-view">
	<header class="changes-header">
		<h1>Changes</h1>
		<a href="/activity" class="new-change-btn">
			<Icon name="plus" size={16} />
			New Change
		</a>
	</header>

	<div class="filters">
		<div class="filter-group">
			<label for="status-filter">Status:</label>
			<select id="status-filter" bind:value={statusFilter}>
				<option value="all">All</option>
				<option value="created">Created</option>
				<option value="drafted">Drafted</option>
				<option value="reviewed">Reviewed</option>
				<option value="approved">Approved</option>
				<option value="implementing">Implementing</option>
				<option value="complete">Complete</option>
			</select>
		</div>
		<div class="filter-group">
			<label for="sort-by">Sort:</label>
			<select id="sort-by" bind:value={sortBy}>
				<option value="updated">Updated</option>
				<option value="created">Created</option>
			</select>
		</div>
	</div>

	{#if changesStore.loading}
		<div class="loading-state">
			<Icon name="loader" size={24} class="spin" />
			<span>Loading changes...</span>
		</div>
	{:else if filteredChanges.length === 0}
		<div class="empty-state">
			<Icon name="inbox" size={48} />
			<h2>No changes found</h2>
			<p>
				{#if statusFilter !== 'all'}
					No changes with status "{statusFilter}".
				{:else}
					Start a new change with <code>/propose</code> in the activity view.
				{/if}
			</p>
		</div>
	{:else}
		<div class="changes-list">
			{#each filteredChanges as change (change.slug)}
				{@const pipeline = derivePipelineState(change.files, change.active_loops)}
				<a href="/changes/{change.slug}" class="change-row">
					<div class="change-main">
						<span class="change-slug">{change.slug}</span>
						<StatusBadge status={change.status} />
					</div>
					<div class="change-meta">
						Created {formatRelativeTime(change.created_at)} by {change.author}
					</div>
					<div class="change-details">
						<PipelineIndicator
							proposal={pipeline.proposal}
							design={pipeline.design}
							spec={pipeline.spec}
							tasks={pipeline.tasks}
							compact
						/>
						{#if change.task_stats}
							<span class="task-count">
								{change.task_stats.completed}/{change.task_stats.total} tasks
							</span>
						{:else if change.status === 'reviewed'}
							<span class="task-count awaiting">awaiting approval</span>
						{/if}
						{#if change.github}
							<span class="github-link">
								<Icon name="external-link" size={12} />
								GH #{change.github.epic_number}
							</span>
						{/if}
					</div>
				</a>
			{/each}
		</div>
	{/if}
</div>

<style>
	.changes-view {
		padding: var(--space-6);
		max-width: 900px;
		margin: 0 auto;
	}

	.changes-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: var(--space-4);
	}

	.changes-header h1 {
		font-size: var(--font-size-xl);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
		margin: 0;
	}

	.new-change-btn {
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

	.new-change-btn:hover {
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

	.changes-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.change-row {
		display: block;
		padding: var(--space-4);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		text-decoration: none;
		color: inherit;
		transition: all var(--transition-fast);
	}

	.change-row:hover {
		border-color: var(--color-accent);
		text-decoration: none;
	}

	.change-main {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		margin-bottom: var(--space-2);
	}

	.change-slug {
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.change-meta {
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
		margin-bottom: var(--space-3);
	}

	.change-details {
		display: flex;
		align-items: center;
		gap: var(--space-4);
	}

	.task-count {
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
	}

	.task-count.awaiting {
		color: var(--color-warning);
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
