<script lang="ts">
	/**
	 * Trajectory Explorer — list of recent agent loop trajectories.
	 *
	 * Left panel: filter sidebar (status, type)
	 * Center panel: chronological trajectory list
	 * Right panel: preview of selected trajectory (collapsed by default)
	 */

	import { invalidate } from '$app/navigation';
	import ThreePanelLayout from '$lib/components/layout/ThreePanelLayout.svelte';
	import Icon from '$lib/components/shared/Icon.svelte';
	import TrajectoryPanel from '$lib/components/trajectory/TrajectoryPanel.svelte';
	import { activityStore } from '$lib/stores/activity.svelte';
	import type { PageData } from './$types';
	import type { TrajectoryListItem } from '$lib/types/trajectory';

	interface Props {
		data: PageData;
	}

	let { data }: Props = $props();

	const items = $derived(data.trajectories);

	// Filter state
	let outcomeFilter = $state<string>('all');
	let roleFilter = $state<string>('all');
	let selectedLoopId = $state<string | null>(null);

	let refreshing = $state(false);

	// Outcome counts for filter badges
	const outcomeCounts = $derived.by(() => {
		const counts: Record<string, number> = { all: items.length };
		for (const item of items) {
			const o = item.outcome ?? 'unknown';
			counts[o] = (counts[o] ?? 0) + 1;
		}
		return counts;
	});

	// Role counts
	const roleCounts = $derived.by(() => {
		const counts: Record<string, number> = { all: items.length };
		for (const item of items) {
			const r = item.role ?? 'unknown';
			counts[r] = (counts[r] ?? 0) + 1;
		}
		return counts;
	});

	// Filtered items
	const filteredItems = $derived.by(() => {
		return items.filter((item: TrajectoryListItem) => {
			if (outcomeFilter !== 'all' && (item.outcome ?? 'unknown') !== outcomeFilter) return false;
			if (roleFilter !== 'all' && (item.role ?? 'unknown') !== roleFilter) return false;
			return true;
		});
	});

	// Unique roles for filter
	const roles = $derived([...new Set(items.map((i: TrajectoryListItem) => i.role ?? 'unknown'))]);

	// Invalidate load data only when a loop finishes — loop_updated fires every tick
	// and the list doesn't change mid-loop.
	$effect(() => {
		const unsubscribe = activityStore.onEvent((event) => {
			if (event.type !== 'loop_completed') return;
			invalidate('app:trajectories');
		});
		return unsubscribe;
	});

	function formatRelativeTime(dateStr: string | undefined): string {
		if (!dateStr) return '—';
		const date = new Date(dateStr);
		const now = Date.now();
		const diffMs = now - date.getTime();
		if (diffMs < 60_000) return 'just now';
		if (diffMs < 3_600_000) return `${Math.floor(diffMs / 60_000)}m ago`;
		if (diffMs < 86_400_000) return `${Math.floor(diffMs / 3_600_000)}h ago`;
		return `${Math.floor(diffMs / 86_400_000)}d ago`;
	}

	function formatDuration(item: TrajectoryListItem): string {
		if (!item.duration) return '—';
		const ms = item.duration;
		if (ms < 1000) return `${ms}ms`;
		if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
		return `${(ms / 60_000).toFixed(1)}m`;
	}

	function getOutcomeClass(outcome: string | undefined): string {
		if (!outcome) return 'status-running';
		if (outcome === 'complete' || outcome === 'success') return 'status-success';
		if (outcome === 'failed' || outcome === 'error' || outcome === 'escalated') return 'status-error';
		return 'status-muted';
	}

	async function handleRefresh() {
		refreshing = true;
		try {
			await invalidate('app:trajectories');
		} finally {
			refreshing = false;
		}
	}
</script>

<svelte:head>
	<title>Trajectories - SemSpec</title>
</svelte:head>

