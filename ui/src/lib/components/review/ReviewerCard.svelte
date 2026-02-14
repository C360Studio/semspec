<script lang="ts">
	import Icon from '../shared/Icon.svelte';
	import type { ReviewerSummary } from '$lib/types/review';
	import { getReviewerLabel } from '$lib/types/review';

	interface Props {
		/** Reviewer summary */
		reviewer: ReviewerSummary;
		/** Whether the reviewer is currently running */
		active?: boolean;
	}

	let { reviewer, active = false }: Props = $props();

	const label = $derived(getReviewerLabel(reviewer.role));

	const iconName = $derived.by(() => {
		if (active) return 'loader';
		if (reviewer.passed) return 'check';
		return 'x';
	});

	const statusClass = $derived.by(() => {
		if (active) return 'active';
		if (reviewer.passed) return 'success';
		return 'error';
	});
</script>

<div class="reviewer-card" class:active class:passed={reviewer.passed} class:failed={!reviewer.passed && !active}>
	<div class="card-header">
		<div class="status-icon {statusClass}">
			<Icon name={iconName} size={14} class={active ? 'spin' : ''} />
		</div>
		<span class="reviewer-label">{label}</span>
		{#if reviewer.finding_count > 0}
			<span class="finding-count">{reviewer.finding_count}</span>
		{/if}
	</div>

	{#if reviewer.summary}
		<p class="reviewer-summary">{reviewer.summary}</p>
	{/if}
</div>

<style>
	.reviewer-card {
		background: var(--color-bg-tertiary);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		padding: var(--space-3);
		min-width: 140px;
	}

	.reviewer-card.passed {
		border-color: var(--color-success);
		background: var(--color-success-muted);
	}

	.reviewer-card.failed {
		border-color: var(--color-error);
		background: var(--color-error-muted);
	}

	.reviewer-card.active {
		border-color: var(--color-info);
		background: var(--color-info-muted);
	}

	.card-header {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}

	.status-icon {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 20px;
		height: 20px;
		border-radius: var(--radius-full);
	}

	.status-icon.success {
		background: var(--color-success);
		color: white;
	}

	.status-icon.error {
		background: var(--color-error);
		color: white;
	}

	.status-icon.active {
		background: var(--color-info);
		color: white;
	}

	.reviewer-label {
		font-size: var(--font-size-sm);
		font-weight: var(--font-weight-medium);
		color: var(--color-text-primary);
		flex: 1;
	}

	.finding-count {
		display: flex;
		align-items: center;
		justify-content: center;
		min-width: 18px;
		height: 18px;
		padding: 0 var(--space-1);
		font-size: 10px;
		font-weight: var(--font-weight-semibold);
		background: var(--color-error);
		color: white;
		border-radius: var(--radius-full);
	}

	.reviewer-summary {
		margin-top: var(--space-2);
		font-size: var(--font-size-xs);
		color: var(--color-text-secondary);
		line-height: var(--line-height-normal);
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
