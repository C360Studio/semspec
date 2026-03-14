<script lang="ts">
	/**
	 * Trajectory Explorer — list of recent agent loop trajectories.
	 *
	 * Left panel: filter sidebar (status, type)
	 * Center panel: chronological trajectory list
	 * Right panel: preview of selected trajectory (collapsed by default)
	 */

	import { onMount } from 'svelte';
	import ThreePanelLayout from '$lib/components/layout/ThreePanelLayout.svelte';
	import Icon from '$lib/components/shared/Icon.svelte';
	import TrajectoryPanel from '$lib/components/trajectory/TrajectoryPanel.svelte';
	import { trajectoryStore } from '$lib/stores/trajectory.svelte';
	import type { Loop } from '$lib/types';

	// Filter state
	let statusFilter = $state<string>('all');
	let typeFilter = $state<string>('all');
	let selectedLoopId = $state<string | null>(null);

	const loops = $derived(trajectoryStore.recentLoops);
	const recentLoading = $derived(trajectoryStore.recentLoading);
	const recentError = $derived(trajectoryStore.recentError);

	// Status counts for filter badges
	const statusCounts = $derived.by(() => {
		const counts: Record<string, number> = { all: loops.length };
		for (const loop of loops) {
			counts[loop.state] = (counts[loop.state] ?? 0) + 1;
		}
		return counts;
	});

	// Type counts (derived from channel_type)
	const typeCounts = $derived.by(() => {
		const counts: Record<string, number> = { all: loops.length };
		for (const loop of loops) {
			const t = loop.channel_type ?? 'unknown';
			counts[t] = (counts[t] ?? 0) + 1;
		}
		return counts;
	});

	// Filtered loops
	const filteredLoops = $derived.by(() => {
		return loops.filter((loop) => {
			if (statusFilter !== 'all' && loop.state !== statusFilter) return false;
			if (typeFilter !== 'all' && loop.channel_type !== typeFilter) return false;
			return true;
		});
	});

	// Unique channel types for filter
	const channelTypes = $derived([...new Set(loops.map((l) => l.channel_type ?? 'unknown'))]);

	// Auto-refresh for running loops
	let refreshInterval: ReturnType<typeof setInterval> | null = null;

	onMount(() => {
		trajectoryStore.listRecent(50);

		// Refresh every 10s if there are running loops
		refreshInterval = setInterval(() => {
			const hasRunning = trajectoryStore.recentLoops.some((l) =>
				['pending', 'executing'].includes(l.state)
			);
			if (hasRunning) {
				trajectoryStore.listRecent(50);
			}
		}, 10_000);

		return () => {
			if (refreshInterval) clearInterval(refreshInterval);
		};
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

	function formatDuration(loop: Loop): string {
		if (!loop.created_at) return '—';
		const start = new Date(loop.created_at).getTime();
		const end = loop.completed_at ? new Date(loop.completed_at).getTime() : Date.now();
		const ms = end - start;
		if (ms < 1000) return `${ms}ms`;
		if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
		return `${(ms / 60_000).toFixed(1)}m`;
	}

	function getStatusClass(state: string): string {
		if (state === 'complete') return 'status-success';
		if (state === 'failed' || state === 'error') return 'status-error';
		if (state === 'executing' || state === 'pending') return 'status-running';
		return 'status-muted';
	}

	function handleRefresh() {
		trajectoryStore.listRecent(50);
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
				<span class="filter-section-label">Status</span>
				<button
					class="filter-btn"
					class:active={statusFilter === 'all'}
					onclick={() => (statusFilter = 'all')}
				>
					<span>All</span>
					<span class="filter-count">{statusCounts.all ?? 0}</span>
				</button>
				{#each ['pending', 'executing', 'complete', 'failed'] as status}
					{#if (statusCounts[status] ?? 0) > 0}
						<button
							class="filter-btn"
							class:active={statusFilter === status}
							onclick={() => (statusFilter = status)}
						>
							<span class="capitalize">{status}</span>
							<span class="filter-count">{statusCounts[status] ?? 0}</span>
						</button>
					{/if}
				{/each}
			</div>

			{#if channelTypes.length > 1}
				<div class="filter-section">
					<span class="filter-section-label">Type</span>
					<button
						class="filter-btn"
						class:active={typeFilter === 'all'}
						onclick={() => (typeFilter = 'all')}
					>
						<span>All</span>
						<span class="filter-count">{typeCounts.all ?? 0}</span>
					</button>
					{#each channelTypes as ctype}
						<button
							class="filter-btn"
							class:active={typeFilter === ctype}
							onclick={() => (typeFilter = ctype)}
						>
							<span class="capitalize">{ctype}</span>
							<span class="filter-count">{typeCounts[ctype] ?? 0}</span>
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
						disabled={recentLoading}
						title="Refresh"
					>
						<Icon name="refresh-cw" size={14} class={recentLoading ? 'spin' : ''} />
						<span>Refresh</span>
					</button>
				</div>
			</header>

			{#if recentError}
				<div class="error-state" data-testid="trajectories-error">
					<Icon name="alert-triangle" size={20} />
					<p>{recentError}</p>
					<button class="btn btn-secondary btn-sm" onclick={handleRefresh}>Retry</button>
				</div>
			{:else if recentLoading && loops.length === 0}
				<div class="loading-state" data-testid="trajectories-loading">
					<p>Loading trajectories...</p>
				</div>
			{:else if filteredLoops.length === 0}
				<div class="empty-state" data-testid="trajectories-empty">
					{#if loops.length === 0}
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
					{#each filteredLoops as loop (loop.loop_id)}
						<a
							href="/trajectories/{loop.loop_id}"
							class="trajectory-item"
							class:selected={selectedLoopId === loop.loop_id}
							data-testid="trajectory-item"
							onmouseenter={() => (selectedLoopId = loop.loop_id)}
							onmouseleave={() => (selectedLoopId = null)}
							onfocus={() => (selectedLoopId = loop.loop_id)}
							onblur={() => (selectedLoopId = null)}
						>
							<div class="item-left">
								<code class="loop-id" data-testid="trajectory-item-id"
									>{loop.loop_id.slice(0, 8)}&hellip;</code
								>
								<div class="item-meta">
									<span class="item-task" title={loop.task_id}>{loop.task_id}</span>
									{#if loop.workflow_slug}
										<span class="item-workflow">{loop.workflow_slug}</span>
									{/if}
								</div>
							</div>
							<div class="item-right">
								<span
									class="status-badge {getStatusClass(loop.state)}"
									data-testid="trajectory-item-status"
								>
									{loop.state}
								</span>
								<span class="item-duration">{formatDuration(loop)}</span>
								<span class="item-time">{formatRelativeTime(loop.created_at)}</span>
								<span class="item-iterations">{loop.iterations}/{loop.max_iterations}</span>
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
	.loading-state,
	.error-state,
	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-12) var(--space-6);
		text-align: center;
		color: var(--color-text-muted);
	}

	.loading-state p,
	.error-state p,
	.empty-state p {
		margin: 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.error-state {
		color: var(--color-error);
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
