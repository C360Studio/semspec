<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import TrajectoryEntryCard from './TrajectoryEntryCard.svelte';
	import { trajectoryStore } from '$lib/stores/trajectory.svelte';

	interface Props {
		loopId: string;
		compact?: boolean;
	}

	let { loopId, compact = false }: Props = $props();

	const trajectory = $derived(trajectoryStore.get(loopId));
	const loading = $derived(trajectoryStore.isLoading(loopId));
	const error = $derived(trajectoryStore.getError(loopId));
	const entries = $derived(trajectory?.entries ?? []);

	const modelCallCount = $derived(
		entries.filter((e) => e.type === 'model_call').length
	);
	const toolCallCount = $derived(
		entries.filter((e) => e.type === 'tool_call').length
	);
	const totalTokens = $derived(
		entries.reduce((sum, e) => sum + (e.tokens_in ?? 0) + (e.tokens_out ?? 0), 0)
	);
	const totalDurationMs = $derived(
		entries.reduce((sum, e) => sum + (e.duration_ms ?? 0), 0)
	);

	function formatTokens(count: number): string {
		if (count >= 1000) return `${(count / 1000).toFixed(1)}k`;
		return String(count);
	}

	function formatDuration(ms: number): string {
		if (ms < 1000) return `${ms}ms`;
		return `${(ms / 1000).toFixed(1)}s`;
	}

	function handleRefresh() {
		trajectoryStore.invalidate(loopId);
		trajectoryStore.fetch(loopId);
	}
</script>

<div class="trajectory-panel" class:compact>
	<div class="panel-header">
		<div class="header-title">
			<Icon name="history" size={compact ? 14 : 16} />
			<span>Trajectory</span>
		</div>

		<div class="header-actions">
			<button
				class="btn-icon"
				onclick={handleRefresh}
				disabled={loading}
				title="Refresh trajectory"
			>
				<Icon name="refresh-cw" size={14} class={loading ? 'spin' : ''} />
			</button>
		</div>
	</div>

	{#if !loading && trajectory && entries.length > 0}
		<div class="summary-bar" class:compact>
			<span class="summary-stat" title="LLM calls">
				<Icon name="brain" size={12} />
				{modelCallCount} LLM
			</span>
			<span class="summary-divider" aria-hidden="true">·</span>
			<span class="summary-stat" title="Tool calls">
				<Icon name="wrench" size={12} />
				{toolCallCount} tool{toolCallCount !== 1 ? 's' : ''}
			</span>
			{#if totalTokens > 0}
				<span class="summary-divider" aria-hidden="true">·</span>
				<span class="summary-stat" title="Total tokens">
					<Icon name="cpu" size={12} />
					{formatTokens(totalTokens)} tokens
				</span>
			{/if}
			{#if totalDurationMs > 0}
				<span class="summary-divider" aria-hidden="true">·</span>
				<span class="summary-stat" title="Total duration">
					<Icon name="clock" size={12} />
					{formatDuration(totalDurationMs)}
				</span>
			{/if}
		</div>
	{/if}

	<div class="panel-content">
		{#if error}
			<div class="error-state">
				<Icon name="alert-triangle" size={20} />
				<p>{error}</p>
				<button class="btn btn-secondary btn-sm" onclick={handleRefresh}>Retry</button>
			</div>
		{:else if loading && !trajectory}
			<div class="loading-state">
				<p>Loading trajectory...</p>
			</div>
		{:else if entries.length === 0}
			<div class="empty-state">
				<Icon name="history" size={compact ? 18 : 24} />
				<p>No trajectory data</p>
				{#if !compact}
					<span class="empty-hint">Execution steps will appear here once the agent runs</span>
				{/if}
			</div>
		{:else}
			<ol class="entry-list">
				{#each entries as entry, i (i)}
					<li class="entry-item">
						<TrajectoryEntryCard {entry} {compact} />
					</li>
				{/each}
			</ol>
		{/if}
	</div>
</div>

<style>
	.trajectory-panel {
		display: flex;
		flex-direction: column;
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		overflow: hidden;
	}

	.panel-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
		padding: var(--space-3) var(--space-4);
		background: var(--color-bg-tertiary);
		border-bottom: 1px solid var(--color-border);
		flex-shrink: 0;
	}

	.compact .panel-header {
		padding: var(--space-2) var(--space-3);
	}

	.header-title {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-semibold);
		color: var(--color-text-primary);
	}

	.compact .header-title {
		font-size: var(--font-size-xs);
	}

	.header-actions {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.btn-icon {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 24px;
		height: 24px;
		background: transparent;
		border: none;
		color: var(--color-text-secondary);
		cursor: pointer;
		border-radius: var(--radius-sm);
		transition: all var(--transition-fast);
		padding: 0;
	}

	.btn-icon:hover:not(:disabled) {
		background: var(--color-bg-elevated);
		color: var(--color-text-primary);
	}

	.btn-icon:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.summary-bar {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-4);
		background: var(--color-bg-primary);
		border-bottom: 1px solid var(--color-border);
		flex-wrap: wrap;
		flex-shrink: 0;
	}

	.summary-bar.compact {
		padding: var(--space-1) var(--space-3);
		gap: var(--space-1);
	}

	.summary-stat {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
		font-family: var(--font-family-mono);
	}

	.summary-divider {
		color: var(--color-text-muted);
		font-size: var(--font-size-xs);
		line-height: 1;
	}

	.panel-content {
		padding: var(--space-4);
		overflow-y: auto;
	}

	.compact .panel-content {
		padding: var(--space-3);
	}

	.entry-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		list-style: none;
		padding: 0;
		margin: 0;
	}

	.compact .entry-list {
		gap: var(--space-1);
	}

	.loading-state,
	.error-state,
	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-6);
		text-align: center;
	}

	.compact .loading-state,
	.compact .error-state,
	.compact .empty-state {
		padding: var(--space-3);
		gap: var(--space-1);
	}

	.loading-state {
		color: var(--color-text-secondary);
	}

	.loading-state p {
		margin: 0;
		font-size: var(--font-size-sm);
		color: var(--color-text-secondary);
	}

	.error-state {
		color: var(--color-error);
	}

	.error-state p {
		color: var(--color-text-secondary);
		margin: 0;
		font-size: var(--font-size-sm);
	}

	.empty-state {
		color: var(--color-text-muted);
	}

	.empty-state p {
		color: var(--color-text-secondary);
		margin: 0;
		font-size: var(--font-size-sm);
	}

	.empty-hint {
		font-size: var(--font-size-xs);
		color: var(--color-text-muted);
	}

	.btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
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
