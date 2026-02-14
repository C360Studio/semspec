<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import ContextBudgetBar from './ContextBudgetBar.svelte';
	import ProvenanceList from './ProvenanceList.svelte';
	import { contextStore } from '$lib/stores/context.svelte';
	import type { ContextBuildResponse } from '$lib/types/context';
	import { getTaskTypeLabel } from '$lib/types/context';

	interface Props {
		/** Context request ID to fetch */
		requestId: string;
		/** Optional pre-loaded response (skips fetch) */
		response?: ContextBuildResponse;
		/** Whether to auto-fetch on mount */
		autoFetch?: boolean;
		/** Compact mode */
		compact?: boolean;
	}

	let { requestId, response: externalResponse, autoFetch = true, compact = false }: Props = $props();

	// Use external response if provided, otherwise fetch from store
	const response = $derived(externalResponse || contextStore.get(requestId));
	const loading = $derived(contextStore.isLoading(requestId));
	const error = $derived(contextStore.getError(requestId));

	// Derived data from response
	const budgetUsage = $derived(contextStore.getBudgetUsage(requestId));
	const provenance = $derived(contextStore.getProvenance(requestId));
	const taskType = $derived(response?.task_type);

	// Fetch on mount if autoFetch and no external response
	$effect(() => {
		if (autoFetch && requestId && !externalResponse && !contextStore.has(requestId)) {
			contextStore.fetch(requestId);
		}
	});

	function handleRefresh() {
		if (requestId) {
			contextStore.fetch(requestId);
		}
	}
</script>

<div class="context-panel" class:compact>
	<div class="panel-header">
		<div class="header-title">
			<Icon name="layers" size={compact ? 14 : 16} />
			<span>Context Assembly</span>
		</div>

		<div class="header-actions">
			{#if taskType}
				<span class="task-type-badge">{getTaskTypeLabel(taskType)}</span>
			{/if}
			<button
				class="btn-icon"
				onclick={handleRefresh}
				disabled={loading}
				title="Refresh context"
			>
				<Icon name="refresh-cw" size={14} class={loading ? 'spin' : ''} />
			</button>
		</div>
	</div>

	<div class="panel-content">
		{#if error}
			<div class="error-state">
				<Icon name="alert-triangle" size={20} />
				<p>{error}</p>
				<button class="btn btn-secondary btn-sm" onclick={handleRefresh}>Retry</button>
			</div>
		{:else if loading && !response}
			<div class="loading-state">
				<Icon name="loader" size={20} class="spin" />
				<p>Loading context...</p>
			</div>
		{:else if response}
			<div class="context-sections">
				<section class="budget-section">
					<ContextBudgetBar
						used={budgetUsage.used}
						budget={budgetUsage.budget}
						truncated={response.truncated}
						{compact}
					/>
				</section>

				<section class="provenance-section">
					<ProvenanceList entries={provenance} {compact} />
				</section>

				{#if response.error}
					<div class="response-error">
						<Icon name="alert-circle" size={14} />
						<span>{response.error}</span>
					</div>
				{/if}
			</div>
		{:else}
			<div class="empty-state">
				<Icon name="inbox" size={24} />
				<p>No context available</p>
				<span class="empty-hint">Context will appear when the agent starts</span>
			</div>
		{/if}
	</div>
</div>

<style>
	.context-panel {
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

	.task-type-badge {
		display: inline-flex;
		align-items: center;
		padding: 2px var(--space-2);
		font-size: 10px;
		font-weight: var(--font-weight-medium);
		background: var(--color-accent-muted);
		color: var(--color-accent);
		border-radius: var(--radius-full);
		text-transform: capitalize;
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
	}

	.btn-icon:hover:not(:disabled) {
		background: var(--color-bg-elevated);
		color: var(--color-text-primary);
	}

	.btn-icon:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.panel-content {
		padding: var(--space-4);
	}

	.compact .panel-content {
		padding: var(--space-3);
	}

	.context-sections {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}

	.compact .context-sections {
		gap: var(--space-3);
	}

	.budget-section {
		/* Wrapper for budget bar - no additional styles needed */
		display: block;
	}

	.provenance-section {
		/* Wrapper for provenance list - no additional styles needed */
		display: block;
	}

	.provenance-section :global(.provenance-list) {
		background: var(--color-bg-tertiary);
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

	.loading-state {
		color: var(--color-info);
	}

	.loading-state p {
		color: var(--color-text-secondary);
		margin: 0;
	}

	.error-state {
		color: var(--color-error);
	}

	.error-state p {
		color: var(--color-text-secondary);
		margin: 0;
	}

	.empty-state {
		color: var(--color-text-muted);
	}

	.empty-state p {
		color: var(--color-text-secondary);
		margin: 0;
	}

	.empty-hint {
		font-size: var(--font-size-xs);
	}

	.response-error {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		background: var(--color-error-muted);
		border: 1px solid var(--color-error);
		border-radius: var(--radius-md);
		color: var(--color-error);
		font-size: var(--font-size-xs);
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