<ThreePanelLayout id="trajectories-list" leftOpen={true} rightOpen={false}>
	{#snippet leftPanel()}
		<div class="filter-panel">
			<div class="filter-header">
				<span class="filter-title">Filters</span>
			</div>

			<div class="filter-section">
				<span class="filter-section-label">Outcome</span>
				<button
					class="filter-btn"
					class:active={outcomeFilter === 'all'}
					onclick={() => (outcomeFilter = 'all')}
				>
					<span>All</span>
					<span class="filter-count">{outcomeCounts.all ?? 0}</span>
				</button>
				{#each Object.keys(outcomeCounts).filter((k) => k !== 'all') as outcome}
					{#if (outcomeCounts[outcome] ?? 0) > 0}
						<button
							class="filter-btn"
							class:active={outcomeFilter === outcome}
							onclick={() => (outcomeFilter = outcome)}
						>
							<span class="capitalize">{outcome}</span>
							<span class="filter-count">{outcomeCounts[outcome] ?? 0}</span>
						</button>
					{/if}
				{/each}
			</div>

			{#if roles.length > 1}
				<div class="filter-section">
					<span class="filter-section-label">Role</span>
					<button
						class="filter-btn"
						class:active={roleFilter === 'all'}
						onclick={() => (roleFilter = 'all')}
					>
						<span>All</span>
						<span class="filter-count">{roleCounts.all ?? 0}</span>
					</button>
					{#each roles as role}
						<button
							class="filter-btn"
							class:active={roleFilter === role}
							onclick={() => (roleFilter = role)}
						>
							<span class="capitalize">{role}</span>
							<span class="filter-count">{roleCounts[role] ?? 0}</span>
						</button>
					{/each}
				</div>
			{/if}
		</div>
	{/snippet}

	{#snippet centerPanel()}
		<div class="trajectories-page" data-testid="trajectories-page">
			<header class="page-header">
				<div class="header-left">
					<h1 data-testid="trajectories-heading">Trajectories</h1>
					<p class="page-description">Recent agent loop execution history</p>
				</div>
				<div class="header-actions">
					<button
						class="btn-icon-labeled"
						onclick={handleRefresh}
						disabled={refreshing}
						title="Refresh"
					>
						<Icon name="refresh-cw" size={14} class={refreshing ? 'spin' : ''} />
						<span>Refresh</span>
					</button>
				</div>
			</header>

			{#if filteredItems.length === 0}
				<div class="empty-state" data-testid="trajectories-empty">
					{#if items.length === 0}
						<Icon name="activity" size={32} />
						<p>No trajectories yet</p>
						<span class="empty-hint">Trajectories are created when agent loops run</span>
					{:else}
						<Icon name="filter" size={24} />
						<p>No trajectories match filters</p>
					{/if}
				</div>
			{:else}
				<div class="trajectory-list" data-testid="trajectory-list">
					{#each filteredItems as item (item.loop_id)}
						<a
							href="/trajectories/{item.loop_id}"
							class="trajectory-item"
							class:selected={selectedLoopId === item.loop_id}
							data-testid="trajectory-item"
							onmouseenter={() => (selectedLoopId = item.loop_id)}
							onmouseleave={() => (selectedLoopId = null)}
							onfocus={() => (selectedLoopId = item.loop_id)}
							onblur={() => (selectedLoopId = null)}
						>
							<div class="item-left">
								<code class="loop-id" data-testid="trajectory-item-id"
									>{item.loop_id.slice(0, 8)}&hellip;</code
								>
								<div class="item-meta">
									<span class="item-task" title={item.task_id}>{item.task_id}</span>
									{#if item.workflow_slug}
										<span class="item-workflow">{item.workflow_slug}</span>
									{/if}
								</div>
							</div>
							<div class="item-right">
								<span
									class="status-badge {getOutcomeClass(item.outcome)}"
									data-testid="trajectory-item-status"
								>
									{item.outcome ?? 'running'}
								</span>
								<span class="item-duration">{formatDuration(item)}</span>
								<span class="item-time">{formatRelativeTime(item.start_time)}</span>
								<span class="item-iterations">{item.iterations}</span>
							</div>
						</a>
					{/each}
				</div>
			{/if}
		</div>
	{/snippet}

	{#snippet rightPanel()}
		<div class="preview-panel">
			<div class="preview-header">
				<span class="preview-title">Preview</span>
			</div>
			<div class="preview-content">
				{#if selectedLoopId}
					<TrajectoryPanel loopId={selectedLoopId} compact={true} />
				{:else}
					<p class="preview-hint">Hover or focus a trajectory to preview</p>
				{/if}
			</div>
		</div>
	{/snippet}
</ThreePanelLayout>

<style>
	/* ---- Filter panel ---- */
	.filter-panel {
		height: 100%;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
	}

	.filter-header {
		padding: var(--space-3) var(--space-4);
		border-bottom: 1px solid var(--color-border);
		flex-shrink: 0;
	}

	.filter-title {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--color-text-muted);
	}

	.filter-section {
		padding: var(--space-3) var(--space-3);
		border-bottom: 1px solid var(--color-border);
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.filter-section-label {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-muted);
		text-transform: uppercase;
		letter-spacing: 0.06em;
		margin-bottom: var(--space-1);
		padding: 0 var(--space-1);
	}

	.filter-btn {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		padding: var(--space-1) var(--space-2);
		background: transparent;
		border: 1px solid transparent;
		border-radius: var(--radius-sm);
		color: var(--color-text-secondary);
		font-size: var(--font-size-sm);
		cursor: pointer;
		text-align: left;
		transition: all var(--transition-fast);
		width: 100%;
	}

	.filter-btn:hover {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
	}

	.filter-btn.active {
		background: var(--color-accent-muted);
		color: var(--color-accent);
		border-color: transparent;
	}

	.filter-count {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-family: var(--font-family-mono);
		flex-shrink: 0;
	}

	.filter-btn.active .filter-count {
		color: var(--color-accent);
	}

	.capitalize {
		text-transform: capitalize;
	}

	/* ---- Center page ---- */
	.trajectories-page {
		height: 100%;
		overflow-y: auto;
		padding: var(--space-6);
	}

	.page-header {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: var(--space-4);
		margin-bottom: var(--space-6);
	}

	.header-left h1 {
		margin: 0 0 var(--space-1);
		font-size: var(--font-size-xl);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.page-description {
		margin: 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.header-actions {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		flex-shrink: 0;
	}

	.btn-icon-labeled {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text-secondary);
		font-size: var(--font-size-sm);
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.btn-icon-labeled:hover:not(:disabled) {
		background: var(--color-bg-elevated);
		color: var(--color-text-primary);
	}

	.btn-icon-labeled:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	/* ---- Trajectory list ---- */
	.trajectory-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.trajectory-item {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-4);
		padding: var(--space-3) var(--space-4);
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text-primary);
		text-decoration: none;
		transition: border-color var(--transition-fast), background-color var(--transition-fast);
	}

	.trajectory-item:hover,
	.trajectory-item.selected {
		border-color: var(--color-accent);
		background: var(--color-bg-tertiary);
		text-decoration: none;
	}

	.item-left {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		min-width: 0;
	}

	.loop-id {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
		flex-shrink: 0;
	}

	.item-meta {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}

	.item-task {
		font-size: var(--font-size-sm);
		color: var(--color-text-primary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		max-width: 280px;
	}

	.item-workflow {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-family: var(--font-family-mono);
	}

	.item-right {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		flex-shrink: 0;
	}

	.status-badge {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-medium);
		padding: 2px var(--space-2);
		border-radius: var(--radius-full);
		text-transform: capitalize;
	}

	.status-success {
		background: var(--color-success-muted);
		color: var(--color-success);
	}

	.status-error {
		background: var(--color-error-muted);
		color: var(--color-error);
	}

	.status-running {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.status-muted {
		background: var(--color-bg-tertiary);
		color: var(--color-text-muted);
	}

	.item-duration,
	.item-time,
	.item-iterations {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
		font-family: var(--font-family-mono);
	}

	/* ---- States ---- */
	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-12) var(--space-6);
		text-align: center;
		color: var(--color-text-muted);
	}

	.empty-state p {
		margin: 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.empty-hint {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		border-radius: var(--radius-md);
		border: 1px solid transparent;
		cursor: pointer;
		transition: all var(--transition-fast);
	}

	.btn-sm {
		padding: var(--space-1) var(--space-2);
		font-size: var(--font-size-xs);
	}

	.btn-secondary {
		background: var(--color-bg-tertiary);
		color: var(--color-text-primary);
		border-color: var(--color-border);
	}

	.btn-secondary:hover {
		background: var(--color-bg-elevated);
	}

	/* ---- Right preview panel ---- */
	.preview-panel {
		height: 100%;
		display: flex;
		flex-direction: column;
	}

	.preview-header {
		padding: var(--space-3) var(--space-4);
		border-bottom: 1px solid var(--color-border);
		background: var(--color-bg-tertiary);
		flex-shrink: 0;
	}

	.preview-title {
		font-size: var(--font-size-xs);
		font-weight: var(--font-weight-semibold);
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--color-text-muted);
	}

	.preview-content {
		flex: 1;
		overflow-y: auto;
		padding: var(--space-3);
	}

	.preview-hint {
		margin: var(--space-6) 0 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-muted);
		text-align: center;
	}

</style>
