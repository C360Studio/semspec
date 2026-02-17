<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import type { TrajectoryEntry } from '$lib/types/trajectory';

	interface Props {
		entry: TrajectoryEntry;
		compact?: boolean;
	}

	let { entry, compact = false }: Props = $props();

	let expanded = $state(false);

	const isModelCall = $derived(entry.type === 'model_call');
	const hasError = $derived(!!entry.error);
	const preview = $derived(entry.type === 'model_call' ? entry.response_preview : entry.result_preview);
	const hasPreview = $derived(!!preview);

	const displayName = $derived(
		entry.type === 'model_call'
			? (entry.model ?? entry.provider ?? 'Unknown model')
			: (entry.tool_name ?? 'Unknown tool')
	);

	const totalTokens = $derived(
		(entry.tokens_in ?? 0) + (entry.tokens_out ?? 0)
	);

	function formatDuration(ms: number | undefined): string {
		if (ms === undefined) return '—';
		if (ms < 1000) return `${ms}ms`;
		return `${(ms / 1000).toFixed(1)}s`;
	}

	function formatTokens(count: number): string {
		if (count >= 1000) return `${(count / 1000).toFixed(1)}k`;
		return String(count);
	}

	function toggleExpanded() {
		expanded = !expanded;
	}
</script>

<div
	class="entry-card"
	class:compact
	class:has-error={hasError}
	class:model-call={isModelCall}
	class:tool-call={!isModelCall}
>
	<div class="card-header">
		<div class="header-left">
			<span class="type-badge" class:badge-model={isModelCall} class:badge-tool={!isModelCall}>
				{#if isModelCall}
					<Icon name="brain" size={10} />
					LLM
				{:else}
					<Icon name="wrench" size={10} />
					Tool
				{/if}
			</span>
			<span class="entry-name">{displayName}</span>
			{#if entry.retries && entry.retries > 0}
				<span class="retry-badge" title="Retried {entry.retries} time{entry.retries === 1 ? '' : 's'}">
					{entry.retries}x
				</span>
			{/if}
		</div>

		<div class="header-right">
			{#if hasError}
				<Icon name="alert-circle" size={14} class="error-icon" />
			{/if}
			{#if hasPreview && !compact}
				<button
					class="expand-btn"
					onclick={toggleExpanded}
					title={expanded ? 'Collapse' : 'Expand preview'}
				>
					<Icon name={expanded ? 'chevron-up' : 'chevron-down'} size={14} />
				</button>
			{/if}
		</div>
	</div>

	<div class="metrics-row">
		{#if isModelCall}
			{#if totalTokens > 0}
				<span class="metric">
					<Icon name="cpu" size={11} />
					{formatTokens(entry.tokens_in ?? 0)} / {formatTokens(entry.tokens_out ?? 0)} tokens
				</span>
			{/if}
			{#if entry.finish_reason}
				<span class="metric finish-reason">{entry.finish_reason}</span>
			{/if}
		{:else}
			<span class="metric">
				<Icon name="clock" size={11} />
				{formatDuration(entry.duration_ms)}
			</span>
			{#if entry.status}
				<span
					class="metric status-chip"
					class:status-success={entry.status === 'success'}
					class:status-error={entry.status === 'error'}
				>
					{entry.status}
				</span>
			{/if}
		{/if}
	</div>

	{#if hasError}
		<div class="error-message">
			<Icon name="alert-triangle" size={12} />
			<span>{entry.error}</span>
		</div>
	{/if}

	{#if expanded && hasPreview && !compact}
		<div class="preview-block">
			<pre class="preview-text">{preview}</pre>
		</div>
	{/if}
</div>

<style>
	.entry-card {
		background: var(--color-bg-secondary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		padding: var(--space-3) var(--space-4);
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		transition: border-color var(--transition-fast);
	}

	.entry-card.compact {
		padding: var(--space-2) var(--space-3);
		gap: var(--space-1);
	}

	.entry-card.has-error {
		border-color: var(--color-error);
		background: color-mix(in srgb, var(--color-error-muted) 30%, var(--color-bg-secondary));
	}

	.card-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
	}

	.header-left {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-width: 0;
	}

	.header-right {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		flex-shrink: 0;
	}

	.type-badge {
		display: inline-flex;
		align-items: center;
		gap: 3px;
		padding: 2px var(--space-2);
		font-size: 10px;
		font-weight: var(--font-weight-semibold);
		border-radius: var(--radius-full);
		flex-shrink: 0;
		letter-spacing: 0.04em;
	}

	.badge-model {
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.badge-tool {
		background: var(--color-warning-muted);
		color: var(--color-warning);
	}

	.entry-name {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.compact .entry-name {
		font-size: var(--font-size-xs);
	}

	.retry-badge {
		display: inline-flex;
		align-items: center;
		padding: 1px var(--space-1);
		font-size: 10px;
		font-weight: var(--font-weight-medium);
		background: var(--color-warning-muted);
		color: var(--color-warning);
		border-radius: var(--radius-sm);
		flex-shrink: 0;
	}

	:global(.error-icon) {
		color: var(--color-error);
	}

	.expand-btn {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 22px;
		height: 22px;
		background: transparent;
		border: none;
		color: var(--color-text-muted);
		cursor: pointer;
		border-radius: var(--radius-sm);
		transition: all var(--transition-fast);
		padding: 0;
	}

	.expand-btn:hover {
		background: var(--color-bg-elevated);
		color: var(--color-text-secondary);
	}

	.metrics-row {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		flex-wrap: wrap;
	}

	.metric {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
		font-family: var(--font-family-mono);
	}

	.finish-reason {
		font-family: var(--font-family-base);
		font-style: italic;
		color: var(--color-text-muted);
	}

	.status-chip {
		font-family: var(--font-family-base);
		padding: 1px var(--space-2);
		border-radius: var(--radius-full);
		font-weight: var(--font-weight-medium);
	}

	.status-success {
		background: var(--color-success-muted);
		color: var(--color-success);
	}

	.status-error {
		background: var(--color-error-muted);
		color: var(--color-error);
	}

	.error-message {
		display: flex;
		align-items: flex-start;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-error);
		padding: var(--space-1) var(--space-2);
		background: var(--color-error-muted);
		border-radius: var(--radius-sm);
	}

	.error-message span {
		line-height: var(--line-height-normal);
	}

	.preview-block {
		margin-top: var(--space-1);
		border-top: 1px solid var(--color-border);
		padding-top: var(--space-2);
	}

	.preview-text {
		font-family: var(--font-family-mono);
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
		white-space: pre-wrap;
		word-break: break-word;
		line-height: var(--line-height-relaxed);
		margin: 0;
		max-height: 200px;
		overflow-y: auto;
	}
</style>
