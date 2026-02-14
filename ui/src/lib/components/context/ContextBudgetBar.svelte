<script lang="ts">
	import { formatTokens } from '$lib/types/context';

	interface Props {
		/** Tokens used */
		used: number;
		/** Total budget */
		budget: number;
		/** Whether content was truncated */
		truncated?: boolean;
		/** Compact mode (smaller text) */
		compact?: boolean;
	}

	let { used, budget, truncated = false, compact = false }: Props = $props();

	const percent = $derived(budget > 0 ? Math.round((used / budget) * 100) : 0);

	const barClass = $derived.by(() => {
		if (percent >= 90) return 'critical';
		if (percent >= 75) return 'high';
		if (percent >= 50) return 'medium';
		return 'low';
	});
</script>

<div class="budget-bar" class:compact>
	<div class="budget-header">
		<span class="budget-label">Context Budget</span>
		<span class="budget-value">
			{formatTokens(used)} / {formatTokens(budget)} tokens
			<span class="budget-percent">({percent}%)</span>
		</span>
	</div>

	<div class="progress-container">
		<div class="progress-bar {barClass}" style="width: {percent}%"></div>
	</div>

	{#if truncated}
		<div class="truncation-warning">
			<span class="warning-icon">!</span>
			<span>Some content was truncated to fit budget</span>
		</div>
	{/if}
</div>

<style>
	.budget-bar {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.budget-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
	}

	.budget-label {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-secondary);
	}

	.budget-value {
		font-size: var(--font-size-sm);
		font-family: var(--font-family-mono);
		color: var(--color-text-primary);
	}

	.budget-percent {
		color: var(--color-text-muted);
		margin-left: var(--space-1);
	}

	.compact .budget-label,
	.compact .budget-value {
		font-size: var(--font-size-xs);
	}

	.progress-container {
		height: 6px;
		background: var(--color-bg-tertiary);
		border-radius: var(--radius-full);
		overflow: hidden;
	}

	.compact .progress-container {
		height: 4px;
	}

	.progress-bar {
		height: 100%;
		border-radius: var(--radius-full);
		transition: width var(--transition-base);
	}

	.progress-bar.low {
		background: var(--color-success);
	}

	.progress-bar.medium {
		background: var(--color-info);
	}

	.progress-bar.high {
		background: var(--color-warning);
	}

	.progress-bar.critical {
		background: var(--color-error);
	}

	.truncation-warning {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--font-size-xs);
		color: var(--color-warning);
	}

	.warning-icon {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 14px;
		height: 14px;
		background: var(--color-warning);
		color: var(--color-bg-primary);
		border-radius: var(--radius-full);
		font-size: 10px;
		font-weight: var(--font-weight-semibold);
	}
</style>
